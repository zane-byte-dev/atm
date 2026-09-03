package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func TestWorkNotificationAdapterPreservesDocumentEffects(t *testing.T) {
	for _, test := range []struct {
		name, status, notification string
		kind                       workapp.EffectKind
	}{
		{"submitted", store.TodoStatusReview, notifyEventReview, workapp.EffectTodoSubmitted},
		{"closed", store.TodoStatusDone, notifyEventDone, workapp.EffectTodoClosed},
	} {
		t.Run(test.name, func(t *testing.T) {
			withIsolatedCommandEnv(t)
			todo := store.Todo{ID: "t1", Title: "Original title", Description: "Original requirement", Priority: "P2", Status: store.TodoStatusOpen, Created: store.Today()}
			if _, err := store.EnsureTodoDoc(&todo); err != nil {
				t.Fatal(err)
			}
			todo.Title, todo.Description, todo.Status = "Projected title", "Projected requirement", test.status
			var delivered []string
			executor := localWorkEffectExecutor{NotifyTodo: func(value *store.Todo, event string) {
				delivered = append(delivered, value.ID+":"+event)
			}}
			if err := executor.ApplyWorkEffect(workapp.Effect{Kind: test.kind, Todo: todo, Message: "Lifecycle evidence"}); err != nil {
				t.Fatal(err)
			}
			if len(delivered) != 1 || delivered[0] != "t1:"+test.notification {
				t.Fatalf("notification adapter received %v", delivered)
			}
			assertWorkEffectDocument(t, todo.ID, "Projected title", "Projected requirement", "Lifecycle evidence")
		})
	}
}

func TestSilentWorkNotificationStillRunsDocumentAndOnDoneEffects(t *testing.T) {
	withIsolatedCommandEnv(t)
	todo := store.Todo{ID: "t1", Title: "Original title", Description: "Original requirement", Priority: "P2", Status: store.TodoStatusOpen, Created: store.Today()}
	if _, err := store.EnsureTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "on-done finished")
	// Quote the fixture path as one shell word; only this test-owned marker is
	// written by the real on-done subprocess.
	todo.OnDone = "printf complete > '" + strings.ReplaceAll(marker, "'", "'\"'\"'") + "'"
	todo.Title, todo.Description, todo.Status = "Accepted title", "Accepted requirement", store.TodoStatusDone
	executor := localWorkEffectExecutor{NotifyTodo: func(*store.Todo, string) {}}
	if err := executor.ApplyWorkEffect(workapp.Effect{Kind: workapp.EffectTodoClosed, Todo: todo, Message: "Acceptance evidence"}); err != nil {
		t.Fatal(err)
	}
	assertWorkEffectDocument(t, todo.ID, "Accepted title", "Accepted requirement", "Acceptance evidence")
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		content, err := os.ReadFile(marker)
		if err == nil && string(content) == "complete" {
			return
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case <-deadline.C:
			t.Fatal("silent notifications skipped the on-done effect")
		case <-tick.C:
		}
	}
}

func assertWorkEffectDocument(t *testing.T, id string, expected ...string) {
	t.Helper()
	content, err := store.ReadTodoDoc(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range expected {
		if !strings.Contains(content, value) {
			t.Fatalf("document projection is missing %q:\n%s", value, content)
		}
	}
	if strings.Contains(content, "Original title") || strings.Contains(content, "Original requirement") {
		t.Fatalf("document retained stale generated metadata:\n%s", content)
	}
}
