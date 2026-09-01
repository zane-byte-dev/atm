package work

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// BindingState explains whether a persisted session binding can still be used
// as the session's current Todo. A binding row can outlive the Todo working-set
// row or a lifecycle transition, so "has a row" and "is currently bound" are
// deliberately different facts.
type BindingState string

const (
	BindingStateUnbound           BindingState = "unbound"
	BindingStateBound             BindingState = "bound"
	BindingStateTodoMissing       BindingState = "todo_missing"
	BindingStateTodoNotInProgress BindingState = "todo_not_in_progress"
)

// TodoSummary is the stable, compact Todo shape returned with session binding
// results. The application layer should not make a transport reconstruct this
// projection from a persistence snapshot.
type TodoSummary struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project,omitempty"`
	Status  string `json:"status"`
}

// BindingContext combines the durable binding with the Todo state that decides
// whether it is usable.
type BindingContext struct {
	State   string                   `json:"state"`
	Binding store.TodoSessionBinding `json:"binding"`
	Todo    *TodoSummary             `json:"todo,omitempty"`
}

type BindInput struct {
	TodoID string `json:"todo_id"`
	Agent  string `json:"agent,omitempty"`
	// Project and CWD are resolved by the adapter. WorkspaceProject is the
	// canonical project detected from CWD; keeping filesystem discovery at the
	// edge makes the workspace guard deterministic for every transport.
	Project          string `json:"project,omitempty"`
	CWD              string `json:"cwd,omitempty"`
	WorkspaceProject string `json:"workspace_project,omitempty"`
	Force            bool   `json:"force,omitempty"`
	ReopenReason     string `json:"reopen_reason,omitempty"`
}

type BindResult struct {
	Binding  store.TodoSessionBinding `json:"binding"`
	Todo     store.Todo               `json:"todo"`
	Reopened bool                     `json:"reopened,omitempty"`
	Effects  []Effect                 `json:"-"`
}

type CurrentInput struct{}

type CurrentResult struct {
	SessionID string          `json:"session_id"`
	Bound     bool            `json:"bound"`
	State     BindingState    `json:"state"`
	Context   *BindingContext `json:"context,omitempty"`
}

type UnbindInput struct {
	Reason string `json:"reason"`
}

type UnbindResult struct {
	SessionID string `json:"session_id"`
	Unbound   bool   `json:"unbound"`
}

// Bind validates the Todo and workspace, starts or resumes the Todo, and
// records the session binding in one WorkState transaction. It then owns the
// two after-commit effects required by Agent handoff: authoritative task-run
// linking (best effort) and Todo document materialization (required).
func (service Service) Bind(
	ctx context.Context,
	call application.Call,
	input BindInput,
) (BindResult, error) {
	sessionID, err := validateSessionCall(ctx, call, true)
	if err != nil {
		return BindResult{}, err
	}
	input, err = normalizeBindInput(input)
	if err != nil {
		return BindResult{}, err
	}

	result := BindResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(input.TodoID)
		if err != nil {
			appErr := application.WrapError(application.CodeNotFound, err.Error(), err)
			appErr.Details = map[string]any{"todo_id": store.NormalizeTodoID(input.TodoID)}
			return appErr
		}
		if !store.TodoIsActive(*todo) {
			appErr := application.NewError(
				application.CodeConflict,
				fmt.Sprintf("cannot bind completed todo %s with status %s; run `atm todo start %s --reopen-reason \"<why work resumed>\"` first, then bind it", todo.ID, todo.Status, todo.ID),
			)
			appErr.Details = map[string]any{
				"todo_id":          todo.ID,
				"current_status":   todo.Status,
				"required_command": "todo start",
				"required_flag":    "--reopen-reason",
				"reopen_required":  true,
			}
			return appErr
		}
		reopenReason := strings.TrimSpace(input.ReopenReason)
		reopenMessage := ""
		if todo.Status == store.TodoStatusReview {
			if reopenReason == "" {
				appErr := application.NewError(
					application.CodeConflict,
					fmt.Sprintf("todo %s is in review; binding it would reopen submitted work; run `atm session bind %s --reopen-reason \"<why review resumed>\"`", todo.ID, todo.ID),
				)
				appErr.Details = map[string]any{
					"todo_id": todo.ID, "current_status": todo.Status,
					"required_flag": "--reopen-reason", "reopen_required": true,
				}
				return appErr
			}
			reopenMessage = "[reopen] " + reopenReason
			if err := store.ValidateTodoLogMessage(reopenMessage, "进展"); err != nil {
				return bindingInvalidArgument(err.Error(), "reopen_reason", input.ReopenReason)
			}
			if unknown := store.UnknownTodoReferences(transaction.Todos(), reopenMessage); len(unknown) > 0 {
				appErr := bindingInvalidArgument(
					fmt.Sprintf("reopen reason references unknown todo IDs: %s", strings.Join(unknown, ", ")),
					"reopen_reason", input.ReopenReason,
				)
				appErr.Details["unknown_todo_ids"] = unknown
				return appErr
			}
			result.Reopened = true
		}
		if unmet := store.UnmetTodoDependencies(transaction.Todos(), *todo); len(unmet) > 0 {
			appErr := application.NewError(
				application.CodeConflict,
				fmt.Sprintf("cannot bind todo %s until dependencies are done: %s", todo.ID, strings.Join(unmet, ", ")),
			)
			appErr.Details = map[string]any{"todo_id": todo.ID, "unmet_dependencies": unmet}
			return appErr
		}
		if err := validateBindingWorkspace(*todo, input); err != nil {
			return err
		}

		todo.Status = store.TodoStatusInProgress
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		if todo.StartTS == nil {
			now := time.Now().In(config.Loc).Unix()
			todo.StartTS = &now
		}
		binding, err := transaction.BindSession(store.TodoSessionBinding{
			SessionID: sessionID,
			TodoID:    todo.ID,
			Agent:     input.Agent,
			Project:   input.Project,
			CWD:       input.CWD,
		})
		if err != nil {
			return fmt.Errorf("bind session: %w", err)
		}
		result.Binding = *binding
		result.Todo = *todo
		if result.Reopened {
			if err := transaction.enqueueEffect(call, EffectTodoStarted, *todo, reopenMessage); err != nil {
				return fmt.Errorf("enqueue reopen effect: %w", err)
			}
		}
		result.Effects, err = transaction.pendingEffects(todo.ID)
		if err != nil {
			return fmt.Errorf("read pending bind effects: %w", err)
		}
		return nil
	})
	if err != nil {
		return BindResult{}, bindingApplicationError("bind session", err)
	}
	if _, err := store.EnsureTodoDoc(&result.Todo); err != nil {
		return BindResult{}, bindingApplicationError("ensure todo document after binding", err)
	}
	return result, nil
}

// Current returns both the durable binding and its lifecycle validity. Stale
// bindings are data, not read failures: callers receive todo_missing or
// todo_not_in_progress and can decide how to render or recover.
func (Service) Current(
	ctx context.Context,
	call application.Call,
	_ CurrentInput,
) (CurrentResult, error) {
	sessionID, err := validateSessionCall(ctx, call, false)
	if err != nil {
		return CurrentResult{}, err
	}
	result := CurrentResult{SessionID: sessionID, State: BindingStateUnbound}
	binding, err := store.CurrentTodoBinding(sessionID)
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return CurrentResult{}, bindingApplicationError("read current session binding", err)
	}
	if binding == nil {
		return result, nil
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return CurrentResult{}, bindingApplicationError("load todos for current session binding", err)
	}
	contexts := BuildBindingContexts([]store.TodoSessionBinding{*binding}, todos)
	result.Context = &contexts[0]
	result.State = BindingState(result.Context.State)
	result.Bound = result.State == BindingStateBound
	return result, nil
}

// Unbind closes the active binding while preserving its history.
func (service Service) Unbind(
	ctx context.Context,
	call application.Call,
	input UnbindInput,
) (UnbindResult, error) {
	sessionID, err := validateSessionCall(ctx, call, true)
	if err != nil {
		return UnbindResult{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return UnbindResult{}, bindingInvalidArgument("unbind reason is required", "reason", input.Reason)
	}
	result := UnbindResult{SessionID: sessionID}
	err = service.Mutate(func(transaction *Transaction) error {
		var err error
		result.Unbound, err = transaction.UnbindSession(sessionID, reason)
		return err
	})
	if err != nil {
		return UnbindResult{}, bindingApplicationError("unbind session", err)
	}
	return result, nil
}

// BuildBindingContexts owns the stale/bound classification shared by the
// Current use case and the session status/history adapters.
func BuildBindingContexts(bindings []store.TodoSessionBinding, todos *store.TodoFile) []BindingContext {
	contexts := make([]BindingContext, 0, len(bindings))
	for _, binding := range bindings {
		item := BindingContext{State: string(BindingStateTodoMissing), Binding: binding}
		if todo := store.FindTodo(todos, binding.TodoID); todo != nil {
			summary := CompactTodo(*todo)
			item.Todo = &summary
			if todo.Status == store.TodoStatusInProgress {
				item.State = string(BindingStateBound)
			} else {
				item.State = string(BindingStateTodoNotInProgress)
			}
		}
		contexts = append(contexts, item)
	}
	return contexts
}

func CompactTodo(todo store.Todo) TodoSummary {
	return TodoSummary{ID: todo.ID, Title: todo.Title, Project: todo.Project, Status: todo.Status}
}

func validateSessionCall(ctx context.Context, call application.Call, mutation bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := call.Validate(); err != nil {
		return "", err
	}
	sessionID := strings.TrimSpace(call.Actor.SessionID)
	if sessionID == "" {
		return "", bindingInvalidArgument("session ID is required", "actor.session_id", call.Actor.SessionID)
	}
	if mutation && call.Actor.Kind == application.ActorController {
		err := application.NewError(application.CodeForbidden, "controllers cannot change an interactive session binding")
		err.Details = map[string]any{"actor_kind": call.Actor.Kind}
		return "", err
	}
	return sessionID, nil
}

func normalizeBindInput(input BindInput) (BindInput, error) {
	input.TodoID = strings.TrimSpace(input.TodoID)
	if input.TodoID == "" {
		return BindInput{}, bindingInvalidArgument("todo ID is required", "todo_id", input.TodoID)
	}
	input.Project = config.CanonicalProject(input.Project)
	input.WorkspaceProject = config.CanonicalProject(input.WorkspaceProject)
	input.CWD = strings.TrimSpace(input.CWD)
	if input.CWD != "" && !filepath.IsAbs(input.CWD) {
		return BindInput{}, bindingInvalidArgument("binding cwd must be an absolute path", "cwd", input.CWD)
	}
	input.Agent = strings.TrimSpace(input.Agent)
	if input.Agent != "" {
		normalized := config.NormalizeAgent(input.Agent)
		if normalized == "" {
			return BindInput{}, bindingInvalidArgument("unknown binding agent", "agent", input.Agent)
		}
		input.Agent = normalized
	}
	return input, nil
}

func validateBindingWorkspace(todo store.Todo, input BindInput) error {
	if input.Force {
		return nil
	}
	expected := config.CanonicalProject(todo.Project)
	if expected == "" || input.CWD == "" || input.WorkspaceProject == "" || input.WorkspaceProject == expected {
		return nil
	}
	err := application.NewError(
		application.CodeConflict,
		fmt.Sprintf(
			"todo %s belongs to project %s but this session is working in %s (project %s); open the right directory, or pass --force if this is deliberate",
			todo.ID, expected, input.CWD, input.WorkspaceProject,
		),
	)
	err.Details = map[string]any{
		"todo_id":           todo.ID,
		"todo_project":      expected,
		"cwd":               input.CWD,
		"workspace_project": input.WorkspaceProject,
	}
	return err
}

func bindingApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	wrapped := application.WrapError(application.CodeUnavailable, operation, err)
	wrapped.Retryable = true
	return wrapped
}

func bindingInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}
