package apphost

import (
	"context"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/work"
)

type CreateInput struct {
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	Project        string `json:"project,omitempty"`
	Priority       string `json:"priority,omitempty"`
	IdempotencyKey string `json:"-"`
}

type UpdateInput struct {
	TodoID       string  `json:"todo_id"`
	ExpectedETag string  `json:"expected_etag"`
	Title        *string `json:"title,omitempty"`
	Description  *string `json:"description,omitempty"`
	Project      *string `json:"project,omitempty"`
	Priority     *string `json:"priority,omitempty"`
}

type StartInput struct {
	TodoID       string `json:"todo_id"`
	ReopenReason string `json:"reopen_reason,omitempty"`
}
type DoneInput struct {
	TodoID string `json:"todo_id"`
	Reason string `json:"reason,omitempty"`
}
type MutationResult struct {
	Todo     TodoView `json:"todo"`
	ETag     string   `json:"etag"`
	Replayed bool     `json:"replayed,omitempty"`
}

func validateWrite(ctx context.Context, call application.Call) error {
	if err := validate(ctx, call); err != nil {
		return err
	}
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginWeb {
		return application.NewError(application.CodeForbidden, "this operation requires a human Web action")
	}
	return nil
}

func (h *Host) CreateTodo(ctx context.Context, call application.Call, input CreateInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateWrite(ctx, call); err != nil {
		return MutationResult{}, err
	}
	if len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) == "" {
		return MutationResult{}, invalid("Idempotency-Key is required and must not exceed 128 bytes")
	}
	result, err := h.work.Add(ctx, call, work.AddInput{Title: input.Title, Description: input.Description, Project: input.Project, Priority: input.Priority, IdempotencyKey: input.IdempotencyKey})
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Todo: view(result.Todo), ETag: work.TodoETag(result.Todo), Replayed: result.Replayed}, nil
}

func (h *Host) UpdateTodo(ctx context.Context, call application.Call, input UpdateInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateWrite(ctx, call); err != nil {
		return MutationResult{}, err
	}
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	if strings.TrimSpace(input.ExpectedETag) == "" {
		return MutationResult{}, invalid("expected_etag is required")
	}
	result, err := h.work.Edit(ctx, call, work.EditInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, Patch: work.EditPatch{Title: input.Title, Description: input.Description, Project: input.Project, Priority: input.Priority}})
	if err != nil {
		return MutationResult{}, err
	}
	return MutationResult{Todo: view(result.Todo), ETag: work.TodoETag(result.Todo)}, nil
}

func (h *Host) StartTodo(ctx context.Context, call application.Call, input StartInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := h.validateLifecycle(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	// Serialize the transition together with delivery. Otherwise concurrent
	// browser retries can both obtain and deliver the same pending outbox row.
	h.effectsMu.Lock()
	defer h.effectsMu.Unlock()
	result, err := h.work.Start(ctx, call, work.StartInput{TodoID: input.TodoID, ReopenReason: input.ReopenReason})
	if err != nil {
		return MutationResult{}, err
	}
	if err = h.work.DeliverEffects(ctx, call, result.Effects, h.effects); err != nil {
		return MutationResult{}, postCommitError(result.Todo, err)
	}
	return MutationResult{Todo: view(result.Todo), ETag: work.TodoETag(result.Todo)}, nil
}

func (h *Host) DoneTodo(ctx context.Context, call application.Call, input DoneInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := h.validateLifecycle(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	h.effectsMu.Lock()
	defer h.effectsMu.Unlock()
	result, err := h.work.Done(ctx, call, work.CloseInput{TodoID: input.TodoID, Reason: input.Reason})
	if err != nil {
		return MutationResult{}, err
	}
	if err = h.work.DeliverEffects(ctx, call, result.Effects, h.effects); err != nil {
		return MutationResult{}, postCommitError(result.Todo, err)
	}
	return MutationResult{Todo: view(result.Todo), ETag: work.TodoETag(result.Todo)}, nil
}

func (h *Host) ArchiveTodo(ctx context.Context, call application.Call, input TodoInput) (MutationResult, error) {
	return h.retention(ctx, call, input, false)
}
func (h *Host) RestoreTodo(ctx context.Context, call application.Call, input TodoInput) (MutationResult, error) {
	return h.retention(ctx, call, input, true)
}

func (h *Host) retention(ctx context.Context, call application.Call, input TodoInput, restore bool) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateWrite(ctx, call); err != nil {
		return MutationResult{}, err
	}
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	var err error
	if restore {
		_, err = h.work.Restore(ctx, call, work.RetentionInput{TodoIDs: []string{input.TodoID}})
	} else {
		_, err = h.work.Archive(ctx, call, work.RetentionInput{TodoIDs: []string{input.TodoID}})
	}
	if err != nil {
		return MutationResult{}, err
	}
	result, err := h.work.ShowIncludingArchived(ctx, call, work.ShowInput{TodoID: input.TodoID})
	if err != nil {
		return MutationResult{}, err
	}
	todo := view(result.Todo)
	todo.Archived = result.Archived
	return MutationResult{Todo: todo, ETag: work.TodoETag(result.Todo)}, nil
}

func (h *Host) validateLifecycle(ctx context.Context, call application.Call, id string) error {
	if err := validateWrite(ctx, call); err != nil {
		return err
	}
	if err := validateTodo(ctx, call, id); err != nil {
		return err
	}
	if h.effects == nil {
		return application.NewError(application.CodeUnavailable, "work effect delivery is not configured")
	}
	return nil
}

func postCommitError(todo work.Todo, cause error) error {
	err := application.WrapError(application.CodeUnavailable, "todo saved, but a local follow-up failed; reload before retrying", cause)
	err.Details = map[string]any{"committed": true, "todo_id": todo.ID, "etag": work.TodoETag(todo)}
	return err
}
