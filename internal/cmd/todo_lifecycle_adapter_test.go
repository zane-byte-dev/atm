package cmd

import (
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoDoneAdapterRejectsAgentAttribution(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	t.Setenv("CODEX_THREAD_ID", "agent-review-thread")
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Await human acceptance", Priority: "P1",
		Status: store.TodoStatusReview, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	err := runTodoDone(todoDoneCmd, []string{"#T01"})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("runTodoDone error = %v, want forbidden", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusReview {
		t.Fatalf("agent acceptance mutated todo: %#v", todo)
	}
}
