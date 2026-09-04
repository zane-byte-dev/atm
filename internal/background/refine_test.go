package background

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

type refineEffectStub struct {
	calls int
	err   error
}

func (executor *refineEffectStub) ApplyWorkEffect(effect work.Effect) error {
	executor.calls++
	if effect.Kind != work.EffectTodoRefined {
		return errors.New("unexpected work effect")
	}
	return executor.err
}

func seedRefineTodo(t *testing.T) work.Todo {
	t.Helper()
	testData(t)
	result, err := work.Default.Add(context.Background(), humanCall(), work.AddInput{
		IdempotencyKey: "seed-refine", Title: "实现完整的收集工作区", Description: "把来源管理和收集结果做完整。", Project: "atm",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Todo
}

func refineRequest(todo work.Todo) Request {
	return Request{Kind: TodoRefine, TodoID: todo.ID, ExpectedETag: work.TodoETag(todo)}
}

func refinementProposal() refine.Proposal {
	return refine.Proposal{
		Title: "完成收集来源和结果管理", Description: "目标：支持来源管理，并整理可操作的收集结果。",
		Complexity: refine.ComplexityComplex, Plan: "先实现来源管理，再实现结果处理。", Reason: "两个可独立验收的能力。",
		Children: []refine.Child{
			{Title: "实现来源管理页面", Description: "支持添加、编辑和停用来源。"},
			{Title: "实现收集结果处理", Description: "支持查看、修正和归档结果。", DependsOnIndexes: []int{0}},
		},
	}
}

func loadRefineTodo(t *testing.T, id string) work.Todo {
	t.Helper()
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, id)
	if todo == nil {
		t.Fatalf("missing task %s", id)
	}
	return *todo
}

func TestTodoRefineUsesNativeDefaultsAndDeliversProjection(t *testing.T) {
	todo := seedRefineTodo(t)
	input := refineRequest(todo)
	input.Hint = "补上验收标准"
	executor := &refineEffectStub{}
	modelCalls := 0
	options := TodoRefineOptions{Effects: executor, Service: work.Service{RefinementModel: work.RefinementModelFunc(
		func(ctx context.Context, snapshot work.RefinementModelInput) (work.RefinementModelOutput, error) {
			modelCalls++
			if snapshot.Todo.ID != todo.ID || snapshot.Options.Hint != input.Hint || !snapshot.Options.AllowSplit ||
				snapshot.Options.MaxChildren != 5 || snapshot.Options.Timeout != refine.DefaultTimeout {
				t.Fatalf("incorrect model input: %+v", snapshot)
			}
			return work.RefinementModelOutput{Proposal: refinementProposal(), Source: "test model"}, nil
		})}}
	result, err := executeTodoRefine(context.Background(), humanCall(), input, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(TodoRefineResult)
	if !got.Changed || !got.Committed || len(got.Children) != 2 || got.TodoID != todo.ID || got.ETag == input.ExpectedETag || modelCalls != 1 || executor.calls != 1 {
		t.Fatalf("unexpected result=%+v model=%d effects=%d", got, modelCalls, executor.calls)
	}
	current := loadRefineTodo(t, todo.ID)
	if len(current.DependsOn) != 2 || work.TodoETag(current) != got.ETag {
		t.Fatalf("refined graph not committed: %+v", current)
	}
	if pending, err := store.ListPendingWorkEffects(todo.ID); err != nil || len(pending) != 0 {
		t.Fatalf("projection not acknowledged: %+v (%v)", pending, err)
	}
}

func TestTodoRefineRejectsStaleETagBeforeModel(t *testing.T) {
	todo := seedRefineTodo(t)
	input := refineRequest(todo)
	newTitle := "用户已更新的任务标题"
	if _, err := work.Default.Edit(context.Background(), humanCall(), work.EditInput{
		TodoID: todo.ID, ExpectedETag: input.ExpectedETag, Patch: work.EditPatch{Title: &newTitle},
	}); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := executeTodoRefine(context.Background(), humanCall(), input, nil, TodoRefineOptions{
		Effects: &refineEffectStub{}, Service: work.Service{RefinementModel: work.RefinementModelFunc(
			func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
				called = true
				return work.RefinementModelOutput{Proposal: refinementProposal()}, nil
			})},
	})
	if !errors.Is(err, application.ErrConflict) || called {
		t.Fatalf("stale request reached model: called=%t error=%v", called, err)
	}
	if current := loadRefineTodo(t, todo.ID); current.Title != newTitle {
		t.Fatalf("stale request changed task: %+v", current)
	}
}

func TestTodoRefinePreservesEditsMadeDuringModelCall(t *testing.T) {
	todo := seedRefineTodo(t)
	newTitle := "模型运行期间的人工修改"
	executor := &refineEffectStub{}
	_, err := executeTodoRefine(context.Background(), humanCall(), refineRequest(todo), nil, TodoRefineOptions{
		Effects: executor, Service: work.Service{RefinementModel: work.RefinementModelFunc(
			func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
				if _, err := work.Default.Edit(context.Background(), humanCall(), work.EditInput{
					TodoID: todo.ID, ExpectedETag: work.TodoETag(todo), Patch: work.EditPatch{Title: &newTitle},
				}); err != nil {
					t.Fatal(err)
				}
				return work.RefinementModelOutput{Proposal: refinementProposal()}, nil
			})},
	})
	if !errors.Is(err, application.ErrConflict) || executor.calls != 0 {
		t.Fatalf("concurrent edit was not rejected: effects=%d error=%v", executor.calls, err)
	}
	if current := loadRefineTodo(t, todo.ID); current.Title != newTitle || len(current.DependsOn) != 0 {
		t.Fatalf("model overwrote concurrent edit: %+v", current)
	}
}

func TestTodoRefineCanceledModelCannotCommit(t *testing.T) {
	todo := seedRefineTodo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := executeTodoRefine(ctx, humanCall(), refineRequest(todo), nil, TodoRefineOptions{
		Effects: &refineEffectStub{}, Service: work.Service{RefinementModel: work.RefinementModelFunc(
			func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
				cancel()
				return work.RefinementModelOutput{Proposal: refinementProposal()}, nil
			})},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled model response was accepted: %v", err)
	}
	if current := loadRefineTodo(t, todo.ID); work.TodoETag(current) != work.TodoETag(todo) {
		t.Fatalf("canceled refinement committed: %+v", current)
	}
}

func TestTodoRefineProjectionFailureRemainsRetryable(t *testing.T) {
	todo := seedRefineTodo(t)
	executor := &refineEffectStub{err: errors.New("temporary document failure")}
	options := TodoRefineOptions{Effects: executor, Service: work.Service{RefinementModel: work.RefinementModelFunc(
		func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
			return work.RefinementModelOutput{Proposal: refinementProposal()}, nil
		})}}
	result, err := executeTodoRefine(context.Background(), humanCall(), refineRequest(todo), nil, options)
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("projection failure was hidden: %v", err)
	}
	got := result.(TodoRefineResult)
	if !got.Committed || !got.Changed {
		t.Fatalf("committed refinement reported as unmodified: %+v", got)
	}
	if pending, err := store.ListPendingWorkEffects(todo.ID); err != nil || len(pending) != 1 {
		t.Fatalf("failed projection not durable: %+v (%v)", pending, err)
	}
	current := loadRefineTodo(t, todo.ID)
	options.Service.RefinementModel = work.RefinementModelFunc(func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
		return work.RefinementModelOutput{Proposal: refine.Proposal{
			Title: current.Title, Description: current.Description, Complexity: refine.ComplexitySimple,
		}}, nil
	})
	executor.err = nil
	result, err = executeTodoRefine(context.Background(), humanCall(), refineRequest(current), nil, options)
	if err != nil || result.(TodoRefineResult).Changed || executor.calls != 2 {
		t.Fatalf("no-change retry did not deliver pending projection: %+v error=%v effects=%d", result, err, executor.calls)
	}
	if pending, err := store.ListPendingWorkEffects(todo.ID); err != nil || len(pending) != 0 {
		t.Fatalf("retry did not acknowledge projection: %+v (%v)", pending, err)
	}
}

func TestTodoRefineNoChangeAndMissingExecutor(t *testing.T) {
	todo := seedRefineTodo(t)
	modelCalls := 0
	options := TodoRefineOptions{Service: work.Service{RefinementModel: work.RefinementModelFunc(
		func(context.Context, work.RefinementModelInput) (work.RefinementModelOutput, error) {
			modelCalls++
			return work.RefinementModelOutput{Proposal: refine.Proposal{
				Title: todo.Title, Description: todo.Description, Complexity: refine.ComplexitySimple,
			}}, nil
		})}}
	if _, err := executeTodoRefine(context.Background(), humanCall(), refineRequest(todo), nil, options); !errors.Is(err, application.ErrUnavailable) || modelCalls != 0 {
		t.Fatalf("missing projection executor reached model: calls=%d error=%v", modelCalls, err)
	}
	executor := &refineEffectStub{}
	options.Effects = executor
	call := application.Call{RequestID: "automatic-refine", Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController}}
	result, err := executeTodoRefine(context.Background(), call, refineRequest(todo), nil, options)
	if err != nil {
		t.Fatal(err)
	}
	got := result.(TodoRefineResult)
	if got.Changed || got.Committed || len(got.Children) != 0 || got.ETag != work.TodoETag(todo) || executor.calls != 0 || modelCalls != 1 {
		t.Fatalf("no-change refinement reported changes: %+v effects=%d model=%d", got, executor.calls, modelCalls)
	}
}
