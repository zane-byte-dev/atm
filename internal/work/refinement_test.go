package work

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/refine"
	"github.com/zane-byte-dev/atm/internal/store"
)

func refinementModel(proposal refine.Proposal) RefinementModel {
	return RefinementModelFunc(func(_ context.Context, _ RefinementModelInput) (RefinementModelOutput, error) {
		return RefinementModelOutput{Proposal: proposal, Source: "test model"}, nil
	})
}

func complexRefinementProposal() refine.Proposal {
	return refine.Proposal{
		Title:       "实现收集闭环",
		Description: "目标：从聊天到 Todo 可回放。",
		Complexity:  refine.ComplexityComplex,
		Reason:      "两块可独立验收的工作",
		Plan:        "先完成契约，再实现落地路径。",
		Children: []refine.Child{
			{Title: "编写分类器契约", Description: "定义 schema 与 prompt。"},
			{Title: "实现收集落地路径", Description: "实现 create 与 append。", DependsOnIndexes: []int{0}},
		},
	}
}

func TestRefineDoesNotSilentlyDiscardUnreadableTodoCard(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "需要读取卡片", Priority: "P1", Status: store.TodoStatusOpen,
		Created: store.Today(),
	})
	if err := os.MkdirAll(store.TodoDocPath("t1"), 0o755); err != nil {
		t.Fatal(err)
	}
	called := false
	service := Service{RefinementModel: RefinementModelFunc(func(
		context.Context, RefinementModelInput,
	) (RefinementModelOutput, error) {
		called = true
		return RefinementModelOutput{}, nil
	})}
	_, err := service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-card-read"), RefineInput{
		TodoID: "t1",
	})
	if !errors.Is(err, application.ErrUnavailable) || called {
		t.Fatalf("Refine error = %v, model called = %v", err, called)
	}
}

func TestRefineSplitsAndEnqueuesOneAtomicProjection(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "做完整的收集闭环", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(), Creator: store.TodoCreatorMe,
	})
	service := Service{RefinementModel: refinementModel(complexRefinementProposal())}
	result, err := service.Refine(context.Background(), lifecycleCall(application.ActorAgent, "refine-split"), RefineInput{
		TodoID: "#T01", AllowSplit: true, MaxChildren: 5,
	})
	if err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if !result.Changed || !result.Prepared.Split || result.Todo.Status != store.TodoStatusOpen ||
		len(result.Children) != 2 || len(result.Todo.DependsOn) != 2 || len(result.Effects) != 1 {
		t.Fatalf("result = %+v", result)
	}
	effect := result.Effects[0]
	if effect.Kind != EffectTodoRefined || len(effect.RelatedTodos) != 2 ||
		!strings.Contains(effect.Message, result.Children[0].ID) || !strings.Contains(effect.Message, "from test model") {
		t.Fatalf("effect = %+v", effect)
	}
	if result.Children[0].Source != refine.ChildSource("t1") || result.Children[0].Creator != "codex" ||
		len(result.Children[1].DependsOn) != 1 || result.Children[1].DependsOn[0] != result.Children[0].ID {
		t.Fatalf("children = %+v", result.Children)
	}
	if store.TodoDocExists("t1") || store.TodoDocExists(result.Children[0].ID) {
		t.Fatal("Refine touched filesystem before its durable effect was delivered")
	}
}

func TestRefineRollsBackGraphWhenProjectionCannotBeEnqueued(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "做完整的收集闭环", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_refine_effect
		BEFORE INSERT ON work_effect_outbox
		WHEN NEW.kind = 'todo_refined'
		BEGIN SELECT RAISE(ABORT, 'injected refine outbox failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	service := Service{RefinementModel: refinementModel(complexRefinementProposal())}
	_, err = service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-rollback"), RefineInput{
		TodoID: "t1", AllowSplit: true,
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Refine error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	parent := store.FindTodo(todos, "t1")
	if len(todos.Items) != 1 || parent == nil || parent.Title != "做完整的收集闭环" ||
		parent.Status != store.TodoStatusOpen || len(parent.DependsOn) != 0 {
		t.Fatalf("partially applied refinement = %+v", todos.Items)
	}
	if pending, pendingErr := store.ListPendingWorkEffects("t1"); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("pending effects after rollback = %+v, err=%v", pending, pendingErr)
	}
}

func TestRefineRejectsStaleModelSnapshot(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "原始标题需要整理", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})
	service := Service{RefinementModel: RefinementModelFunc(func(
		_ context.Context,
		_ RefinementModelInput,
	) (RefinementModelOutput, error) {
		if err := Default.Mutate(func(transaction *Transaction) error {
			todo, err := transaction.Todo("t1")
			if err != nil {
				return err
			}
			todo.Title = "模型运行期间发生的人工修改"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return RefinementModelOutput{Proposal: refine.Proposal{
			Title: "模型建议的新标题", Description: "目标：完成整理。", Complexity: refine.ComplexitySimple,
		}}, nil
	})}
	_, err := service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-stale"), RefineInput{
		TodoID: "t1", AllowSplit: true,
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("stale Refine error = %v, want conflict", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Title != "模型运行期间发生的人工修改" {
		t.Fatalf("concurrent change was overwritten: %+v", todo)
	}
	if pending, pendingErr := store.ListPendingWorkEffects("t1"); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("stale refinement enqueued effects = %+v, err=%v", pending, pendingErr)
	}
}

func TestRefineDryRunAndTypedFailuresDoNotWrite(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "需要整理的开放任务", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "已经关闭的任务", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
	)
	calls := 0
	service := Service{RefinementModel: RefinementModelFunc(func(
		_ context.Context,
		_ RefinementModelInput,
	) (RefinementModelOutput, error) {
		calls++
		return RefinementModelOutput{Proposal: refine.Proposal{
			Title: "整理后的开放任务", Description: "目标：描述清楚。", Complexity: refine.ComplexitySimple,
		}}, nil
	})}
	dry, err := service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-dry"), RefineInput{
		TodoID: "t1", AllowSplit: true, DryRun: true,
	})
	if err != nil || !dry.DryRun || !dry.Changed || len(dry.Effects) != 0 {
		t.Fatalf("dry run = %+v, err=%v", dry, err)
	}
	if _, err := service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-closed"), RefineInput{
		TodoID: "t2", AllowSplit: true,
	}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("closed Refine error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("model calls = %d, closed todo should fail before the port", calls)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil || store.FindTodo(todos, "t1").Title != "需要整理的开放任务" {
		t.Fatalf("dry run wrote state: %+v, err=%v", todos, loadErr)
	}

	failing := Service{RefinementModel: RefinementModelFunc(func(
		_ context.Context,
		_ RefinementModelInput,
	) (RefinementModelOutput, error) {
		return RefinementModelOutput{}, errors.New("model offline")
	})}
	if _, err := failing.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-model-error"), RefineInput{
		TodoID: "t1", AllowSplit: true,
	}); !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("model failure = %v, want unavailable", err)
	}
}

func TestRefineRejectsFanOutOutsideTheFixedModelContract(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "需要整理的开放任务", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today(),
	})
	modelCalls := 0
	service := Service{RefinementModel: RefinementModelFunc(func(
		_ context.Context,
		_ RefinementModelInput,
	) (RefinementModelOutput, error) {
		modelCalls++
		return RefinementModelOutput{Proposal: complexRefinementProposal()}, nil
	})}
	for _, value := range []int{-1, refine.DefaultMaxChildren + 1} {
		_, err := service.Refine(context.Background(), lifecycleCall(application.ActorHuman, "refine-limit"), RefineInput{
			TodoID: "t1", AllowSplit: true, MaxChildren: value,
		})
		if !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("MaxChildren %d error = %v, want invalid_argument", value, err)
		}
	}
	if modelCalls != 0 {
		t.Fatalf("invalid fan-out reached model %d times", modelCalls)
	}
}
