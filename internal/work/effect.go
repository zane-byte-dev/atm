package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// EffectKind identifies a projection update owned by the Work application
// service. The payload is persisted in work_effect_outbox before an adapter can
// observe it.
type EffectKind string

const (
	EffectTodoSubmitted          EffectKind = "todo_submitted"
	EffectTodoWaiting            EffectKind = "todo_waiting"
	EffectTodoStarted            EffectKind = "todo_started"
	EffectTodoUpdated            EffectKind = "todo_updated"
	EffectTodoRefined            EffectKind = "todo_refined"
	EffectTodoClosed             EffectKind = "todo_closed"
	EffectTodoAwakened           EffectKind = "todo_awakened"
	EffectTodoDependencyAwakened EffectKind = "todo_dependency_awakened"
)

// Effect is the typed, durable form presented to CLI/controller adapters. Its
// ID remains stable across retries with different request IDs. Delivery is
// at-least-once: adapters acknowledge only after the whole effect succeeds, so
// a crash between applying and acknowledging may apply it again.
type Effect struct {
	ID        string `json:"id"`
	RequestID string `json:"request_id"`
	TodoID    string `json:"todo_id"`
	// CauseTodoID links a derived effect, such as waking a dependent, to the
	// lifecycle transition that produced it. It lets an idempotent retry recover
	// every still-pending projection without conflating unrelated wake events.
	CauseTodoID   string       `json:"cause_todo_id,omitempty"`
	Kind          EffectKind   `json:"kind"`
	Todo          store.Todo   `json:"todo"`
	RelatedTodos  []store.Todo `json:"related_todos,omitempty"`
	Message       string       `json:"message,omitempty"`
	CreatedAt     int64        `json:"created_at"`
	AttemptCount  int          `json:"attempt_count"`
	LastAttemptAt *int64       `json:"last_attempt_at,omitempty"`
	LastError     string       `json:"last_error,omitempty"`
}

type effectPayload struct {
	Todo         store.Todo   `json:"todo"`
	Message      string       `json:"message,omitempty"`
	CauseTodoID  string       `json:"cause_todo_id,omitempty"`
	RelatedTodos []store.Todo `json:"related_todos,omitempty"`
}

type PendingEffectsInput struct {
	TodoID string `json:"todo_id,omitempty"`
}

type PendingEffectsResult struct {
	Effects []Effect `json:"effects"`
}

type CompleteEffectInput struct {
	EffectID string `json:"effect_id"`
}

type FailEffectInput struct {
	EffectID string `json:"effect_id"`
	Error    string `json:"error"`
}

// EffectExecutor is the adapter port for projections outside SQLite. Work owns
// delivery order and acknowledgement; CLI/controller adapters own the actual
// filesystem and notification operations.
type EffectExecutor interface {
	ApplyWorkEffect(Effect) error
}

func (transaction *Transaction) enqueueEffect(call application.Call, kind EffectKind, todo store.Todo, message string) error {
	return transaction.enqueueEffectWithCause(call, kind, todo, message, "")
}

func (transaction *Transaction) enqueueEffectWithCause(
	call application.Call,
	kind EffectKind,
	todo store.Todo,
	message string,
	causeTodoID string,
) error {
	payloadJSON, err := encodeEffectPayload(todo, message, causeTodoID, nil)
	if err != nil {
		return err
	}
	return transaction.state.EnqueueWorkEffect(store.WorkEffectRecord{
		ID:          "we_" + uuid.NewString(),
		RequestID:   call.RequestID,
		TodoID:      todo.ID,
		Kind:        string(kind),
		PayloadJSON: payloadJSON,
		CreatedAt:   time.Now().UTC().UnixNano(),
	})
}

// enqueueOrReplaceEffect coalesces an undelivered projection of the same kind.
// Wait uses this when a caller updates the condition before its earlier document
// sync has run; the stable row then carries the latest Todo snapshot.
func (transaction *Transaction) enqueueOrReplaceEffect(call application.Call, kind EffectKind, todo store.Todo, message string) error {
	records, err := transaction.state.PendingWorkEffects(todo.ID)
	if err != nil {
		return err
	}
	payloadJSON, err := encodeEffectPayload(todo, message, "", nil)
	if err != nil {
		return err
	}
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Kind == string(kind) {
			return transaction.state.UpdatePendingWorkEffectPayload(records[index].ID, payloadJSON)
		}
	}
	return transaction.state.EnqueueWorkEffect(store.WorkEffectRecord{
		ID:          "we_" + uuid.NewString(),
		RequestID:   call.RequestID,
		TodoID:      todo.ID,
		Kind:        string(kind),
		PayloadJSON: payloadJSON,
		CreatedAt:   time.Now().UTC().UnixNano(),
	})
}

// enqueueOrReplaceRefinementEffect coalesces only the same logical refine
// request. A different request is another human-requested analysis pass and
// must retain its own append event rather than overwriting history.
func (transaction *Transaction) enqueueOrReplaceRefinementEffect(
	call application.Call,
	parent store.Todo,
	children []store.Todo,
	analysis string,
) error {
	records, err := transaction.state.PendingWorkEffects(parent.ID)
	if err != nil {
		return err
	}
	payloadJSON, err := encodeEffectPayload(parent, analysis, "", children)
	if err != nil {
		return err
	}
	for index := len(records) - 1; index >= 0; index-- {
		if records[index].Kind == string(EffectTodoRefined) && records[index].RequestID == call.RequestID {
			return transaction.state.UpdatePendingWorkEffectPayload(records[index].ID, payloadJSON)
		}
	}
	return transaction.state.EnqueueWorkEffect(store.WorkEffectRecord{
		ID:          "we_" + uuid.NewString(),
		RequestID:   call.RequestID,
		TodoID:      parent.ID,
		Kind:        string(EffectTodoRefined),
		PayloadJSON: payloadJSON,
		CreatedAt:   time.Now().UTC().UnixNano(),
	})
}

func encodeEffectPayload(todo store.Todo, message, causeTodoID string, relatedTodos []store.Todo) (string, error) {
	payload, err := json.Marshal(effectPayload{
		Todo: todo, Message: message, CauseTodoID: causeTodoID, RelatedTodos: relatedTodos,
	})
	if err != nil {
		return "", fmt.Errorf("encode work effect payload: %w", err)
	}
	return string(payload), nil
}

func (transaction *Transaction) pendingEffects(todoID string) ([]Effect, error) {
	records, err := transaction.state.PendingWorkEffects(todoID)
	if err != nil {
		return nil, err
	}
	return decodeEffects(records)
}

func decodeEffects(records []store.WorkEffectRecord) ([]Effect, error) {
	effects := make([]Effect, 0, len(records))
	for _, record := range records {
		var payload effectPayload
		if err := json.Unmarshal([]byte(record.PayloadJSON), &payload); err != nil {
			return nil, fmt.Errorf("decode work effect %s payload: %w", record.ID, err)
		}
		if payload.Todo.ID == "" || payload.Todo.ID != record.TodoID {
			return nil, fmt.Errorf("decode work effect %s payload: todo ID does not match outbox row", record.ID)
		}
		effects = append(effects, Effect{
			ID:            record.ID,
			RequestID:     record.RequestID,
			TodoID:        record.TodoID,
			CauseTodoID:   payload.CauseTodoID,
			Kind:          EffectKind(record.Kind),
			Todo:          payload.Todo,
			RelatedTodos:  payload.RelatedTodos,
			Message:       payload.Message,
			CreatedAt:     record.CreatedAt,
			AttemptCount:  record.AttemptCount,
			LastAttemptAt: record.LastAttemptAt,
			LastError:     record.LastError,
		})
	}
	return effects, nil
}

// PendingEffects returns every unacknowledged effect, optionally narrowed to a
// Todo. Submit and Wait also include this pending set in their result, so normal
// command retries repair a previous post-commit failure without a separate
// recovery command.
func (service Service) PendingEffects(ctx context.Context, call application.Call, input PendingEffectsInput) (PendingEffectsResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return PendingEffectsResult{}, effectUnavailable("list pending work effects", err)
	}
	if err := call.Validate(); err != nil {
		return PendingEffectsResult{}, err
	}
	todoID := ""
	if strings.TrimSpace(input.TodoID) != "" {
		if !store.LooksLikeTodoID(input.TodoID) {
			return PendingEffectsResult{}, effectInvalidArgument("invalid todo ID", "todo_id", input.TodoID)
		}
		todoID = store.NormalizeTodoID(input.TodoID)
	}
	records, err := store.ListPendingWorkEffects(todoID)
	if err != nil {
		return PendingEffectsResult{}, effectUnavailable("list pending work effects", err)
	}
	effects, err := decodeEffects(records)
	if err != nil {
		return PendingEffectsResult{}, effectUnavailable("list pending work effects", err)
	}
	return PendingEffectsResult{Effects: effects}, nil
}

// CompleteEffect acknowledges one successfully applied effect. Repeating an
// acknowledgement is safe, which covers a transport losing the first reply.
func (service Service) CompleteEffect(ctx context.Context, call application.Call, input CompleteEffectInput) error {
	if err := validateEffectCommand(ctx, call, input.EffectID); err != nil {
		return err
	}
	if err := store.CompleteWorkEffect(input.EffectID); err != nil {
		if errors.Is(err, store.ErrWorkEffectNotFound) {
			return effectNotFound(input.EffectID, err)
		}
		return effectUnavailable("complete work effect", err)
	}
	return nil
}

// FailEffect records a delivery failure while deliberately leaving the effect
// pending. Error text is diagnostic only and is bounded before persistence.
func (service Service) FailEffect(ctx context.Context, call application.Call, input FailEffectInput) error {
	if err := validateEffectCommand(ctx, call, input.EffectID); err != nil {
		return err
	}
	message := strings.TrimSpace(input.Error)
	if message == "" {
		message = "effect application failed"
	}
	if len(message) > 4000 {
		message = message[:4000]
	}
	if err := store.FailWorkEffect(input.EffectID, message); err != nil {
		if errors.Is(err, store.ErrWorkEffectNotFound) {
			return effectNotFound(input.EffectID, err)
		}
		return effectUnavailable("record work effect failure", err)
	}
	return nil
}

// DeliverEffects applies pending rows in order and acknowledges each one only
// after its executor succeeds. On failure it records attempt metadata and leaves
// the row pending. This is at-least-once: an executor may have succeeded just
// before the process dies or an acknowledgement fails.
func (service Service) DeliverEffects(
	ctx context.Context,
	call application.Call,
	effects []Effect,
	executor EffectExecutor,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return effectUnavailable("deliver work effects", err)
	}
	if err := call.Validate(); err != nil {
		return err
	}
	if executor == nil {
		return effectInvalidArgument("work effect executor is required", "executor", nil)
	}
	for _, effect := range effects {
		applyErr := executor.ApplyWorkEffect(effect)
		if applyErr != nil {
			failErr := service.FailEffect(ctx, call, FailEffectInput{
				EffectID: effect.ID,
				Error:    applyErr.Error(),
			})
			if failErr != nil {
				return fmt.Errorf("apply work effect %s: %v; record failure: %w", effect.ID, applyErr, failErr)
			}
			return applyErr
		}
		if err := service.CompleteEffect(ctx, call, CompleteEffectInput{EffectID: effect.ID}); err != nil {
			return fmt.Errorf("acknowledge work effect %s: %w", effect.ID, err)
		}
	}
	return nil
}

func validateEffectCommand(ctx context.Context, call application.Call, effectID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return effectUnavailable("update work effect", err)
	}
	if err := call.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(effectID) == "" {
		return effectInvalidArgument("effect ID is required", "effect_id", effectID)
	}
	return nil
}

func effectInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func effectNotFound(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
	err.Details = map[string]any{"effect_id": id}
	return err
}

func effectUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}
