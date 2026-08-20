package work

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type BulkAction string

const (
	BulkDone BulkAction = "done"
	BulkMove BulkAction = "move"
	BulkEdit BulkAction = "edit"
)

type BulkInput struct {
	Action    BulkAction `json:"action"`
	TodoIDs   []string   `json:"todo_ids"`
	Project   string     `json:"project,omitempty"`
	Status    string     `json:"status,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Confirmed bool       `json:"confirmed"`
}

type BulkResult struct {
	Action   BulkAction            `json:"action"`
	Todos    []Todo                `json:"todos"`
	Awakened []store.TodoWakeEvent `json:"awakened"`
	Effects  []Effect              `json:"-"`
}

// Bulk applies one intent to an exact Todo set in a single WorkState
// transaction. Lifecycle changes, Session unbinding, dependency reconciliation
// and durable projection effects therefore either all commit or all roll back.
//
// Confirmed records that an adapter obtained an explicit batch action. Like all
// replayable CLI flags and payload fields it is a workflow guard, not proof of
// human presence; Done separately enforces the same ActorHuman policy as the
// single-Todo transition.
func (service Service) Bulk(ctx context.Context, call application.Call, input BulkInput) (BulkResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return BulkResult{}, err
	}
	action, err := normalizeBulkAction(input.Action)
	if err != nil {
		return BulkResult{}, err
	}
	if action == BulkDone && call.Actor.Kind != application.ActorHuman {
		err := application.NewError(application.CodeForbidden,
			"only a human actor may mark a todo done; agents must use todo submit")
		err.Details = map[string]any{
			"actor_kind": call.Actor.Kind, "required_actor_kind": application.ActorHuman,
		}
		return BulkResult{}, err
	}
	if !input.Confirmed {
		err := application.NewError(application.CodeForbidden, "bulk todo mutation requires explicit confirmation")
		err.Details = map[string]any{"action": action, "todo_ids": input.TodoIDs}
		return BulkResult{}, err
	}
	ids, err := normalizeBulkTodoIDs(input.TodoIDs)
	if err != nil {
		return BulkResult{}, err
	}

	project := ""
	if strings.TrimSpace(input.Project) != "" {
		project = config.CanonicalProject(input.Project)
	}
	status := ""
	if strings.TrimSpace(input.Status) != "" {
		status, err = normalizeMetadataStatus(input.Status)
		if err != nil {
			return BulkResult{}, err
		}
	}
	switch action {
	case BulkMove:
		if project == "" {
			return BulkResult{}, lifecycleInvalidArgument("bulk move requires a project", "project", input.Project)
		}
	case BulkEdit:
		if project == "" && status == "" {
			return BulkResult{}, lifecycleInvalidArgument(
				"bulk edit requires a project or status", "input", input,
			)
		}
	}

	reason := strings.TrimSpace(input.Reason)
	closeStatus := ""
	message := ""
	if action == BulkDone {
		closeStatus = store.TodoStatusDone
		if reason != "" {
			message = fmt.Sprintf("[%s] %s", closeStatus, reason)
			if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
				return BulkResult{}, lifecycleInvalidArgument(err.Error(), "reason", input.Reason)
			}
		}
	}

	result := BulkResult{Action: action, Todos: []Todo{}, Awakened: []store.TodoWakeEvent{}, Effects: []Effect{}}
	err = service.Mutate(func(transaction *Transaction) error {
		selected := make([]*store.Todo, 0, len(ids))
		selectedIDs := make(map[string]bool, len(ids))
		for _, id := range ids {
			todo, findErr := transaction.Todo(id)
			if findErr != nil {
				return lifecycleTodoNotFound(id, findErr)
			}
			selected = append(selected, todo)
			selectedIDs[todo.ID] = true
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
		if action == BulkDone {
			for _, todo := range selected {
				if todo.Status != closeStatus && !store.TodoIsActive(*todo) {
					return lifecycleConflict(
						fmt.Sprintf("cannot mark todo %s %s because it is already %s; start it to reopen first", todo.ID, closeStatus, todo.Status),
						todo.ID, todo.Status,
					)
				}
			}
		}

		now := time.Now().In(config.Loc).Unix()
		today := store.Today()
		for _, todo := range selected {
			switch action {
			case BulkDone:
				if todo.Status == closeStatus {
					continue
				}
				todo.Status = closeStatus
				todo.Closed = &today
				todo.DoneTS = &now
				if reason == "" {
					todo.ClosedReason = nil
				} else {
					value := reason
					todo.ClosedReason = &value
				}
				if err := transaction.enqueueEffect(call, EffectTodoClosed, *todo, message); err != nil {
					return fmt.Errorf("enqueue bulk close effect for %s: %w", todo.ID, err)
				}
				if _, err := transaction.UnbindTodoSessions(todo.ID, "bulk:"+todo.Status); err != nil {
					return fmt.Errorf("unbind bulk-closed todo %s: %w", todo.ID, err)
				}
			case BulkMove:
				todo.Project = project
				if err := transaction.enqueueOrReplaceEffect(call, EffectTodoUpdated, *todo, ""); err != nil {
					return fmt.Errorf("enqueue bulk move effect for %s: %w", todo.ID, err)
				}
			case BulkEdit:
				if project != "" {
					todo.Project = project
				}
				if status != "" {
					todo.Status = status
				}
				if todo.Status != store.TodoStatusInProgress {
					todo.WakeCondition = ""
					todo.ReviewAt = ""
				}
				if status != "" && todo.Status != store.TodoStatusInProgress {
					if _, err := transaction.UnbindTodoSessions(todo.ID, "bulk-status:"+todo.Status); err != nil {
						return fmt.Errorf("unbind bulk-edited todo %s: %w", todo.ID, err)
					}
				}
				if err := transaction.enqueueOrReplaceEffect(call, EffectTodoUpdated, *todo, ""); err != nil {
					return fmt.Errorf("enqueue bulk edit effect for %s: %w", todo.ID, err)
				}
			}
		}

		if action == BulkDone {
			reconciled, reconcileErr := transaction.reconcileDependencies(call, "")
			if reconcileErr != nil {
				return reconcileErr
			}
			result.Awakened = append(result.Awakened, reconciled.Awakened...)
		}
		for _, todo := range selected {
			result.Todos = append(result.Todos, cloneTodo(*todo))
		}

		pending, pendingErr := transaction.pendingEffects("")
		if pendingErr != nil {
			return pendingErr
		}
		for _, effect := range pending {
			// Include every older projection for a selected Todo, not only the
			// effect created by this call. Delivery is ordered by created_at; if a
			// failed waiting/start projection were skipped here and drained after
			// the new close/update, its stale snapshot could overwrite the newer
			// document metadata.
			if selectedIDs[effect.TodoID] {
				result.Effects = append(result.Effects, effect)
				continue
			}
			if action == BulkDone && effect.Kind == EffectTodoDependencyAwakened &&
				(effect.RequestID == call.RequestID || todoDependenciesIntersect(effect.Todo.DependsOn, selectedIDs)) {
				result.Effects = append(result.Effects, effect)
			}
		}
		return nil
	})
	if err != nil {
		return BulkResult{}, lifecycleApplicationError("bulk "+string(action)+" todos", err)
	}
	return result, nil
}

func normalizeBulkAction(value BulkAction) (BulkAction, error) {
	action := BulkAction(strings.ToLower(strings.TrimSpace(string(value))))
	switch action {
	case BulkDone, BulkMove, BulkEdit:
		return action, nil
	default:
		// `drop` was an archive alias here. `todo archive` already takes a list of
		// IDs, so batching it a second way only added a second spelling.
		return "", lifecycleInvalidArgument(
			fmt.Sprintf("unsupported bulk action %q (use done, move, or edit)", value), "action", value,
		)
	}
}

func normalizeBulkTodoIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, lifecycleInvalidArgument("at least one todo ID is required", "todo_ids", values)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(values))
	for index, value := range values {
		if err := validateLifecycleTodoID(value); err != nil {
			if appErr, ok := err.(*application.Error); ok {
				appErr.Details["index"] = index
			}
			return nil, err
		}
		id := store.NormalizeTodoID(value)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func todoDependenciesIntersect(dependencies []string, selected map[string]bool) bool {
	for _, dependency := range dependencies {
		if selected[store.NormalizeTodoID(dependency)] {
			return true
		}
	}
	return false
}
