package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestRunTodoBatchAddReadsCommandInputAndKeepsOutputShape(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatal(err)
	}
	withHumanCLI(t)

	oldJSON := jsonOutput
	oldPriority, oldProject, oldStatus := todoAddPriorityFlag, todoAddProjectFlag, todoAddStatusFlag
	oldSource, oldCreator := todoSourceFlag, todoAddCreatorFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoAddPriorityFlag, todoAddProjectFlag, todoAddStatusFlag = oldPriority, oldProject, oldStatus
		todoSourceFlag, todoAddCreatorFlag = oldSource, oldCreator
		todoAddCmd.SetIn(os.Stdin)
		todoAddCmd.SetErr(os.Stderr)
	})
	jsonOutput = false
	todoAddPriorityFlag, todoAddProjectFlag, todoAddStatusFlag = "P1", "", "open"
	todoSourceFlag, todoAddCreatorFlag = "", ""
	todoAddCmd.SetErr(io.Discard)
	todoAddCmd.SetIn(strings.NewReader(`project: atm
source: adapter-test
items:
  - title: First batch item
  - title: Waiting batch item
    status: waiting
    wake: external result
    creator: claude
`))

	var runErr error
	output := captureStdout(t, func() { runErr = runTodoBatchAdd(todoAddCmd) })
	if runErr != nil {
		t.Fatalf("runTodoBatchAdd: %v", runErr)
	}
	if output != "Added t1: First batch item\nAdded t2: Waiting batch item\n" {
		t.Fatalf("output = %q", output)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 2 || todos.Items[0].Project != "atm" || todos.Items[0].Source != "adapter-test" ||
		todos.Items[0].Creator != "me" || todos.Items[1].Creator != "claude" ||
		todos.Items[1].WakeCondition != "external result" {
		t.Fatalf("todos = %+v", todos.Items)
	}
}

func TestRunTodoMoveKeepsHumanRenderingWhileUsingWorkService(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Move through adapter", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "old", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	oldJSON, oldProject := jsonOutput, todoMoveProjectFlag
	t.Cleanup(func() {
		jsonOutput, todoMoveProjectFlag = oldJSON, oldProject
	})
	jsonOutput = false
	todoMoveProjectFlag = "atm"

	var runErr error
	output := captureStdout(t, func() { runErr = runTodoMove(todoMoveCmd, []string{"#T01"}) })
	if runErr != nil {
		t.Fatalf("runTodoMove: %v", runErr)
	}
	if output != "Moved t1: old → atm\n" {
		t.Fatalf("output = %q", output)
	}
}
