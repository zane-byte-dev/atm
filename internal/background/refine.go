package background

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

// TodoRefineOptions supplies the model and document projection ports. The
// background runtime never chooses an executor capable of running shell hooks.
// Its composition root must inject the same controlled adapter as Web changes.
type TodoRefineOptions struct {
	Service work.Service
	Effects work.EffectExecutor
}

type TodoRefineChild struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type TodoRefineResult struct {
	TodoID   string            `json:"todo_id"`
	ETag     string            `json:"etag"`
	Changed  bool              `json:"changed"`
	Children []TodoRefineChild `json:"children"`
	Summary  string            `json:"summary"`
	// Committed distinguishes a completed database mutation whose durable
	// document projection still needs retry from a model or conflict failure.
	Committed bool `json:"committed"`
}

func executeTodoRefine(ctx context.Context, call application.Call, input Request, progress func(string), options TodoRefineOptions) (any, error) {
	if ctx == nil {
		return nil, invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := call.Validate(); err != nil {
		return nil, err
	}
	if call.Actor.Kind != application.ActorHuman && call.Actor.Kind != application.ActorController {
		return nil, application.NewError(application.CodeForbidden, "task refinement requires a human or runtime controller")
	}
	if !store.LooksLikeTodoID(input.TodoID) {
		return nil, invalid("a valid todo_id is required")
	}
	input.TodoID = store.NormalizeTodoID(input.TodoID)
	input.ExpectedETag = strings.TrimSpace(input.ExpectedETag)
	if input.ExpectedETag == "" || len(input.ExpectedETag) > 200 {
		return nil, invalid("expected_etag is required")
	}
	input.Hint = strings.TrimSpace(input.Hint)
	if utf8.RuneCountInString(input.Hint) > 500 {
		return nil, invalid("refinement hint must not exceed 500 characters")
	}
	if options.Effects == nil {
		return nil, application.NewError(application.CodeUnavailable, "task refinement document executor is unavailable")
	}
	if progress == nil {
		progress = func(string) {}
	}
	progress("核对任务版本并准备优化")
	service := options.Service
	model := service.RefinementModel
	if model == nil {
		model = work.RefinementModelFunc(func(ctx context.Context, input work.RefinementModelInput) (work.RefinementModelOutput, error) {
			proposal, source, err := refine.Propose(ctx, input.Todo, input.Card, input.Options)
			return work.RefinementModelOutput{Proposal: proposal, Source: source}, err
		})
	}
	service.RefinementModel = work.RefinementModelFunc(func(ctx context.Context, snapshot work.RefinementModelInput) (work.RefinementModelOutput, error) {
		// Work owns this exact snapshot through its compare-at-commit check. A
		// separate adapter read followed by Refine would leave a race in between.
		if actual := work.TodoETag(snapshot.Todo); actual != input.ExpectedETag {
			err := application.NewError(application.CodeConflict, "task changed before refinement; reload and retry")
			err.Details = map[string]any{"todo_id": snapshot.Todo.ID, "current_etag": actual}
			return work.RefinementModelOutput{}, err
		}
		if err := ctx.Err(); err != nil {
			return work.RefinementModelOutput{}, err
		}
		progress("优化任务描述与可执行步骤")
		output, err := model.Propose(ctx, snapshot)
		if err != nil {
			return output, err
		}
		// A model adapter that ignores cancellation must not commit a response
		// after the user canceled its background job.
		if err := ctx.Err(); err != nil {
			return work.RefinementModelOutput{}, err
		}
		progress("核对最新任务并保存优化")
		return output, nil
	})
	result, err := service.Refine(ctx, call, work.RefineInput{
		TodoID: input.TodoID, AllowSplit: true, MaxChildren: refine.DefaultMaxChildren,
		Hint: input.Hint, Timeout: refine.DefaultTimeout,
	})
	if err != nil {
		return nil, err
	}
	response := TodoRefineResult{
		TodoID: result.Todo.ID, ETag: work.TodoETag(result.Todo), Changed: result.Changed,
		Children: []TodoRefineChild{}, Committed: result.Changed,
		Summary: "当前任务无需调整",
	}
	for _, child := range result.Children {
		response.Children = append(response.Children, TodoRefineChild{ID: child.ID, Title: child.Title})
	}
	if result.Changed {
		response.Summary = "已优化任务描述"
		if len(response.Children) > 0 {
			response.Summary = fmt.Sprintf("已优化任务，新增 %d 个子任务", len(response.Children))
		}
	}
	if len(result.Effects) > 0 {
		progress("更新任务文档")
		if err := service.DeliverEffects(ctx, call, result.Effects, refinementEffectsOnly{next: options.Effects}); err != nil {
			response.Committed = true
			response.Summary = "任务已保存，文档更新待重试"
			failure := application.WrapError(application.CodeUnavailable, "task refinement committed but document projection is pending", err)
			failure.Details = map[string]any{"committed": true, "todo_id": response.TodoID, "etag": response.ETag}
			return response, failure
		}
	}
	return response, nil
}

// The model's result cannot authorize unrelated pending effects such as hooks
// or lifecycle notifications, even if an adapter returns a broader outbox list.
type refinementEffectsOnly struct{ next work.EffectExecutor }

func (executor refinementEffectsOnly) ApplyWorkEffect(effect work.Effect) error {
	if effect.Kind != work.EffectTodoRefined {
		return application.NewError(application.CodeForbidden, "unexpected task refinement effect")
	}
	return executor.next.ApplyWorkEffect(effect)
}
