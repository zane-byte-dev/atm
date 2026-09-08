package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestTodoTimestampMigrationPreservesDataAndReadOnlyDoesNotMigrate(t *testing.T) {
	withTempStore(t)
	done := int64(200)
	seedTodos(t,
		Todo{ID: "t1", Title: "completed", Status: "done", Priority: "P1", Created: "2026-09-01", DoneTS: &done, Tags: []string{"keep"}},
		Todo{ID: "t2", Title: "latest document", Status: "open", Priority: "P0", Created: "2026-09-01", DependsOn: []string{"t1"}},
		Todo{ID: "t3", Title: "no document", Status: "open", Priority: "P2", Created: "2026-09-01"},
	)
	if err := os.MkdirAll(TodoDocDir(), 0755); err != nil {
		t.Fatal(err)
	}
	for id, stamp := range map[string]int64{"t1": 100, "t2": 300} {
		if err := os.WriteFile(TodoDocPath(id), []byte("preserved "+id), 0600); err != nil {
			t.Fatal(err)
		}
		ts := time.Unix(stamp, 0)
		if err := os.Chtimes(TodoDocPath(id), ts, ts); err != nil {
			t.Fatal(err)
		}
	}
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Construct the actual previous column shape, not just a version label on
	// the new schema, so this exercises the only supported migration accurately.
	if _, err := db.Exec(`ALTER TABLE todos DROP COLUMN updated_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version=57`); err != nil {
		t.Fatal(err)
	}
	readOnly, err := OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := readOnly.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != 57 {
		t.Fatalf("read upgraded schema: version=%d, err=%v", version, err)
	}
	readOnly.Close()
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, map[string]int64{"t1": 500}); err == nil {
		t.Fatal("background projection unexpectedly upgraded old schema")
	}
	if err := ensureSchema(db); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]int64{"t1": 200, "t2": 300, "t3": 0} {
		var got int64
		if err := db.QueryRow(`SELECT updated_at FROM todos WHERE id=?`, id).Scan(&got); err != nil || got != want {
			t.Fatalf("%s timestamp=%d, want %d, err=%v", id, got, want, err)
		}
	}
	var tags, dependencies, todos int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM todo_tags),(SELECT COUNT(*) FROM todo_dependencies),(SELECT COUNT(*) FROM todos)`).Scan(&tags, &dependencies, &todos); err != nil || tags != 1 || dependencies != 1 || todos != 3 {
		t.Fatalf("migration lost rows: tags=%d dependencies=%d todos=%d err=%v", tags, dependencies, todos, err)
	}
	data, err := os.ReadFile(TodoDocPath("t2"))
	info, statErr := os.Stat(TodoDocPath("t2"))
	if err != nil || statErr != nil || string(data) != "preserved t2" || info.ModTime().Unix() != 300 {
		t.Fatalf("migration rewrote document: %q, %v, %v", data, err, statErr)
	}
	if err := ensureSchema(db); err != nil {
		t.Fatalf("current schema re-open failed: %v", err)
	}
}

func TestTodoDocumentTimestampProjectionIsBoundedMonotonicAndNoopAware(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "one"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE todos SET updated_at=100 WHERE id='t1'`); err != nil {
		t.Fatal(err)
	}
	readRevision := func() int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(`SELECT revision FROM workspace_changes WHERE domain='todos'`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := readRevision()
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, map[string]int64{"t1": 50, "t999": 900}); err != nil {
		t.Fatal(err)
	}
	if got := readRevision(); got != before {
		t.Fatalf("older/unknown observations changed revision: %d -> %d", before, got)
	}
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, map[string]int64{"t1": 200}); err != nil {
		t.Fatal(err)
	}
	if got := readRevision(); got != before+1 {
		t.Fatalf("new document timestamp was not projected once: %d -> %d", before, got)
	}
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, map[string]int64{"t1": 200}); err != nil {
		t.Fatal(err)
	}
	if got := readRevision(); got != before+1 {
		t.Fatalf("same observation changed revision: %d", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ProjectTodoDocumentTimes(ctx, config.AtmDir, map[string]int64{"t1": 300}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled projection: %v", err)
	}
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, map[string]int64{"../escape": 300}); err == nil {
		t.Fatal("invalid observation ID accepted")
	}
	tooMany := map[string]int64{}
	for index := 0; index <= 200; index++ {
		tooMany[string(rune(index))] = 1
	}
	if err := ProjectTodoDocumentTimes(context.Background(), config.AtmDir, tooMany); err == nil {
		t.Fatal("unbounded projection accepted")
	}
	missing := t.TempDir()
	if err := ProjectTodoDocumentTimes(context.Background(), missing, map[string]int64{"t1": 300}); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := os.Stat(filepath.Join(missing, "atm.db")); !os.IsNotExist(err) {
		t.Fatalf("projection created missing database: %v", err)
	}
}

func TestTodoTimestampTracksMetadataButNotPositionRepacking(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "one"), openTodo("t2", "two"))
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE todos SET updated_at=100`); err != nil {
		t.Fatal(err)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var unchanged int64
	if err := db.QueryRow(`SELECT updated_at FROM todos WHERE id='t2'`).Scan(&unchanged); err != nil || unchanged != 100 {
		t.Fatalf("position-only change stamped another task: %d, %v", unchanged, err)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t2").Tags = []string{"changed"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var updated int64
	if err := db.QueryRow(`SELECT updated_at FROM todos WHERE id='t2'`).Scan(&updated); err != nil || updated <= unchanged {
		t.Fatalf("child metadata update not stamped: %d, %v", updated, err)
	}
}
