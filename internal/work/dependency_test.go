package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func dependencyTestCall() application.Call {
	return application.Call{
		RequestID: "dependency-request",
		Actor:     application.Actor{Kind: application.ActorAgent, Origin: application.OriginCLI, Agent: "codex"},
	}
}

func TestAddDependencyOwnsWaitingBindingAndIdempotency(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "First", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Second", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t3", Title: "Dependent", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "dependency-session", TodoID: "t3"}); err != nil {
		t.Fatal(err)
	}

	for _, dependencyID := range []string{"t1", "t2", "t2"} {
		result, err := Default.AddDependency(context.Background(), dependencyTestCall(), AddDependencyInput{
			TodoID: "#T03", DependencyID: dependencyID,
		})
		if err != nil {
			t.Fatalf("AddDependency %s: %v", dependencyID, err)
		}
		if result.Todo.Status != store.TodoStatusWaiting {
			t.Fatalf("result todo = %+v", result.Todo)
		}
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, "t3")
	if todo == nil || !reflect.DeepEqual(todo.DependsOn, []string{"t1", "t2"}) ||
		todo.WakeCondition != "waiting for todos: t1, t2" {
		t.Fatalf("dependent = %+v", todo)
	}
	if binding, err := store.CurrentTodoBinding("dependency-session"); err != nil || binding != nil {
		t.Fatalf("binding = %+v, err=%v", binding, err)
	}
	pending, err := Default.PendingEffects(context.Background(), dependencyTestCall(), PendingEffectsInput{TodoID: "t3"})
	if err != nil || len(pending.Effects) != 1 || pending.Effects[0].Kind != EffectTodoWaiting {
		t.Fatalf("pending = %+v, err=%v", pending.Effects, err)
	}
}

func TestRemoveLastDependencyWakesWithDurableEffect(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent", Priority: "P1", Status: store.TodoStatusWaiting,
			WakeCondition: "waiting for todos: t1", DependsOn: []string{"t1"}, Created: store.Today()},
	)
	result, err := Default.RemoveDependency(context.Background(), dependencyTestCall(), RemoveDependencyInput{
		TodoID: "t2", DependencyID: "t1",
	})
	if err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}
	if !result.Removed || result.Todo.Status != store.TodoStatusOpen || result.Todo.WakeCondition != "" ||
		len(result.Awakened) != 1 || result.Awakened[0].Reason != "all structured dependencies removed" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != EffectTodoDependencyAwakened ||
		result.Effects[0].Message != "[wake] all structured dependencies removed" {
		t.Fatalf("effects = %+v", result.Effects)
	}
}

func TestRemoveDependencyDeliversOlderWaitingProjectionBeforeWake(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
	)
	added, err := Default.AddDependency(context.Background(), dependencyTestCall(), AddDependencyInput{
		TodoID: "t2", DependencyID: "t1",
	})
	if err != nil || len(added.Effects) != 1 || added.Effects[0].Kind != EffectTodoWaiting {
		t.Fatalf("AddDependency effects = %+v, err=%v", added.Effects, err)
	}
	// Simulate the process dying after commit but before delivering/acking the
	// waiting projection, then the user removing the dependency in a new call.
	removed, err := Default.RemoveDependency(context.Background(), dependencyTestCall(), RemoveDependencyInput{
		TodoID: "t2", DependencyID: "t1",
	})
	if err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}
	if len(removed.Effects) != 2 || removed.Effects[0].Kind != EffectTodoWaiting ||
		removed.Effects[1].Kind != EffectTodoDependencyAwakened ||
		removed.Effects[0].Todo.Status != store.TodoStatusWaiting ||
		removed.Effects[1].Todo.Status != store.TodoStatusOpen {
		t.Fatalf("ordered effects = %+v", removed.Effects)
	}
}

func TestDependencyServiceRejectsCycleAtomically(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "One", Priority: "P1", Status: store.TodoStatusOpen, DependsOn: []string{"t2"}, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Two", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
	)
	_, err := Default.AddDependency(context.Background(), dependencyTestCall(), AddDependencyInput{
		TodoID: "t2", DependencyID: "t1",
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("cycle error = %v, want conflict", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t2"); todo == nil || len(todo.DependsOn) != 0 {
		t.Fatalf("cycle edge persisted: %+v", todo)
	}
}

func TestListDependenciesIncludesArchivedCompletion(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Finished", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent", Priority: "P1", Status: store.TodoStatusOpen, DependsOn: []string{"t1"}, Created: store.Today()},
	)
	if err := Default.Mutate(func(transaction *Transaction) error {
		_, err := transaction.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Default.ListDependencies(context.Background(), dependencyTestCall(), ListDependenciesInput{TodoID: "t2"})
	if err != nil {
		t.Fatalf("ListDependencies: %v", err)
	}
	if len(result.Dependencies) != 1 || result.Dependencies[0].Status != store.TodoStatusDone ||
		!result.Dependencies[0].Met {
		t.Fatalf("dependencies = %+v", result.Dependencies)
	}
}
