package apphost

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func hashWorkspaceFiles(ctx context.Context, root string) (string, error) {
	return hashWorkspaceFilesIncremental(ctx, root, &workspaceHashState{files: map[string]workspaceHashFile{}}, true)
}

func changeTrackerFixture(t *testing.T) (string, *sql.DB, *ChangeTracker) {
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

func TestChangeTrackerDetectsExternalProcessCommits(t *testing.T) {
	dir, _, tracker := changeTrackerFixture(t)
	before := fingerprintsForTest(t, tracker, "todos", "sessions")
	for i := 0; i < 3; i++ {
		externalTodoCommit(t, dir)
		after := fingerprintsForTest(t, tracker, "todos", "sessions")
		if before["todos"] == after["todos"] {
			t.Fatalf("missed external commit %d: before=%v after=%v", i, before, after)
		}
		if before["sessions"] != after["sessions"] {
			t.Fatalf("todo commit invalidated sessions: before=%v after=%v", before, after)
		}
		before = after
	}
}

func TestTodoDocumentProjectionBatchesRetriesAndSkipsUnchangedFiles(t *testing.T) {
	dir, db, tracker := changeTrackerFixture(t)
	root := filepath.Join(dir, "todos")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 205; id++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("t%d.md", id)), []byte("document"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	calls, projected := 0, 0
	tracker.SetTodoDocumentProjector(func(ctx context.Context, dataDir string, batch map[string]int64) error {
		calls++
		if dataDir != dir || len(batch) > 200 || len(batch) == 0 {
			t.Fatalf("invalid projection: dir=%s count=%d", dataDir, len(batch))
		}
		if calls == 1 {
			return errors.New("temporarily busy")
		}
		projected += len(batch)
		_, err := db.ExecContext(ctx, `UPDATE workspace_changes SET revision=revision+1 WHERE domain='todos'`)
		return err
	})
	if _, err := tracker.Fingerprints(context.Background(), []string{"todos"}); err == nil {
		t.Fatal("projection failure was hidden")
	}
	before := fingerprintsForTest(t, tracker, "todos")
	if calls != 3 || projected != 205 {
		t.Fatalf("retry lost documents: calls=%d projected=%d", calls, projected)
	}
	after := fingerprintsForTest(t, tracker, "todos")
	if calls != 3 || !reflect.DeepEqual(before, after) {
		t.Fatalf("unchanged projection was repeated: calls=%d before=%v after=%v", calls, before, after)
	}
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(root, "t1.md"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	changed := fingerprintsForTest(t, tracker, "todos")
	if calls != 4 || projected != 206 || reflect.DeepEqual(after, changed) {
		t.Fatalf("incremental projection not observed: calls=%d projected=%d", calls, projected)
	}
}

func TestTodoDocumentProjectionIsDurableBeforeResetBaseline(t *testing.T) {
	testHost(t)
	seed(t, card("t1", "external edit", "open", "atm"))
	root := filepath.Join(config.AtmDir, "todos")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "t1.md")
	if err := os.WriteFile(path, []byte("edited outside ATM"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	tracker := NewChangeTracker(config.AtmDir)
	defer tracker.Close()
	tracker.SetTodoDocumentProjector(store.ProjectTodoDocumentTimes)
	before := fingerprintsForTest(t, tracker, "todos")
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var updated int64
	if err := db.QueryRow(`SELECT updated_at FROM todos WHERE id='t1'`).Scan(&updated); err != nil {
		t.Fatal(err)
	}
	if updated != stamp.Unix() {
		t.Fatalf("projected updated_at=%d want=%d", updated, stamp.Unix())
	}
	if after := fingerprintsForTest(t, tracker, "todos"); !reflect.DeepEqual(before, after) {
		t.Fatalf("projection caused duplicate change: %v -> %v", before, after)
	}
}

func TestChangeTrackerIgnoresTelemetryAndRolledBackWrites(t *testing.T) {
	_, db, tracker := changeTrackerFixture(t)
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
	dir, _, tracker := changeTrackerFixture(t)
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
	_, _, tracker := changeTrackerFixture(t)
	fingerprintsForTest(t, tracker, "todos")
	database := tracker.db
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tracker.Fingerprints(ctx, []string{"todos"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fingerprint: %v", err)
	}
	tracker.Close()
	tracker.Close()
	if _, err := tracker.Fingerprints(context.Background(), []string{"todos"}); !errors.Is(err, errChangeTrackerClosed) {
		t.Fatalf("closed tracker reopened: %v", err)
	}
	tracker.SetTodoDocumentProjector(store.ProjectTodoDocumentTimes)
	if tracker.db != nil || tracker.files != nil || tracker.projectTodos != nil || tracker.projectedTodos != nil {
		t.Fatal("closed tracker recreated resources")
	}
	if err := database.QueryRowContext(context.Background(), `SELECT 1`).Scan(new(int)); err == nil {
		t.Fatalf("close left connection usable: %v", err)
	}
}

func TestChangeTrackerCloseDefersCleanupUntilActiveProjectionExits(t *testing.T) {
	dir, _, tracker := changeTrackerFixture(t)
	if err := os.MkdirAll(filepath.Join(dir, "todos"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "todos", "t1.md"), []byte("external edit"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	tracker.SetTodoDocumentProjector(func(context.Context, string, map[string]int64) error {
		close(entered)
		<-release
		return nil
	})
	finished := make(chan error, 1)
	go func() {
		_, err := tracker.Fingerprints(context.Background(), []string{"todos"})
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("projection did not start")
	}
	closed := make(chan struct{})
	go func() { tracker.Close(); tracker.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked indefinitely on an uncooperative projection")
	}
	if _, err := tracker.Fingerprints(context.Background(), []string{"todos"}); !errors.Is(err, errChangeTrackerClosed) {
		t.Fatalf("closed tracker allowed another scan: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Close tore down resources still used by the projection: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released projection did not finish")
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.db != nil || tracker.files != nil || tracker.projectTodos != nil || tracker.projectedTodos != nil {
		t.Fatal("finished projection did not close tracker resources")
	}
}
