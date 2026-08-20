package work

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type SubmitInput struct {
	TodoID    string `json:"todo_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type SubmitResult struct {
	Todo            store.Todo `json:"todo"`
	AlreadyReview   bool       `json:"already_review"`
	UnboundSessions int        `json:"unbound_sessions"`
	Effects         []Effect   `json:"-"`
}

// Submit moves one in-progress Todo to review. Lifecycle mutation and binding
// closure share WorkStateTx with a durable outbox insert. The returned effects
// are therefore a view of committed work, not the only copy of it; retrying an
// already-review Todo returns any effects that are still unacknowledged.
func (service Service) Submit(
	ctx context.Context,
	call application.Call,
	input SubmitInput,
) (SubmitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SubmitResult{}, err
	}
	if err := call.Validate(); err != nil {
		return SubmitResult{}, err
	}
	result := SubmitResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		sessionID := input.SessionID
		if strings.TrimSpace(sessionID) == "" {
			sessionID = call.Actor.SessionID
		}
		todoID, err := transaction.resolveTransitionTodoID(input.TodoID, sessionID, submitTransitionTarget)
		if err != nil {
			return err
		}
		todo, err := transaction.Todo(todoID)
		if err != nil {
			appErr := application.WrapError(application.CodeNotFound, err.Error(), err)
			appErr.Details = map[string]any{"todo_id": todoID}
			return appErr
		}
		result.Todo = *todo
		if todo.Status == store.TodoStatusReview {
			result.AlreadyReview = true
			result.Effects, err = transaction.pendingEffects(todo.ID)
			return err
		}
		if todo.Status != store.TodoStatusInProgress {
			appErr := application.NewError(
				application.CodeConflict,
				fmt.Sprintf("cannot submit todo %s with status %s", todo.ID, todo.Status),
			)
			appErr.Details = map[string]any{
				"todo_id":         todo.ID,
				"current_status":  todo.Status,
				"required_status": store.TodoStatusInProgress,
			}
			return appErr
		}

		message := submitLogMessage(input.Reason)
		if err := store.ValidateTodoLogMessage(message, "进展"); err != nil {
			return submitInvalidArgument(err.Error(), "reason", input.Reason)
		}
		if unknown := store.UnknownTodoReferences(transaction.Todos(), message); len(unknown) > 0 {
			appErr := submitInvalidArgument(
				fmt.Sprintf("todo log references unknown todo IDs: %s; create and verify structured todos before logging them", strings.Join(unknown, ", ")),
				"reason",
				input.Reason,
			)
			appErr.Details["unknown_todo_ids"] = unknown
			return appErr
		}

		todo.Status = store.TodoStatusReview
		todo.WakeCondition = ""
		todo.ReviewAt = ""
		result.Todo = *todo
		if err := transaction.enqueueEffect(call, EffectTodoSubmitted, *todo, message); err != nil {
			return fmt.Errorf("enqueue submit effect: %w", err)
		}
		result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, "submit:review")
		if err != nil {
			return fmt.Errorf("unbind todo sessions before submit: %w", err)
		}
		result.Effects, err = transaction.pendingEffects(todo.ID)
		return err
	})
	if err != nil {
		var appErr *application.Error
		if errors.As(err, &appErr) {
			return SubmitResult{}, appErr
		}
		wrapped := application.WrapError(application.CodeUnavailable, "submit todo", err)
		wrapped.Retryable = true
		return SubmitResult{}, wrapped
	}
	return result, nil
}

func submitLogMessage(reason string) string {
	message := "[submit]"
	if reason != "" {
		message += " " + reason
	}
	return message
}

func submitInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}
