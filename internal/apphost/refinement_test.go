package apphost

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
)

func TestCreateAutomaticallyEnqueuesOneStableRefinement(t *testing.T) {
	h := testHost(t)
	old := config.TodoRefineOnAdd
	config.TodoRefineOnAdd = true
	t.Cleanup(func() { config.TodoRefineOnAdd = old })
	started := make(chan background.Request, 1)
	var calls atomic.Int32
	m, err := background.New(background.Options{DataDir: h.dataDir, WithConfig: h.WithConfig, Execute: func(ctx context.Context, _ application.Call, r background.Request, _ func(string)) (any, error) {
		calls.Add(1)
		started <- r
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.AttachRuntime(m, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	input := CreateInput{Title: "Refine this task", IdempotencyKey: "private-create-key"}
	first, err := h.CreateTodo(context.Background(), webCall(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.RefinementJob == nil || first.RefinementJob.TodoID != first.Todo.ID || len(first.Warnings) != 0 {
		t.Fatalf("create=%+v", first)
	}
	r := <-started
	if r.Kind != background.TodoRefine || r.TodoID != first.Todo.ID || r.ExpectedETag != first.ETag {
		t.Fatalf("refine request=%+v", r)
	}
	retry, err := h.CreateTodo(context.Background(), webCall(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !retry.Replayed || retry.Todo.ID != first.Todo.ID || retry.RefinementJob == nil || retry.RefinementJob.ID != first.RefinementJob.ID || calls.Load() != 1 {
		t.Fatalf("retry=%+v calls=%d", retry, calls.Load())
	}
}

func TestAutoRefinementUnavailableStillReturnsCreatedTodo(t *testing.T) {
	h := testHost(t)
	old := config.TodoRefineOnAdd
	config.TodoRefineOnAdd = true
	t.Cleanup(func() { config.TodoRefineOnAdd = old })
	result, err := h.CreateTodo(context.Background(), webCall(), CreateInput{Title: "Keep the created task", IdempotencyKey: "create-without-runtime"})
	if err != nil || result.Todo.ID == "" || len(result.Warnings) != 1 || result.RefinementJob != nil {
		t.Fatalf("create=%+v err=%v", result, err)
	}
	if _, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: result.Todo.ID}); err != nil {
		t.Fatalf("warning lost created todo: %v", err)
	}
	config.TodoRefineOnAdd = false
	off, err := h.CreateTodo(context.Background(), webCall(), CreateInput{Title: "No automatic model use", IdempotencyKey: "disabled-auto"})
	if err != nil || off.RefinementJob != nil || len(off.Warnings) != 0 {
		t.Fatalf("disabled auto=%+v %v", off, err)
	}
}
