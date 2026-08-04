package cmd

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoDoneAutomaticallyWakesStructuredDependents(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldReason := jsonOutput, todoReasonFlag
	t.Cleanup(func() { jsonOutput, todoReasonFlag = oldJSON, oldReason })
	jsonOutput = true
	todoReasonFlag = "dependency completed"
	if err := seedTodos(store.Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: "in_progress", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent", Priority: "P1", Status: store.TodoStatusWaiting, WakeCondition: "waiting", DependsOn: []string{"t1"}, Created: store.Today()}); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	var runErr error
	out := captureStdout(t, func() { runErr = runTodoDone(todoDoneCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatalf("done: %v", runErr)
	}
	var completed store.Todo
	if err := json.Unmarshal([]byte(out), &completed); err != nil || completed.ID != "t1" {
		t.Fatalf("done output = %q, err = %v", out, err)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("load todos: %v", err)
	}
	dependent := store.FindTodo(tf, "t2")
	if dependent == nil || dependent.Status != store.TodoStatusOpen || dependent.WakeCondition != "" {
		t.Fatalf("dependent = %#v", dependent)
	}
}

func TestTodoWakeDefaultsToOpen(t *testing.T) {
	statusFlag := todoWakeCmd.Flags().Lookup("status")
	if statusFlag == nil || statusFlag.DefValue != store.TodoStatusOpen {
		t.Fatalf("wake status default = %#v, want open", statusFlag)
	}
}

func TestTodoDependRemoveLastGeneratedWaitWakesTodo(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = true
	if err := seedTodos(store.Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: "open", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent", Priority: "P1", Status: store.TodoStatusWaiting, WakeCondition: "waiting for todos: t1", DependsOn: []string{"t1"}, Created: store.Today()}); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	var runErr error
	captureStdout(t, func() { runErr = runTodoDependRemove(todoDependRemoveCmd, []string{"t2", "t1"}) })
	if runErr != nil {
		t.Fatalf("remove dependency: %v", runErr)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(tf, "t2")
	if todo == nil || todo.Status != store.TodoStatusOpen || todo.WakeCondition != "" || len(todo.DependsOn) != 0 {
		t.Fatalf("todo = %#v", todo)
	}
}

func TestTodoDependAddIsIdempotentAndRefreshesDerivedWakeCondition(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = true
	if err := seedTodos(store.Todo{ID: "t1", Title: "First prerequisite", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Second prerequisite", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t3", Title: "Dependent", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()}); err != nil {
		t.Fatalf("save todos: %v", err)
	}

	for _, dependencyID := range []string{"t1", "t2", "t2"} {
		var runErr error
		captureStdout(t, func() {
			runErr = runTodoDependAdd(todoDependAddCmd, []string{"t3", dependencyID})
		})
		if runErr != nil {
			t.Fatalf("add %s: %v", dependencyID, runErr)
		}
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(tf, "t3")
	if todo == nil {
		t.Fatal("dependent todo missing")
	}
	if got, want := todo.DependsOn, []string{"t1", "t2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("depends_on = %#v, want %#v", got, want)
	}
	if got, want := todo.WakeCondition, "waiting for todos: t1, t2"; got != want {
		t.Fatalf("wake_condition = %q, want %q", got, want)
	}
}
