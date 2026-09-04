package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestWorkspaceChangeTrackingIsDomainScopedAndTransactional(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "changes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{`CREATE TABLE todos(id INTEGER PRIMARY KEY,value TEXT)`, `CREATE TABLE sessions(id INTEGER PRIMARY KEY,value TEXT)`, `CREATE TABLE cli_invocations(id INTEGER PRIMARY KEY,value TEXT)`} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = InstallWorkspaceChangeTracking(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	revision := func(domain string) int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(`SELECT revision FROM workspace_changes WHERE domain=?`, domain).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, statement := range []string{`INSERT INTO todos(value) VALUES ('one')`, `UPDATE todos SET value='two'`, `DELETE FROM todos`} {
		before := revision("todos")
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
		if got := revision("todos"); got != before+1 {
			t.Fatalf("%s: todos revision=%d want=%d", statement, got, before+1)
		}
		if got := revision("sessions"); got != 0 {
			t.Fatalf("todo write invalidated sessions=%d", got)
		}
	}
	before := revision("todos")
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO todos(value) VALUES ('rolled back')`); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := revision("todos"); got != before {
		t.Fatalf("rollback changed revision=%d want=%d", got, before)
	}
	if _, err := db.Exec(`INSERT INTO cli_invocations(value) VALUES ('telemetry')`); err != nil {
		t.Fatal(err)
	}
	if got := revision("todos"); got != before {
		t.Fatalf("telemetry invalidated todos=%d", got)
	}
	if _, err := db.Exec(`INSERT INTO sessions(value) VALUES ('session')`); err != nil {
		t.Fatal(err)
	}
	if got := revision("sessions"); got != 1 {
		t.Fatalf("sessions revision=%d", got)
	}
	if got := revision("todos"); got != before {
		t.Fatalf("session write invalidated todos=%d", got)
	}
	// Reinstallation is harmless and does not duplicate triggers or reset state.
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = InstallWorkspaceChangeTracking(tx); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO todos(value) VALUES ('after reinstall')`); err != nil {
		t.Fatal(err)
	}
	if got := revision("todos"); got != before+1 {
		t.Fatalf("reinstalled tracking revision=%d want=%d", got, before+1)
	}
}
