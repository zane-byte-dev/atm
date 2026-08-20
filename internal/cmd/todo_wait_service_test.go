package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestRunTodoWaitIsAThinServiceAdapter(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldWake, oldReview := jsonOutput, todoWaitWakeFlag, todoWaitReviewAtFlag
	t.Cleanup(func() {
		jsonOutput, todoWaitWakeFlag, todoWaitReviewAtFlag = oldJSON, oldWake, oldReview
	})
	jsonOutput = false
	todoWaitWakeFlag = "external approval"
	todoWaitReviewAtFlag = "2026-09-01"

	todo := store.Todo{
		ID: "t1", Title: "Wait through service", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoWait(todoWaitCmd, []string{"#T01"})
	})
	if runErr != nil {
		t.Fatalf("runTodoWait: %v", runErr)
	}
	want := "Waiting t1: Wait through service\n  Wake:   external approval\n  Review: 2026-09-01\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
	if binding, err := store.CurrentTodoBinding("session-1"); err != nil || binding != nil {
		t.Fatalf("binding = %+v, err=%v", binding, err)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "- **状态**: waiting（等待中）") {
		t.Fatalf("todo document was not synchronized after commit:\n%s", doc)
	}
}
