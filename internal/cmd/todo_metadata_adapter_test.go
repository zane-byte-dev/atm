package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// A batch key this grammar does not have is rejected, not skipped. `status:`
// and `wake:` were accepted until creation was fixed to open; silently dropping
// them would turn an existing batch file into a pile of plain open Todos while
// reporting success, so the whole batch fails and nothing is written.
func TestRunTodoBatchAddRejectsUnknownKeysWithoutWritingAnything(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatal(err)
	}
	withHumanCLI(t)
	t.Cleanup(func() {
		todoAddCmd.SetIn(os.Stdin)
		todoAddCmd.SetErr(os.Stderr)
	})
	todoAddCmd.SetErr(io.Discard)

	for _, test := range []struct{ name, input, wants string }{
		{
			name:  "retired item key",
			input: "items:\n  - title: Fine\n  - title: Stale\n    status: in_progress\n",
			wants: "field status not found in a batch item",
		},
		{
			name:  "misspelled item key",
			input: "items:\n  - titel: Typo\n",
			wants: "field titel not found in a batch item",
		},
		{
			name:  "unknown top-level key",
			input: "projekt: atm\nitems:\n  - title: Fine\n",
			wants: "field projekt not found in batch input",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			todoAddCmd.SetIn(strings.NewReader(test.input))
			err := runTodoBatchAdd(todoAddCmd)
			if err == nil || !strings.Contains(err.Error(), test.wants) {
				t.Fatalf("error = %v, want it to mention %q", err, test.wants)
			}
			// The Go type name is an implementation detail of the decoder.
			if strings.Contains(err.Error(), "cmd.batch") {
				t.Fatalf("error leaks the internal type name: %v", err)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(todos.Items) != 0 {
				t.Fatalf("rejected batch still wrote todos: %+v", todos.Items)
			}
		})
	}
}

// JSON is a YAML subset and both go through the same decoder, so KnownFields
// must not have made the JSON form stricter than its own grammar.
func TestRunTodoBatchAddStillAcceptsJSONInput(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatal(err)
	}
	withHumanCLI(t)
	t.Cleanup(func() {
		todoAddCmd.SetIn(os.Stdin)
		todoAddCmd.SetErr(os.Stderr)
	})
	todoAddCmd.SetErr(io.Discard)
	todoAddCmd.SetIn(strings.NewReader(
		`{"project":"atm","items":[{"title":"From JSON","creator":"claude"}]}`,
	))

	captureStdout(t, func() {
		if err := runTodoBatchAdd(todoAddCmd); err != nil {
			t.Fatalf("runTodoBatchAdd: %v", err)
		}
	})
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 1 || todos.Items[0].Title != "From JSON" ||
		todos.Items[0].Project != "atm" || todos.Items[0].Creator != "claude" {
		t.Fatalf("todos = %+v", todos.Items)
	}
}

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
