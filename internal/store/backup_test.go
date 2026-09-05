package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

// TestSnapshotKeepsOwnRecordsAndDropsRebuildable is the core contract: a backup
// must carry everything that has nowhere else to come from and must not carry
// the session mirror, which `atm sync` reproduces.
func TestSnapshotKeepsOwnRecordsAndDropsRebuildable(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO todos (id,position,title,priority,status,created)
		VALUES ('t1',1,'keep me','P1','open','2026-08-05')`); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id,short_id,agent,project,file_path,created_ts,last_ts)
		VALUES ('s-1','s1','claude','atm','/tmp/s-1.jsonl',100,200)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := SnapshotOwnRecords(snapshot); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	copied, err := openNoMigrate(snapshot, true)
	if err != nil {
		t.Fatalf("open snapshot: %v", err)
	}
	defer copied.Close()

	var title string
	if err := copied.QueryRow(`SELECT title FROM todos WHERE id='t1'`).Scan(&title); err != nil {
		t.Fatalf("todo did not survive the snapshot: %v", err)
	}
	if title != "keep me" {
		t.Fatalf("todo title = %q, want %q", title, "keep me")
	}
	// The tables must survive with no rows. A dropped table would restore into a
	// database whose schema version claims it exists, and nothing recreates it.
	for _, table := range RebuildableTables() {
		exists, err := tableExists(copied, table)
		if err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("snapshot dropped %s instead of emptying it", table)
		}
		var rows int
		if err := copied.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&rows); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if rows != 0 {
			t.Fatalf("%s still holds %d row(s)", table, rows)
		}
	}
}

func TestSnapshotPreservesBuiltinAccountingAndExecutionReceipts(t *testing.T) {
	db := openTempDB(t)
	calls := []BuiltinModelCall{
		{Task: "collection", Model: "fixture-model", InputTokens: 100, CacheHitTokens: 20, OutputTokens: 10, TS: 123, OK: true},
		{Task: "todo-refine", Model: "fixture-model", InputTokens: 50, OutputTokens: 5, TS: 124, OK: false},
	}
	if err := RecordBuiltinUsage(db, "atm-runtime-job", calls); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO sessions (id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('external','ext','codex','','/fixture',1,2)`,
		`INSERT INTO usage (session_id,model,input_tokens) VALUES ('external','fixture-model',200)`,
		`INSERT INTO usage_events (session_id,model,ts,input_tokens) VALUES ('external','fixture-model',1,200)`,
		`INSERT INTO background_jobs (id,idempotency_key,payload_hash,kind,status,request_json,result_json,created_at) VALUES ('job-id','key','hash','collect.run','running','{}','{}',1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := SnapshotOwnRecords(snapshot); err != nil {
		t.Fatal(err)
	}
	copied, err := openNoMigrate(snapshot, false)
	if err != nil {
		t.Fatal(err)
	}
	defer copied.Close()
	var sessions, events, input, cache, jobs int
	if err := copied.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := copied.QueryRow(`SELECT COUNT(*),SUM(input_tokens),SUM(cache_read_tokens) FROM usage_events`).Scan(&events, &input, &cache); err != nil {
		t.Fatal(err)
	}
	if err := copied.QueryRow(`SELECT COUNT(*) FROM background_jobs`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || events != 2 || input != 130 || cache != 20 || jobs != 1 {
		t.Fatalf("snapshot: sessions=%d events=%d input=%d cache=%d jobs=%d", sessions, events, input, cache, jobs)
	}
	// A journal that was copied just before its unlink can replay after restore.
	// Stable IDs must replace exactly this operation without counting it twice.
	if err := RecordBuiltinUsage(copied, "atm-runtime-job", calls); err != nil {
		t.Fatal(err)
	}
	if err := copied.QueryRow(`SELECT COUNT(*),SUM(input_tokens) FROM usage_events`).Scan(&events, &input); err != nil {
		t.Fatal(err)
	}
	if events != 2 || input != 130 {
		t.Fatalf("restored journal replay: events=%d input=%d", events, input)
	}
}

func TestSnapshotRefusesExistingDestination(t *testing.T) {
	openTempDB(t)
	existing := filepath.Join(t.TempDir(), "snapshot.db")
	if err := os.WriteFile(existing, []byte("do not clobber"), 0600); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	if err := SnapshotOwnRecords(existing); err == nil {
		t.Fatal("expected refusal to overwrite an existing snapshot")
	}
	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "do not clobber" {
		t.Fatalf("destination was modified: %q", data)
	}
}

// TestReadSchemaVersionAtSkipsSchemaValidation covers why backup exists: a database
// below MinUpgradableVersion must still be readable enough to archive, and Open
// is exactly what refuses it.
func TestReadSchemaVersionAtSkipsSchemaValidation(t *testing.T) {
	openTempDB(t)
	stale := MinUpgradableVersion - 1
	writable, err := openNoMigrate(config.AtmDB, false)
	if err != nil {
		t.Fatalf("open writable: %v", err)
	}
	if _, err := writable.Exec(`UPDATE schema_version SET version = ?`, stale); err != nil {
		t.Fatalf("downgrade schema version: %v", err)
	}
	writable.Close()

	version, err := ReadSchemaVersionAt(config.AtmDB)
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != stale {
		t.Fatalf("version = %d, want %d", version, stale)
	}

	// The same database through Open must be rejected, and the message has to
	// point at the escape hatch rather than straight at deletion.
	db, err := Open()
	if db != nil {
		db.Close()
	}
	if err == nil {
		t.Fatal("expected Open to reject an unupgradable schema")
	}
	if !strings.Contains(err.Error(), "atm backup") {
		t.Fatalf("rejection does not mention the escape hatch: %v", err)
	}

	// And the snapshot must still work on it.
	snapshot := filepath.Join(t.TempDir(), "old.db")
	if err := SnapshotOwnRecords(snapshot); err != nil {
		t.Fatalf("snapshot of an unupgradable database: %v", err)
	}
}

func TestReadSchemaVersionAtMissingDatabase(t *testing.T) {
	oldDB := config.AtmDB
	config.AtmDB = filepath.Join(t.TempDir(), "absent.db")
	t.Cleanup(func() { config.AtmDB = oldDB })
	if _, err := ReadSchemaVersionAt(config.AtmDB); err != ErrDatabaseMissing {
		t.Fatalf("err = %v, want ErrDatabaseMissing", err)
	}
}

// Version 0 is a claim — it goes into the backup manifest a restore checks, and
// diagnose prints it instead of the error field it keeps for this case — so only
// the two states that really mean "no version recorded" may report it. A file
// that is not a database has to come back as an error.
func TestReadSchemaVersionAtSeparatesNoVersionFromUnreadable(t *testing.T) {
	dir := t.TempDir()
	oldDB := config.AtmDB
	t.Cleanup(func() { config.AtmDB = oldDB })

	// Not a database at all: the catalogue read fails.
	corrupt := filepath.Join(dir, "corrupt.db")
	if err := os.WriteFile(corrupt, []byte("this is not a sqlite file"), 0600); err != nil {
		t.Fatal(err)
	}
	if version, err := ReadSchemaVersionAt(corrupt); err == nil {
		t.Fatalf("a corrupt file reported version %d with no error", version)
	}

	// A real database with no schema_version table: someone else's, and 0 is the
	// honest answer.
	foreign := filepath.Join(dir, "foreign.db")
	db, err := openNoMigrate2(t, foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if version, err := ReadSchemaVersionAt(foreign); err != nil || version != 0 {
		t.Fatalf("foreign database = %d, %v; want 0, nil", version, err)
	}

	// The table exists but holds nothing: a database caught mid-creation.
	empty := filepath.Join(dir, "empty-version.db")
	db, err = openNoMigrate2(t, empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_version (version INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if version, err := ReadSchemaVersionAt(empty); err != nil || version != 0 {
		t.Fatalf("versionless table = %d, %v; want 0, nil", version, err)
	}
}

// openNoMigrate2 creates the file first, because openNoMigrate refuses a path
// that does not exist yet and this test needs to build databases by hand.
func openNoMigrate2(t *testing.T, path string) (*sql.DB, error) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	file.Close()
	return openNoMigrate(path, false)
}
