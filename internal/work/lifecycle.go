package work

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type StartInput struct {
	TodoID       string `json:"todo_id"`
	ReopenReason string `json:"reopen_reason,omitempty"`
}

type StartResult struct {
	Todo           Todo     `json:"todo"`
	AlreadyStarted bool     `json:"already_started"`
	Reopened       bool     `json:"reopened,omitempty"`
	Effects        []Effect `json:"-"`
}

type ReturnToOpenInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag,omitempty"`
}

type ReturnToOpenResult struct {
	Todo            Todo     `json:"todo"`
	AlreadyOpen     bool     `json:"already_open"`
	UnboundSessions int      `json:"unbound_sessions"`
	Effects         []Effect `json:"-"`
}

type CloseInput struct {
	TodoID    string `json:"todo_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type CloseResult struct {
	Todo            Todo                  `json:"todo"`
	AlreadyClosed   bool                  `json:"already_closed"`
	UnboundSessions int                   `json:"unbound_sessions"`
	Awakened        []store.TodoWakeEvent `json:"awakened,omitempty"`
	Effects         []Effect              `json:"-"`
}

type WakeInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag,omitempty"`
	Reason       string `json:"reason"`
}

type WakeResult struct {
	Todo            Todo     `json:"todo"`
	AlreadyAwake    bool     `json:"already_awake"`
	UnboundSessions int      `json:"unbound_sessions"`
	Effects         []Effect `json:"-"`
}

// dependencyReconcileMutation is the shared in-transaction result used by
// Close and dependency add/remove. Keeping the helper on Transaction guarantees
// the derived wake transitions, binding closures, and outbox rows commit or
// roll back together with their cause.
type dependencyReconcileMutation struct {
	Awakened []store.TodoWakeEvent
	Effects  []Effect
}

// Start begins or explicitly reopens a Todo. Closed-attempt timestamps are
// reset together with the lifecycle state; document projection is represented
// by a durable effect so a failed filesystem write can be retried safely.
func (service Service) Start(ctx context.Context, call application.Call, input StartInput) (StartResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return StartResult{}, err
	}
	if err := validateLifecycleTodoID(input.TodoID); err != nil {
		return StartResult{}, err
	}

	result := StartResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(input.TodoID)
		if err != nil {
			return lifecycleTodoNotFound(input.TodoID, err)
		}
		if unmet := store.UnmetTodoDependencies(transaction.Todos(), *todo); len(unmet) > 0 {
			return lifecycleConflict(
				fmt.Sprintf("cannot start todo %s until dependencies are done: %s", todo.ID, strings.Join(unmet, ", ")),
				todo.ID, todo.Status,
			)
		}
		wasClosed := !store.TodoIsActive(*todo)
		wasReview := todo.Status == store.TodoStatusReview
		reopenReason := strings.TrimSpace(input.ReopenReason)
		message := ""
		if wasClosed || wasReview {
			if reopenReason == "" {
				appErr := lifecycleConflict(
					fmt.Sprintf("todo %s is %s; reopening review or completed work requires an audit reason; run `atm todo start %s --reopen-reason \"<why work resumed>\"`", todo.ID, todo.Status, todo.ID),
					todo.ID, todo.Status,
				)
				appErr.Details["required_flag"] = "--reopen-reason"
				appErr.Details["reopen_required"] = true
				return appErr
			}
			message = "[reopen] " + reopenReason
			if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
				return lifecycleInvalidArgument(err.Error(), "reopen_reason", input.ReopenReason)
			}
			if unknown := store.UnknownTodoReferences(transaction.Todos(), message); len(unknown) > 0 {
				err := lifecycleInvalidArgument(
					fmt.Sprintf("reopen reason references unknown todo IDs: %s", strings.Join(unknown, ", ")),
					"reopen_reason", input.ReopenReason,
				)
				err.Details["unknown_todo_ids"] = unknown
				return err
			}
		}
		changed := todo.Status != store.TodoStatusInProgress || todo.StartTS == nil ||
			todo.WakeCondition != "" || todo.ReviewAt != "" || wasClosed
		if wasClosed {
			now := time.Now().In(config.Loc).Unix()
			todo.StartTS = &now
			todo.DoneTS = nil
			todo.Closed = nil
			todo.ClosedReason = nil
		} else if todo.StartTS == nil {
			now := time.Now().In(config.Loc).Unix()
			todo.StartTS = &now
		}
		todo.Status = store.TodoStatusInProgress
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		result.Todo = cloneTodo(*todo)
		result.AlreadyStarted = !changed
		result.Reopened = wasClosed || wasReview
		if changed {
			if err := transaction.enqueueOrReplaceEffect(call, EffectTodoStarted, *todo, message); err != nil {
				return fmt.Errorf("enqueue start effect: %w", err)
			}
		}
		result.Effects, err = transaction.pendingEffects(todo.ID)
		return err
	})
	if err != nil {
		return StartResult{}, lifecycleApplicationError("start todo", err)
	}
	return result, nil
}

// ReturnToOpen moves in-progress work back to the backlog without sharing the
// generic metadata editor. Review and completed work must be explicitly
// reopened through Start so their audit trail cannot be bypassed.
func (service Service) ReturnToOpen(
	ctx context.Context,
	call application.Call,
	input ReturnToOpenInput,
) (ReturnToOpenResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return ReturnToOpenResult{}, err
	}
	if err := validateLifecycleTodoID(input.TodoID); err != nil {
		return ReturnToOpenResult{}, err
	}

	result := ReturnToOpenResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(input.TodoID)
		if err != nil {
			return lifecycleTodoNotFound(input.TodoID, err)
		}
		if err := checkExpectedTodo(call, *todo, input.ExpectedETag); err != nil {
			return err
		}
		if todo.Status == store.TodoStatusDone || todo.Status == store.TodoStatusReview {
			return lifecycleConflict(
				fmt.Sprintf("cannot return %s todo %s to open; use start --reopen-reason to reopen it", todo.Status, todo.ID),
				todo.ID, todo.Status,
			)
		}
		if todo.Status == store.TodoStatusOpen && todo.WakeCondition == "" && todo.ReviewAt == "" &&
			todo.StartTS == nil && todo.DoneTS == nil && todo.Closed == nil && todo.ClosedReason == nil {
			result.Todo = cloneTodo(*todo)
			result.AlreadyOpen = true
			result.Effects, err = transaction.pendingEffects(todo.ID)
			return err
		}

		todo.Status = store.TodoStatusOpen
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		todo.StartTS = nil
		todo.DoneTS = nil
		todo.Closed = nil
		todo.ClosedReason = nil
		if err := transaction.enqueueOrReplaceEffect(call, EffectTodoUpdated, *todo, ""); err != nil {
			return fmt.Errorf("enqueue return-to-open effect: %w", err)
		}
		result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, "open")
		if err != nil {
			return fmt.Errorf("unbind todo sessions before returning to open: %w", err)
		}
		result.Todo = cloneTodo(*todo)
		result.Effects, err = transaction.pendingEffects(todo.ID)
		return err
	})
	if err != nil {
		return ReturnToOpenResult{}, lifecycleApplicationError("return todo to open", err)
	}
	return result, nil
}

// Done is the human acceptance transition. Actor attribution from the CLI
// environment is an authorization signal, not strong operating-system
// authentication; nevertheless enforcing the policy here prevents ordinary
// Agent hot paths from converting implementation completion directly to done.
// Agents must use Submit and leave acceptance to a human actor.
func (service Service) Done(ctx context.Context, call application.Call, input CloseInput) (CloseResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return CloseResult{}, err
	}
	if call.Actor.Kind != application.ActorHuman {
		err := application.NewError(application.CodeForbidden, "only a human actor may mark a todo done; agents must use todo submit")
		err.Details = map[string]any{"actor_kind": call.Actor.Kind, "required_actor_kind": application.ActorHuman}
		return CloseResult{}, err
	}
	// The authenticated Web click is the acceptance decision, not a request to
	// justify it. Keep an honest action receipt when the human supplies no
	// optional note. CLI acceptance still requires evidence.
	if humanAcceptanceClickOrigin(call.Actor.Origin) && strings.TrimSpace(input.Reason) == "" {
		input.Reason = store.TodoGUICompletionReceipt
	}
	return service.close(ctx, call, input, store.TodoStatusDone, doneTransitionTarget)
}

func humanAcceptanceClickOrigin(origin application.Origin) bool {
	return origin == application.OriginWeb
}

func (service Service) close(
	_ context.Context,
	call application.Call,
	input CloseInput,
	status string,
	target transitionTargetSpec,
) (CloseResult, error) {
	if strings.TrimSpace(input.TodoID) != "" {
		if err := validateLifecycleTodoID(input.TodoID); err != nil {
			return CloseResult{}, err
		}
	}
	reason := strings.TrimSpace(input.Reason)
	message := ""
	if reason != "" {
		message = fmt.Sprintf("[%s] %s", status, reason)
		if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
			return CloseResult{}, lifecycleInvalidArgument(err.Error(), "reason", input.Reason)
		}
	}

	result := CloseResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		sessionID := strings.TrimSpace(input.SessionID)
		if sessionID == "" {
			sessionID = call.Actor.SessionID
		}
		todoID, err := transaction.resolveTransitionTodoID(input.TodoID, sessionID, target)
		if err != nil {
			return err
		}
		todo, err := transaction.Todo(todoID)
		if err != nil {
			return lifecycleTodoNotFound(todoID, err)
		}
		result.Todo = cloneTodo(*todo)
		if todo.Status == status {
			result.AlreadyClosed = true
			result.Effects, err = transaction.pendingLifecycleEffects(todo.ID, EffectTodoClosed)
			return err
		}
		if status == store.TodoStatusDone && !humanAcceptanceClickOrigin(call.Actor.Origin) {
			if reasonErr := store.ValidateTodoCompletionReason(reason); reasonErr != nil {
				appErr := lifecycleInvalidArgument(reasonErr.Error(), "reason", input.Reason)
				appErr.Details["required_flag"] = "--reason"
				appErr.Details["evidence_required"] = true
				return appErr
			}
		}
		if !store.TodoIsActive(*todo) {
			return lifecycleConflict(
				fmt.Sprintf("cannot mark todo %s %s because it is already %s; start it to reopen first", todo.ID, status, todo.Status),
				todo.ID, todo.Status,
			)
		}
		if message != "" {
			if unknown := store.UnknownTodoReferences(transaction.Todos(), message); len(unknown) > 0 {
				err := lifecycleInvalidArgument(
					fmt.Sprintf("todo log references unknown todo IDs: %s; create and verify structured todos before closing them", strings.Join(unknown, ", ")),
					"reason", input.Reason,
				)
				err.Details["unknown_todo_ids"] = unknown
				return err
			}
		}

		todo.Status = status
		today := store.Today()
		todo.Closed = &today
		now := time.Now().In(config.Loc).Unix()
		todo.DoneTS = &now
		if reason == "" {
			todo.ClosedReason = nil
		} else {
			todo.ClosedReason = &reason
		}
		if status == store.TodoStatusDone {
			reconciled, reconcileErr := transaction.reconcileDependencies(call, todo.ID)
			if reconcileErr != nil {
				return reconcileErr
			}
			result.Awakened = reconciled.Awakened
		}
		result.Todo = cloneTodo(*todo)
		if err := transaction.enqueueEffect(call, EffectTodoClosed, *todo, message); err != nil {
			return fmt.Errorf("enqueue close effect: %w", err)
		}
		result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, status)
		if err != nil {
			return fmt.Errorf("unbind todo sessions before close: %w", err)
		}
		result.Effects, err = transaction.pendingLifecycleEffects(todo.ID, EffectTodoClosed)
		return err
	})
	if err != nil {
		return CloseResult{}, lifecycleApplicationError("close todo", err)
	}
	return result, nil
}

// Wake resolves one explicit external/manual wait. The progress entry and
// document refresh are a durable effect; the lifecycle mutation and any stale
// binding closure remain one transaction.
func (service Service) Wake(ctx context.Context, call application.Call, input WakeInput) (WakeResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return WakeResult{}, err
	}
	if err := validateLifecycleTodoID(input.TodoID); err != nil {
		return WakeResult{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return WakeResult{}, lifecycleInvalidArgument("wake reason is required", "reason", input.Reason)
	}
	message := "[wake] " + reason
	if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
		return WakeResult{}, lifecycleInvalidArgument(err.Error(), "reason", input.Reason)
	}

	result := WakeResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(input.TodoID)
		if err != nil {
			return lifecycleTodoNotFound(input.TodoID, err)
		}
		if err := checkExpectedTodo(call, *todo, input.ExpectedETag); err != nil {
			return err
		}
		if todo.Status != store.TodoStatusInProgress || (todo.WakeCondition == "" && todo.ReviewAt == "") {
			pending, pendingErr := transaction.pendingEffects(todo.ID)
			if pendingErr != nil {
				return pendingErr
			}
			for _, effect := range pending {
				if effect.Kind == EffectTodoAwakened && todo.Status == store.TodoStatusInProgress {
					result.Todo = cloneTodo(*todo)
					result.AlreadyAwake = true
					result.Effects = append(result.Effects, effect)
				}
			}
			if result.AlreadyAwake {
				return nil
			}
			return lifecycleConflict(
				fmt.Sprintf("cannot wake todo %s with status %s", todo.ID, todo.Status), todo.ID, todo.Status,
			)
		}
		if unknown := store.UnknownTodoReferences(transaction.Todos(), message); len(unknown) > 0 {
			err := lifecycleInvalidArgument(
				fmt.Sprintf("todo log references unknown todo IDs: %s; create and verify structured todos before waking them", strings.Join(unknown, ", ")),
				"reason", input.Reason,
			)
			err.Details["unknown_todo_ids"] = unknown
			return err
		}
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		result.Todo = cloneTodo(*todo)
		if err := transaction.enqueueEffect(call, EffectTodoAwakened, *todo, message); err != nil {
			return fmt.Errorf("enqueue wake effect: %w", err)
		}
		result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, "wake:"+store.TodoStatusInProgress)
		if err != nil {
			return fmt.Errorf("unbind todo sessions before wake: %w", err)
		}
		pending, err := transaction.pendingEffects(todo.ID)
		if err != nil {
			return err
		}
		for _, effect := range pending {
			if effect.Kind == EffectTodoAwakened {
				result.Effects = append(result.Effects, effect)
			}
		}
		return nil
	})
	if err != nil {
		return WakeResult{}, lifecycleApplicationError("wake todo", err)
	}
	return result, nil
}

func (transaction *Transaction) reconcileDependencies(
	call application.Call,
	causeTodoID string,
) (dependencyReconcileMutation, error) {
	result := dependencyReconcileMutation{Awakened: store.ReconcileTodoDependencies(transaction.Todos())}
	for _, event := range result.Awakened {
		todo := store.FindTodo(transaction.Todos(), event.TodoID)
		if todo == nil {
			return dependencyReconcileMutation{}, fmt.Errorf("awakened todo %s disappeared from WorkState transaction", event.TodoID)
		}
		if _, err := transaction.UnbindTodoSessions(todo.ID, "dependency:"+todo.Status); err != nil {
			return dependencyReconcileMutation{}, fmt.Errorf("unbind reconciled todo %s sessions: %w", todo.ID, err)
		}
		if err := transaction.enqueueEffectWithCause(
			call, EffectTodoDependencyAwakened, *todo, "[wake] "+event.Reason, causeTodoID,
		); err != nil {
			return dependencyReconcileMutation{}, fmt.Errorf("enqueue reconcile effect for %s: %w", todo.ID, err)
		}
	}
	pending, err := transaction.pendingEffects("")
	if err != nil {
		return dependencyReconcileMutation{}, err
	}
	for _, effect := range pending {
		if effect.Kind == EffectTodoDependencyAwakened && effect.CauseTodoID == causeTodoID {
			result.Effects = append(result.Effects, effect)
		}
	}
	return result, nil
}

// pendingLifecycleEffects returns the primary lifecycle effect plus derived
// effects whose cause is that Todo. This is the retry bridge after Close has
// already removed the current Session binding.
func (transaction *Transaction) pendingLifecycleEffects(todoID string, primary EffectKind) ([]Effect, error) {
	effects, err := transaction.pendingEffects("")
	if err != nil {
		return nil, err
	}
	result := make([]Effect, 0, len(effects))
	for _, effect := range effects {
		if (effect.TodoID == todoID && effect.Kind == primary) || effect.CauseTodoID == todoID {
			result = append(result, effect)
		}
	}
	return result, nil
}

func validateLifecycleCall(ctx context.Context, call application.Call) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return lifecycleUnavailable("todo lifecycle request canceled", err)
	}
	return call.Validate()
}

func validateLifecycleTodoID(value string) error {
	if strings.TrimSpace(value) == "" {
		return lifecycleInvalidArgument("todo ID is required", "todo_id", value)
	}
	if !store.LooksLikeTodoID(value) {
		return lifecycleInvalidArgument(fmt.Sprintf("invalid todo ID %q", value), "todo_id", value)
	}
	return nil
}

func lifecycleConflict(message, todoID, currentStatus string) *application.Error {
	err := application.NewError(application.CodeConflict, message)
	err.Details = map[string]any{"todo_id": todoID, "current_status": currentStatus}
	return err
}

func lifecycleTodoNotFound(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
	err.Details = map[string]any{"todo_id": store.NormalizeTodoID(id)}
	return err
}

func lifecycleInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func lifecycleUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}

func lifecycleApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return lifecycleUnavailable(operation, err)
}
