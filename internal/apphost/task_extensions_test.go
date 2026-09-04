package apphost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

type taskProjection struct{}

func (taskProjection) ApplyWorkEffect(effect work.Effect) error {
	if effect.Kind == work.EffectTodoProgress {
		return work.ApplyProgressEffect(effect)
	}
	return nil
}

func TestTaskExtensionsPreserveIdentityAndRejectStaleEdits(t *testing.T) {
	h := testHost(t)
	h.SetWorkEffects(taskProjection{})
	ctx, call := context.Background(), webCall()
	seed(t, card("t1", "Work", "in_progress", "atm"), card("t2", "Other", "open", "atm"))
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := h.UpdateTodoWait(ctx, call, WaitInput{TodoID: "t1", ExpectedETag: shown.ETag, WakeCondition: "release becomes available", ReviewAt: "2026-09-10"})
	if err != nil || waiting.Todo.WakeCondition == "" || waiting.ETag == shown.ETag {
		t.Fatalf("wait result=%+v err=%v", waiting, err)
	}
	stale := []func() error{
		func() error {
			_, err := h.SetTodoPlan(ctx, call, PlanInput{TodoID: "t1", ExpectedETag: shown.ETag})
			return err
		},
		func() error {
			_, err := h.AppendTodoProgress(ctx, call, ProgressInput{TodoID: "t1", ExpectedETag: shown.ETag, Message: "Checked release"})
			return err
		},
		func() error {
			_, err := h.AddTodoDependency(ctx, call, DependencyInput{TodoID: "t1", ExpectedETag: shown.ETag, DependencyID: "t2"})
			return err
		},
		func() error {
			_, err := h.RemoveTodoDependency(ctx, call, DependencyInput{TodoID: "t1", ExpectedETag: shown.ETag, DependencyID: "t2"})
			return err
		},
		func() error {
			_, err := h.WakeTodo(ctx, call, WakeInput{TodoID: "t1", ExpectedETag: shown.ETag, Reason: "release arrived"})
			return err
		},
	}
	for index, run := range stale {
		if err := run(); !errors.Is(err, application.ErrConflict) {
			t.Fatalf("stale operation %d: %v", index, err)
		}
	}
	agent := call
	agent.Actor.Kind = application.ActorAgent
	if _, err := h.SetTodoPlan(ctx, agent, PlanInput{TodoID: "t1", ExpectedETag: waiting.ETag}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent masquerade: %v", err)
	}
	if _, err := h.WakeTodo(ctx, call, WakeInput{TodoID: "t1", ExpectedETag: waiting.ETag}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("wake without reason: %v", err)
	}
	awake, err := h.WakeTodo(ctx, call, WakeInput{TodoID: "t1", ExpectedETag: waiting.ETag, Reason: "release arrived"})
	if err != nil || awake.Todo.WakeCondition != "" || awake.Todo.ReviewAt != "" || awake.Todo.Status != "in_progress" {
		t.Fatalf("wake result=%+v err=%v", awake, err)
	}
}

func TestTaskExtensionsConcurrentPlansAndDependenciesHaveOneWinner(t *testing.T) {
	h := testHost(t)
	h.SetWorkEffects(taskProjection{})
	other := New("other-host")
	other.SetWorkEffects(taskProjection{})
	ctx, call := context.Background(), webCall()
	seed(t, card("t1", "Work", "open", "atm"), card("t2", "A", "open", "atm"), card("t3", "B", "open", "atm"))
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"plan", "dependency"} {
		outcomes := make(chan error, 2)
		var workers sync.WaitGroup
		for index, host := range []*Host{h, other} {
			workers.Add(1)
			go func(index int, host *Host) {
				defer workers.Done()
				var err error
				if mode == "plan" {
					_, err = host.SetTodoPlan(ctx, call, PlanInput{TodoID: "t1", ExpectedETag: shown.ETag, Items: []work.PlanItem{{Step: []string{"First", "Second"}[index], Status: work.PlanPending}}})
				} else {
					_, err = host.AddTodoDependency(ctx, call, DependencyInput{TodoID: "t1", ExpectedETag: shown.ETag, DependencyID: []string{"t2", "t3"}[index]})
				}
				outcomes <- err
			}(index, host)
		}
		workers.Wait()
		close(outcomes)
		won, conflicted := 0, 0
		for err := range outcomes {
			if err == nil {
				won++
			} else if errors.Is(err, application.ErrConflict) {
				conflicted++
			} else {
				t.Fatalf("%s failed: %v (cause: %+v)", mode, err, errors.Unwrap(err))
			}
		}
		if won != 1 || conflicted != 1 {
			t.Fatalf("%s: won=%d conflicted=%d", mode, won, conflicted)
		}
	}
}

func TestProgressEffectReplayPreservesDistinctActionsAndUserText(t *testing.T) {
	h := testHost(t)
	h.SetWorkEffects(taskProjection{})
	ctx, call := context.Background(), webCall()
	seed(t, card("t1", "Work", "in_progress", "atm"))
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	message := strings.Repeat("界", store.TodoProgressMaxRunes)
	for range 2 {
		if _, err := h.AppendTodoProgress(ctx, call, ProgressInput{TodoID: "t1", ExpectedETag: shown.ETag, Message: message}); err != nil {
			t.Fatal(err)
		}
	}
	effects, err := store.ListWorkEffects("t1")
	if err != nil || len(effects) != 2 {
		t.Fatalf("effects=%+v err=%v", effects, err)
	}
	content, err := store.ReadTodoDoc("t1")
	if err != nil || strings.Count(content, message) != 2 {
		t.Fatalf("progress count=%d err=%v", strings.Count(content, message), err)
	}
	progress := content[strings.Index(content, "\n## 进展\n"):]
	if strings.Contains(progress, "atm-progress:") {
		t.Fatal("delivery marker polluted progress text")
	}
	if _, err := h.AppendTodoProgress(ctx, call, ProgressInput{TodoID: "t1", ExpectedETag: shown.ETag, Message: message + "界"}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("oversized progress: %v", err)
	}
	// Replay one persisted event through the same projection entry point.
	var first work.Effect
	first.ID, first.Kind, first.TodoID, first.Todo = effects[0].ID, work.EffectTodoProgress, "t1", card("t1", "Work", "in_progress", "atm")
	first.Message, first.CreatedAt = message, effects[0].CreatedAt
	if err := work.ApplyProgressEffect(first); err != nil {
		t.Fatal(err)
	}
	content, _ = store.ReadTodoDoc("t1")
	if strings.Count(content, message) != 2 {
		t.Fatal("replay duplicated an existing progress action")
	}
}
