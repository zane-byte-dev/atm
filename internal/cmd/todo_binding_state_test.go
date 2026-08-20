package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/store"
)

func setCommandFlagForTest(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("flag %s not found", name)
	}
	oldValue, oldChanged := flag.Value.String(), flag.Changed
	if err := flag.Value.Set(value); err != nil {
		t.Fatalf("set flag %s: %v", name, err)
	}
	flag.Changed = true
	t.Cleanup(func() {
		_ = flag.Value.Set(oldValue)
		flag.Changed = oldChanged
	})
}

func TestNonWorkingTodoTransitionsUnbindSessions(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	oldJSON := jsonOutput
	oldEditStatus, oldBulkStatus, oldBulkReason := todoEditStatusFlag, todoBulkStatusFlag, todoBulkReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoEditStatusFlag = oldEditStatus
		todoBulkStatusFlag = oldBulkStatus
		todoBulkReasonFlag = oldBulkReason
	})
	jsonOutput = false

	t.Run("edit status", func(t *testing.T) {
		if err := seedTodos(store.Todo{
			ID: "t1", Title: "Edit transition", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "edit-session", TodoID: "t1"}); err != nil {
			t.Fatal(err)
		}
		// `edit --status` now only returns work to open; review and done are
		// reached through submit and done. Returning to open is still a
		// transition out of in_progress, which is what has to release the session.
		setCommandFlagForTest(t, todoEditCmd, "status", store.TodoStatusOpen)
		if err := runTodoEdit(todoEditCmd, []string{"t1"}); err != nil {
			t.Fatalf("edit: %v", err)
		}
		if binding, err := store.CurrentTodoBinding("edit-session"); err != nil || binding != nil {
			t.Fatalf("binding after edit = %#v, err=%v", binding, err)
		}
	})

	t.Run("bulk close", func(t *testing.T) {
		if err := seedTodos(store.Todo{
			ID: "t2", Title: "Bulk transition", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "bulk-session", TodoID: "t2"}); err != nil {
			t.Fatal(err)
		}
		todoBulkReasonFlag = ""
		if err := runTodoBulk(todoBulkCmd, []string{"done", "t2"}); err != nil {
			t.Fatalf("bulk done: %v", err)
		}
		if binding, err := store.CurrentTodoBinding("bulk-session"); err != nil || binding != nil {
			t.Fatalf("binding after bulk done = %#v, err=%v", binding, err)
		}
	})

	t.Run("dependency wait", func(t *testing.T) {
		if err := seedTodos(store.Todo{ID: "t3", Title: "Dependent work", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
			store.Todo{ID: "t4", Title: "Dependency", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "dependency-session", TodoID: "t3"}); err != nil {
			t.Fatal(err)
		}
		if err := runTodoDependAdd(todoDependAddCmd, []string{"t3", "t4"}); err != nil {
			t.Fatalf("depend add: %v", err)
		}
		if binding, err := store.CurrentTodoBinding("dependency-session"); err != nil || binding != nil {
			t.Fatalf("binding after dependency wait = %#v, err=%v", binding, err)
		}
	})
}
