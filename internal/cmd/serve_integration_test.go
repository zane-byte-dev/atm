package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	webassets "github.com/zane-byte-dev/atm/app/web"
	"github.com/zane-byte-dev/atm/internal/apphost"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	webapp "github.com/zane-byte-dev/atm/internal/web"
)

// This helper runs the real Cobra tree in a separate process. A marker in the
// test-owned directory prevents an accidentally inherited variable from pointing
// the helper at the user's ordinary database. HOME is never changed.
func TestServeCLIProcessHelper(t *testing.T) {
	if os.Getenv("ATM_WEB_CLI_TEST_HELPER") != "1" {
		return
	}
	dataDir := os.Getenv("ATM_WEB_CLI_TEST_DIR")
	if !filepath.IsAbs(dataDir) {
		t.Fatal("absolute test data directory is required")
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".atm-web-cli-fixture")); err != nil {
		t.Fatal("refusing an unmarked test data directory")
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("ATM_WEB_CLI_TEST_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	for _, key := range cliAttributionEnvironment() {
		if err := os.Setenv(key, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := apphost.ConfigureDataDir(dataDir); err != nil {
		t.Fatal(err)
	}
	SetVersion("web-integration-test")
	os.Args = append([]string{"atm"}, args...)
	rootCmd.SetArgs(args)
	Execute()
	os.Exit(0)
}

func fixtureCLICommand(t *testing.T, ctx context.Context, dataDir string, args ...string) *exec.Cmd {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, program, "-test.run=^TestServeCLIProcessHelper$")
	command.Env = append(os.Environ(), "ATM_WEB_CLI_TEST_HELPER=1", "ATM_WEB_CLI_TEST_DIR="+dataDir, "ATM_WEB_CLI_TEST_ARGS="+string(encoded), "ATM_SKIP_LOCAL_NOTIFICATION=1")
	return command
}

func runFixtureCLI(t *testing.T, dataDir string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := fixtureCLICommand(t, ctx, dataDir, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("CLI %v: %v\n%s\n%s", args, err, output, stderr.String())
	}
	return output
}

func runFixtureCLIError(t *testing.T, dataDir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := fixtureCLICommand(t, ctx, dataDir, args...).CombinedOutput()
	if err == nil || ctx.Err() != nil {
		t.Fatalf("CLI %v: want a prompt command error, got %v / %v: %s", args, err, ctx.Err(), output)
	}
	return string(output)
}

func prepareServeFixture(t *testing.T) string {
	t.Helper()
	withIsolatedCommandEnv(t)
	if err := os.MkdirAll(config.AtmDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.AtmDir, ".atm-web-cli-fixture"), []byte("test-owned\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.SetStrictReadOnly(false) })
	return config.AtmDir
}

type fixtureBrowser struct {
	client       *http.Client
	origin, csrf string
}

func startFixtureWorkspace(t *testing.T, dataDir string) (*webapp.Server, *fixtureBrowser) {
	t.Helper()
	database, err := readWorkspaceDatabase()
	if err != nil {
		t.Fatal(err)
	}
	host := apphost.New("web-integration-test")
	host.SetWorkEffects(localWorkEffectExecutor{NotifyTodo: func(*store.Todo, string) {}})
	server, err := webapp.Start(webapp.Options{DataDir: dataDir, Version: "web-integration-test", Port: 0, Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ATM test</title>")}}, Dispatch: host.Call, Attachment: host.Attachment, AllowWrites: !database.UpgradeRequired, DataUpgradeRequired: database.UpgradeRequired})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, connectFixtureBrowser(t, dataDir, server.Info().Origin)
}

func connectFixtureBrowser(t *testing.T, dataDir, origin string) *fixtureBrowser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &fixtureBrowser{client: &http.Client{Jar: jar, Timeout: 5 * time.Second}, origin: origin}
	link, err := webapp.OpenExisting(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ticket := strings.Split(link, "#ticket=")[1]
	var exchanged struct {
		CSRF string `json:"csrf_token"`
	}
	browser.post(t, "auth/exchange", map[string]string{"ticket": ticket}, 200, &exchanged)
	browser.csrf = exchanged.CSRF
	if browser.csrf == "" {
		t.Fatal("missing CSRF")
	}
	return browser
}

func (browser *fixtureBrowser) assertCapabilities(t *testing.T, writable, upgrade bool) {
	t.Helper()
	response, err := browser.client.Get(browser.origin + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			Capabilities struct {
				TodoWrite           bool `json:"todo_write"`
				DataUpgradeRequired bool `json:"data_upgrade_required"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || envelope.Data.Capabilities.TodoWrite != writable || envelope.Data.Capabilities.DataUpgradeRequired != upgrade {
		t.Fatalf("bootstrap capabilities: status=%d %+v, want writable=%v upgrade=%v", response.StatusCode, envelope.Data.Capabilities, writable, upgrade)
	}
}

func (browser *fixtureBrowser) post(t *testing.T, method string, input any, status int, result any) {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, browser.origin+"/api/v1/"+method, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", browser.origin)
	req.Header.Set("X-ATM-CSRF", browser.csrf)
	response, err := browser.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("HTTP %s: %d, want %d: %s", method, response.StatusCode, status, content)
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(content, &envelope); err != nil {
		t.Fatal(err)
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			t.Fatalf("decode %s: %v: %s", method, err, content)
		}
	}
}

func TestServeIndependentCLIAndHTTPShareStateAndDetectStaleEdits(t *testing.T) {
	dataDir := prepareServeFixture(t)
	createdJSON := runFixtureCLI(t, dataDir, "todo", "add", "Created by a separate CLI process", "--project", "fixture", "--source", "integration", "--desc", "initial description", "--json")
	var created store.Todo
	if err := json.Unmarshal(createdJSON, &created); err != nil {
		t.Fatalf("CLI create: %v: %s", err, createdJSON)
	}
	server, browser := startFixtureWorkspace(t, dataDir)
	browser.assertCapabilities(t, true, false)
	var before apphost.ShowResult
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, &before)
	if before.Todo.Description != "initial description" || before.ETag == "" {
		t.Fatalf("initial HTTP view %+v", before)
	}

	// The child is a real CLI process and the server remains available while it
	// changes the same database, without any call through HTTP from that CLI.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := fixtureCLICommand(t, ctx, dataDir, "todo", "edit", created.ID, "--desc", "newer description from CLI", "--json")
	var childOutput bytes.Buffer
	command.Stdout = &childOutput
	command.Stderr = &childOutput
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var during apphost.ListResult
	browser.post(t, "todo.list", map[string]any{"limit": 20}, 200, &during)
	if err := command.Wait(); err != nil {
		t.Fatalf("concurrent CLI edit: %v: %s", err, childOutput.String())
	}
	var after apphost.ShowResult
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, &after)
	if after.Todo.Description != "newer description from CLI" || after.ETag == before.ETag {
		t.Fatalf("HTTP did not observe CLI commit: %+v", after)
	}
	browser.post(t, "todo.update", map[string]string{"todo_id": created.ID, "expected_etag": before.ETag, "description": "stale browser overwrite"}, 409, nil)
	browser.post(t, "todo.update", map[string]string{"todo_id": created.ID, "expected_etag": after.ETag, "description": "saved through authenticated HTTP"}, 200, nil)
	cliShow := runFixtureCLI(t, dataDir, "todo", "show", created.ID, "--json")
	var cliTodo store.Todo
	if err := json.Unmarshal(cliShow, &cliTodo); err != nil {
		t.Fatal(err)
	}
	if cliTodo.Description != "saved through authenticated HTTP" {
		t.Fatalf("CLI did not observe HTTP commit: %s", cliShow)
	}

	// Stop also exercises the real Cobra path and the capability-protected
	// loopback control endpoint, not a direct server.Close call.
	runFixtureCLI(t, dataDir, "serve", "stop", "--data-dir", dataDir, "--json")
	waitCtx, stopWait := context.WithTimeout(context.Background(), 3*time.Second)
	defer stopWait()
	if err := server.Wait(waitCtx); err != nil || waitCtx.Err() != nil {
		t.Fatalf("CLI did not stop server: %v / %v", err, waitCtx.Err())
	}
	runFixtureCLI(t, dataDir, "todo", "edit", created.ID, "--title", "CLI still works after workspace exits", "--json")
	cliShow = runFixtureCLI(t, dataDir, "todo", "show", created.ID, "--json")
	if err := json.Unmarshal(cliShow, &cliTodo); err != nil {
		t.Fatal(err)
	}
	if cliTodo.Title != "CLI still works after workspace exits" {
		t.Fatalf("offline CLI failed: %s", cliShow)
	}
}

func seedServeSchema54(t *testing.T, dataDir string) store.Todo {
	t.Helper()
	createdJSON := runFixtureCLI(t, dataDir, "todo", "add", "Existing schema 54 task", "--source", "integration", "--json")
	var created store.Todo
	if err := json.Unmarshal(createdJSON, &created); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"DROP TABLE work_create_idempotency", "UPDATE schema_version SET version=54"} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return created
}

func assertServeSchema54(t *testing.T) {
	t.Helper()
	version, err := store.ReadSchemaVersionAt(config.AtmDB)
	if err != nil || version != 54 {
		t.Fatalf("unexpected schema: v%d, %v", version, err)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var tables int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='work_create_idempotency'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatalf("a read-only command created the v55 table (%d)", tables)
	}
}

func TestServeSchema54ReadOnlyThenExplicitBackupAndMigration(t *testing.T) {
	dataDir := prepareServeFixture(t)
	created := seedServeSchema54(t, dataDir)
	server, browser := startFixtureWorkspace(t, dataDir)
	browser.assertCapabilities(t, false, true)
	var listed apphost.ListResult
	browser.post(t, "todo.list", map[string]any{"limit": 20}, 200, &listed)
	if listed.Total != 1 {
		t.Fatalf("schema 54 list = %+v", listed)
	}
	var shown apphost.ShowResult
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, &shown)
	browser.post(t, "todo.doc", map[string]string{"todo_id": created.ID}, 200, nil)
	// Every new workspace must be usable on the same legacy read-only index.
	// In particular the old collector snapshot/history APIs wrote on reads;
	// these Web readers must never open their migration-capable counterparts.
	for _, method := range []string{"session.list", "session.status", "usage.snapshot", "quota.cached", "knowledge.catalog", "knowledge.query", "memory.recall", "collect.overview", "collect.items", "day.snapshot", "settings.get"} {
		browser.post(t, method, map[string]any{}, 200, nil)
	}
	assertServeSchema54(t)
	for _, method := range []string{"todo.create", "todo.update", "todo.start", "todo.done", "todo.archive", "todo.restore"} {
		browser.post(t, method, map[string]string{"todo_id": created.ID, "expected_etag": shown.ETag, "description": "must remain unchanged"}, 403, nil)
	}
	for _, method := range []string{"knowledge.document.create", "knowledge.collection.create", "memory.create", "memory.supersede", "collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted", "settings.preferences.save"} {
		browser.post(t, method, map[string]any{}, 403, nil)
	}
	runFixtureCLI(t, dataDir, "serve", "status", "--data-dir", dataDir, "--json")
	runFixtureCLI(t, dataDir, "serve", "--data-dir", dataDir)
	refused := runFixtureCLIError(t, dataDir, "serve", "migrate", "--data-dir", dataDir, "--json")
	if !strings.Contains(refused, "atm serve stop") {
		t.Fatalf("migration must explain that workspace is running: %s", refused)
	}
	assertServeSchema54(t)
	if _, err := os.Stat(filepath.Join(dataDir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("refused migration created a backup directory: %v", err)
	}
	runFixtureCLI(t, dataDir, "serve", "stop", "--data-dir", dataDir, "--json")
	waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.Wait(waitCtx); err != nil || waitCtx.Err() != nil {
		t.Fatalf("stop did not finish: %v / %v", err, waitCtx.Err())
	}
	assertServeSchema54(t)

	var migrated serveMigrationResult
	content := runFixtureCLI(t, dataDir, "serve", "migrate", "--data-dir", dataDir, "--json")
	if err := json.Unmarshal(content, &migrated); err != nil {
		t.Fatalf("migration result: %v: %s", err, content)
	}
	backupDir, err := filepath.EvalSymlinks(filepath.Join(dataDir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if !migrated.Migrated || migrated.FromSchema != 54 || migrated.ToSchema != store.SchemaVersion || !filepath.IsAbs(migrated.Archive) || filepath.Dir(migrated.Archive) != backupDir {
		t.Fatalf("migration result: %+v", migrated)
	}
	for path, mode := range map[string]os.FileMode{filepath.Dir(migrated.Archive): 0700, migrated.Archive: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("backup permissions %s: %v / %v", path, info, err)
		}
	}
	manifest, err := readBackupManifest(migrated.Archive)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != 54 || manifest.Database != "atm.db" {
		t.Fatalf("backup was not captured before migration: %+v", manifest)
	}
	extracted := t.TempDir()
	if _, err := extractBackupArchive(migrated.Archive, extracted); err != nil {
		t.Fatal(err)
	}
	version, err := store.ReadSchemaVersionAt(filepath.Join(extracted, "atm.db"))
	if err != nil || version != 54 {
		t.Fatalf("backup schema: v%d, %v", version, err)
	}
	backupDSN := (&url.URL{Scheme: "file", Path: filepath.Join(extracted, "atm.db")}).String() + "?mode=ro"
	backupDB, err := sql.Open("sqlite", backupDSN)
	if err != nil {
		t.Fatal(err)
	}
	var title string
	queryErr := backupDB.QueryRow("SELECT title FROM todos WHERE id=?", created.ID).Scan(&title)
	backupDB.Close()
	if queryErr != nil || title != created.Title {
		t.Fatalf("backup did not preserve task: %q, %v", title, queryErr)
	}
	version, err = store.ReadSchemaVersionAt(config.AtmDB)
	if err != nil || version != store.SchemaVersion {
		t.Fatalf("live fixture schema: v%d, %v", version, err)
	}
	_, browser = startFixtureWorkspace(t, dataDir)
	browser.assertCapabilities(t, true, false)
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, &shown)
	browser.post(t, "todo.update", map[string]string{"todo_id": created.ID, "expected_etag": shown.ETag, "description": "editing works after explicit upgrade"}, 200, nil)
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, &shown)
	if shown.Todo.Description != "editing works after explicit upgrade" {
		t.Fatalf("write after upgrade failed: %+v", shown)
	}
}

func TestServeMigrationBackupFailureDoesNotUpgrade(t *testing.T) {
	dataDir := prepareServeFixture(t)
	seedServeSchema54(t, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "backups"), []byte("a file blocks backup creation"), 0600); err != nil {
		t.Fatal(err)
	}
	refused := runFixtureCLIError(t, dataDir, "serve", "migrate", "--data-dir", dataDir, "--json")
	if !strings.Contains(refused, "database was not migrated") {
		t.Fatalf("backup error did not explain no migration: %s", refused)
	}
	assertServeSchema54(t)
}

func TestServeEmptyDirectoryDoesNotRequireOrRunMigration(t *testing.T) {
	dataDir := prepareServeFixture(t)
	_, browser := startFixtureWorkspace(t, dataDir)
	browser.assertCapabilities(t, true, false)
	browser.post(t, "todo.list", map[string]any{"limit": 20}, 200, nil)
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("empty workspace read initialized database: %v", err)
	}
	refused := runFixtureCLIError(t, dataDir, "serve", "migrate", "--data-dir", dataDir, "--json")
	if !strings.Contains(refused, "no existing ATM database") {
		t.Fatalf("unexpected empty migration result: %s", refused)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("empty migration initialized database: %v", err)
	}
}

// With the full release's embedded assets, this also enters runServe through
// Cobra in a child process, including its startup checks and exit hooks.
func TestServeEmbeddedCLIStartupKeepsSchema54ReadOnly(t *testing.T) {
	if _, err := webassets.Assets(); err != nil {
		t.Skip("requires go test -tags webui to exercise the embedded CLI workspace")
	}
	dataDir := prepareServeFixture(t)
	created := seedServeSchema54(t, dataDir)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	command := fixtureCLICommand(t, ctx, dataDir, "serve", "--data-dir", dataDir, "--port", "0")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan struct{})
	var processErr error
	go func() { processErr = command.Wait(); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("CLI workspace process did not exit")
		}
	})
	readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var info *webapp.Instance
waitForCLI:
	for {
		status, err := webapp.ReadStatus(readyCtx, dataDir)
		if err == nil && status.Running {
			info = status.Instance
			break waitForCLI
		}
		select {
		case <-done:
			t.Fatalf("CLI exited before startup: %v\n%s\n%s", processErr, stdout.String(), stderr.String())
		case <-readyCtx.Done():
			t.Fatal("CLI did not publish its workspace within 5 seconds")
		case <-ticker.C:
		}
	}
	browser := connectFixtureBrowser(t, dataDir, info.Origin)
	browser.assertCapabilities(t, false, true)
	browser.post(t, "todo.show", map[string]string{"todo_id": created.ID}, 200, nil)
	browser.post(t, "todo.update", map[string]string{"todo_id": created.ID, "description": "must not write"}, 403, nil)
	runFixtureCLI(t, dataDir, "serve", "stop", "--data-dir", dataDir, "--json")
	select {
	case <-done:
		if processErr != nil {
			t.Fatalf("CLI exit after stop: %v\n%s", processErr, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CLI did not exit after serve stop")
	}
	assertServeSchema54(t)
	if !strings.Contains(stdout.String(), "Database v54 is read-only") {
		t.Fatalf("CLI did not explain the read-only mode: %s", stdout.String())
	}
}
