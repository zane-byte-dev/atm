package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestMaintainValidatesPolicyAndCoalescesDurableProjection(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{
			ID: "t1", Title: "Bounded upkeep", Priority: "P1", Status: store.TodoStatusOpen,
			Tags: []string{"personal"}, Created: store.Today(),
		},
		store.Todo{ID: "t2", Title: "Closed upkeep", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
	)
	call := lifecycleCall(application.ActorAgent, "maintain-1")

	if _, err := Default.Maintain(context.Background(), call, MaintainInput{TodoID: "t1", Limit: 0}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, err := Default.Maintain(context.Background(), call, MaintainInput{TodoID: "t2", Limit: 2}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("closed todo error = %v", err)
	}

	first, err := Default.Maintain(context.Background(), call, MaintainInput{TodoID: "#T01", Limit: 2})
	if err != nil {
		t.Fatalf("Maintain: %v", err)
	}
	if first.AlreadyMaintained || first.Todo.MaintenanceLimit != 2 ||
		!reflect.DeepEqual(first.Todo.Tags, []string{store.TodoTagMaintenance, "personal"}) ||
		len(first.Effects) != 1 || first.Effects[0].Kind != EffectTodoUpdated ||
		first.Effects[0].Todo.MaintenanceLimit != 2 {
		t.Fatalf("first = %+v", first)
	}

	retry, err := Default.Maintain(context.Background(), lifecycleCall(application.ActorAgent, "maintain-2"), MaintainInput{
		TodoID: "t1", Limit: 2,
	})
	if err != nil {
		t.Fatalf("retry Maintain: %v", err)
	}
	if !retry.AlreadyMaintained || len(retry.Effects) != 1 || retry.Effects[0].ID != first.Effects[0].ID {
		t.Fatalf("retry = %+v", retry)
	}
	if err := Default.DeliverEffects(context.Background(), call, retry.Effects,
		effectExecutorFunc(func(Effect) error { return nil })); err != nil {
		t.Fatalf("deliver maintenance projection: %v", err)
	}
	afterDelivery, err := Default.Maintain(context.Background(), lifecycleCall(application.ActorAgent, "maintain-3"), MaintainInput{
		TodoID: "t1", Limit: 2,
	})
	if err != nil || !afterDelivery.AlreadyMaintained || len(afterDelivery.Effects) != 0 {
		t.Fatalf("after delivery = %+v, err=%v", afterDelivery, err)
	}
}

func TestMaintainRollsBackWhenProjectionCannotBeEnqueued(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Atomic upkeep", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_maintenance_effect
		BEFORE INSERT ON work_effect_outbox
		WHEN NEW.kind = 'todo_updated'
		BEGIN SELECT RAISE(ABORT, 'injected maintenance outbox failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Default.Maintain(context.Background(), lifecycleCall(application.ActorHuman, "maintain-rollback"), MaintainInput{
		TodoID: "t1", Limit: 4,
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Maintain error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	todo := store.FindTodo(todos, "t1")
	if todo == nil || todo.MaintenanceLimit != 0 || store.TodoHasTag(*todo, store.TodoTagMaintenance) {
		t.Fatalf("rolled-back todo = %+v", todo)
	}
	if pending, pendingErr := store.ListPendingWorkEffects("t1"); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("pending effects after rollback = %+v, err=%v", pending, pendingErr)
	}
}
