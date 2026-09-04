package apphost

import (
	"context"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/work"
)

type PlanInput struct {
	TodoID       string          `json:"todo_id"`
	ExpectedETag string          `json:"expected_etag"`
	BaseRevision int64           `json:"base_revision"`
	Explanation  string          `json:"explanation,omitempty"`
	Items        []work.PlanItem `json:"items"`
}

type PlanResult struct {
	MutationResult
	Plan work.PlanSnapshot `json:"plan"`
}

type ProgressInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag"`
	Message      string `json:"message"`
}

type DependencyInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag"`
	DependencyID string `json:"dependency_id"`
}

type WaitInput struct {
	TodoID        string `json:"todo_id"`
	ExpectedETag  string `json:"expected_etag"`
	WakeCondition string `json:"wake_condition"`
	ReviewAt      string `json:"review_at"`
}

type WakeInput struct {
	TodoID       string `json:"todo_id"`
	ExpectedETag string `json:"expected_etag"`
	Reason       string `json:"reason"`
}

func validateExtension(ctx context.Context, call application.Call, todoID, etag string) error {
	if err := validateWrite(ctx, call); err != nil {
		return err
	}
	if err := validateTodo(ctx, call, todoID); err != nil {
		return err
	}
	if strings.TrimSpace(etag) == "" {
		return invalid("expected_etag is required")
	}
	return nil
}

func mutationView(todo work.Todo) MutationResult {
	return MutationResult{Todo: view(todo), ETag: work.TodoETag(todo)}
}

func (h *Host) SetTodoPlan(ctx context.Context, call application.Call, input PlanInput) (PlanResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return PlanResult{}, err
	}
	result, err := h.work.SetPlan(ctx, call, work.SetPlanInput{
		TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, BaseRevision: input.BaseRevision,
		Explanation: input.Explanation, Items: input.Items,
	})
	if err != nil {
		return PlanResult{}, err
	}
	return PlanResult{MutationResult: mutationView(result.Todo), Plan: result.Plan}, nil
}

func (h *Host) AppendTodoProgress(ctx context.Context, call application.Call, input ProgressInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return MutationResult{}, err
	}
	if h.effects == nil {
		return MutationResult{}, application.NewError(application.CodeUnavailable, "work effect delivery is not configured")
	}
	h.effectsMu.Lock()
	defer h.effectsMu.Unlock()
	result, err := h.work.AppendProgress(ctx, call, work.ProgressInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, Message: input.Message})
	if err != nil {
		return MutationResult{}, err
	}
	if err := h.work.DeliverEffects(ctx, call, result.Effects, h.effects); err != nil {
		return MutationResult{}, postCommitError(result.Todo, err)
	}
	return mutationView(result.Todo), nil
}

func (h *Host) TodoDependencies(ctx context.Context, call application.Call, input TodoInput) (work.ListDependenciesResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return work.ListDependenciesResult{}, err
	}
	return h.work.ListDependencies(ctx, call, work.ListDependenciesInput{TodoID: input.TodoID})
}

func (h *Host) AddTodoDependency(ctx context.Context, call application.Call, input DependencyInput) (MutationResult, error) {
	return h.mutateDependency(ctx, call, input, false)
}

func (h *Host) RemoveTodoDependency(ctx context.Context, call application.Call, input DependencyInput) (MutationResult, error) {
	return h.mutateDependency(ctx, call, input, true)
}

func (h *Host) mutateDependency(ctx context.Context, call application.Call, input DependencyInput, remove bool) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return MutationResult{}, err
	}
	if err := h.validateLifecycle(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	if err := validateTodo(ctx, call, input.DependencyID); err != nil {
		return MutationResult{}, err
	}
	h.effectsMu.Lock()
	defer h.effectsMu.Unlock()
	var todo work.Todo
	var effects []work.Effect
	if remove {
		result, err := h.work.RemoveDependency(ctx, call, work.RemoveDependencyInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, DependencyID: input.DependencyID})
		if err != nil {
			return MutationResult{}, err
		}
		todo, effects = result.Todo, result.Effects
	} else {
		result, err := h.work.AddDependency(ctx, call, work.AddDependencyInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, DependencyID: input.DependencyID})
		if err != nil {
			return MutationResult{}, err
		}
		todo, effects = result.Todo, result.Effects
	}
	if err := h.work.DeliverEffects(ctx, call, effects, h.effects); err != nil {
		return MutationResult{}, postCommitError(todo, err)
	}
	return mutationView(todo), nil
}

func (h *Host) UpdateTodoWait(ctx context.Context, call application.Call, input WaitInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return MutationResult{}, err
	}
	result, err := h.work.Edit(ctx, call, work.EditInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag,
		Patch: work.EditPatch{WakeCondition: &input.WakeCondition, ReviewAt: &input.ReviewAt}})
	if err != nil {
		return MutationResult{}, err
	}
	return mutationView(result.Todo), nil
}

func (h *Host) WakeTodo(ctx context.Context, call application.Call, input WakeInput) (MutationResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateExtension(ctx, call, input.TodoID, input.ExpectedETag); err != nil {
		return MutationResult{}, err
	}
	if err := h.validateLifecycle(ctx, call, input.TodoID); err != nil {
		return MutationResult{}, err
	}
	h.effectsMu.Lock()
	defer h.effectsMu.Unlock()
	result, err := h.work.Wake(ctx, call, work.WakeInput{TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, Reason: input.Reason})
	if err != nil {
		return MutationResult{}, err
	}
	if err := h.work.DeliverEffects(ctx, call, result.Effects, h.effects); err != nil {
		return MutationResult{}, postCommitError(result.Todo, err)
	}
	return mutationView(result.Todo), nil
}
