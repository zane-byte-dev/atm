package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoStartReopensClosedTodoWithFreshLifecycle(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = false

	closed := "2026-07-01"
	reason := "first attempt completed"
	oldStart := int64(100)
	oldDone := int64(200)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Reopen me", Priority: "P1", Status: store.TodoStatusDone,
		Created: store.Today(), Closed: &closed, ClosedReason: &reason,
		StartTS: &oldStart, DoneTS: &oldDone,
	}); err != nil {
		t.Fatal(err)
	}

	before := time.Now().Unix()
	if err := runTodoStart(todoStartCmd, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, "t1")
	if todo == nil {
		t.Fatal("reopened todo not found")
	}
	if todo.Status != store.TodoStatusInProgress {
		t.Fatalf("status = %q, want in_progress", todo.Status)
	}
	if todo.StartTS == nil || *todo.StartTS < before || *todo.StartTS == oldStart {
		t.Fatalf("start timestamp = %v, want a fresh timestamp", todo.StartTS)
	}
	if todo.DoneTS != nil || todo.Closed != nil || todo.ClosedReason != nil {
		t.Fatalf("closed lifecycle survived reopen: %#v", todo)
	}
}

func TestTodoSubmitMovesInProgressToReviewAndIsIdempotent(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldReason := jsonOutput, todoSubmitReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoSubmitReasonFlag = oldReason
	})
	jsonOutput = false
	todoSubmitReasonFlag = "agent codex completed run t1-123"
	todo := store.Todo{
		ID: "t1", Title: "Explicit submission", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "submit-session", TodoID: todo.ID,
	}); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := runTodoSubmit(todoSubmitCmd, []string{todo.ID}); err != nil {
			t.Fatalf("submit attempt %d: %v", attempt+1, err)
		}
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	submitted := store.FindTodo(todos, todo.ID)
	if submitted == nil || submitted.Status != store.TodoStatusReview {
		t.Fatalf("submitted todo = %#v", submitted)
	}
	binding, err := store.CurrentTodoBinding("submit-session")
	if err != nil {
		t.Fatal(err)
	}
	if binding != nil {
		t.Fatalf("active binding survived submission: %#v", binding)
	}
	history, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Reason != "submit:review" || history[0].UnboundAt == nil {
		t.Fatalf("binding history = %#v", history)
	}
	doc, err := store.ReadTodoDoc(todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(doc, "[submit] agent codex completed run t1-123"); count != 1 {
		t.Fatalf("submit log count = %d\n%s", count, doc)
	}
}

func TestTodoSubmitRejectsWorkThatWasNotStarted(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Not started", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := runTodoSubmit(todoSubmitCmd, []string{"t1"}); err == nil || !strings.Contains(err.Error(), "cannot submit") {
		t.Fatalf("submit error = %v", err)
	}
}

func TestTodoSubmitIgnoresObsoleteJSONWriteObstacles(t *testing.T) {
	withTempAtmDir(t)
	oldReason := todoSubmitReasonFlag
	t.Cleanup(func() { todoSubmitReasonFlag = oldReason })
	todoSubmitReasonFlag = "ready for review"
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Failure-safe submit", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "submit-failure-session", TodoID: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := runTodoSubmit(todoSubmitCmd, []string{"t1"}); err != nil {
		t.Fatalf("submit through SQLite: %v", err)
	}
	binding, err := store.CurrentTodoBinding("submit-failure-session")
	if err != nil {
		t.Fatal(err)
	}
	if binding != nil {
		t.Fatalf("active binding survived submit: %#v", binding)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, "t1")
	if todo == nil || todo.Status != store.TodoStatusReview {
		t.Fatalf("persisted todo = %#v, want review", todo)
	}
}

func TestStatusTransitionIgnoresObsoleteJSONWriteObstacles(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldEditStatus := jsonOutput, todoEditStatusFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoEditStatusFlag = oldEditStatus
	})
	jsonOutput = false

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Failure-safe transition", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "failure-safe-session", TodoID: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	setCommandFlagForTest(t, todoEditCmd, "status", store.TodoStatusOpen)
	if err := runTodoEdit(todoEditCmd, []string{"t1"}); err != nil {
		t.Fatalf("edit through SQLite: %v", err)
	}

	binding, err := store.CurrentTodoBinding("failure-safe-session")
	if err != nil {
		t.Fatal(err)
	}
	if binding != nil {
		t.Fatalf("active binding survived transition: %#v", binding)
	}
	history, err := store.ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].UnboundAt == nil || history[0].Reason != "status-style:open" {
		t.Fatalf("structured binding audit = %#v", history)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, "t1")
	if todo == nil || todo.Status != store.TodoStatusOpen {
		t.Fatalf("persisted todo = %#v, want open", todo)
	}
}

func TestClosingTodoInSameStateIsIdempotent(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	oldJSON, oldReason := jsonOutput, todoReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoReasonFlag = oldReason
	})
	jsonOutput = true
	todoReasonFlag = ""
	closed := store.Today()
	doneTS := int64(123)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Already complete", Priority: "P1", Status: store.TodoStatusDone,
		Created: store.Today(), Closed: &closed, DoneTS: &doneTS,
	}); err != nil {
		t.Fatal(err)
	}

	var runErr error
	captureStdout(t, func() { runErr = runTodoDone(todoDoneCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	todo := store.FindTodo(todos, "t1")
	if todo == nil || todo.DoneTS == nil || *todo.DoneTS != doneTS {
		t.Fatalf("repeated close mutated completion timestamp: %#v", todo)
	}
}
