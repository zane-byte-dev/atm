package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"

	"github.com/zane-byte-dev/atm/internal/config"
)

// migrateTodoUpdatedAt is the sole 57 -> 58 migration. It only runs through
// Open, never OpenReadOnly, and preserves the old done_ts/document-mtime sort
// values before normal writes begin maintaining the timestamp monotonically.
func migrateTodoUpdatedAt(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Acquire the writer lock before checking the version, so simultaneous CLI
	// starts do not both attempt ALTER TABLE on the same previous schema.
	if _, err := tx.Exec(`UPDATE schema_version SET version=version`); err != nil {
		return err
	}
	var version int
	if err := tx.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		return err
	}
	if version == SchemaVersion {
		return tx.Commit()
	}
	if version != 57 {
		return fmt.Errorf("cannot migrate todo timestamps from schema v%d", version)
	}
	if _, err := tx.Exec(`ALTER TABLE todos ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	// Work through bounded scalar batches. Existing Markdown is inspected only
	// once during this explicit write migration and is never rewritten.
	lastID := ""
	for {
		rows, err := tx.Query(`SELECT id,COALESCE(done_ts,0) FROM todos WHERE id>? ORDER BY id LIMIT 200`, lastID)
		if err != nil {
			return err
		}
		times := make(map[string]int64)
		for rows.Next() {
			var id string
			var updated int64
			if err := rows.Scan(&id, &updated); err != nil {
				rows.Close()
				return err
			}
			if info, err := os.Stat(filepath.Join(config.AtmDir, "todos", id+".md")); err == nil {
				updated = max(updated, info.ModTime().Unix())
			} else if !os.IsNotExist(err) {
				rows.Close()
				return fmt.Errorf("inspect todo document %s: %w", id, err)
			}
			times[id] = updated
			lastID = id
		}
		if err := closeTodoPageRows(rows); err != nil {
			return err
		}
		if len(times) == 0 {
			break
		}
		for id, updated := range times {
			if _, err := tx.Exec(`UPDATE todos SET updated_at=? WHERE id=?`, updated, id); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version=?`, SchemaVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// ProjectTodoDocumentTimes applies at most 200 changed-document observations
// from the background watcher. updated_at means the last task change: deleting
// a document or restoring an older mtime never rolls that timestamp backwards.
// This deliberately uses an existing database without Open/ensureSchema: a
// watcher must never create a database or upgrade an old installation implicitly.
// It is not called while a Todo write transaction or document lock is held.
func ProjectTodoDocumentTimes(ctx context.Context, dataDir string, observations map[string]int64) error {
	if len(observations) == 0 {
		return ctx.Err()
	}
	if len(observations) > 200 {
		return fmt.Errorf("todo document projection accepts at most 200 observations")
	}
	ids := make([]string, 0, len(observations))
	for id := range observations {
		if !LooksLikeTodoID(id) || NormalizeTodoID(id) != id || observations[id] < 0 {
			return fmt.Errorf("invalid todo document observation %q", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	path := filepath.Join(dataDir, "atm.db")
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=rw&_pragma=busy_timeout(1000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A version query would establish a read snapshot before acquiring the WAL
	// write lock. Let this bounded first write acquire it directly instead.
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE todos SET updated_at=? WHERE id=? AND updated_at<?`, observations[id], id, observations[id]); err != nil {
			return err
		}
	}
	return tx.Commit()
}
