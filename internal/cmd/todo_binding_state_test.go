package cmd

import (
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestNonWorkingTodoTransitionsUnbindSessions(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	oldJSON := jsonOutput
	oldBulkProject, oldBulkReason := todoBulkProjectFlag, todoBulkReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoBulkProjectFlag = oldBulkProject
		todoBulkReasonFlag = oldBulkReason
	})
	jsonOutput = false

	t.Run("bulk close", func(t *testing.T) {
		if err := seedTodos(store.Todo{
			ID: "t2", Title: "Bulk transition", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "bulk-session", TodoID: "t2"}); err != nil {
			t.Fatal(err)
		}
		todoBulkReasonFlag = "verified bulk transition output"
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
