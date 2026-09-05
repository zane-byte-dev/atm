package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

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

func TestServeHelpDescriptionsHaveNoAccidentalIndentation(t *testing.T) {
	texts := map[string]string{"serve": serveCmd.Long}
	for _, child := range serveCmd.Commands() {
		if child.Name() == "migrate" {
			texts["serve migrate"] = child.Long
		}
	}
	for name, value := range texts {
		if value == "" {
			t.Fatalf("%s has no long help", name)
		}
		for _, line := range strings.Split(value, "\n") {
			if strings.HasPrefix(line, "\t") {
				t.Fatalf("%s help contains an accidentally indented line: %q", name, line)
			}
		}
	}
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
	if !database.Missing && database.Version < store.SchemaVersion {
		t.Fatalf("fixture database schema v%d is stale", database.Version)
	}
	host := apphost.New("web-integration-test")
	host.SetWorkEffects(localWorkEffectExecutor{NotifyTodo: func(*store.Todo, string) {}})
	server, err := webapp.Start(webapp.Options{DataDir: dataDir, Version: "web-integration-test", Port: 0, Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ATM test</title>")}}, Dispatch: host.Call, Attachment: host.Attachment, StartRuntime: func(webapp.Instance, func(...string)) (func(context.Context) error, error) {
		return func(context.Context) error { return nil }, nil
	}})
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

func (browser *fixtureBrowser) assertCapabilities(t *testing.T) {
	t.Helper()
	response, err := browser.client.Get(browser.origin + "/api/v1/bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			Capabilities struct {
				TodoWrite bool `json:"todo_write"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || !envelope.Data.Capabilities.TodoWrite {
		t.Fatalf("bootstrap capabilities: status=%d %+v", response.StatusCode, envelope.Data.Capabilities)
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
	browser.assertCapabilities(t)
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
	var cliShowResult struct {
		Todo store.Todo `json:"todo"`
	}
	if err := json.Unmarshal(cliShow, &cliShowResult); err != nil {
		t.Fatal(err)
	}
	if cliShowResult.Todo.Description != "saved through authenticated HTTP" {
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
	if err := json.Unmarshal(cliShow, &cliShowResult); err != nil {
		t.Fatal(err)
	}
	if cliShowResult.Todo.Title != "CLI still works after workspace exits" {
		t.Fatalf("offline CLI failed: %s", cliShow)
	}
}

func seedUnsupportedServeSchema(t *testing.T, dataDir string) {
	t.Helper()
	runFixtureCLI(t, dataDir, "todo", "add", "Existing task", "--source", "integration", "--json")
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_version SET version=?", store.MinUpgradableVersion-1); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServeRejectsUnsupportedSchemaWithoutOpeningViewer(t *testing.T) {
	dataDir := prepareServeFixture(t)
	seedUnsupportedServeSchema(t, dataDir)
	refused := runFixtureCLIError(t, dataDir, "serve", "--data-dir", dataDir)
	if !strings.Contains(refused, "no longer supported") {
		t.Fatalf("unexpected stale-schema failure: %s", refused)
	}
	version, err := store.ReadSchemaVersionAt(config.AtmDB)
	if err != nil || version != store.MinUpgradableVersion-1 {
		t.Fatalf("stale database changed: version=%d err=%v", version, err)
	}
}

func TestServeEmptyDirectoryDoesNotRequireOrRunMigration(t *testing.T) {
	dataDir := prepareServeFixture(t)
	_, browser := startFixtureWorkspace(t, dataDir)
	browser.assertCapabilities(t)
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

func TestServeMigrateCurrentDatabaseIsNoop(t *testing.T) {
	dataDir := prepareServeFixture(t)
	runFixtureCLI(t, dataDir, "todo", "add", "Current schema task", "--source", "integration", "--json")
	var result serveMigrationResult
	content := runFixtureCLI(t, dataDir, "serve", "migrate", "--data-dir", dataDir, "--json")
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("migration result: %v: %s", err, content)
	}
	if result.Migrated || result.FromSchema != store.SchemaVersion || result.ToSchema != store.SchemaVersion || result.Archive != "" {
		t.Fatalf("current database migration = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "backups")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-op migration created backup directory: %v", err)
	}
}
