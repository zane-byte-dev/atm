package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestBulkDoneRequiresHumanAndExplicitConfirmation(t *testing.T) {
	for _, test := range []struct {
		name      string
		call      application.Call
		confirmed bool
	}{
		{name: "agent", call: lifecycleCall(application.ActorAgent, "bulk-agent"), confirmed: true},
		{name: "unconfirmed human", call: lifecycleCall(application.ActorHuman, "bulk-unconfirmed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Must remain review", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
			})
			_, err := Default.Bulk(context.Background(), test.call, BulkInput{
				Action: BulkDone, TodoIDs: []string{"t1"}, Confirmed: test.confirmed,
			})
			if !errors.Is(err, application.ErrForbidden) {
				t.Fatalf("Bulk error = %v, want forbidden", err)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusReview {
				t.Fatalf("forbidden Bulk mutated todo: %+v, err=%v", todos, loadErr)
			}
			if effects, effectErr := store.ListPendingWorkEffects(""); effectErr != nil || len(effects) != 0 {
				t.Fatalf("forbidden Bulk wrote outbox: %+v, err=%v", effects, effectErr)
			}
		})
	}
}

func TestBulkDoneAtomicallyClosesDeduplicatesAndWakes(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "First accepted", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Second accepted", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		store.Todo{ID: "t3", Title: "Ready after batch", Priority: "P1", Status: store.TodoStatusInProgress,
			WakeCondition: "waiting for todos: t1, t2", DependsOn: []string{"t1", "t2"}, Created: store.Today()},
	)
	for _, binding := range []store.TodoSessionBinding{
		{SessionID: "bulk-one", TodoID: "t1"}, {SessionID: "bulk-two", TodoID: "t2"},
	} {
		if _, err := store.BindTodoSession(binding); err != nil {
			t.Fatal(err)
		}
	}

	call := lifecycleCall(application.ActorHuman, "bulk-done")
	result, err := Default.Bulk(context.Background(), call, BulkInput{
		Action: BulkDone, TodoIDs: []string{"#T01", "t2", "01"}, Reason: "accepted together", Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Bulk: %v", err)
	}
	if result.Action != BulkDone || len(result.Todos) != 2 || result.Todos[0].ID != "t1" || result.Todos[1].ID != "t2" {
		t.Fatalf("result todos = %+v", result.Todos)
	}
	if len(result.Awakened) != 1 || result.Awakened[0].TodoID != "t3" {
		t.Fatalf("awakened = %+v", result.Awakened)
	}
	closed, awakened := 0, 0
	firstEffectIDs := make([]string, 0, len(result.Effects))
	for _, effect := range result.Effects {
		firstEffectIDs = append(firstEffectIDs, effect.ID)
		switch effect.Kind {
		case EffectTodoClosed:
			closed++
			if effect.Message != "[done] accepted together" {
				t.Fatalf("close message = %q", effect.Message)
			}
		case EffectTodoDependencyAwakened:
			awakened++
		}
	}
	if closed != 2 || awakened != 1 {
		t.Fatalf("effects = %+v", result.Effects)
	}
	for _, sessionID := range []string{"bulk-one", "bulk-two"} {
		if binding, err := store.CurrentTodoBinding(sessionID); err != nil || binding != nil {
			t.Fatalf("binding %s = %+v, err=%v", sessionID, binding, err)
		}
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t1", "t2"} {
		todo := store.FindTodo(todos, id)
		if todo.Status != store.TodoStatusDone || todo.ClosedReason == nil || *todo.ClosedReason != "accepted together" || todo.DoneTS == nil {
			t.Fatalf("closed %s = %+v", id, todo)
		}
	}
	if todo := store.FindTodo(todos, "t3"); todo.Status != store.TodoStatusInProgress || todo.WakeCondition != "" {
		t.Fatalf("dependent = %+v", todo)
	}

	retry, err := Default.Bulk(context.Background(), lifecycleCall(application.ActorHuman, "bulk-retry"), BulkInput{
		Action: BulkDone, TodoIDs: []string{"t1", "t2"}, Reason: "accepted together", Confirmed: true,
	})
	if err != nil {
		t.Fatalf("retry Bulk: %v", err)
	}
	if len(retry.Awakened) != 0 {
		t.Fatalf("retry awakened = %+v", retry.Awakened)
	}
	retryEffectIDs := make([]string, 0, len(retry.Effects))
	for _, effect := range retry.Effects {
		retryEffectIDs = append(retryEffectIDs, effect.ID)
	}
	if !reflect.DeepEqual(retryEffectIDs, firstEffectIDs) {
		t.Fatalf("retry effects = %v, want stable %v", retryEffectIDs, firstEffectIDs)
	}
}

func TestBulkRollsBackTodosBindingsWakeAndOutboxTogether(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "First rollback", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Second rollback", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today()},
		store.Todo{ID: "t3", Title: "Must remain waiting", Priority: "P1", Status: store.TodoStatusInProgress,
			WakeCondition: "waiting for todos: t1, t2", DependsOn: []string{"t1", "t2"}, Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "bulk-rollback", TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_bulk_unbind
		BEFORE UPDATE OF unbound_at ON todo_session_bindings
		WHEN NEW.reason = 'bulk:done'
		BEGIN SELECT RAISE(ABORT, 'injected bulk unbind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Default.Bulk(context.Background(), lifecycleCall(application.ActorHuman, "bulk-rollback"), BulkInput{
		Action: BulkDone, TodoIDs: []string{"t1", "t2"}, Reason: "verified before injected rollback", Confirmed: true,
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Bulk error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusReview ||
		store.FindTodo(todos, "t2").Status != store.TodoStatusReview || store.FindTodo(todos, "t3").Status != store.TodoStatusInProgress {
		t.Fatalf("rolled back todos = %+v, err=%v", todos, loadErr)
	}
	if binding, bindErr := store.CurrentTodoBinding("bulk-rollback"); bindErr != nil || binding == nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
	if effects, effectErr := store.ListPendingWorkEffects(""); effectErr != nil || len(effects) != 0 {
		t.Fatalf("outbox after rollback = %+v, err=%v", effects, effectErr)
	}
}

func TestBulkOwnsReasonAndRejectsRemovedActions(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Validate first", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Validate second", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
	)
	call := lifecycleCall(application.ActorHuman, "bulk-validation")

	if _, err := Default.Bulk(context.Background(), call, BulkInput{
		Action: BulkDone, TodoIDs: []string{"t1"}, Reason: "depends on t404", Confirmed: true,
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unknown reason reference error = %v", err)
	}
	if _, err := Default.Bulk(context.Background(), call, BulkInput{
		Action: BulkDone, TodoIDs: []string{"t2"}, Confirmed: true,
	}); err != nil {
		t.Fatalf("idempotent done: %v", err)
	}
	// `drop` is no longer a bulk action: it archived, which `todo archive` already
	// does for a list of IDs.
	if _, err := Default.Bulk(context.Background(), call, BulkInput{
		Action: BulkAction("drop"), TodoIDs: []string{"t2"}, Confirmed: true,
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("bulk drop error = %v, want invalid argument", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusOpen ||
		store.FindTodo(todos, "t2").Status != store.TodoStatusDone {
		t.Fatalf("invalid bulk changed todos = %+v, err=%v", todos, err)
	}
}

func TestBulkMoveReturnsDurableUpdateEffectsWithoutChangingLifecycle(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Move one", Priority: "P1", Status: store.TodoStatusInProgress, Project: "old", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Move two", Priority: "P1", Status: store.TodoStatusInProgress, Project: "old", Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "bulk-edit", TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	call := lifecycleCall(application.ActorAgent, "bulk-move")
	moved, err := Default.Bulk(context.Background(), call, BulkInput{
		Action: BulkMove, TodoIDs: []string{"t1", "t2"}, Project: "  atm  ", Confirmed: true,
	})
	if err != nil || len(moved.Effects) != 2 {
		t.Fatalf("move = %+v, err=%v", moved, err)
	}
	for _, effect := range moved.Effects {
		if effect.Kind != EffectTodoUpdated || effect.Todo.Project != "atm" {
			t.Fatalf("move effect = %+v", effect)
		}
	}
	if binding, err := store.CurrentTodoBinding("bulk-edit"); err != nil || binding == nil || binding.TodoID != "t2" {
		t.Fatalf("binding after bulk move = %+v, err=%v", binding, err)
	}
	for _, todo := range moved.Todos {
		if todo.Status != store.TodoStatusInProgress {
			t.Fatalf("bulk move changed lifecycle: %+v", moved.Todos)
		}
	}
}

func TestBulkReturnsOlderSelectedTodoProjectionsBeforeNewMutation(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Close after projection failure", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
	})
	if err := Default.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo("t1")
		if err != nil {
			return err
		}
		stale := cloneTodo(*todo)
		stale.Status = store.TodoStatusInProgress
		stale.WakeCondition = "external release"
		return transaction.enqueueEffect(lifecycleCall(application.ActorAgent, "older-projection"), EffectTodoWaiting, stale, "")
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Bulk(context.Background(), lifecycleCall(application.ActorHuman, "new-close"), BulkInput{
		Action: BulkDone, TodoIDs: []string{"t1"}, Reason: "verified pending projection ordering", Confirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Effects) != 2 || result.Effects[0].Kind != EffectTodoWaiting || result.Effects[1].Kind != EffectTodoClosed {
		t.Fatalf("ordered effects = %+v", result.Effects)
	}
}
