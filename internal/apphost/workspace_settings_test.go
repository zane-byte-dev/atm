package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func seedWorkspaceDay(t *testing.T) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	day, _ := time.ParseInLocation(time.DateOnly, "2026-09-02", workspaceLocation())
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ai_day_features(day,timezone,session_count,turn_count,tool_calls,source_count,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,built_at,feature_version) VALUES ('2026-09-02','Asia/Shanghai',2,9,18,1,100,200,30,900,123,3)`, nil},
		{`INSERT INTO ai_day_results(day,state,concept_id,title,explanation,generated_at,engine_version) VALUES ('2026-09-02','ready','code_architect','代码架构师','已存储的结果',124,3)`, nil},
		{`INSERT INTO ai_day_feature_details(day,event_count,active_seconds) VALUES ('2026-09-02',3,720)`, nil},
		{`INSERT INTO ai_day_sources(source,enabled,semantic_enabled,updated_at) VALUES ('codex',1,0,123)`, nil},
		{`INSERT INTO ai_day_badge_days(day,badge_id,qualified,selected,level) VALUES ('2026-09-02','code_architect',1,1,1)`, nil},
		{`INSERT INTO ai_day_badge_progress(badge_id,level,qualified_days,updated_at) VALUES ('code_architect',1,1,123)`, nil},
		{`INSERT INTO ai_day_events(event_id,occurred_at,source,session_hash,event_type,input_tokens,output_tokens,semantic_labels_json,schema_version,ingested_at) VALUES ('private-event',?,'codex','private-session','turn',100,200,'["private-semantic"]',2,123)`, []any{day.Add(9 * time.Hour).Unix()}},
		{`INSERT INTO ai_day_events(event_id,occurred_at,source,session_hash,event_type,schema_version,ingested_at) VALUES ('private-event2',?,'codex','private-session','tool',2,123)`, []any{day.Add(10 * time.Hour).Unix()}},
		{`INSERT INTO ai_day_events(event_id,occurred_at,source,session_hash,event_type,schema_version,ingested_at) VALUES ('previous-day',?,'codex','private-session','turn',2,123)`, []any{day.Add(-time.Second).Unix()}},
		// The Web runtime must keep this existing schema readable without
		// upgrading it to add the new Todo idempotency table.
		{`UPDATE schema_version SET version=54`, nil},
		{`DROP TABLE IF EXISTS work_create_idempotency`, nil},
	} {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkspaceAIDayReadsStoredSchema54WithoutRebuild(t *testing.T) {
	h := testHost(t)
	seedWorkspaceDay(t)
	before, err := os.ReadFile(config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	ctx, call := context.Background(), webCall()
	snapshot, err := h.AIDaySnapshot(ctx, call, AIDayRangeInput{From: "2026-09-01", To: "2026-09-03"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Indexed || len(snapshot.History) != 1 || snapshot.History[0].WorkTokens != 330 || snapshot.History[0].ActiveSeconds != 720 || snapshot.History[0].GeneratedAt != 124 {
		t.Fatalf("stored history changed: %+v", snapshot)
	}
	if snapshot.Atlas == nil || snapshot.Atlas.Unlocked != 1 || snapshot.Privacy == nil || len(snapshot.Privacy.Sources) != 1 || snapshot.Privacy.Sources[0].SemanticEnabled {
		t.Fatalf("lost stored badges/privacy: %+v", snapshot)
	}
	shown, err := h.AIDayShow(ctx, call, aiday.DayInput{Day: "2026-09-02"})
	if err != nil || shown.Concept == nil || shown.Concept.Explanation != "已存储的结果" || shown.GeneratedAt != 124 {
		t.Fatalf("stored day=%+v err=%v", shown, err)
	}
	if _, err := h.AIDayShow(ctx, call, aiday.DayInput{Day: "2026-09-03"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing date must stay missing: %v", err)
	}
	ledger, err := h.AIDayLedger(ctx, call, AIDayLedgerInput{Day: "2026-09-02", Limit: 1, Offset: 1})
	if err != nil || ledger.Total != 2 || len(ledger.Items) != 1 || ledger.Items[0].EventType != "turn" || ledger.Items[0].InputTokens != 100 {
		t.Fatalf("ledger pagination/timezone=%+v err=%v", ledger, err)
	}
	encoded, _ := json.Marshal(ledger)
	for _, secret := range []string{"private-event", "private-session", "private-semantic", "session_hash", "semantic_labels", "event_id", "file_path"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("ledger exposed %q: %s", secret, encoded)
		}
	}
	after, err := os.ReadFile(config.AtmDB)
	if err != nil || string(before) != string(after) {
		t.Fatalf("reading AI Day changed the database: %v", err)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != 54 {
		t.Fatalf("read migrated schema: %d, %v", version, err)
	}
	if exists, err := workspaceHasTable(ctx, db, "work_create_idempotency"); err != nil || exists {
		t.Fatalf("read materialized a newer schema table: %v, %v", exists, err)
	}
}

func TestWorkspaceSettingsWhitelistAndReadOnlyEmptyState(t *testing.T) {
	h := testHost(t)
	ctx, call := context.Background(), webCall()
	for method, raw := range map[string]string{
		"day.snapshot":              `{"sync":true}`,
		"day.show":                  `{"day":"2026-09-02","rebuild":true}`,
		"day.ledger":                `{"day":"2026-09-02","source_path":"/private"}`,
		"settings.get":              `{"credentials":true}`,
		"settings.preferences.save": `{"owner_name":"test","text_model_base_url":"https://example.com"}`,
	} {
		if _, err := h.callWorkspaceSettings(ctx, call, method, json.RawMessage(raw)); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("%s accepted unknown input: %v", method, err)
		}
	}
	for _, method := range []string{"day.feedback", "day.data.delete", "day.source.set", "config.save", "config.credential.save", "guard.settings", "doctor.check"} {
		if _, err := h.callWorkspaceSettings(ctx, call, method, json.RawMessage(`{}`)); !errors.Is(err, application.ErrNotFound) {
			t.Fatalf("exposed %s: %v", method, err)
		}
	}
	snapshot, err := h.AIDaySnapshot(ctx, call, AIDayRangeInput{})
	if err != nil || snapshot.Indexed || len(snapshot.History) != 0 || snapshot.Atlas != nil {
		t.Fatalf("missing database snapshot=%+v err=%v", snapshot, err)
	}
	settings, err := h.WorkspaceSettings(ctx, call)
	if err != nil || settings.Sync.Indexed {
		t.Fatalf("missing database settings=%+v err=%v", settings, err)
	}
	entries, err := os.ReadDir(config.AtmDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read created files: %v, %v", entries, err)
	}
	for _, input := range []AIDayRangeInput{{From: "2026-02-30"}, {From: "2026-09-03", To: "2026-09-02"}, {From: "2025-01-01", To: "2026-01-02"}} {
		if _, err := h.AIDaySnapshot(ctx, call, input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted range %+v: %v", input, err)
		}
	}
	for _, input := range []AIDayLedgerInput{{Day: "not-a-day"}, {Day: "2026-09-02", Limit: 101}, {Day: "2026-09-02", Offset: -1}, {Day: "2026-09-02", Offset: 100001}} {
		if _, err := h.AIDayLedger(ctx, call, input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted ledger input %+v: %v", input, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.WorkspaceSettings(cancelled, call); !errors.Is(err, context.Canceled) {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

func TestWorkspaceSettingsRedactsCredentialsCommandsAndDiagnostics(t *testing.T) {
	h := testHost(t)
	beforeName, beforeSource, beforeURL, beforePrompt := config.TextModelName, config.TextModelSource, config.TextModelBaseURL, config.TodoRefinePrompt
	beforeQuota, beforeCollection := config.QuotaProviders, config.CollectionConnectors
	t.Cleanup(func() {
		config.TextModelName, config.TextModelSource, config.TextModelBaseURL, config.TodoRefinePrompt = beforeName, beforeSource, beforeURL, beforePrompt
		config.QuotaProviders, config.CollectionConnectors = beforeQuota, beforeCollection
	})
	const secret = "DO-NOT-SERIALIZE-THIS"
	config.TextModelName, config.TextModelSource = "test-model", "test-provider"
	config.TextModelBaseURL, config.TodoRefinePrompt = "https://user:"+secret+"@example.test/?token="+secret, "Refine using the owner’s project conventions"
	config.QuotaProviders = map[string]config.QuotaProviderConfig{"team-quota": {Command: "/private/" + secret, Args: []string{secret}}}
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{"team-chat": {Command: secret, LoginCommand: secret}}
	// Resident settings read the file revision and its matching effective
	// values; seed the persisted fixture rather than stale package globals.
	settingsFile, err := json.Marshal(config.FileConfig{TextModelName: config.TextModelName, TextModelSource: config.TextModelSource, TextModelBaseURL: config.TextModelBaseURL, TodoRefinePrompt: config.TodoRefinePrompt, QuotaProviders: config.QuotaProviders, CollectionConnectors: config.CollectionConnectors})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ConfigPath, settingsFile, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.CredentialsPath(), []byte(`{"deepseek_api_key":"`+secret+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	seedWorkspaceDay(t)
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	// Reopen without migration only to seed a diagnostic from an older sync.
	db, err = sql.Open("sqlite", config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_health(scope,last_status,last_error) VALUES ('all','failed',?)`, secret+config.AtmDir); err != nil {
		t.Fatal(err)
	}
	db.Close()
	settings, err := h.WorkspaceSettings(context.Background(), webCall())
	if err != nil || !settings.Model.CredentialConfigured || settings.Model.Name != "test-model" || !settings.Sync.HasError || settings.Sync.Status != "failed" || len(settings.Providers) != 2 {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
	encoded, _ := json.Marshal(settings)
	for _, forbidden := range []string{secret, config.AtmDir, "api_key", "command", "args", "login", "guard", "last_error"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("settings exposed %q: %s", forbidden, encoded)
		}
	}
	if err := os.Chmod(config.CredentialsPath(), 0644); err != nil {
		t.Fatal(err)
	}
	settings, err = h.WorkspaceSettings(context.Background(), webCall())
	if err != nil || settings.Model.CredentialStatus != "unavailable" {
		t.Fatalf("credential problem should have a redacted state: %+v, %v", settings.Model, err)
	}
}

func TestWorkspacePersonalPreferenceUpdatesOnlyNickname(t *testing.T) {
	h := testHost(t)
	beforeOwner := config.OwnerName
	t.Cleanup(func() { config.OwnerName = beforeOwner })
	if err := os.WriteFile(config.ConfigPath, []byte(`{"owner_name":"Before","future_private_setting":{"token":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, call := context.Background(), webCall()
	for _, name := range []string{"", "\n", strings.Repeat("字", 81), "name\nnext"} {
		if _, err := h.SaveWorkspacePreferences(ctx, call, WorkspacePreferencesInput{OwnerName: name}); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted nickname %q: %v", name, err)
		}
	}
	actor := call
	actor.Actor.Kind = application.ActorAgent
	if _, err := h.SaveWorkspacePreferences(ctx, actor, WorkspacePreferencesInput{OwnerName: "Agent"}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent changed owner preference: %v", err)
	}
	settings, err := h.SaveWorkspacePreferences(ctx, call, WorkspacePreferencesInput{OwnerName: "  新名字  "})
	if err != nil || settings.OwnerName != "新名字" {
		t.Fatalf("save=%+v err=%v", settings, err)
	}
	var raw map[string]json.RawMessage
	content, err := os.ReadFile(config.ConfigPath)
	if err != nil || json.Unmarshal(content, &raw) != nil || len(raw) != 2 || string(raw["owner_name"]) != `"新名字"` || !strings.Contains(string(raw["future_private_setting"]), "keep") {
		t.Fatalf("save changed other config fields: %s, %v", content, err)
	}
	if _, err := os.Stat(config.AtmDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("personal setting created a database: %v", err)
	}
}

func TestWorkspacePersonalPreferenceKeepsStartupDataDirectory(t *testing.T) {
	h := testHost(t)
	beforeOwner := config.OwnerName
	t.Cleanup(func() { config.OwnerName = beforeOwner })
	seed(t, card("t1", "original workspace", "open", "atm"))
	startupDir, startupDB, startupConfig := config.AtmDir, config.AtmDB, config.ConfigPath
	otherDir := t.TempDir()
	otherDB := filepath.Join(otherDir, "atm.db")
	db, err := sql.Open("sqlite", otherDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_version(version INTEGER); INSERT INTO schema_version VALUES(54); CREATE TABLE marker(value TEXT); INSERT INTO marker VALUES('another account')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	otherBefore, err := os.ReadFile(otherDB)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"owner_name": "Before", "data_dir": otherDir})
	if err := os.WriteFile(startupConfig, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, call := context.Background(), webCall()
	settings, err := h.SaveWorkspacePreferences(ctx, call, WorkspacePreferencesInput{OwnerName: "After"})
	if err != nil || settings.OwnerName != "After" || settings.Sync.SchemaVersion != store.SchemaVersion {
		t.Fatalf("save read the configured directory instead of startup directory: %+v, %v", settings, err)
	}
	if config.AtmDir != startupDir || config.AtmDB != startupDB || config.ConfigPath != startupConfig {
		t.Fatalf("save changed the resident data paths: %s, %s, %s", config.AtmDir, config.AtmDB, config.ConfigPath)
	}
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil || shown.Todo.Title != "original workspace" {
		t.Fatalf("todo read changed workspace: %+v, %v", shown, err)
	}
	created, err := h.CreateTodo(ctx, call, CreateInput{Title: "still original workspace", IdempotencyKey: "settings-directory-test"})
	if err != nil || created.Todo.ID == "" {
		t.Fatalf("todo write changed workspace: %+v, %v", created, err)
	}
	otherAfter, err := os.ReadFile(otherDB)
	if err != nil || string(otherAfter) != string(otherBefore) {
		t.Fatalf("saving a preference changed the other database: %v", err)
	}
	entries, err := os.ReadDir(otherDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("saving a preference wrote into the other data directory: %v, %v", entries, err)
	}
}

func TestWorkspaceBusinessSettingsRevisionPinnedPathsAndWriteOnlyCredentials(t *testing.T) {
	h := testHost(t)
	beforeName, beforeURL, beforePrompt := config.OwnerName, config.TextModelBaseURL, config.TodoRefinePrompt
	beforeEnabled, beforeInterval := config.CollectionEnabled, config.CollectionIntervalMinutes
	t.Cleanup(func() {
		config.OwnerName, config.TextModelBaseURL, config.TodoRefinePrompt = beforeName, beforeURL, beforePrompt
		config.CollectionEnabled, config.CollectionIntervalMinutes = beforeEnabled, beforeInterval
	})
	other := t.TempDir()
	raw, _ := json.Marshal(map[string]any{"owner_name": "Initial", "data_dir": other, "future_private_setting": map[string]string{"token": "preserve"}})
	if err := os.WriteFile(config.ConfigPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, call := context.Background(), webCall()
	snapshot, err := h.WorkspaceSettings(ctx, call)
	if err != nil || len(snapshot.Revision) != 64 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	name, urlValue, prompt := "Changed", "https://models.example.test/v1", "Keep project details"
	enabled, interval := true, 15
	input := WorkspaceBusinessInput{Revision: snapshot.Revision, SettingsPatch: config.SettingsPatch{OwnerName: &name, TextModelBaseURL: &urlValue, TodoRefinePrompt: &prompt, CollectionEnabled: &enabled, CollectionIntervalMinutes: &interval}}
	saved, err := h.SaveWorkspaceBusiness(ctx, call, input)
	if err != nil || saved.OwnerName != name || saved.Model.BaseURL != urlValue || saved.Preferences.TodoRefinePrompt != prompt || !saved.Preferences.CollectionEnabled || saved.Preferences.CollectionIntervalMinutes != interval {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	if config.AtmDir != h.dataDir || config.AtmDB != h.databasePath || config.ConfigPath != h.configPath {
		t.Fatal("settings redirected authenticated workspace")
	}
	if _, err := h.SaveWorkspaceBusiness(ctx, call, input); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("stale settings accepted: %v", err)
	}
	agent := call
	agent.Actor.Kind = application.ActorAgent
	if _, err := h.SaveWorkspaceBusiness(ctx, agent, input); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent changed settings: %v", err)
	}
	for _, body := range []string{`{"revision":"` + saved.Revision + `","data_dir":"/private"}`, `{"revision":"` + saved.Revision + `","collection_connectors":{"x":{"command":"sh"}}}`, `{"revision":"` + saved.Revision + `","guard":{"enabled":false}}`} {
		if _, err := h.callWorkspaceSettings(ctx, call, "settings.business.save", json.RawMessage(body)); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("unsafe field accepted: %s %v", body, err)
		}
	}
	const secret = "key-DO-NOT-ECHO"
	status, err := h.saveWorkspaceCredential(ctx, call, &config.CredentialSaveInput{APIKey: secret})
	if err != nil || !status.Configured {
		t.Fatalf("credential=%+v %v", status, err)
	}
	saved, err = h.WorkspaceSettings(ctx, call)
	encoded, _ := json.Marshal(saved)
	if err != nil || !saved.Model.CredentialConfigured || strings.Contains(string(encoded), secret) {
		t.Fatalf("credential leaked or absent: %s %v", encoded, err)
	}
	if _, err := os.Stat(filepath.Join(other, config.CredentialsFileName)); !os.IsNotExist(err) {
		t.Fatalf("credential wrote to unpinned data dir: %v", err)
	}
	if _, err := h.saveWorkspaceCredential(ctx, call, nil); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(config.ConfigPath)
	if !strings.Contains(string(content), "preserve") {
		t.Fatal("unrelated config lost")
	}
	if entries, _ := os.ReadDir(other); len(entries) != 0 {
		t.Fatalf("other directory mutated: %+v", entries)
	}
}
