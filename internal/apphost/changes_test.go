package apphost

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

func changeTrackerFixture(t *testing.T, tracked bool) (string, *sql.DB, *ChangeTracker) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "atm.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, query := range []string{`PRAGMA journal_mode=WAL`, `CREATE TABLE todos(id INTEGER PRIMARY KEY,value TEXT)`, `CREATE TABLE sessions(id INTEGER PRIMARY KEY,value TEXT)`, `CREATE TABLE cli_invocations(id INTEGER PRIMARY KEY,value TEXT)`} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if tracked {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := store.InstallWorkspaceChangeTracking(tx); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	tracker := NewChangeTracker(dir)
	t.Cleanup(tracker.Close)
	return dir, db, tracker
}

func fingerprintsForTest(t *testing.T, tracker *ChangeTracker, domains ...string) map[string]string {
	t.Helper()
	values, err := tracker.Fingerprints(context.Background(), domains)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

// This helper runs as a separate OS process using the already-built test
// executable. Its only database path is the temporary fixture passed by parent.
func TestChangeTrackerExternalProcessHelper(t *testing.T) {
	path := os.Getenv("ATM_TEST_CHANGE_TRACKER_DB")
	if path == "" {
		t.Skip("subprocess helper")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO todos(value) VALUES ('external process commit')`); err != nil {
		t.Fatal(err)
	}
}

func externalTodoCommit(t *testing.T, dir string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestChangeTrackerExternalProcessHelper$")
	command.Env = append(os.Environ(), "ATM_TEST_CHANGE_TRACKER_DB="+filepath.Join(dir, "atm.db"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("external writer: %v\n%s", err, output)
	}
}

func TestChangeTrackerPinnedConnectionDetectsExternalProcessCommits(t *testing.T) {
	for _, tracked := range []bool{false, true} {
		name := "legacy data_version"
		if tracked {
			name = "tracked domains"
		}
		t.Run(name, func(t *testing.T) {
			dir, _, tracker := changeTrackerFixture(t, tracked)
			before := fingerprintsForTest(t, tracker, "todos", "sessions")
			pinned := tracker.conn
			if pinned == nil {
				t.Fatal("fingerprints did not pin a connection")
			}
			// Idle pool recycling cannot substitute a new connection whose
			// PRAGMA data_version starts with an unrelated baseline.
			tracker.db.SetMaxIdleConns(0)
			for i := 0; i < 3; i++ {
				externalTodoCommit(t, dir)
				after := fingerprintsForTest(t, tracker, "todos", "sessions")
				if before["todos"] == after["todos"] {
					t.Fatalf("missed external commit %d: before=%v after=%v", i, before, after)
				}
				if tracked && before["sessions"] != after["sessions"] {
					t.Fatalf("todo commit invalidated sessions: before=%v after=%v", before, after)
				}
				if tracker.conn != pinned {
					t.Fatal("data_version reader switched connections")
				}
				before = after
			}
		})
	}
}

func TestChangeTrackerIgnoresTelemetryAndRolledBackWrites(t *testing.T) {
	_, db, tracker := changeTrackerFixture(t, true)
	before := fingerprintsForTest(t, tracker, "todos", "sessions", "usage", "knowledge")
	if _, err := db.Exec(`INSERT INTO cli_invocations(value) VALUES ('read-only CLI telemetry')`); err != nil {
		t.Fatal(err)
	}
	if after := fingerprintsForTest(t, tracker, "todos", "sessions", "usage", "knowledge"); !reflect.DeepEqual(before, after) {
		t.Fatalf("telemetry changed workspaces: before=%v after=%v", before, after)
	}
	tx, err := db.Begin()
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
	if after := fingerprintsForTest(t, tracker, "todos", "sessions", "usage", "knowledge"); !reflect.DeepEqual(before, after) {
		t.Fatalf("rollback changed workspaces: before=%v after=%v", before, after)
	}
	if _, err := db.Exec(`INSERT INTO sessions(value) VALUES ('new session')`); err != nil {
		t.Fatal(err)
	}
	after := fingerprintsForTest(t, tracker, "todos", "sessions", "usage", "knowledge")
	if after["sessions"] == before["sessions"] || after["todos"] != before["todos"] || after["usage"] != before["usage"] || after["knowledge"] != before["knowledge"] {
		t.Fatalf("incorrect session domain update: before=%v after=%v", before, after)
	}
}

func TestChangeTrackerHashesSameLengthEditsAndAtomicReplacement(t *testing.T) {
	dir, _, tracker := changeTrackerFixture(t, true)
	docs := filepath.Join(dir, "knowledge")
	if err := os.MkdirAll(docs, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(docs, "note.md")
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	write(path, "alpha")
	first := fingerprintsForTest(t, tracker, "knowledge", "todos")
	write(path, "bravo")
	second := fingerprintsForTest(t, tracker, "knowledge", "todos")
	if first["knowledge"] == second["knowledge"] || first["todos"] != second["todos"] {
		t.Fatalf("same-size/time content edit not isolated: %v -> %v", first, second)
	}
	replacement := filepath.Join(docs, ".replacement")
	write(replacement, "delta")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	third := fingerprintsForTest(t, tracker, "knowledge", "todos")
	if second["knowledge"] == third["knowledge"] || second["todos"] != third["todos"] {
		t.Fatalf("atomic replacement not isolated: %v -> %v", second, third)
	}
}

func TestWorkspaceHashReusesUnchangedContentDigest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("alpha"), 0600); err != nil {
		t.Fatal(err)
	}
	state := &workspaceHashState{files: map[string]workspaceHashFile{}}
	first, err := hashWorkspaceFilesIncremental(context.Background(), root, state, false)
	if err != nil {
		t.Fatal(err)
	}
	if state.contentReads != 1 {
		t.Fatalf("initial content reads = %d, want 1", state.contentReads)
	}
	second, err := hashWorkspaceFilesIncremental(context.Background(), root, state, false)
	if err != nil {
		t.Fatal(err)
	}
	if second != first || state.contentReads != 1 {
		t.Fatalf("unchanged scan = %q/%d, want %q/1", second, state.contentReads, first)
	}
	if err := os.WriteFile(path, []byte("bravo"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := hashWorkspaceFilesIncremental(context.Background(), root, state, false)
	if err != nil {
		t.Fatal(err)
	}
	if third == second || state.contentReads != 2 {
		t.Fatalf("changed scan = %q/%d, want a new digest and 2 reads", third, state.contentReads)
	}
}

func TestWorkspaceHashNeverFollowsOutsideSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(secret, []byte("secret one"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-directory")); err != nil {
		t.Fatal(err)
	}
	before, err := hashWorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("secret two, different content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "added.md"), []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := hashWorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("external symlink content changed workspace hash: %s -> %s", before, after)
	}
}

func TestWorkspaceHashIgnoresHiddenDocumentsAndDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "visible.md"), []byte("visible"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := hashWorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".hidden.md"), []byte("private staging draft"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "visible.md.lock"), []byte("writer lock"), 0600); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, ".staging")
	if err := os.MkdirAll(hidden, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hidden, "draft.md"), []byte("hidden directory document"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := hashWorkspaceFiles(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("hidden workspace staging content changed hash: %s -> %s", before, after)
	}
}

func TestChangeTrackerCancellationAndCloseReleaseConnection(t *testing.T) {
	_, _, tracker := changeTrackerFixture(t, true)
	fingerprintsForTest(t, tracker, "todos")
	connection := tracker.conn
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tracker.Fingerprints(ctx, []string{"todos"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fingerprint: %v", err)
	}
	tracker.Close()
	tracker.Close()
	if err := connection.QueryRowContext(context.Background(), `SELECT 1`).Scan(new(int)); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("close left connection usable: %v", err)
	}
}
