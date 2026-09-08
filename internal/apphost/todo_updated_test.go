package apphost

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

func TestTodoUpdatedAtUsesDurableProjectionWithoutDocumentReads(t *testing.T) {
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
		if got := todoUpdatedAt(todo); got != done {
			t.Fatalf("summary read unprojected document mtime: got %d want %d", got, done)
		}
	}
	todo.UpdatedAt = 400
	if got := todoUpdatedAt(todo); got != 400 {
		t.Fatalf("durable timestamp ignored: got %d", got)
	}
}
