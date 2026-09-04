package store

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// An exclusive rollback-journal transaction makes the reader's brief lock
// collision deterministic; the production race was WAL recovery (extended
// SQLITE_BUSY 261) when a superseding writer closed its last connection.
func TestReadOnlyWaitsForShortWriterLockWithoutImmutableFallback(t *testing.T) {
	withTempStore(t)
	previous := strictReadOnly.Swap(true)
	t.Cleanup(func() { strictReadOnly.Store(previous) })
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	writer, err := sql.Open("sqlite", config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	if _, err := writer.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer writer.Exec("ROLLBACK")
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		reader, err := OpenReadOnly()
		if err != nil {
			result <- err
			return
		}
		defer reader.Close()
		var version int
		if err := reader.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil {
			result <- err
			return
		}
		if version != SchemaVersion {
			result <- fmt.Errorf("wrong version: %d", version)
			return
		}
		if _, err := reader.Exec("UPDATE schema_version SET version=1"); err == nil {
			result <- fmt.Errorf("reader accepted a write")
			return
		}
		result <- nil
	}()
	<-started
	select {
	case err := <-result:
		t.Fatalf("read bypassed the lock or failed instead of waiting: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := writer.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("reader did not recover after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not resume after writer released lock")
	}
	after, err := os.ReadFile(config.AtmDB)
	if err != nil || string(before) != string(after) {
		t.Fatalf("read-only connection changed database: %v", err)
	}
}
