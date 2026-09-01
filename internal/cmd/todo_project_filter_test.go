package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// `todo list --project` used to be the one project filter in the CLI that
// compared for exact equality, case included. `--project ATM` therefore returned
// an empty list rather than an error, which is the worst answer available: a
// caller reading JSON cannot tell "no such project" from "nothing open in it",
// and an agent takes the empty array as fact.
//
// It now uses config.ProjectMatches, the same matcher `session list` and
// `session search` already used.
func TestTodoListProjectFilterIsCaseInsensitiveSubstring(t *testing.T) {
	withTempAtmDir(t)
	todos := []store.Todo{
		{ID: "t1", Title: "In atm", Priority: "P1", Status: store.TodoStatusOpen,
			Created: store.Today(), Project: "atm"},
		{ID: "t2", Title: "In atm-private", Priority: "P1", Status: store.TodoStatusOpen,
			Created: store.Today(), Project: "atm-private"},
		{ID: "t3", Title: "Elsewhere", Priority: "P1", Status: store.TodoStatusOpen,
			Created: store.Today(), Project: "wanda"},
	}
	if err := seedTodos(todos...); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	oldJSON, oldProject := jsonOutput, todoProjectFlag
	oldStatus, oldPriority := todoStatusFlag, todoListPriorityFlag
	oldQuery, oldCreator := todoListQueryFlag, todoListCreatorFlag
	oldLimit, oldOffset := todoListLimitFlag, todoListOffsetFlag
	t.Cleanup(func() {
		jsonOutput, todoProjectFlag = oldJSON, oldProject
		todoStatusFlag, todoListPriorityFlag = oldStatus, oldPriority
		todoListQueryFlag, todoListCreatorFlag = oldQuery, oldCreator
		todoListLimitFlag, todoListOffsetFlag = oldLimit, oldOffset
	})
	jsonOutput = true
	todoStatusFlag, todoListPriorityFlag = "", ""
	todoListQueryFlag, todoListCreatorFlag = "", ""
	todoListLimitFlag, todoListOffsetFlag = 0, 0

	list := func(project string) []store.Todo {
		t.Helper()
		todoProjectFlag = project
		out := captureStdout(t, func() {
			if err := runTodoList(todoListCmd, nil); err != nil {
				t.Fatalf("runTodoList(--project %q): %v", project, err)
			}
		})
		var listed []store.Todo
		if err := json.Unmarshal([]byte(out), &listed); err != nil {
			t.Fatalf("decode %q: %v", out, err)
		}
		return listed
	}

	ids := func(listed []store.Todo) string {
		out := ""
		for _, todo := range listed {
			out += todo.ID + " "
		}
		return out
	}

	// The case that used to return nothing at all.
	if got := list("ATM"); len(got) != 2 {
		t.Errorf("--project ATM returned %s, want t1 and t2", ids(got))
	}
	if got := list("atm"); len(got) != 2 {
		t.Errorf("--project atm returned %s, want t1 and t2", ids(got))
	}
	// A substring narrows to one project without needing its full name.
	if got := list("private"); len(got) != 1 || got[0].ID != "t2" {
		t.Errorf("--project private returned %s, want t2", ids(got))
	}
	if got := list("WANDA"); len(got) != 1 || got[0].ID != "t3" {
		t.Errorf("--project WANDA returned %s, want t3", ids(got))
	}
	// An empty filter is not a filter.
	if got := list(""); len(got) != 3 {
		t.Errorf("--project '' returned %s, want all three", ids(got))
	}
	// A project that does not exist still returns nothing — the point was never to
	// match more, it was to stop case alone deciding.
	if got := list("nosuchproject"); len(got) != 0 {
		t.Errorf("--project nosuchproject returned %s, want nothing", ids(got))
	}
}
