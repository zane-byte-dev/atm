package work

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type effectExecutorFunc func(Effect) error

func (apply effectExecutorFunc) ApplyWorkEffect(effect Effect) error {
	return apply(effect)
}

func TestDeliverEffectsLeavesFailurePendingAndAcknowledgesSuccess(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Deliver durable effect", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	call := submitCall(application.ActorAgent, application.OriginCLI)
	result, err := Default.Submit(context.Background(), call, SubmitInput{TodoID: "t1"})
	if err != nil || len(result.Effects) != 1 {
		t.Fatalf("Submit = %+v, err=%v", result, err)
	}
	injected := errors.New("projection unavailable")
	if err := Default.DeliverEffects(context.Background(), call, result.Effects,
		effectExecutorFunc(func(Effect) error { return injected })); !errors.Is(err, injected) {
		t.Fatalf("DeliverEffects failure = %v", err)
	}
	pending, err := Default.PendingEffects(context.Background(), call, PendingEffectsInput{TodoID: "t1"})
	if err != nil || len(pending.Effects) != 1 || pending.Effects[0].AttemptCount != 1 ||
		pending.Effects[0].LastError != injected.Error() {
		t.Fatalf("pending after failure = %+v, err=%v", pending, err)
	}
	if err := Default.DeliverEffects(context.Background(), call, pending.Effects,
		effectExecutorFunc(func(Effect) error { return nil })); err != nil {
		t.Fatalf("DeliverEffects success: %v", err)
	}
	pending, err = Default.PendingEffects(context.Background(), call, PendingEffectsInput{TodoID: "t1"})
	if err != nil || len(pending.Effects) != 0 {
		t.Fatalf("pending after success = %+v, err=%v", pending, err)
	}
}
