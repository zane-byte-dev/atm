package config

import (
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
	oldCollectionModel := CollectionModelCommand
	oldTextModelBaseURL, oldTextModelName := TextModelBaseURL, TextModelName
	oldCollectionModelRunners := CollectionModelRunners
	oldCollectionConnectors := CollectionConnectors
	oldQuotaProviders := QuotaProviders
	oldTodoRefineOnAdd := TodoRefineOnAdd

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
	CollectionModelCommand = "codex"
	TextModelBaseURL = "https://api.deepseek.com"
	TextModelName = "deepseek-v4-flash"
	CollectionModelRunners = nil
	CollectionConnectors = nil
	QuotaProviders = nil
	TodoRefineOnAdd = true

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
		CollectionModelCommand = oldCollectionModel
		TextModelBaseURL, TextModelName = oldTextModelBaseURL, oldTextModelName
		CollectionModelRunners = oldCollectionModelRunners
		CollectionConnectors = oldCollectionConnectors
		QuotaProviders = oldQuotaProviders
		TodoRefineOnAdd = oldTodoRefineOnAdd
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
  "collection_model_command": "rule",
  "text_model_base_url": "https://deepseek.example.test/v1/",
  "text_model_name": "deepseek-test",
  "collection_model_runners": {"house": {"command": "~/bin/house-cli", "args": ["--schema", "{{schema_path}}"], "output_field": "result", "timeout_seconds": 60}},
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
	if !CollectionEnabled || CollectionIntervalMinutes != 7 || CollectionLookbackMinutes != 90 ||
		CollectionModelCommand != "rule" {
		t.Fatalf("collection config = enabled:%v interval:%d lookback:%d model:%s",
			CollectionEnabled, CollectionIntervalMinutes, CollectionLookbackMinutes,
			CollectionModelCommand)
	}
	if TextModelBaseURL != "https://deepseek.example.test/v1" || TextModelName != "deepseek-test" {
		t.Fatalf("text model config = base:%s model:%s", TextModelBaseURL, TextModelName)
	}
	if runner := CollectionModelRunners["house"]; runner.Command != "~/bin/house-cli" ||
		len(runner.Args) != 2 || runner.OutputField != "result" || runner.TimeoutSeconds != 60 {
		t.Fatalf("collection model runners = %#v", CollectionModelRunners)
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

func TestCollectionModelCandidatesSplitsTheChain(t *testing.T) {
	withTempConfigHome(t)
	got := CollectionModelCandidates(" grok --effort low , codex ,, ")
	if len(got) != 2 || got[0] != "grok --effort low" || got[1] != "codex" {
		t.Fatalf("candidates = %#v", got)
	}
	// An unset command still resolves to the configured default chain.
	CollectionModelCommand = "grok,codex"
	if got := CollectionModelCandidates(""); len(got) != 2 || got[0] != "grok" {
		t.Fatalf("default candidates = %#v", got)
	}
}

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

func TestTodoRefineOnAddDefaultsOnAndCanBeDisabled(t *testing.T) {
	withTempConfigHome(t)
	if !TodoRefineOnAdd {
		t.Fatal("todo refine after add is on unless configured off")
	}
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"todo_refine_on_add":false}`), 0644); err != nil {
		t.Fatal(err)
	}
	LoadConfig()
	if TodoRefineOnAdd {
		t.Fatal("explicit false must turn desktop auto-refine off")
	}
	t.Setenv("ATM_TODO_REFINE_ON_ADD", "1")
	LoadConfig()
	if !TodoRefineOnAdd {
		t.Fatal("env must force auto-refine on")
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
