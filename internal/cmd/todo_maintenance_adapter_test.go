package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoMaintainAdapterPreservesRenderingAndProjectsDocument(t *testing.T) {
	withTempAtmDir(t)
	todo := store.Todo{
		ID: "t1", Title: "Bounded maintenance", Priority: "P2",
		Status: store.TodoStatusOpen, Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	oldJSON, oldLimit := jsonOutput, todoMaintainLimitFlag
	t.Cleanup(func() {
		jsonOutput, todoMaintainLimitFlag = oldJSON, oldLimit
	})

	jsonOutput = false
	todoMaintainLimitFlag = 2
	var runErr error
	textOutput := captureStdout(t, func() { runErr = runTodoMaintain(todoMaintainCmd, []string{"#T01"}) })
	if runErr != nil {
		t.Fatalf("runTodoMaintain: %v", runErr)
	}
	if textOutput != "Maintaining t1 (limit 2): Bounded maintenance\n" {
		t.Fatalf("text output = %q", textOutput)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "- **标签**: maintenance") {
		t.Fatalf("maintenance tag was not projected:\n%s", doc)
	}

	jsonOutput = true
	todoMaintainLimitFlag = 4
	jsonText := captureStdout(t, func() { runErr = runTodoMaintain(todoMaintainCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatalf("JSON runTodoMaintain: %v", runErr)
	}
	var rendered store.Todo
	if err := json.Unmarshal([]byte(jsonText), &rendered); err != nil {
		t.Fatalf("JSON output = %q, err=%v", jsonText, err)
	}
	if rendered.ID != "t1" || rendered.MaintenanceLimit != 4 || !store.TodoHasTag(rendered, store.TodoTagMaintenance) {
		t.Fatalf("rendered todo = %+v", rendered)
	}
}
