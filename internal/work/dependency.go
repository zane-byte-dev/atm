package work

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type AddDependencyInput struct {
	TodoID       string `json:"todo_id"`
	DependencyID string `json:"dependency_id"`
}

type AddDependencyResult struct {
	Todo     Todo                  `json:"todo"`
	Awakened []store.TodoWakeEvent `json:"awakened,omitempty"`
	Effects  []Effect              `json:"-"`
}

type RemoveDependencyInput struct {
	TodoID       string `json:"todo_id"`
	DependencyID string `json:"dependency_id"`
}

type RemoveDependencyResult struct {
	Removed  bool                  `json:"removed"`
	Todo     Todo                  `json:"todo"`
	Awakened []store.TodoWakeEvent `json:"awakened,omitempty"`
	Effects  []Effect              `json:"-"`
}

type ListDependenciesInput struct {
	TodoID string `json:"todo_id"`
}

type DependencyView struct {
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
	Status string `json:"status"`
	Met    bool   `json:"met"`
}

type ListDependenciesResult struct {
	TodoID       string           `json:"todo_id"`
	Dependencies []DependencyView `json:"dependencies"`
}

// AddDependency atomically adds one graph edge, derives the Todo's waiting
// state, closes stale bindings and reconciles every dependency-satisfied Todo.
// Filesystem projections are represented by durable Work effects.
func (service Service) AddDependency(
	ctx context.Context,
	call application.Call,
	input AddDependencyInput,
) (AddDependencyResult, error) {
	if err := validateDependencyCall(ctx, call); err != nil {
		return AddDependencyResult{}, err
	}
	todoID, dependencyID, err := normalizeDependencyIDs(input.TodoID, input.DependencyID)
	if err != nil {
		return AddDependencyResult{}, err
	}

	result := AddDependencyResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(todoID)
		if findErr != nil {
			return dependencyTodoNotFound("todo_id", todoID, findErr)
		}
		if todoID == dependencyID {
			return dependencyInvalidArgument(
				fmt.Sprintf("todo %s cannot depend on itself", todoID), "dependency_id", dependencyID,
			)
		}
		if _, archived := store.ArchivedStatus(transaction.Todos(), dependencyID); !archived &&
			store.FindTodo(transaction.Todos(), dependencyID) == nil {
			cause := fmt.Errorf("dependency todo not found: %s", dependencyID)
			return dependencyTodoNotFound("dependency_id", dependencyID, cause)
		}

		previousStatus, previousWake := todo.Status, todo.WakeCondition
		if addErr := store.AddTodoDependency(transaction.Todos(), todoID, dependencyID); addErr != nil {
			appErr := application.WrapError(application.CodeConflict, addErr.Error(), addErr)
			appErr.Details = map[string]any{"todo_id": todoID, "dependency_id": dependencyID}
			return appErr
		}
		if store.TodoIsActive(*todo) && len(store.UnmetTodoDependencies(transaction.Todos(), *todo)) > 0 {
			todo.Status = store.TodoStatusWaiting
			todo.WakeCondition = store.TodoDependencyWakeCondition(*todo)
		}
		if todo.Status == store.TodoStatusWaiting &&
			(todo.Status != previousStatus || todo.WakeCondition != previousWake) {
			if enqueueErr := transaction.enqueueOrReplaceEffect(call, EffectTodoWaiting, *todo, ""); enqueueErr != nil {
				return fmt.Errorf("enqueue dependency wait effect: %w", enqueueErr)
			}
		}

		reconciled, reconcileErr := transaction.reconcileDependencies(call, todoID)
		if reconcileErr != nil {
			return reconcileErr
		}
		result.Awakened = reconciled.Awakened
		result.Effects = append(result.Effects, reconciled.Effects...)
		if todo.Status != store.TodoStatusInProgress {
			if _, unbindErr := transaction.UnbindTodoSessions(todo.ID, "dependency:"+todo.Status); unbindErr != nil {
				return fmt.Errorf("unbind todo sessions before dependency wait: %w", unbindErr)
			}
		}
		pending, pendingErr := transaction.pendingEffects(todo.ID)
		if pendingErr != nil {
			return pendingErr
		}
		result.Effects = appendDependencyPrimaryEffects(result.Effects, pending)
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if err != nil {
		return AddDependencyResult{}, dependencyApplicationError("add todo dependency", err)
	}
	return result, nil
}

func (service Service) RemoveDependency(
	ctx context.Context,
	call application.Call,
	input RemoveDependencyInput,
) (RemoveDependencyResult, error) {
	if err := validateDependencyCall(ctx, call); err != nil {
		return RemoveDependencyResult{}, err
	}
	todoID, dependencyID, err := normalizeDependencyIDs(input.TodoID, input.DependencyID)
	if err != nil {
		return RemoveDependencyResult{}, err
	}

	result := RemoveDependencyResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(todoID)
		if findErr != nil {
			return dependencyTodoNotFound("todo_id", todoID, findErr)
		}
		result.Removed, findErr = store.RemoveTodoDependency(transaction.Todos(), todoID, dependencyID)
		if findErr != nil {
			return findErr
		}

		if result.Removed && todo.Status == store.TodoStatusWaiting &&
			strings.HasPrefix(todo.WakeCondition, "waiting for todos: ") {
			if len(todo.DependsOn) == 0 {
				todo.Status = store.TodoStatusOpen
				todo.WakeCondition = ""
				todo.ReviewAt = ""
				event := store.TodoWakeEvent{
					TodoID: todo.ID, Dependencies: []string{}, Reason: "all structured dependencies removed",
				}
				result.Awakened = append(result.Awakened, event)
				if _, unbindErr := transaction.UnbindTodoSessions(todo.ID, "dependency:"+todo.Status); unbindErr != nil {
					return fmt.Errorf("unbind todo sessions after dependency removal: %w", unbindErr)
				}
				if enqueueErr := transaction.enqueueEffectWithCause(
					call, EffectTodoDependencyAwakened, *todo, "[wake] "+event.Reason, todoID,
				); enqueueErr != nil {
					return fmt.Errorf("enqueue dependency removal wake effect: %w", enqueueErr)
				}
			} else {
				todo.WakeCondition = store.TodoDependencyWakeCondition(*todo)
				if enqueueErr := transaction.enqueueOrReplaceEffect(call, EffectTodoWaiting, *todo, ""); enqueueErr != nil {
					return fmt.Errorf("enqueue dependency wait refresh: %w", enqueueErr)
				}
			}
		}

		reconciled, reconcileErr := transaction.reconcileDependencies(call, todoID)
		if reconcileErr != nil {
			return reconcileErr
		}
		result.Awakened = append(result.Awakened, reconciled.Awakened...)
		result.Effects = append(result.Effects, reconciled.Effects...)
		pending, pendingErr := transaction.pendingEffects(todo.ID)
		if pendingErr != nil {
			return pendingErr
		}
		result.Effects = appendDependencyPrimaryEffects(result.Effects, pending)
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if err != nil {
		return RemoveDependencyResult{}, dependencyApplicationError("remove todo dependency", err)
	}
	return result, nil
}

func (service Service) ListDependencies(
	ctx context.Context,
	call application.Call,
	input ListDependenciesInput,
) (ListDependenciesResult, error) {
	if err := validateDependencyCall(ctx, call); err != nil {
		return ListDependenciesResult{}, err
	}
	todoID, err := normalizeDependencyID("todo_id", input.TodoID)
	if err != nil {
		return ListDependenciesResult{}, err
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return ListDependenciesResult{}, dependencyUnavailable("load todo dependencies", err)
	}
	todo := store.FindTodo(todos, todoID)
	if todo == nil {
		return ListDependenciesResult{}, dependencyTodoNotFound(
			"todo_id", todoID, store.TodoNotFoundError(todos, todoID),
		)
	}
	result := ListDependenciesResult{
		TodoID:       todo.ID,
		Dependencies: make([]DependencyView, 0, len(todo.DependsOn)),
	}
	for _, id := range todo.DependsOn {
		view := DependencyView{ID: id, Status: "missing"}
		if dependency := store.FindTodo(todos, id); dependency != nil {
			view.Title = dependency.Title
			view.Status = dependency.Status
			view.Met = dependency.Status == store.TodoStatusDone
		} else if status, archived := store.ArchivedStatus(todos, id); archived {
			view.Status = status
			view.Met = status == store.TodoStatusDone
		}
		result.Dependencies = append(result.Dependencies, view)
	}
	return result, nil
}

func appendDependencyPrimaryEffects(existing, pending []Effect) []Effect {
	seen := make(map[string]struct{}, len(existing))
	for _, effect := range existing {
		seen[effect.ID] = struct{}{}
	}
	for _, effect := range pending {
		if effect.Kind != EffectTodoWaiting && effect.Kind != EffectTodoDependencyAwakened {
			continue
		}
		if _, duplicate := seen[effect.ID]; duplicate {
			continue
		}
		existing = append(existing, effect)
		seen[effect.ID] = struct{}{}
	}
	// A previous command may have committed an older projection and crashed
	// before delivery. Apply that snapshot before a newer wake projection so the
	// document converges to the transaction's final state instead of regressing
	// from open back to waiting.
	sort.SliceStable(existing, func(left, right int) bool {
		if existing[left].CreatedAt == existing[right].CreatedAt {
			return existing[left].ID < existing[right].ID
		}
		return existing[left].CreatedAt < existing[right].CreatedAt
	})
	return existing
}

func validateDependencyCall(ctx context.Context, call application.Call) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return dependencyUnavailable("todo dependency request canceled", err)
	}
	return call.Validate()
}

func normalizeDependencyIDs(todoID, dependencyID string) (string, string, error) {
	normalizedTodoID, err := normalizeDependencyID("todo_id", todoID)
	if err != nil {
		return "", "", err
	}
	normalizedDependencyID, err := normalizeDependencyID("dependency_id", dependencyID)
	if err != nil {
		return "", "", err
	}
	return normalizedTodoID, normalizedDependencyID, nil
}

func normalizeDependencyID(field, value string) (string, error) {
	if strings.TrimSpace(value) == "" || !store.LooksLikeTodoID(value) {
		return "", dependencyInvalidArgument("valid todo ID is required", field, value)
	}
	return store.NormalizeTodoID(value), nil
}

func dependencyTodoNotFound(field, id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
	err.Details = map[string]any{"field": field, field: store.NormalizeTodoID(id)}
	return err
}

func dependencyInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func dependencyUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}

func dependencyApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return dependencyUnavailable(operation, err)
}
