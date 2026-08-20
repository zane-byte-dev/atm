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
	oldPriority, oldProject := todoAddPriorityFlag, todoAddProjectFlag
	oldSource, oldCreator := todoSourceFlag, todoAddCreatorFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoAddPriorityFlag, todoAddProjectFlag = oldPriority, oldProject
		todoSourceFlag, todoAddCreatorFlag = oldSource, oldCreator
		todoAddCmd.SetIn(os.Stdin)
		todoAddCmd.SetErr(os.Stderr)
	})
	jsonOutput = false
	todoAddPriorityFlag, todoAddProjectFlag = "P1", ""
	todoSourceFlag, todoAddCreatorFlag = "", ""
	todoAddCmd.SetErr(io.Discard)
	todoAddCmd.SetIn(strings.NewReader(`project: atm
source: adapter-test
items:
  - title: First batch item
  - title: Second batch item
    creator: claude
`))

	var runErr error
	output := captureStdout(t, func() { runErr = runTodoBatchAdd(todoAddCmd) })
	if runErr != nil {
		t.Fatalf("runTodoBatchAdd: %v", runErr)
	}
	if output != "Added t1: First batch item\nAdded t2: Second batch item\n" {
		t.Fatalf("output = %q", output)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	// Batch items carry metadata only: every created Todo is open, with no
	// waiting presentation to inherit.
	if len(todos.Items) != 2 || todos.Items[0].Project != "atm" || todos.Items[0].Source != "adapter-test" ||
		todos.Items[0].Creator != "me" || todos.Items[1].Creator != "claude" ||
		todos.Items[0].Status != store.TodoStatusOpen || todos.Items[1].Status != store.TodoStatusOpen ||
		todos.Items[1].WakeCondition != "" {
		t.Fatalf("todos = %+v", todos.Items)
	}
}

// `todo move` was merged into `todo edit --project`: reassigning a project is
// one metadata field, not its own verb. The shorthand ID still has to resolve,
// which is why this goes through the adapter rather than the work service.
func TestRunTodoEditReassignsProjectThroughTheMetadataAdapter(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Move through adapter", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "old", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	oldJSON, oldProject := jsonOutput, todoEditProjectFlag
	t.Cleanup(func() {
		jsonOutput, todoEditProjectFlag = oldJSON, oldProject
		todoEditCmd.Flags().Lookup("project").Changed = false
	})
	jsonOutput = false
	todoEditProjectFlag = "atm"
	todoEditCmd.Flags().Lookup("project").Changed = true

	var runErr error
	output := captureStdout(t, func() { runErr = runTodoEdit(todoEditCmd, []string{"#T01"}) })
	if runErr != nil {
		t.Fatalf("runTodoEdit: %v", runErr)
	}
	if output != "Updated t1: Move through adapter\n" {
		t.Fatalf("output = %q", output)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Project != "atm" {
		t.Fatalf("todo = %+v", todo)
	}
}
