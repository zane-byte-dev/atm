package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func withTodoAddFlags(t *testing.T) {
	t.Helper()
	oldJSON := jsonOutput
	oldPriority, oldProject := todoAddPriorityFlag, todoAddProjectFlag
	oldCreator := todoAddCreatorFlag
	oldSource, oldDesc, oldDescFile := todoSourceFlag, todoDescFlag, todoDescFileFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoAddPriorityFlag, todoAddProjectFlag = oldPriority, oldProject
		todoAddCreatorFlag = oldCreator
		todoSourceFlag, todoDescFlag, todoDescFileFlag = oldSource, oldDesc, oldDescFile
		todoAddCmd.SetErr(os.Stderr)
	})
	jsonOutput = false
	todoAddPriorityFlag, todoAddProjectFlag = "P1", "atm"
	todoAddCreatorFlag = ""
	todoSourceFlag, todoDescFlag, todoDescFileFlag = "", "", ""
	todoAddCmd.SetErr(io.Discard)
}

func addTodoForTest(t *testing.T, title string) store.Todo {
	t.Helper()
	captureStdout(t, func() {
		if err := runTodoAdd(todoAddCmd, []string{title}); err != nil {
			t.Fatalf("runTodoAdd(%q): %v", title, err)
		}
	})
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	return tf.Items[len(tf.Items)-1]
}

func TestRunTodoAddRecordsWhoFiledIt(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatalf("seed todos: %v", err)
	}
	withTodoAddFlags(t)
	withHumanCLI(t)

	// A plain terminal is the human, and nothing in the environment says
	// otherwise.
	if got := addTodoForTest(t, "Filed from a terminal").Creator; got != store.TodoCreatorMe {
		t.Errorf("creator = %q, want %q", got, store.TodoCreatorMe)
	}

	// An agent session in the environment identifies the agent without the
	// caller having to pass anything.
	t.Setenv("CODEX_THREAD_ID", "thread-1")
	if got := addTodoForTest(t, "Filed by an agent").Creator; got != "codex" {
		t.Errorf("creator = %q, want codex", got)
	}

	// An explicit --creator wins: an agent whose CLI exports no session ID, or
	// a wrapper filing on someone else's behalf, has to be able to say so.
	todoAddCreatorFlag = "qoder"
	if got := addTodoForTest(t, "Filed with an explicit creator").Creator; got != "qoder" {
		t.Errorf("creator = %q, want qoder", got)
	}
}

func TestRunTodoAddRejectsAnUnknownCreator(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(); err != nil {
		t.Fatalf("seed todos: %v", err)
	}
	withTodoAddFlags(t)
	withHumanCLI(t)

	todoAddCreatorFlag = "mystery-agent"
	err := runTodoAdd(todoAddCmd, []string{"Filed by nobody in particular"})
	if err == nil {
		t.Fatal("an unknown creator should be rejected, not stored")
	}
	if !strings.Contains(err.Error(), "unknown creator") {
		t.Errorf("error = %q, want it to name the problem", err)
	}
	tf, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatalf("load todos: %v", loadErr)
	}
	if len(tf.Items) != 0 {
		t.Errorf("rejected creator still created a todo: %#v", tf.Items)
	}
}

func TestRunTodoListFiltersByCreator(t *testing.T) {
	withTempAtmDir(t)
	mine := store.Todo{ID: "t1", Title: "Mine", Priority: "P1", Status: store.TodoStatusOpen,
		Created: store.Today(), Creator: store.TodoCreatorMe}
	collected := store.Todo{ID: "t2", Title: "Collected", Priority: "P1", Status: store.TodoStatusOpen,
		Created: store.Today(), Creator: store.TodoCreatorCollect}
	legacy := store.Todo{ID: "t3", Title: "Legacy", Priority: "P1", Status: store.TodoStatusOpen,
		Created: store.Today()}
	if err := seedTodos(mine, collected, legacy); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	oldJSON, oldCreator := jsonOutput, todoListCreatorFlag
	oldStatus, oldPriority := todoStatusFlag, todoListPriorityFlag
	oldProject, oldQuery := todoProjectFlag, todoListQueryFlag
	t.Cleanup(func() {
		jsonOutput, todoListCreatorFlag = oldJSON, oldCreator
		todoStatusFlag, todoListPriorityFlag = oldStatus, oldPriority
		todoProjectFlag, todoListQueryFlag = oldProject, oldQuery
	})
	jsonOutput = true
	todoStatusFlag, todoListPriorityFlag = "", ""
	todoProjectFlag, todoListQueryFlag = "", ""

	// The Chinese alias filters the same rows as the stored token.
	todoListCreatorFlag = "收集"
	out := captureStdout(t, func() {
		if err := runTodoList(todoListCmd, nil); err != nil {
			t.Fatalf("runTodoList: %v", err)
		}
	})
	var listed []store.Todo
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode list output %q: %v", out, err)
	}
	if len(listed) != 1 || listed[0].ID != "t2" {
		t.Fatalf("--creator 收集 returned %#v", listed)
	}

	// An unfiltered list still shows everything, including todos with no
	// creator recorded.
	todoListCreatorFlag = ""
	out = captureStdout(t, func() {
		if err := runTodoList(todoListCmd, nil); err != nil {
			t.Fatalf("runTodoList: %v", err)
		}
	})
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("decode list output %q: %v", out, err)
	}
	if len(listed) != 3 {
		t.Fatalf("unfiltered list returned %d todos, want 3", len(listed))
	}

	// A typo is an error, not an empty list that looks like an answer.
	todoListCreatorFlag = "collct"
	if err := runTodoList(todoListCmd, nil); err == nil {
		t.Fatal("an unknown --creator should be rejected")
	}
}
