package cmd

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoSubmitEffectFailureRemainsPendingAndRetryRepairsIt(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldReason := jsonOutput, todoSubmitReasonFlag
	t.Cleanup(func() {
		jsonOutput = oldJSON
		todoSubmitReasonFlag = oldReason
	})
	jsonOutput = false
	todoSubmitReasonFlag = "durable projection recovery"
	t.Setenv("ATM_SESSION_ID", "outbox-session")
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Keep failed effect", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "outbox-session", TodoID: "t1",
	}); err != nil {
		t.Fatal(err)
	}

	// A directory at the document path makes AppendTodoLog fail after the
	// lifecycle transaction has committed, without changing database access.
	docPath := store.TodoDocPath("t1")
	if err := os.MkdirAll(docPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := runTodoSubmit(todoSubmitCmd, nil); err == nil ||
		!strings.Contains(err.Error(), "append todo log after submit") {
		t.Fatalf("first submit error = %v", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusReview {
		t.Fatalf("committed todo = %+v", todo)
	}
	pending, err := store.ListPendingWorkEffects("t1")
	if err != nil || len(pending) != 1 || pending[0].AttemptCount != 1 || pending[0].CompletedAt != nil {
		t.Fatalf("pending failed effect = %+v, err=%v", pending, err)
	}
	stableID := pending[0].ID

	if err := os.Remove(docPath); err != nil {
		t.Fatal(err)
	}
	// Submit is now lifecycle-idempotent, but it still returns the persisted
	// effect to this new request and acknowledges it after the repair succeeds.
	if err := runTodoSubmit(todoSubmitCmd, nil); err != nil {
		t.Fatalf("retry submit: %v", err)
	}
	if pending, err = store.ListPendingWorkEffects("t1"); err != nil || len(pending) != 0 {
		t.Fatalf("pending after repair = %+v, err=%v", pending, err)
	}
	var completedAt sql.NullInt64
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT completed_at FROM work_effect_outbox WHERE id=?`, stableID).Scan(&completedAt); err != nil {
		t.Fatalf("read acknowledged effect: %v", err)
	}
	if !completedAt.Valid {
		t.Fatal("repaired effect was not acknowledged")
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(doc, "[submit] durable projection recovery"); count != 1 {
		t.Fatalf("submit log count = %d\n%s", count, doc)
	}
}

func TestTodoSubmitDoesNotRecoverAnOldBindingWithoutPendingEffect(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("ATM_SESSION_ID", "old-binding-session")
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Already projected", Priority: "P1",
		Status: store.TodoStatusReview, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "old-binding-session", TodoID: "t1",
	}); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.UnbindTodoSession("old-binding-session", "submit:review"); err != nil || !changed {
		t.Fatalf("unbind old binding: changed=%v err=%v", changed, err)
	}
	if err := runTodoSubmit(todoSubmitCmd, nil); err == nil || !strings.Contains(err.Error(), "no Todo is bound") {
		t.Fatalf("submit with old binding error = %v", err)
	}
}
