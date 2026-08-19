package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withTempConfigHome(t *testing.T) string {
	t.Helper()
	oldHome := Home
	oldCodexSessions := CodexSessions
	oldClaudeProjects := ClaudeProjects
	oldCopilotWorkspaces := CopilotWorkspaces
	oldQoderDB, oldQoderCLIProjects, oldQoderWorkDB := QoderDB, QoderCLIProjects, QoderWorkDB
	oldGrokSessions := GrokSessions
	oldAtmDir, oldAtmDB, oldConfigPath := AtmDir, AtmDB, ConfigPath
	oldCST := Loc
	oldPricing, oldSubscriptions := Pricing, Subscriptions
	oldProjectAliases := ProjectAliases
	oldGrokLiveQuota := GrokLiveQuota
	oldCollectionEnabled := CollectionEnabled
	oldCollectionInterval := CollectionIntervalMinutes
	oldCollectionLookback := CollectionLookbackMinutes
	oldTextModelBaseURL, oldTextModelName := TextModelBaseURL, TextModelName
	oldTextModelSource, oldTodoRefinePrompt := TextModelSource, TodoRefinePrompt
	oldCollectionConnectors := CollectionConnectors
	oldQuotaProviders := QuotaProviders
	oldTodoRefineOnAdd := TodoRefineOnAdd
	oldGuard := Guard

	home := t.TempDir()
	Home = home
	CodexSessions = filepath.Join(Home, ".codex", "sessions")
	ClaudeProjects = filepath.Join(Home, ".claude", "projects")
	CopilotWorkspaces = filepath.Join(Home, "Library", "Application Support", "Code", "User", "workspaceStorage")
	QoderDB = filepath.Join(Home, "Library", "Application Support", "Qoder", "SharedClientCache", "cache", "db", "local.db")
	QoderCLIProjects = filepath.Join(Home, ".qoder", "projects")
	QoderWorkDB = filepath.Join(Home, "Library", "Application Support", "QoderWork", "data", "agents.db")
	GrokSessions = filepath.Join(Home, ".grok", "sessions")
	AtmDir = filepath.Join(Home, ".atm")
	AtmDB = filepath.Join(AtmDir, "atm.db")
	ConfigPath = filepath.Join(AtmDir, "config.json")
	Loc = time.FixedZone("CST", 8*3600)
	Pricing = nil
	Subscriptions = nil
	ProjectAliases = nil
	GrokLiveQuota = false
	CollectionEnabled = false
	CollectionIntervalMinutes = 5
	CollectionLookbackMinutes = 60
	TextModelBaseURL = "https://api.deepseek.com"
	TextModelName = "deepseek-v4-flash"
	TextModelSource = "deepseek"
	TodoRefinePrompt = DefaultTodoRefinePrompt
	CollectionConnectors = nil
	QuotaProviders = nil
	TodoRefineOnAdd = false
	Guard = GuardConfig{}

	t.Cleanup(func() {
		Home = oldHome
		CodexSessions = oldCodexSessions
		ClaudeProjects = oldClaudeProjects
		CopilotWorkspaces = oldCopilotWorkspaces
		QoderDB, QoderCLIProjects, QoderWorkDB = oldQoderDB, oldQoderCLIProjects, oldQoderWorkDB
		GrokSessions = oldGrokSessions
		AtmDir, AtmDB, ConfigPath = oldAtmDir, oldAtmDB, oldConfigPath
		Loc = oldCST
		Pricing, Subscriptions = oldPricing, oldSubscriptions
		ProjectAliases = oldProjectAliases
		GrokLiveQuota = oldGrokLiveQuota
		CollectionEnabled = oldCollectionEnabled
		CollectionIntervalMinutes = oldCollectionInterval
		CollectionLookbackMinutes = oldCollectionLookback
		TextModelBaseURL, TextModelName = oldTextModelBaseURL, oldTextModelName
		TextModelSource, TodoRefinePrompt = oldTextModelSource, oldTodoRefinePrompt
		CollectionConnectors = oldCollectionConnectors
		QuotaProviders = oldQuotaProviders
		TodoRefineOnAdd = oldTodoRefineOnAdd
		Guard = oldGuard
	})
	return home
}

func TestInitAndLoadConfig(t *testing.T) {
	home := withTempConfigHome(t)
	if err := InitConfig(); err != nil {
		t.Fatalf("InitConfig: %v", err)
	}
	if _, err := os.Stat(ConfigPath); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if info, err := os.Stat(AtmDir); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("ATM data directory mode = %v, %v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(ConfigPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config file mode = %v, %v", info.Mode().Perm(), err)
	}
	if err := InitConfig(); err == nil {
		t.Fatal("InitConfig should fail when config already exists")
	}

	custom := `{
  "timezone": "UTC",
  "claude_projects": "~/claude-projects",
  "codex_sessions": "~/codex-sessions",
  "copilot_workspaces": "~/copilot-workspaces",
  "qoder_db": "~/qoder/local.db",
  "qodercli_projects": "~/qodercli-projects",
  "qoderwork_db": "~/qoderwork/agents.db",
  "grok_sessions": "~/grok-sessions",
  "collection_enabled": true,
  "collection_interval_minutes": 7,
  "collection_lookback_minutes": 90,
  "text_model_base_url": "https://deepseek.example.test/v1/",
  "text_model_name": "deepseek-test",
  "text_model_source": "company gateway",
  "todo_refine_prompt": "Prefer acceptance criteria that name observable behavior.",
  "collection_connectors": {"slack": {"command": "~/bin/atm-connector-slack", "args": ["--workspace", "example"], "timeout_seconds": 30}},
  "quota_providers": {"example": {"command": "~/bin/atm-quota-example", "args": ["--profile", "work"], "timeout_seconds": 8, "visible_metrics": ["amount"]}},
  "data_dir": "~/atm-data",
  "pricing": {"test-model": [1, 2, 3, 4]},
  "subscriptions": {"codex": 20},
  "project_aliases": {"atm-worktree": "atm"}
}`
	if err := os.WriteFile(ConfigPath, []byte(custom), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	LoadConfig()

	if Loc.String() != "UTC" {
		t.Fatalf("timezone = %s", Loc)
	}
	if ClaudeProjects != filepath.Join(home, "claude-projects") ||
		CodexSessions != filepath.Join(home, "codex-sessions") ||
		CopilotWorkspaces != filepath.Join(home, "copilot-workspaces") ||
		QoderDB != filepath.Join(home, "qoder", "local.db") ||
		QoderCLIProjects != filepath.Join(home, "qodercli-projects") ||
		QoderWorkDB != filepath.Join(home, "qoderwork", "agents.db") ||
		GrokSessions != filepath.Join(home, "grok-sessions") ||
		AtmDir != filepath.Join(home, "atm-data") ||
		AtmDB != filepath.Join(home, "atm-data", "atm.db") {
		t.Fatalf("paths not expanded: claude=%s codex=%s copilot=%s qoder=%s qodercli=%s qoderwork=%s grok=%s data=%s db=%s", ClaudeProjects, CodexSessions, CopilotWorkspaces, QoderDB, QoderCLIProjects, QoderWorkDB, GrokSessions, AtmDir, AtmDB)
	}
	if CanonicalProject("ATM-WORKTREE") != "atm" {
		t.Fatalf("project aliases = %#v", ProjectAliases)
	}
	if Pricing["test-model"] != [4]float64{1, 2, 3, 4} || Subscriptions["codex"] != 20 {
		t.Fatalf("pricing = %#v, subscriptions = %#v", Pricing, Subscriptions)
	}
	if !CollectionEnabled || CollectionIntervalMinutes != 7 || CollectionLookbackMinutes != 90 {
		t.Fatalf("collection config = enabled:%v interval:%d lookback:%d",
			CollectionEnabled, CollectionIntervalMinutes, CollectionLookbackMinutes)
	}
	if TextModelBaseURL != "https://deepseek.example.test/v1" || TextModelName != "deepseek-test" ||
		TextModelSource != "company gateway" || !strings.Contains(TodoRefinePrompt, "observable behavior") {
		t.Fatalf("text model config = base:%s model:%s source:%s prompt:%q",
			TextModelBaseURL, TextModelName, TextModelSource, TodoRefinePrompt)
	}
	if connector := CollectionConnectors["slack"]; connector.Command != "~/bin/atm-connector-slack" ||
		len(connector.Args) != 2 || connector.TimeoutSeconds != 30 {
		t.Fatalf("collection connectors = %#v", CollectionConnectors)
	}
	if provider := QuotaProviders["example"]; provider.Command != "~/bin/atm-quota-example" ||
		len(provider.Args) != 2 || provider.TimeoutSeconds != 8 ||
		len(provider.VisibleMetrics) != 1 || provider.VisibleMetrics[0] != "amount" {
		t.Fatalf("quota providers = %#v", QuotaProviders)
	}
	if shown := ShowConfig(); shown == "" || !containsAll(shown, "UTC", "atm-data", "test-model", "subscriptions", "codex") {
		t.Fatalf("ShowConfig = %q", shown)
	}
}

// Classification no longer launches a CLI, but the sessions those runs left on
// disk still have to be recognised so the parsers keep skipping them.
func TestIsCollectionModelWorkdirMatchesEncodedPaths(t *testing.T) {
	if !IsCollectionModelWorkdir("/private/var/folders/kq/T/" + CollectionModelWorkdirPrefix + "2291227821") {
		t.Fatal("scratch path not recognised")
	}
	if !IsCollectionModelWorkdir("%2Fprivate%2Fvar%2FT%2F" + CollectionModelWorkdirPrefix + "42") {
		t.Fatal("URL-encoded scratch path not recognised")
	}
	if IsCollectionModelWorkdir("/Users/mj/mox/atm") {
		t.Fatal("a real project must not look like a scratch directory")
	}
}

// A tool that is not on PATH is only findable again through this record. Without
// it, `atm guard install dws --bin ...` would succeed and then be invisible to
// status and doctor — losing the checks for a shim that was overwritten or is
// being bypassed, for exactly the tool most worth checking.
func TestSaveGuardToolBinRemembersWhereAGateWentAndKeepsEverythingElse(t *testing.T) {
	withTempConfigHome(t)
	existing := `{"timezone":"UTC","future_setting":{"nested":true},` +
		`"guard":{"wait_seconds":30,"tools":{"a1":{"rules":[{"id":"keep-me","path":["repo"]}]}}}}`
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	if err := SaveGuardToolBin("dws", "/Users/x/.qoderwork/bin/dws"); err != nil {
		t.Fatalf("SaveGuardToolBin: %v", err)
	}
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var reloaded FileConfig
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("result is not valid config JSON: %v\n%s", err, data)
	}
	if got := reloaded.Guard.Tools["dws"].Bin; got != "/Users/x/.qoderwork/bin/dws" {
		t.Fatalf("dws bin = %q", got)
	}
	// Another tool's rules, the guard's own tuning, and a field this build does not
	// know about must all survive.
	if len(reloaded.Guard.Tools["a1"].Rules) != 1 ||
		reloaded.Guard.Tools["a1"].Rules[0].ID != "keep-me" {
		t.Fatalf("a1 rules lost: %+v", reloaded.Guard.Tools["a1"])
	}
	if reloaded.Guard.WaitSeconds != 30 {
		t.Fatalf("wait_seconds = %d, want 30", reloaded.Guard.WaitSeconds)
	}
	if !strings.Contains(string(data), `"future_setting"`) {
		t.Fatalf("unknown field dropped:\n%s", data)
	}

	// Recording a second tool must not displace the first.
	if err := SaveGuardToolBin("a1", "/Users/x/.local/bin/a1"); err != nil {
		t.Fatalf("second save: %v", err)
	}
	data, _ = os.ReadFile(ConfigPath)
	reloaded = FileConfig{}
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatal(err)
	}
	if reloaded.Guard.Tools["dws"].Bin == "" || reloaded.Guard.Tools["a1"].Bin == "" {
		t.Fatalf("tools clobbered each other: %+v", reloaded.Guard.Tools)
	}
	if len(reloaded.Guard.Tools["a1"].Rules) != 1 {
		t.Fatalf("recording a1's bin dropped its rules: %+v", reloaded.Guard.Tools["a1"])
	}
}

func TestSetConfigValueWritesAndPreservesUnknownFields(t *testing.T) {
	withTempConfigHome(t)
	// Pre-existing config with a field this build knows nothing about: a
	// round-trip through SetConfigValue must keep it byte-for-byte usable.
	existing := `{"timezone":"UTC","future_setting":{"nested":true},"grok_sessions":"~/grok-sessions"}`
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := SetConfigValue("grok_live_quota", true); err != nil {
		t.Fatalf("SetConfigValue: %v", err)
	}
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"grok_live_quota": true`, `"future_setting"`, `"nested": true`, `"timezone": "UTC"`, `"grok_sessions"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("config after set is missing %q:\n%s", want, data)
		}
	}
	if info, err := os.Stat(ConfigPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("updated config mode = %v, %v", info.Mode().Perm(), err)
	}

	// Flipping back to false must overwrite, not append.
	if err := SetConfigValue("grok_live_quota", false); err != nil {
		t.Fatalf("SetConfigValue(false): %v", err)
	}
	LoadConfig()
	if GrokLiveQuota {
		t.Fatal("GrokLiveQuota should be false after set false + reload")
	}

	// No config file at all: set creates one.
	if err := os.Remove(ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := SetConfigValue("grok_live_quota", true); err != nil {
		t.Fatalf("SetConfigValue on missing file: %v", err)
	}
	LoadConfig()
	if !GrokLiveQuota {
		t.Fatal("GrokLiveQuota should be true after set true + reload")
	}
}

func TestGrokLiveQuotaEnvOverridesConfig(t *testing.T) {
	withTempConfigHome(t)
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"grok_live_quota":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ATM_GROK_LIVE_QUOTA", "0")
	LoadConfig()
	if GrokLiveQuota {
		t.Fatal("env=0 must force live quota off even when config says true")
	}

	t.Setenv("ATM_GROK_LIVE_QUOTA", "1")
	if err := os.WriteFile(ConfigPath, []byte(`{"grok_live_quota":false}`), 0644); err != nil {
		t.Fatal(err)
	}
	LoadConfig()
	if !GrokLiveQuota {
		t.Fatal("env=1 must force live quota on even when config says false")
	}

	// Unset / unrecognized values leave the config value in charge.
	t.Setenv("ATM_GROK_LIVE_QUOTA", "maybe")
	LoadConfig()
	if GrokLiveQuota {
		t.Fatal("unrecognized env value must fall back to config (false)")
	}
}

func TestTodoRefineOnAddDefaultsOffAndCanBeEnabled(t *testing.T) {
	withTempConfigHome(t)
	LoadConfig()
	if TodoRefineOnAdd {
		t.Fatal("refining on add is opt-in: 优化 is an action, not a side effect of filing")
	}
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"todo_refine_on_add":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	LoadConfig()
	if !TodoRefineOnAdd {
		t.Fatal("explicit true must turn desktop auto-refine on")
	}
	t.Setenv("ATM_TODO_REFINE_ON_ADD", "0")
	LoadConfig()
	if TodoRefineOnAdd {
		t.Fatal("env must force auto-refine off")
	}
}

func TestTodoRefinePromptDefaultsConservativeAndBlankRestoresIt(t *testing.T) {
	withTempConfigHome(t)
	for _, want := range []string{"默认将任务判定为 simple", "连续实施阶段", "分别验收和关闭"} {
		if !strings.Contains(TodoRefinePrompt, want) {
			t.Fatalf("default refine prompt missing %q:\n%s", want, TodoRefinePrompt)
		}
	}
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"todo_refine_prompt":""}`), 0644); err != nil {
		t.Fatal(err)
	}
	TodoRefinePrompt = "custom policy"
	LoadConfig()
	if TodoRefinePrompt != DefaultTodoRefinePrompt {
		t.Fatalf("blank config should restore default, got %q", TodoRefinePrompt)
	}
}

func TestProjectFromPathUsesGitOriginAndAliases(t *testing.T) {
	withTempConfigHome(t)
	root := filepath.Join(t.TempDir(), "temporary-checkout-name")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	gitConfig := "[remote \"origin\"]\n\turl = git@github.com:zane-byte-dev/atm.git\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(gitConfig), 0644); err != nil {
		t.Fatal(err)
	}
	ProjectAliases = map[string]string{"atm": "agent-task-manager"}
	if got := ProjectFromPath(filepath.Join(root, "internal", "cmd")); got != "agent-task-manager" {
		t.Fatalf("ProjectFromPath = %q", got)
	}
}

func TestDateRangeTailJSONLAndHelpers(t *testing.T) {
	withTempConfigHome(t)
	start, end, err := DateRange("2026-06-27")
	if err != nil {
		t.Fatalf("DateRange: %v", err)
	}
	if start.Format("2006-01-02") != "2026-06-27" || end.Sub(start) != 24*time.Hour {
		t.Fatalf("range = %s %s", start, end)
	}
	if TsToCST(1_783_200_000_000).Unix() != 1_783_200_000 {
		t.Fatalf("TsToCST milliseconds conversion failed")
	}

	fp := filepath.Join(t.TempDir(), "tail.jsonl")
	data := "{\"a\":\"first\"}\nnot json\n{\"a\":\"second\"}\n{\"a\":\"third\"}\n"
	if err := os.WriteFile(fp, []byte(data), 0644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	records := TailJSONL(fp, 2)
	if len(records) != 2 || GetStr(records[0], "a") != "second" || GetStr(records[1], "a") != "third" {
		t.Fatalf("records = %#v", records)
	}

	msg := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "tool_use", "name": "Read"},
			map[string]any{"type": "text", "text": "world"},
		},
	}
	if got := ExtractText(msg); got != "hello\n\nworld" {
		t.Fatalf("ExtractText = %q", got)
	}
	if NormalizeAgent("GitHub-Copilot") != "copilot" || NormalizeAgent("qoder-cli") != "qodercli" || NormalizeAgent("qoder-work") != "qoderwork" || NormalizeAgent("grok-build") != "grokbuild" || NormalizeAgent("nope") != "" {
		t.Fatalf("NormalizeAgent failed")
	}
	if v, ok := GetFloat(map[string]any{"n": 1.5}, "n"); !ok || v != 1.5 {
		t.Fatalf("GetFloat = %f %v", v, ok)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
