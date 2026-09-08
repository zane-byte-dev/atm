package apphost

import (
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTodoUpdatedAtUsesLatestAvailableEvidence(t *testing.T) {
	testHost(t)
	done := int64(200)
	todo := work.Todo{ID: "t1", DoneTS: &done}
	if got := todoUpdatedAt(todo); got != 200 {
		t.Fatalf("completion fallback %d", got)
	}
	path := store.TodoDocPath(todo.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("progress"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, ts := range []int64{100, 300} {
		stamp := time.Unix(ts, 0)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		want := max(ts, done)
		if got := todoUpdatedAt(todo); got != want {
			t.Fatalf("got %d want %d", got, want)
		}
	}
}
