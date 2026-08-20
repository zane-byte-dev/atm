package work

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type RetentionAction string

const (
	RetentionArchive RetentionAction = "archive"
	RetentionRestore RetentionAction = "restore"
)

type RetentionInput struct {
	TodoIDs   []string `json:"todo_ids"`
	SessionID string   `json:"session_id,omitempty"`
}

type RetentionResult struct {
	Moved     []string `json:"moved"`
	Unchanged []string `json:"unchanged,omitempty"`
}

func (service Service) Archive(ctx context.Context, call application.Call, input RetentionInput) (RetentionResult, error) {
	return service.moveRetention(ctx, call, input, RetentionArchive)
}

func (service Service) Restore(ctx context.Context, call application.Call, input RetentionInput) (RetentionResult, error) {
	return service.moveRetention(ctx, call, input, RetentionRestore)
}

// moveRetention implements the single archive layer. Lifecycle state is
// preserved across archive and restore.
func (service Service) moveRetention(
	ctx context.Context,
	call application.Call,
	input RetentionInput,
	action RetentionAction,
) (RetentionResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return RetentionResult{}, err
	}
	var ids []string
	var err error
	if len(input.TodoIDs) > 0 {
		ids, err = normalizeRetentionIDs(input.TodoIDs)
		if err != nil {
			return RetentionResult{}, err
		}
	} else if strings.TrimSpace(input.SessionID) == "" {
		return RetentionResult{}, lifecycleInvalidArgument("at least one todo ID is required", "todo_ids", input.TodoIDs)
	}
	result := RetentionResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		if len(ids) == 0 {
			binding, bindingErr := transaction.currentSessionBinding(strings.TrimSpace(input.SessionID))
			if bindingErr != nil {
				return bindingErr
			}
			if binding == nil {
				return transitionTargetUnavailable(input.SessionID)
			}
			ids = []string{binding.TodoID}
		}
		move := make([]string, 0, len(ids))
		for _, id := range ids {
			todo := store.FindTodo(transaction.Todos(), id)
			_, archived := store.ArchivedStatus(transaction.Todos(), id)
			switch action {
			case RetentionArchive:
				if archived {
					result.Unchanged = append(result.Unchanged, id)
					continue
				}
				if todo == nil {
					return lifecycleTodoNotFound(id, store.TodoNotFoundError(transaction.Todos(), id))
				}
				move = append(move, id)
			case RetentionRestore:
				if todo != nil {
					result.Unchanged = append(result.Unchanged, id)
					continue
				}
				if !archived {
					return lifecycleTodoNotFound(id, store.TodoNotFoundError(transaction.Todos(), id))
				}
				move = append(move, id)
			default:
				return lifecycleInvalidArgument("unknown retention action", "action", action)
			}
		}

		if len(move) == 0 {
			return nil
		}
		var moveErr error
		switch action {
		case RetentionArchive:
			result.Moved, moveErr = transaction.ArchiveTodos(move)
		case RetentionRestore:
			result.Moved, moveErr = transaction.UnarchiveTodos(move)
		}
		return moveErr
	})
	if err != nil {
		return RetentionResult{}, lifecycleApplicationError(string(action)+" todos", err)
	}
	return result, nil
}

type DeleteSelector struct {
	TodoID  string `json:"todo_id,omitempty"`
	Project string `json:"project,omitempty"`
}

// DeletePlan is the exact target set a human was shown before confirmation.
// Delete rechecks it under the WorkState write lock, preventing a project-wide
// confirmation from silently covering Todos created after the prompt.
type DeletePlan struct {
	Selector DeleteSelector `json:"selector"`
	TodoIDs  []string       `json:"todo_ids"`
}

type DeleteInput struct {
	Plan      DeletePlan `json:"plan"`
	Confirmed bool       `json:"confirmed"`
}

type DeleteResult struct {
	Deleted []string `json:"deleted"`
}

func (Service) PlanDelete(ctx context.Context, call application.Call, selector DeleteSelector) (DeletePlan, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return DeletePlan{}, err
	}
	selector.TodoID = strings.TrimSpace(selector.TodoID)
	selector.Project = strings.TrimSpace(selector.Project)
	if (selector.TodoID == "") == (selector.Project == "") {
		return DeletePlan{}, lifecycleInvalidArgument(
			"provide exactly one todo ID or project", "selector", selector,
		)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return DeletePlan{}, lifecycleUnavailable("plan permanent todo deletion", err)
	}
	plan := DeletePlan{Selector: selector}
	if selector.TodoID != "" {
		if err := validateLifecycleTodoID(selector.TodoID); err != nil {
			return DeletePlan{}, err
		}
		id := store.NormalizeTodoID(selector.TodoID)
		if store.FindTodo(todos, id) == nil {
			if _, archived := store.ArchivedStatus(todos, id); !archived {
				return DeletePlan{}, lifecycleTodoNotFound(selector.TodoID, store.TodoNotFoundError(todos, selector.TodoID))
			}
		}
		plan.Selector.TodoID = id
		plan.TodoIDs = []string{id}
		return plan, nil
	}
	for _, todo := range todos.Items {
		if todo.Project == selector.Project {
			plan.TodoIDs = append(plan.TodoIDs, todo.ID)
		}
	}
	if len(plan.TodoIDs) == 0 {
		return DeletePlan{}, lifecycleInvalidArgument("no todos found for project: "+selector.Project, "project", selector.Project)
	}
	sort.Strings(plan.TodoIDs)
	return plan, nil
}

// Delete permanently removes the exact confirmed plan. The Confirmed boolean
// is a workflow guard, not proof of human identity: CLI flags and IPC payloads
// are replayable. Adapters remain responsible for obtaining the confirmation.
func (service Service) Delete(ctx context.Context, call application.Call, input DeleteInput) (DeleteResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return DeleteResult{}, err
	}
	if !input.Confirmed {
		return DeleteResult{}, application.NewError(application.CodeForbidden, "permanent todo deletion requires explicit confirmation")
	}
	expected, err := normalizeRetentionIDs(input.Plan.TodoIDs)
	if err != nil {
		return DeleteResult{}, err
	}
	selector := input.Plan.Selector
	selector.TodoID = strings.TrimSpace(selector.TodoID)
	selector.Project = strings.TrimSpace(selector.Project)
	if (selector.TodoID == "") == (selector.Project == "") {
		return DeleteResult{}, lifecycleInvalidArgument("delete plan has an invalid selector", "selector", selector)
	}

	result := DeleteResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		actual := make([]string, 0, len(expected))
		if selector.TodoID != "" {
			id := store.NormalizeTodoID(selector.TodoID)
			if store.FindTodo(transaction.Todos(), id) != nil {
				actual = append(actual, id)
			} else if _, archived := store.ArchivedStatus(transaction.Todos(), id); archived {
				actual = append(actual, id)
			}
		} else {
			for _, todo := range transaction.Todos().Items {
				if todo.Project == selector.Project {
					actual = append(actual, todo.ID)
				}
			}
		}
		sort.Strings(actual)
		if !equalStrings(actual, expected) {
			err := application.NewError(application.CodeConflict, "delete target changed after confirmation; confirm the current target set again")
			err.Details = map[string]any{"expected_todo_ids": expected, "actual_todo_ids": actual}
			return err
		}
		deleted, deleteErr := transaction.PermanentlyDeleteTodos(actual)
		result.Deleted = deleted
		return deleteErr
	})
	if err != nil {
		return DeleteResult{}, lifecycleApplicationError("permanently delete todos", err)
	}
	if err := cleanupDeletedTodoArtifacts(result.Deleted); err != nil {
		return result, err
	}
	return result, nil
}

func cleanupDeletedTodoArtifacts(ids []string) error {
	var failures []string
	for _, id := range ids {
		if err := os.Remove(store.TodoDocPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			failures = append(failures, fmt.Sprintf("remove document %s: %v", id, err))
		}
		if err := store.CleanupTodoAssets(id); err != nil {
			failures = append(failures, fmt.Sprintf("remove assets %s: %v", id, err))
		}
	}
	if len(failures) > 0 {
		err := application.NewError(application.CodeUnavailable, "todos were deleted but artifact cleanup failed")
		err.Details = map[string]any{"failures": failures, "todo_ids": ids}
		return err
	}
	return nil
}

func normalizeRetentionIDs(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, lifecycleInvalidArgument("at least one todo ID is required", "todo_ids", values)
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if err := validateLifecycleTodoID(value); err != nil {
			return nil, err
		}
		id := store.NormalizeTodoID(value)
		if !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	sort.Strings(result)
	return result, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
