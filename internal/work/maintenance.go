package work

import (
	"context"
	"fmt"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type MaintainInput struct {
	TodoID string `json:"todo_id"`
	Limit  int    `json:"limit"`
}

type MaintainResult struct {
	Todo              Todo     `json:"todo"`
	AlreadyMaintained bool     `json:"already_maintained"`
	Effects           []Effect `json:"-"`
}

// Maintain marks active work as a bounded maintenance lane. The tag and limit
// are one application mutation, and the existing Markdown projection is
// requested through the Work outbox in the same transaction.
func (service Service) Maintain(
	ctx context.Context,
	call application.Call,
	input MaintainInput,
) (MaintainResult, error) {
	if err := validateLifecycleCall(ctx, call); err != nil {
		return MaintainResult{}, err
	}
	if err := validateLifecycleTodoID(input.TodoID); err != nil {
		return MaintainResult{}, err
	}
	if input.Limit < 1 {
		return MaintainResult{}, lifecycleInvalidArgument("maintenance limit must be at least 1", "limit", input.Limit)
	}

	result := MaintainResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(input.TodoID)
		if err != nil {
			return lifecycleTodoNotFound(input.TodoID, err)
		}
		if !store.TodoIsActive(*todo) {
			return lifecycleConflict(
				fmt.Sprintf("cannot maintain todo %s with status %s", todo.ID, todo.Status), todo.ID, todo.Status,
			)
		}

		changed := !store.TodoHasTag(*todo, store.TodoTagMaintenance) || todo.MaintenanceLimit != input.Limit
		store.AddTodoTag(todo, store.TodoTagMaintenance)
		todo.MaintenanceLimit = input.Limit
		result.Todo = cloneTodo(*todo)
		result.AlreadyMaintained = !changed
		if changed {
			if err := transaction.enqueueOrReplaceEffect(call, EffectTodoUpdated, *todo, ""); err != nil {
				return fmt.Errorf("enqueue maintenance projection: %w", err)
			}
		}
		pending, err := transaction.pendingEffects(todo.ID)
		if err != nil {
			return err
		}
		for _, effect := range pending {
			if effect.Kind == EffectTodoUpdated {
				result.Effects = append(result.Effects, effect)
			}
		}
		return nil
	})
	if err != nil {
		return MaintainResult{}, lifecycleApplicationError("maintain todo", err)
	}
	return result, nil
}
