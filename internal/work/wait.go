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

// WaitInput describes the optional conditions supplied by an adapter. Empty
// values retain the corresponding condition already stored on the Todo. This
// is important when a caller adds a calendar review to an existing external
// wake condition (or vice versa).
type WaitInput struct {
	TodoID        string `json:"todo_id"`
	SessionID     string `json:"session_id,omitempty"`
	WakeCondition string `json:"wake_condition,omitempty"`
	ReviewAt      string `json:"review_at,omitempty"`
}

type WaitResult struct {
	Todo            store.Todo `json:"todo"`
	UnboundSessions int        `json:"unbound_sessions"`
	Effects         []Effect   `json:"-"`
}

// Wait is a compatibility operation that adds waiting presentation metadata to
// in-progress work and releases every bound session. It does not introduce a
// lifecycle state: the Todo remains in_progress.
func (service Service) Wait(
	ctx context.Context,
	call application.Call,
	input WaitInput,
) (WaitResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return WaitResult{}, waitUnavailable("wait todo", err)
	}
	if err := call.Validate(); err != nil {
		return WaitResult{}, err
	}

	if strings.TrimSpace(input.TodoID) != "" && !store.LooksLikeTodoID(input.TodoID) {
		return WaitResult{}, waitInvalidArgument(
			fmt.Sprintf("invalid todo ID %q", input.TodoID), "todo_id", input.TodoID,
		)
	}
	if input.WakeCondition != "" && strings.TrimSpace(input.WakeCondition) == "" {
		return WaitResult{}, waitInvalidArgument(
			"wake condition cannot be blank", "wake_condition", input.WakeCondition,
		)
	}
	if err := ValidateReviewAt(input.ReviewAt); err != nil {
		return WaitResult{}, waitInvalidArgument(err.Error(), "review_at", input.ReviewAt)
	}

	result := WaitResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		sessionID := input.SessionID
		if strings.TrimSpace(sessionID) == "" {
			sessionID = call.Actor.SessionID
		}
		todoID, err := transaction.resolveTransitionTodoID(input.TodoID, sessionID, waitTransitionTarget)
		if err != nil {
			return err
		}
		todo, err := transaction.Todo(todoID)
		if err != nil {
			appErr := application.WrapError(application.CodeNotFound, err.Error(), err)
			appErr.Details = map[string]any{"todo_id": todoID}
			return appErr
		}
		if !store.TodoIsActive(*todo) {
			appErr := application.NewError(
				application.CodeConflict,
				fmt.Sprintf("cannot wait todo %s with status %s", todo.ID, todo.Status),
			)
			appErr.Details = map[string]any{
				"todo_id":        todo.ID,
				"current_status": todo.Status,
			}
			return appErr
		}

		wakeCondition := todo.WakeCondition
		if input.WakeCondition != "" {
			wakeCondition = input.WakeCondition
		}
		reviewAt := todo.ReviewAt
		if input.ReviewAt != "" {
			reviewAt = input.ReviewAt
		}
		if wakeCondition == "" && reviewAt == "" {
			appErr := waitInvalidArgument(
				"wait requires --wake or --review-at", "wake_condition", input.WakeCondition,
			)
			appErr.Details["review_at"] = input.ReviewAt
			return appErr
		}

		changed := todo.Status != store.TodoStatusInProgress ||
			todo.WakeCondition != wakeCondition || todo.ReviewAt != reviewAt
		if !changed {
			result.Todo = *todo
			result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, "waiting")
			if err != nil {
				return fmt.Errorf("unbind todo sessions before waiting: %w", err)
			}
			result.Effects, err = transaction.pendingEffects(todo.ID)
			return err
		}

		todo.WakeCondition = wakeCondition
		todo.ReviewAt = reviewAt
		todo.Status = store.TodoStatusInProgress
		if todo.StartTS == nil {
			now := time.Now().In(config.Loc).Unix()
			todo.StartTS = &now
		}
		result.Todo = *todo
		if err := transaction.enqueueOrReplaceEffect(call, EffectTodoWaiting, *todo, ""); err != nil {
			return fmt.Errorf("enqueue wait effect: %w", err)
		}
		result.UnboundSessions, err = transaction.UnbindTodoSessions(todo.ID, "waiting")
		if err != nil {
			return fmt.Errorf("unbind todo sessions before waiting: %w", err)
		}
		result.Effects, err = transaction.pendingEffects(todo.ID)
		return err
	})
	if err != nil {
		var appErr *application.Error
		if errors.As(err, &appErr) {
			return WaitResult{}, appErr
		}
		return WaitResult{}, waitUnavailable("wait todo", err)
	}
	return result, nil
}

// ValidateReviewAt is the shared domain validator for every command that
// accepts a Todo review date. Empty means unset; otherwise the stable format is
// a calendar date in the configured ATM location.
func ValidateReviewAt(value string) error {
	if value == "" {
		return nil
	}
	if _, err := time.ParseInLocation("2006-01-02", value, config.Loc); err != nil {
		return fmt.Errorf("invalid review date %q (use YYYY-MM-DD)", value)
	}
	return nil
}

func waitInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func waitUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}
