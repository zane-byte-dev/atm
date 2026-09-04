package cmd

import (
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoRuntimeNotificationUsesLifecycleIdentity(t *testing.T) {
	start := int64(100)
	todo := store.Todo{ID: "t1", Title: "before", Created: "2026-09-03", StartTS: &start}
	before := todoRuntimeNotification(&todo, notifyEventReview, "ATM", "review", todo.Title)
	todo.Title = "after"
	after := todoRuntimeNotification(&todo, notifyEventReview, "ATM", "review", todo.Title)
	if before.DedupKey != after.DedupKey {
		t.Fatal("unrelated title edit changed transition identity")
	}
	if before.ID != "todo-t1" || before.Kind != "todo_review" || before.ObjectID != "t1" || before.Action != "post" {
		t.Fatalf("wrong notification contract: %+v", before)
	}
	start = 200
	reopened := todoRuntimeNotification(&todo, notifyEventReview, "ATM", "review", todo.Title)
	if before.DedupKey == reopened.DedupKey {
		t.Fatal("reopened turn reuses previous review receipt")
	}
	done := todoRuntimeNotification(&todo, notifyEventDone, "ATM", "done", todo.Title)
	if done.DedupKey == reopened.DedupKey {
		t.Fatal("distinct lifecycle events have same receipt")
	}
}
