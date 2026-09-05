package cmd

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoBulkAdapterPreservesJSONAndDeliversDocumentEffect(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	oldJSON, oldProject, oldReason := jsonOutput, todoBulkProjectFlag, todoBulkReasonFlag
	t.Cleanup(func() {
		jsonOutput, todoBulkProjectFlag, todoBulkReasonFlag = oldJSON, oldProject, oldReason
	})
	todo := store.Todo{
		ID: "t1", Title: "Move through bulk adapter", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "old", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	jsonOutput = true
	todoBulkProjectFlag = "atm"
	todoBulkReasonFlag = ""

	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoBulk(todoBulkCmd, []string{"move", "#T01", "t1"})
	})
	if runErr != nil {
		t.Fatalf("runTodoBulk: %v", runErr)
	}
	var payload struct {
		Action   string                `json:"action"`
		Todos    []store.Todo          `json:"todos"`
		Awakened []store.TodoWakeEvent `json:"awakened"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if payload.Action != "move" || len(payload.Todos) != 1 || payload.Todos[0].Project != "atm" || len(payload.Awakened) != 0 {
		t.Fatalf("payload = %+v", payload)
	}
	document, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(document, "**项目**: atm") {
		t.Fatalf("document = %q, err=%v", document, err)
	}
	if pending, err := store.ListPendingWorkEffects("t1"); err != nil || len(pending) != 0 {
		t.Fatalf("delivered effect remains pending: %+v, err=%v", pending, err)
	}
}

func TestTodoBulkAdapterDerivesAgentDonePolicy(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	t.Setenv("CODEX_THREAD_ID", "bulk-agent-thread")
	oldProject, oldReason := todoBulkProjectFlag, todoBulkReasonFlag
	t.Cleanup(func() {
		todoBulkProjectFlag, todoBulkReasonFlag = oldProject, oldReason
	})
	todoBulkProjectFlag, todoBulkReasonFlag = "", ""
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Agent cannot accept", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	err := runTodoBulk(todoBulkCmd, []string{"done", "t1"})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent bulk done error = %v, want forbidden", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusReview {
		t.Fatalf("Agent bulk done mutated todo: %+v, err=%v", todos, loadErr)
	}
}
