package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// seedBackupSource fills the data directory with one record of each kind a
// backup is responsible for: a row only this database holds, a knowledge file,
// a todo document, and a session row that must NOT come back.
func seedBackupSource(t *testing.T) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO todos (id,position,title,priority,status,created)
		VALUES ('t1',1,'irreplaceable','P1','open','2026-08-05')`); err != nil {
		t.Fatalf("seed todo: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO todo_images
		(todo_id,position,stored_name,original_name,media_type,size_bytes)
		VALUES ('t1',0,'01-screen.png','screen.png','image/png',13)`); err != nil {
		t.Fatalf("seed todo image: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id,short_id,agent,project,file_path,created_ts,last_ts)
		VALUES ('s-1','s1','claude','atm','/tmp/s-1.jsonl',100,200)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	for path, content := range map[string]string{
		filepath.Join("knowledge", "ops", "runbook.md"):         "# runbook\n",
		filepath.Join("todos", "t1.md"):                         "# irreplaceable\n",
		filepath.Join("todos", "assets", "t1", "01-screen.png"): "managed image",
		"config.json": "{}\n",
	} {
		full := filepath.Join(config.AtmDir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatalf("mkdir for %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func runBackupTo(t *testing.T, target string) {
	t.Helper()
	old := backupOutputFlag
	backupOutputFlag = target
	t.Cleanup(func() { backupOutputFlag = old })
	captureStdout(t, func() {
		if err := runBackup(backupCmd, nil); err != nil {
			t.Fatalf("backup: %v", err)
		}
	})
}

// TestBackupRestoreRoundTrip is the acceptance path: archive one data directory,
// restore it into an empty one, and find the records that have nowhere else to
// come from.
func TestBackupRestoreRoundTrip(t *testing.T) {
	withTempAtmDir(t)
	seedBackupSource(t)
	accounting, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordBuiltinUsage(accounting, "atm-backup-fixture", []store.BuiltinModelCall{{Task: "collection", Model: "fixture", InputTokens: 25, OutputTokens: 5, TS: 123, OK: true}}); err != nil {
		t.Fatal(err)
	}
	accounting.Close()
	pendingPath := filepath.Join("model-usage-pending", "pending-job.jsonl")
	if err := os.MkdirAll(filepath.Join(config.AtmDir, "model-usage-pending"), 0700); err != nil {
		t.Fatal(err)
	}
	pending := []byte("{\"Task\":\"collection\",\"Model\":\"fixture\",\"InputTokens\":10,\"TS\":124}\n")
	if err := os.WriteFile(filepath.Join(config.AtmDir, pendingPath), pending, 0600); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	runBackupTo(t, archive)

	// A fresh machine: same archive, empty data directory.
	fresh := t.TempDir()
	config.AtmDir = fresh
	config.AtmDB = filepath.Join(fresh, "atm.db")

	restoreYesFlag = true
	t.Cleanup(func() { restoreYesFlag = false })
	captureStdout(t, func() {
		if err := runRestore(restoreCmd, []string{archive}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})

	db, err := store.Open()
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer db.Close()
	var title string
	if err := db.QueryRow(`SELECT title FROM todos WHERE id='t1'`).Scan(&title); err != nil {
		t.Fatalf("todo did not survive the round trip: %v", err)
	}
	if title != "irreplaceable" {
		t.Fatalf("title = %q, want %q", title, "irreplaceable")
	}
	var imageRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM todo_images WHERE todo_id='t1'`).Scan(&imageRows); err != nil || imageRows != 1 {
		t.Fatalf("todo image metadata did not survive: rows=%d err=%v", imageRows, err)
	}

	// External sessions are rebuildable; ATM's own accounting has no transcript
	// and must survive alongside a pending journal from an interrupted job.
	var sessions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE agent<>'atm'`).Scan(&sessions); err != nil {
		t.Fatalf("restored database cannot read sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("restore brought back %d session row(s); sync should own them", sessions)
	}
	var builtinInput int
	if err := db.QueryRow(`SELECT input_tokens FROM usage WHERE session_id='atm-backup-fixture'`).Scan(&builtinInput); err != nil || builtinInput != 25 {
		t.Fatalf("builtin accounting: input=%d err=%v", builtinInput, err)
	}
	if restored, err := os.ReadFile(filepath.Join(fresh, pendingPath)); err != nil || string(restored) != string(pending) {
		t.Fatalf("pending accounting journal not restored: %q %v", restored, err)
	}

	for _, path := range []string{
		filepath.Join("knowledge", "ops", "runbook.md"),
		filepath.Join("todos", "t1.md"),
		filepath.Join("todos", "assets", "t1", "01-screen.png"),
		"config.json",
	} {
		if _, err := os.Stat(filepath.Join(fresh, path)); err != nil {
			t.Fatalf("%s did not survive the round trip: %v", path, err)
		}
	}
}

func TestBackupExplicitlyExcludesCredentials(t *testing.T) {
	withTempAtmDir(t)
	seedBackupSource(t)
	if err := config.SaveTextModelAPIKey("must-not-be-backed-up"); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	runBackupTo(t, archive)
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(header.Name, config.CredentialsFileName) {
			t.Fatalf("credentials unexpectedly present in backup as %q", header.Name)
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "must-not-be-backed-up") {
			t.Fatalf("credential leaked into backup entry %q", header.Name)
		}
	}
	unbacked, err := unbackedEntries()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(unbacked, config.CredentialsFileName) {
		t.Fatalf("credentials should be an explicit exclusion, unbacked=%v", unbacked)
	}
}

func TestBackupRefusesToOverwriteExistingArchive(t *testing.T) {
	withTempAtmDir(t)
	seedBackupSource(t)

	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := os.WriteFile(archive, []byte("previous backup"), 0600); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	old := backupOutputFlag
	backupOutputFlag = archive
	t.Cleanup(func() { backupOutputFlag = old })

	err := runBackup(backupCmd, nil)
	if err == nil {
		t.Fatal("expected backup to refuse an existing archive path")
	}
	data, readErr := os.ReadFile(archive)
	if readErr != nil {
		t.Fatalf("read archive: %v", readErr)
	}
	if string(data) != "previous backup" {
		t.Fatal("backup overwrote an existing archive")
	}
}

// TestRestoreRejectsNewerSchema guards the case where extraction would produce a
// database this build misreads instead of refuses.
func TestRestoreRejectsNewerSchema(t *testing.T) {
	withTempAtmDir(t)
	archive := filepath.Join(t.TempDir(), "future.tar.gz")
	writeTestArchive(t, archive, backupManifest{
		SchemaVersion: store.SchemaVersion + 1,
		Database:      "atm.db",
	}, map[string]string{"atm.db": "not a real database"})

	err := runRestore(restoreCmd, []string{archive})
	if err == nil {
		t.Fatal("expected restore to reject an archive from a newer build")
	}
	if !strings.Contains(err.Error(), "upgrade atm") {
		t.Fatalf("error does not tell the user what to do: %v", err)
	}
	if _, statErr := os.Stat(config.AtmDB); statErr == nil {
		t.Fatal("restore wrote a database despite rejecting the archive")
	}
}

// TestBackupWithoutDatabaseArchivesFiles covers the case where the database is
// gone but the plain-file records are not. Refusing here would send someone who
// asked for a backup to `atm sync`, which is advice for a different problem.
func TestBackupWithoutDatabaseArchivesFiles(t *testing.T) {
	withTempAtmDir(t)
	knowledge := filepath.Join(config.AtmDir, "knowledge", "ops", "runbook.md")
	if err := os.MkdirAll(filepath.Dir(knowledge), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(knowledge, []byte("# runbook\n"), 0600); err != nil {
		t.Fatalf("write knowledge: %v", err)
	}

	archive := filepath.Join(t.TempDir(), "files.tar.gz")
	runBackupTo(t, archive)

	manifest, err := readBackupManifest(archive)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if manifest.Database != "" {
		t.Fatalf("manifest claims a database %q that was never archived", manifest.Database)
	}
	if len(manifest.EmptiedTables) != 0 {
		t.Fatalf("manifest reports emptied tables with no database: %v", manifest.EmptiedTables)
	}
	if len(manifest.Contents) != 1 || manifest.Contents[0] != "knowledge" {
		t.Fatalf("contents = %v, want [knowledge]", manifest.Contents)
	}

	fresh := t.TempDir()
	config.AtmDir = fresh
	config.AtmDB = filepath.Join(fresh, "atm.db")
	restoreYesFlag = true
	t.Cleanup(func() { restoreYesFlag = false })
	captureStdout(t, func() {
		if err := runRestore(restoreCmd, []string{archive}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(fresh, "knowledge", "ops", "runbook.md")); err != nil {
		t.Fatalf("file-only archive did not restore: %v", err)
	}
}

func TestBackupRefusesWhenThereIsNothingToArchive(t *testing.T) {
	withTempAtmDir(t)
	// withTempAtmDir points at an existing empty directory; remove it so this
	// also covers a data directory that was never created.
	if err := os.RemoveAll(config.AtmDir); err != nil {
		t.Fatalf("clear data dir: %v", err)
	}
	archive := filepath.Join(t.TempDir(), "empty.tar.gz")
	old := backupOutputFlag
	backupOutputFlag = archive
	t.Cleanup(func() { backupOutputFlag = old })

	err := runBackup(backupCmd, nil)
	if err == nil {
		t.Fatal("expected backup to refuse an empty data directory")
	}
	if !strings.Contains(err.Error(), "nothing to back up") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(archive); statErr == nil {
		t.Fatal("backup left an empty archive behind")
	}
}

func TestRestoreRejectsArchiveWithoutManifest(t *testing.T) {
	withTempAtmDir(t)
	archive := filepath.Join(t.TempDir(), "random.tar.gz")
	writeRawTestArchive(t, archive, map[string]string{"atm.db": "unrelated tarball"})

	err := runRestore(restoreCmd, []string{archive})
	if err == nil {
		t.Fatal("expected restore to reject an archive it did not create")
	}
	if !strings.Contains(err.Error(), backupManifestName) {
		t.Fatalf("error does not name the missing manifest: %v", err)
	}
}

// TestRestoreRejectsPathTraversal covers the classic tar escape: an entry whose
// name resolves outside the extraction root.
func TestRestoreRejectsPathTraversal(t *testing.T) {
	withTempAtmDir(t)
	outside := filepath.Join(t.TempDir(), "outside.txt")
	archive := filepath.Join(t.TempDir(), "evil.tar.gz")
	writeTestArchive(t, archive, backupManifest{
		SchemaVersion: store.SchemaVersion,
		Database:      "atm.db",
	}, map[string]string{
		"../../../../../../../../" + strings.TrimPrefix(outside, "/"): "escaped",
	})

	err := runRestore(restoreCmd, []string{archive})
	if err == nil {
		t.Fatal("expected restore to reject a traversal entry")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error does not identify the traversal: %v", err)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("restore wrote outside the data directory")
	}
}

// TestRestoreMovesExistingDataAside checks the reversibility promise: replaced
// data has to still exist somewhere after a restore.
func TestRestoreMovesExistingDataAside(t *testing.T) {
	withTempAtmDir(t)
	seedBackupSource(t)
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	runBackupTo(t, archive)

	// Change a file after the backup, then restore over it.
	marker := filepath.Join(config.AtmDir, "knowledge", "ops", "runbook.md")
	if err := os.WriteFile(marker, []byte("# edited after the backup\n"), 0600); err != nil {
		t.Fatalf("edit knowledge file: %v", err)
	}

	restoreYesFlag = true
	t.Cleanup(func() { restoreYesFlag = false })
	captureStdout(t, func() {
		if err := runRestore(restoreCmd, []string{archive}); err != nil {
			t.Fatalf("restore: %v", err)
		}
	})

	restored, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(restored) != "# runbook\n" {
		t.Fatalf("archive content did not win: %q", restored)
	}

	entries, err := os.ReadDir(config.AtmDir)
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	aside := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "pre-restore-") {
			aside = filepath.Join(config.AtmDir, entry.Name())
		}
	}
	if aside == "" {
		t.Fatal("restore did not keep the replaced data")
	}
	previous, err := os.ReadFile(filepath.Join(aside, "knowledge", "ops", "runbook.md"))
	if err != nil {
		t.Fatalf("read set-aside file: %v", err)
	}
	if string(previous) != "# edited after the backup\n" {
		t.Fatalf("set-aside copy is not the pre-restore content: %q", previous)
	}
}

func TestSafeExtractPathRejectsEscapes(t *testing.T) {
	for _, name := range []string{
		"../escape",
		"a/../../escape",
		"/absolute/path",
		"..",
	} {
		if _, err := safeExtractPath(name); err == nil {
			t.Fatalf("safeExtractPath accepted %q", name)
		}
	}
	inside, err := safeExtractPath("knowledge/ops/runbook.md")
	if err != nil {
		t.Fatalf("safeExtractPath rejected a legitimate entry: %v", err)
	}
	// Relative, so the caller decides which root it lands in and checks the
	// result there.
	if filepath.IsAbs(inside) || inside != filepath.FromSlash("knowledge/ops/runbook.md") {
		t.Fatalf("resolved entry = %q", inside)
	}
	// Joining it against a root has to stay inside that root — the property the
	// extraction loop then re-checks for every entry it writes.
	root := t.TempDir()
	if joined := filepath.Join(root, inside); !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
		t.Fatalf("joined path %q is not under %q", joined, root)
	}
}

// The whole guard, exercised through the code that writes files: a hostile
// archive must fail the restore rather than land a single byte outside staging.
func TestExtractBackupArchiveRefusesAnEscapingEntry(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "hostile.tgz")
	writeTestArchive(t, archivePath, backupManifest{
		SchemaVersion: store.SchemaVersion,
		Database:      "atm.db",
	}, map[string]string{
		"../escaped.md": "# should never be written\n",
	})

	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := extractBackupArchive(archivePath, staging); err == nil {
		t.Fatal("extraction accepted an entry that escapes staging")
	} else if !strings.Contains(err.Error(), "escapes the extraction directory") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.md")); !os.IsNotExist(err) {
		t.Fatalf("the escaping entry was written next to staging: %v", err)
	}
}

// `tar -C dir .` writes a "." entry for the archive root. `atm backup` does not,
// but an archive built by hand is still a reasonable thing to restore, and the
// entry has to be skipped rather than treated as an escape.
func TestExtractBackupArchiveSkipsTheArchiveRootEntry(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "with-root.tgz")
	writeTestArchive(t, archivePath, backupManifest{
		SchemaVersion: store.SchemaVersion,
		Database:      "atm.db",
	}, map[string]string{
		"./":                       "",
		"knowledge/ops/runbook.md": "# runbook\n",
	})

	staging := filepath.Join(dir, "staging")
	if err := os.MkdirAll(staging, 0700); err != nil {
		t.Fatal(err)
	}
	top, err := extractBackupArchive(archivePath, staging)
	if err != nil {
		t.Fatalf("the archive root entry failed extraction: %v", err)
	}
	if len(top) != 1 || top[0] != "knowledge" {
		t.Fatalf("top-level entries = %v, want [knowledge]", top)
	}
	if _, err := os.Stat(filepath.Join(staging, "knowledge", "ops", "runbook.md")); err != nil {
		t.Fatalf("the real entry was not extracted: %v", err)
	}
}

// writeTestArchive builds an archive with a manifest, for cases that need a
// specific manifest rather than a real backup.
func writeTestArchive(t *testing.T, target string, manifest backupManifest, files map[string]string) {
	t.Helper()
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	entries := map[string]string{backupManifestName: string(manifestJSON)}
	for name, content := range files {
		entries[name] = content
	}
	writeRawTestArchive(t, target, entries)
}

func writeRawTestArchive(t *testing.T, target string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(target)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	archive := tar.NewWriter(gz)
	// The manifest has to lead: readBackupManifest scans in order and extraction
	// validates against whatever it found first.
	names := make([]string, 0, len(entries))
	if _, ok := entries[backupManifestName]; ok {
		names = append(names, backupManifestName)
	}
	for name := range entries {
		if name != backupManifestName {
			names = append(names, name)
		}
	}
	for _, name := range names {
		content := entries[name]
		// A trailing slash means a directory entry, which is how tar writes one and
		// the only way a test can produce the "./" archive root.
		if strings.HasSuffix(name, "/") {
			if err := archive.WriteHeader(&tar.Header{
				Name:     name,
				Mode:     0700,
				Typeflag: tar.TypeDir,
			}); err != nil {
				t.Fatalf("write dir header %s: %v", name, err)
			}
			continue
		}
		if err := archive.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0600,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := archive.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
}
