package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	Home              = os.Getenv("HOME")
	CodexSessions     = filepath.Join(Home, ".codex", "sessions")
	ClaudeProjects    = filepath.Join(Home, ".claude", "projects")
	PiSessions        = filepath.Join(Home, ".pi", "agent", "sessions")
	CopilotWorkspaces = defaultCopilotWorkspaces()
	QoderDB           = defaultQoderDB()
	QoderCLIProjects  = filepath.Join(Home, ".qoder", "projects")
	QoderWorkDB       = defaultQoderWorkDB()
	GrokSessions      = filepath.Join(Home, ".grok", "sessions")
	// AntigravityDir is the Antigravity IDE's own data directory, which is not
	// beside its Electron profile: the app stores chrome state under
	// ~/Library/Application Support/Antigravity but every transcript, summary and
	// artifact under ~/.gemini/antigravity.
	//
	// Only the IDE is covered. ~/.gemini/antigravity-cli holds the CLI's
	// conversations in the same format, but on a real install it accounts for a
	// handful of model calls against the IDE's hundreds, and it keeps a live -wal
	// that the immutable read below cannot see. A second agent for that is more
	// surface than the numbers justify.
	AntigravityDir = filepath.Join(Home, ".gemini", "antigravity")
	AtmDir         = filepath.Join(Home, ".atm")
	AtmDB          = filepath.Join(Home, ".atm", "atm.db")
	Loc            = time.FixedZone("CST", 8*3600)
	ConfigPath     = filepath.Join(Home, ".atm", "config.json")
)

func defaultCopilotWorkspaces() string {
	if runtime.GOOS == "linux" {
		return filepath.Join(Home, ".config", "Code", "User", "workspaceStorage")
	}
	return filepath.Join(Home, "Library", "Application Support", "Code", "User", "workspaceStorage")
}

func defaultQoderDB() string {
	if runtime.GOOS == "linux" {
		return filepath.Join(Home, ".config", "Qoder", "SharedClientCache", "cache", "db", "local.db")
	}
	return filepath.Join(Home, "Library", "Application Support", "Qoder", "SharedClientCache", "cache", "db", "local.db")
}

func defaultQoderWorkDB() string {
	if runtime.GOOS == "linux" {
		return filepath.Join(Home, ".config", "QoderWork", "data", "agents.db")
	}
	return filepath.Join(Home, "Library", "Application Support", "QoderWork", "data", "agents.db")
}

// AntigravityConversations is where one SQLite database per conversation lives.
// It is derived rather than stored so that overriding AntigravityDir moves both
// this and the summary index together.
func AntigravityConversations() string {
	return filepath.Join(AntigravityDir, "conversations")
}

// AntigravitySummaries is the index that maps a conversation id to its title and
// workspace folder. The conversation databases themselves carry neither, so
// without this file a session has no name and no project.
func AntigravitySummaries() string {
	return filepath.Join(AntigravityDir, "agyhub_summaries_proto.pb")
}

var (
	Pricing        map[string][4]float64
	Subscriptions  map[string]float64
	ProjectAliases map[string]string
	// OwnerName is how the single human behind this installation wants to be
	// named. Todos record the human creator as "me" — a stable token that stays
	// correct if the nickname changes — and only the display layer reads this.
	// Empty means "我", which every ATM user can read without configuring
	// anything.
	OwnerName = ""
	// GrokLiveQuota gates the network call to the Grok billing API. Off by
	// default so `atm quota` stays local-only unless the user opts in via
	// config or the ATM_GROK_LIVE_QUOTA env override.
	GrokLiveQuota = false
	// Automatic collection reads private messaging data and creates work items,
	// so it is opt-in even though the implementation remains local-first.
	CollectionEnabled         = false
	CollectionIntervalMinutes = 5
	CollectionLookbackMinutes = 60
	// Synced chat is kept for this many days; 0 keeps it forever. Reading a
	// conversation stores it so it can be searched and read offline, and that
	// archive would otherwise grow for as long as the sources stay enabled.
	CollectionMessageRetentionDays = 90
	// Default knowledge destination when an explicitly saved collection
	// conclusion (or optional manual digest) names no collection of its own.
	CollectionDigestCollection = "inbox"
	// How long a due digest waits for more insights before spending a model call.
	// Background callers poll far more often than a day's chat changes, and each
	// regeneration rewrites the same document, so without this the same day would
	// be summarised over and over.
	CollectionDigestIntervalMinutes = 60
	// CollectionConnectors registers optional executable connectors. Each
	// connector speaks ATM's versioned JSON protocol over stdin/stdout, so a
	// private or third-party integration does not need to be linked into ATM.
	CollectionConnectors map[string]CollectionConnectorConfig
	// QuotaProviders registers optional executable quota providers. Providers
	// keep service credentials and private endpoints outside ATM while returning
	// versioned, provider-neutral cards for the CLI and App.
	QuotaProviders map[string]QuotaProviderConfig
	// Guard holds the outbound action gate's overrides. The built-in rules apply
	// with no config at all; this only widens or retunes them.
	Guard GuardConfig
	// TextModelBaseURL and TextModelName configure ATM's narrow built-in text
	// service. Credentials deliberately stay out of config.json: the CLI reads
	// ~/.atm/credentials.json, with DEEPSEEK_API_KEY as an ephemeral override.
	TextModelBaseURL = "https://api.deepseek.com"
	TextModelName    = "deepseek-v4-flash"
	// TextModelSource is the short, human-facing provenance label persisted
	// with model-produced Todo analysis. It is explicit rather than inferred
	// from the endpoint: an OpenAI-compatible gateway URL does not identify the
	// model that actually answered.
	TextModelSource = "deepseek"
	// TodoRefinePrompt is editable policy appended to ATM's fixed safety and
	// output-shape prompt. The default is deliberately conservative: phases of
	// one feature stay in one Todo instead of becoming analysis/design/test
	// checklist children.
	TodoRefinePrompt = DefaultTodoRefinePrompt
	// TodoRefineOnAdd runs one schema-constrained pass right after a human
	// files a todo in the App: polish the card and, when the work is
	// independently trackable, split it. CLI `todo add` never does this unless
	// `--refine` is passed — agents already write structured cards and a
	// network model call would break `id=$(atm todo add ...)`.
	//
	// Default off. Refining on add rewrites the card before anyone has looked
	// at it, and a second pass on an already-structured card returns the same
	// text — so the automatic one was in practice the only one, and it landed
	// unasked. 优化 is a detail-page action now; turn this on to get it on
	// every new todo as well.
	TodoRefineOnAdd = false
)

const DefaultTodoRefinePrompt = `任务拆分默认采用保守策略：
- 默认将任务判定为 simple。
- 只有至少存在两个可独立交付、独立验收、独立关闭的成果时，才判定为 complex 并创建子任务。
- “分析、设计、编码、测试、集成、发布”等同一功能的连续实施阶段不是独立成果，不得仅因存在多个步骤就拆成子任务。
- 如果同一个实现者可以在一次连续工作会话中完成全部验收，保持 simple，把必要步骤写入 plan，不创建子任务。
- 创建子任务时，reason 必须说明这些成果为何能够分别验收和关闭；无法说明则不要拆分。
- 信息不足时保留原始事实，并在约束中明确标注待确认内容，不要自行补全。`

// CollectionModelWorkdirPrefix named the scratch directory ATM gave each run
// back when classification drove an Agent CLI. Classification is now a plain
// HTTP call that creates no directory and no session, but the sessions those
// runs left behind are still on disk, so the parsers still have to recognise and
// skip them: without this they would surface as real ATM sessions in throwaway
// projects.
const CollectionModelWorkdirPrefix = "atm-collection-model-"

// IsCollectionModelWorkdir reports whether a path (or a CLI's URL-encoded
// rendering of one) points inside one of those leftover scratch directories.
func IsCollectionModelWorkdir(path string) bool {
	return strings.Contains(path, CollectionModelWorkdirPrefix)
}

type CollectionConnectorConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type QuotaProviderConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	// VisibleMetrics optionally limits provider cards to the listed metric IDs.
	// An absent or empty list preserves the provider's complete response.
	VisibleMetrics []string `json:"visible_metrics,omitempty"`
}

// GuardJSONArg reads a preview value out of a positional argument that is itself
// JSON, which is how aone-kit carries a tool's parameters.
type GuardJSONArg struct {
	// Index is 1-based over the command's leading non-flag tokens.
	Index int `json:"index"`
	// Path is dotted, e.g. "fieldName_0.content".
	Path string `json:"path"`
}

// GuardExtractor locates one piece of a gated command's preview — who it reaches,
// and what it says. Extraction is presentation only: a rule that matches is
// always gated, whether or not a preview could be built from it.
type GuardExtractor struct {
	// Flags are alternatives, tried in order; both --flag value and --flag=value
	// are read.
	Flags []string `json:"flags,omitempty"`
	// Positional is 1-based over the command's leading non-flag tokens.
	Positional int           `json:"positional,omitempty"`
	JSONArg    *GuardJSONArg `json:"json_arg,omitempty"`
}

// GuardRule describes one command shape that must not run unreviewed.
//
// Path and ArgvPattern are both evaluated against only the *leading* run of
// non-flag tokens, never the whole command line. That is what stops a message
// body from choosing its own rule: an agent sending
// `--text "ata::message-ding-talk-send-to-webhook"` must not have that text
// gate — or worse, mis-preview — an unrelated read.
type GuardRule struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Path must appear as a consecutive run of subcommand tokens, e.g.
	// ["chat","message","send"].
	Path []string `json:"path,omitempty"`
	// ArgvPattern is matched against single tokens and should be anchored, for
	// commands whose dangerous action is encoded inside one argument.
	ArgvPattern string         `json:"argv_pattern,omitempty"`
	Target      GuardExtractor `json:"target,omitzero"`
	Title       GuardExtractor `json:"title,omitzero"`
	Body        GuardExtractor `json:"body,omitzero"`
	// Enabled is a pointer so "absent" (on, the default) is distinct from an
	// explicit false. Switching a built-in rule off is the only way to stop gating
	// one action of a tool without giving up the gate on that tool entirely.
	Enabled *bool `json:"enabled,omitempty"`
}

// IsEnabled resolves the pointer. A rule with nothing said about it is on: the
// built-ins exist because those actions are worth stopping.
func (r GuardRule) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

// HasMatcher reports whether the rule says which commands it is about. A rule
// without one is a patch onto a built-in of the same id, not a rule in its own
// right — that is how "switch this built-in off" is expressed without having to
// restate its matcher and risk drifting from it.
func (r GuardRule) HasMatcher() bool {
	return len(r.Path) > 0 || strings.TrimSpace(r.ArgvPattern) != ""
}

type GuardToolConfig struct {
	// Bin is the path the shim was installed at. Empty means exec.LookPath.
	Bin string `json:"bin,omitempty"`
	// Rules replace the built-in rules of the same id and add new ones. Setting
	// this to an empty list does not disable the built-ins; remove the tool or
	// uninstall its shim for that.
	Rules []GuardRule `json:"rules,omitempty"`
}

// GuardConfig configures the outbound action gate. The wait is agent-scale and
// the expiry is human-scale, deliberately: the wait only has to catch a user who
// is already at the desk, and a request outliving it is the designed path rather
// than a failure.
type GuardConfig struct {
	WaitSeconds         int                        `json:"wait_seconds,omitempty"`
	ExpireMinutes       int                        `json:"expire_minutes,omitempty"`
	DenyCooldownMinutes int                        `json:"deny_cooldown_minutes,omitempty"`
	Tools               map[string]GuardToolConfig `json:"tools,omitempty"`
}

type FileConfig struct {
	Timezone          string `json:"timezone,omitempty"`
	OwnerName         string `json:"owner_name,omitempty"`
	ClaudeProjects    string `json:"claude_projects,omitempty"`
	CodexSessions     string `json:"codex_sessions,omitempty"`
	PiSessions        string `json:"pi_sessions,omitempty"`
	CopilotWorkspaces string `json:"copilot_workspaces,omitempty"`
	QoderDB           string `json:"qoder_db,omitempty"`
	QoderCLIProjects  string `json:"qodercli_projects,omitempty"`
	QoderWorkDB       string `json:"qoderwork_db,omitempty"`
	GrokSessions      string `json:"grok_sessions,omitempty"`
	AntigravityDir    string `json:"antigravity_dir,omitempty"`
	// Pointer so "absent" (keep default) is distinct from an explicit false.
	GrokLiveQuota             *bool `json:"grok_live_quota,omitempty"`
	CollectionEnabled         *bool `json:"collection_enabled,omitempty"`
	CollectionIntervalMinutes int   `json:"collection_interval_minutes,omitempty"`
	CollectionLookbackMinutes int   `json:"collection_lookback_minutes,omitempty"`
	// Pointer because 0 is a meaningful setting here: keep chat forever.
	CollectionMessageRetentionDays *int                                 `json:"collection_message_retention_days,omitempty"`
	TextModelBaseURL               string                               `json:"text_model_base_url,omitempty"`
	TextModelName                  string                               `json:"text_model_name,omitempty"`
	TextModelSource                string                               `json:"text_model_source,omitempty"`
	TodoRefinePrompt               string                               `json:"todo_refine_prompt,omitempty"`
	CollectionConnectors           map[string]CollectionConnectorConfig `json:"collection_connectors,omitempty"`
	// Pointer so "absent" (keep the on-by-default) is distinct from false.
	TodoRefineOnAdd *bool                          `json:"todo_refine_on_add,omitempty"`
	QuotaProviders  map[string]QuotaProviderConfig `json:"quota_providers,omitempty"`
	DataDir         string                         `json:"data_dir,omitempty"`
	Pricing         map[string][4]float64          `json:"pricing,omitempty"`
	Subscriptions   map[string]float64             `json:"subscriptions,omitempty"`
	ProjectAliases  map[string]string              `json:"project_aliases,omitempty"`
	Guard           GuardConfig                    `json:"guard,omitzero"`
}

func LoadConfig() {
	TodoRefinePrompt = DefaultTodoRefinePrompt
	loadConfigFile()
	applyEnvOverrides()
}

func loadConfigFile() {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return
	}
	var cfg FileConfig
	if json.Unmarshal(data, &cfg) != nil {
		return
	}
	if cfg.Timezone != "" {
		loc, err := time.LoadLocation(cfg.Timezone)
		if err == nil {
			Loc = loc
		}
	}
	if cfg.OwnerName != "" {
		OwnerName = cfg.OwnerName
	}
	if cfg.ClaudeProjects != "" {
		ClaudeProjects = expandHome(cfg.ClaudeProjects)
	}
	if cfg.CodexSessions != "" {
		CodexSessions = expandHome(cfg.CodexSessions)
	}
	if cfg.PiSessions != "" {
		PiSessions = expandHome(cfg.PiSessions)
	}
	if cfg.CopilotWorkspaces != "" {
		CopilotWorkspaces = expandHome(cfg.CopilotWorkspaces)
	}
	if cfg.QoderDB != "" {
		QoderDB = expandHome(cfg.QoderDB)
	}
	if cfg.QoderCLIProjects != "" {
		QoderCLIProjects = expandHome(cfg.QoderCLIProjects)
	}
	if cfg.QoderWorkDB != "" {
		QoderWorkDB = expandHome(cfg.QoderWorkDB)
	}
	if cfg.GrokSessions != "" {
		GrokSessions = expandHome(cfg.GrokSessions)
	}
	if cfg.AntigravityDir != "" {
		AntigravityDir = expandHome(cfg.AntigravityDir)
	}
	if cfg.GrokLiveQuota != nil {
		GrokLiveQuota = *cfg.GrokLiveQuota
	}
	if cfg.CollectionEnabled != nil {
		CollectionEnabled = *cfg.CollectionEnabled
	}
	if cfg.CollectionIntervalMinutes > 0 {
		CollectionIntervalMinutes = cfg.CollectionIntervalMinutes
	}
	if cfg.CollectionLookbackMinutes > 0 {
		CollectionLookbackMinutes = cfg.CollectionLookbackMinutes
	}
	if cfg.CollectionMessageRetentionDays != nil && *cfg.CollectionMessageRetentionDays >= 0 {
		CollectionMessageRetentionDays = *cfg.CollectionMessageRetentionDays
	}
	if strings.TrimSpace(cfg.TextModelBaseURL) != "" {
		TextModelBaseURL = strings.TrimRight(strings.TrimSpace(cfg.TextModelBaseURL), "/")
	}
	if strings.TrimSpace(cfg.TextModelName) != "" {
		TextModelName = strings.TrimSpace(cfg.TextModelName)
	}
	if strings.TrimSpace(cfg.TextModelSource) != "" {
		TextModelSource = strings.Join(strings.Fields(cfg.TextModelSource), " ")
	}
	TodoRefinePrompt = DefaultTodoRefinePrompt
	if strings.TrimSpace(cfg.TodoRefinePrompt) != "" {
		TodoRefinePrompt = strings.TrimSpace(cfg.TodoRefinePrompt)
	}
	if cfg.TodoRefineOnAdd != nil {
		TodoRefineOnAdd = *cfg.TodoRefineOnAdd
	}
	CollectionConnectors = cfg.CollectionConnectors
	QuotaProviders = cfg.QuotaProviders
	Guard = cfg.Guard
	if cfg.DataDir != "" {
		AtmDir = expandHome(cfg.DataDir)
		AtmDB = filepath.Join(AtmDir, "atm.db")
	}
	Pricing = cfg.Pricing
	Subscriptions = cfg.Subscriptions
	ProjectAliases = cfg.ProjectAliases
}

// applyEnvOverrides lets ATM_GROK_LIVE_QUOTA=0/1 force the live-quota switch
// regardless of the config file — useful for debugging and one-off checks
// (`ATM_GROK_LIVE_QUOTA=1 atm quota --json`) without editing config.json.
func applyEnvOverrides() {
	switch strings.ToLower(os.Getenv("ATM_GROK_LIVE_QUOTA")) {
	case "1", "true", "on", "yes":
		GrokLiveQuota = true
	case "0", "false", "off", "no":
		GrokLiveQuota = false
	}
	switch strings.ToLower(os.Getenv("ATM_COLLECTION_ENABLED")) {
	case "1", "true", "on", "yes":
		CollectionEnabled = true
	case "0", "false", "off", "no":
		CollectionEnabled = false
	}
	switch strings.ToLower(os.Getenv("ATM_TODO_REFINE_ON_ADD")) {
	case "1", "true", "on", "yes":
		TodoRefineOnAdd = true
	case "0", "false", "off", "no":
		TodoRefineOnAdd = false
	}
}

// SetConfigValue rewrites one key in ~/.atm/config.json, preserving every
// other field (including ones this build does not know about). Used by
// `atm config set` so GUI toggles have a stable write path.
func SetConfigValue(key string, value any) error {
	raw, err := loadRawConfig()
	if err != nil {
		return err
	}
	raw[key] = value
	return saveRawConfig(raw)
}

// SaveGuardToolBin records where a tool's gate was installed.
//
// Without this the gate loses track of its own installation: a tool that is not
// on PATH — which is the normal case for one only ever invoked by absolute path —
// could be gated successfully and then be invisible to `guard status` and
// `atm doctor`, so the checks for a shim that was overwritten or walked around
// would never run for it. That is precisely the tool most worth checking.
//
// Uninstall deliberately leaves the path recorded: "not enabled, at this path" is
// a more useful answer than "not found", and it means a reinstall does not need
// --bin again.
func SaveGuardToolBin(tool, bin string) error {
	raw, err := loadRawConfig()
	if err != nil {
		return err
	}
	guardRaw, _ := raw["guard"].(map[string]any)
	if guardRaw == nil {
		guardRaw = map[string]any{}
	}
	toolsRaw, _ := guardRaw["tools"].(map[string]any)
	if toolsRaw == nil {
		toolsRaw = map[string]any{}
	}
	toolRaw, _ := toolsRaw[tool].(map[string]any)
	if toolRaw == nil {
		toolRaw = map[string]any{}
	}
	toolRaw["bin"] = bin
	toolsRaw[tool] = toolRaw
	guardRaw["tools"] = toolsRaw
	raw["guard"] = guardRaw
	if err := saveRawConfig(raw); err != nil {
		return err
	}
	ReloadGuard()
	return nil
}

// SaveGuardRule upserts one rule by id under a tool, creating the tool entry if
// this is the first thing said about it. Registering a CLI is this plus an
// install: the rule is what makes the gate mean anything, since a gated tool with
// no rules passes every invocation straight through.
func SaveGuardRule(tool string, rule GuardRule) error {
	tool = strings.TrimSpace(tool)
	rule.ID = strings.TrimSpace(rule.ID)
	if tool == "" || rule.ID == "" {
		return fmt.Errorf("tool and rule id are required")
	}
	encoded, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	var asMap map[string]any
	if err := json.Unmarshal(encoded, &asMap); err != nil {
		return err
	}
	return mutateGuardTool(tool, func(toolRaw map[string]any) {
		rules, _ := toolRaw["rules"].([]any)
		replaced := false
		for index, existing := range rules {
			if entry, ok := existing.(map[string]any); ok && entry["id"] == rule.ID {
				rules[index] = asMap
				replaced = true
				break
			}
		}
		if !replaced {
			rules = append(rules, asMap)
		}
		toolRaw["rules"] = rules
	})
}

// RemoveGuardRule drops a user rule. A built-in of the same id comes back, which
// is the honest outcome: removing an override is not the same as switching the
// action off, and the latter is what `enabled: false` is for.
func RemoveGuardRule(tool, ruleID string) error {
	return mutateGuardTool(tool, func(toolRaw map[string]any) {
		rules, _ := toolRaw["rules"].([]any)
		kept := make([]any, 0, len(rules))
		for _, existing := range rules {
			if entry, ok := existing.(map[string]any); ok && entry["id"] == ruleID {
				continue
			}
			kept = append(kept, existing)
		}
		toolRaw["rules"] = kept
	})
}

// RemoveGuardTool forgets a tool entirely: its rules and its recorded install
// path. It deliberately does not touch the filesystem — a shim has to be removed
// by `atm guard uninstall`, and silently leaving one in place while forgetting
// where it is would be the worst of both.
func RemoveGuardTool(tool string) error {
	raw, err := loadRawConfig()
	if err != nil {
		return err
	}
	guardRaw, _ := raw["guard"].(map[string]any)
	if guardRaw == nil {
		return nil
	}
	toolsRaw, _ := guardRaw["tools"].(map[string]any)
	if toolsRaw == nil {
		return nil
	}
	delete(toolsRaw, tool)
	guardRaw["tools"] = toolsRaw
	raw["guard"] = guardRaw
	if err := saveRawConfig(raw); err != nil {
		return err
	}
	ReloadGuard()
	return nil
}

// mutateGuardTool applies a change to one tool's entry, leaving every other
// field in the file — including ones this build does not know about — untouched.
func mutateGuardTool(tool string, apply func(toolRaw map[string]any)) error {
	raw, err := loadRawConfig()
	if err != nil {
		return err
	}
	guardRaw, _ := raw["guard"].(map[string]any)
	if guardRaw == nil {
		guardRaw = map[string]any{}
	}
	toolsRaw, _ := guardRaw["tools"].(map[string]any)
	if toolsRaw == nil {
		toolsRaw = map[string]any{}
	}
	toolRaw, _ := toolsRaw[tool].(map[string]any)
	if toolRaw == nil {
		toolRaw = map[string]any{}
	}
	apply(toolRaw)
	toolsRaw[tool] = toolRaw
	guardRaw["tools"] = toolsRaw
	raw["guard"] = guardRaw
	if err := saveRawConfig(raw); err != nil {
		return err
	}
	ReloadGuard()
	return nil
}

// ReloadGuard re-reads the guard section from disk.
//
// Called after every guard write so a command that reports what it just changed
// reports the new state, not the one it started the process with. Narrow on
// purpose: a full LoadConfig would also re-apply env overrides and reset the
// refine prompt, none of which a rule edit has any business touching.
func ReloadGuard() {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		return
	}
	var cfg FileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	Guard = cfg.Guard
}

// loadRawConfig reads the file as an untyped map so a write preserves every other
// field, including ones this build does not know about.
func loadRawConfig() (map[string]any, error) {
	raw := map[string]any{}
	if data, err := os.ReadFile(ConfigPath); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("config file %s is not valid JSON: %w", ConfigPath, err)
		}
	}
	return raw, nil
}

func saveRawConfig(raw map[string]any) error {
	if err := os.MkdirAll(AtmDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(AtmDir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(ConfigPath, b, 0600); err != nil {
		return err
	}
	return os.Chmod(ConfigPath, 0600)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(Home, p[2:])
	}
	return p
}

func ShowConfig() string {
	cfg := FileConfig{
		Timezone:                       Loc.String(),
		OwnerName:                      OwnerName,
		ClaudeProjects:                 ClaudeProjects,
		CodexSessions:                  CodexSessions,
		PiSessions:                     PiSessions,
		CopilotWorkspaces:              CopilotWorkspaces,
		QoderDB:                        QoderDB,
		QoderCLIProjects:               QoderCLIProjects,
		QoderWorkDB:                    QoderWorkDB,
		GrokSessions:                   GrokSessions,
		AntigravityDir:                 AntigravityDir,
		GrokLiveQuota:                  &GrokLiveQuota,
		CollectionEnabled:              &CollectionEnabled,
		CollectionIntervalMinutes:      CollectionIntervalMinutes,
		CollectionLookbackMinutes:      CollectionLookbackMinutes,
		CollectionMessageRetentionDays: &CollectionMessageRetentionDays,
		TextModelBaseURL:               TextModelBaseURL,
		TextModelName:                  TextModelName,
		TextModelSource:                TextModelSource,
		TodoRefinePrompt:               TodoRefinePrompt,
		CollectionConnectors:           CollectionConnectors,
		TodoRefineOnAdd:                &TodoRefineOnAdd,
		QuotaProviders:                 QuotaProviders,
		DataDir:                        AtmDir,
		Pricing:                        Pricing,
		Subscriptions:                  Subscriptions,
		ProjectAliases:                 ProjectAliases,
		Guard:                          Guard,
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

func InitConfig() error {
	if _, err := os.Stat(ConfigPath); err == nil {
		return fmt.Errorf("config file already exists: %s", ConfigPath)
	}
	if err := os.MkdirAll(AtmDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(AtmDir, 0700); err != nil {
		return err
	}
	cfg := FileConfig{
		Timezone:                       "Asia/Shanghai",
		ClaudeProjects:                 "~/.claude/projects",
		CodexSessions:                  "~/.codex/sessions",
		PiSessions:                     "~/.pi/agent/sessions",
		CopilotWorkspaces:              shortenHome(defaultCopilotWorkspaces()),
		QoderDB:                        shortenHome(defaultQoderDB()),
		QoderCLIProjects:               "~/.qoder/projects",
		QoderWorkDB:                    shortenHome(defaultQoderWorkDB()),
		GrokSessions:                   "~/.grok/sessions",
		AntigravityDir:                 "~/.gemini/antigravity",
		CollectionEnabled:              &CollectionEnabled,
		CollectionIntervalMinutes:      CollectionIntervalMinutes,
		CollectionLookbackMinutes:      CollectionLookbackMinutes,
		CollectionMessageRetentionDays: &CollectionMessageRetentionDays,
		TextModelBaseURL:               TextModelBaseURL,
		TextModelName:                  TextModelName,
		TextModelSource:                TextModelSource,
		TodoRefinePrompt:               TodoRefinePrompt,
		TodoRefineOnAdd:                &TodoRefineOnAdd,
		DataDir:                        "~/.atm",
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	b = append(b, '\n')
	if err := os.WriteFile(ConfigPath, b, 0600); err != nil {
		return err
	}
	return os.Chmod(ConfigPath, 0600)
}

func shortenHome(p string) string {
	if strings.HasPrefix(p, Home+"/") {
		return "~/" + p[len(Home)+1:]
	}
	return p
}

func DateRange(dateStr string) (start, end time.Time, err error) {
	now := time.Now().In(Loc)
	switch strings.ToLower(dateStr) {
	case "today":
		dateStr = now.Format("2006-01-02")
	case "yesterday":
		dateStr = now.AddDate(0, 0, -1).Format("2006-01-02")
	}
	t, err := time.ParseInLocation("2006-01-02", dateStr, Loc)
	if err != nil {
		return
	}
	return t, t.AddDate(0, 0, 1), nil
}

func TsToCST(ts float64) time.Time {
	if ts > 1e12 {
		ts /= 1000.0
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).In(Loc)
}

func HeadJSONL(path string, n int) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var records []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() && len(records) < n {
		var m map[string]any
		if json.Unmarshal(scanner.Bytes(), &m) == nil {
			records = append(records, m)
		}
	}
	return records
}

func TailJSONL(path string, n int) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	chunk := int64(n * 4096)
	if chunk > size {
		chunk = size
	}
	if _, err := f.Seek(size-chunk, io.SeekStart); err != nil {
		return nil
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	var records []map[string]any
	for _, line := range lines {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			records = append(records, m)
		}
	}
	return records
}

// SampleJSONL reads records from evenly-spaced byte positions throughout a
// JSONL file. It returns up to numPoints * recordsPerPoint records, providing
// a representative cross-section of a potentially large file.
func SampleJSONL(path string, numPoints, recordsPerPoint int) []map[string]any {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()
	if size == 0 {
		return nil
	}

	var records []map[string]any
	for i := 0; i < numPoints; i++ {
		offset := size * int64(i) / int64(numPoints)
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			continue
		}
		// skip to next newline boundary (unless at file start)
		if offset > 0 {
			buf := make([]byte, 1)
			for {
				_, err := f.Read(buf)
				if err != nil || buf[0] == '\n' {
					break
				}
			}
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		count := 0
		for scanner.Scan() && count < recordsPerPoint {
			var m map[string]any
			if json.Unmarshal(scanner.Bytes(), &m) == nil {
				records = append(records, m)
			}
			count++
		}
	}
	return records
}

func ProjectName(dirName string) string {
	s := dirName
	homePrefix := strings.ReplaceAll(Home, "/", "-")
	for _, suffix := range []string{"-work-", "-"} {
		prefix := homePrefix + suffix
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
			break
		}
	}
	return CanonicalProject(s)
}

func CanonicalProject(project string) string {
	project = strings.TrimSpace(project)
	if project == "" {
		return ""
	}
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		if seen[project] {
			break
		}
		seen[project] = true
		next := ""
		for alias, canonical := range ProjectAliases {
			if strings.EqualFold(strings.TrimSpace(alias), project) {
				next = strings.TrimSpace(canonical)
				break
			}
		}
		if next == "" || next == project {
			break
		}
		project = next
	}
	return project
}

func ProjectMatches(project, filter string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}
	return strings.Contains(strings.ToLower(CanonicalProject(project)), strings.ToLower(CanonicalProject(filter)))
}

// ProjectFromPath uses a Git repository's origin name when available, so
// worktrees and differently named checkout directories share one project.
// It falls back to the repository root basename, then the input basename.
func ProjectFromPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	root := findGitRoot(abs)
	if root == "" {
		return CanonicalProject(filepath.Base(filepath.Clean(abs)))
	}
	if remote := gitOriginProject(root); remote != "" {
		return CanonicalProject(remote)
	}
	return CanonicalProject(filepath.Base(root))
}

func findGitRoot(path string) string {
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	for {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

func gitOriginProject(root string) string {
	configPath := filepath.Join(root, ".git", "config")
	marker := filepath.Join(root, ".git")
	if info, err := os.Stat(marker); err == nil && !info.IsDir() {
		data, readErr := os.ReadFile(marker)
		if readErr == nil {
			line := strings.TrimSpace(string(data))
			if strings.HasPrefix(line, "gitdir:") {
				gitDir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(root, gitDir)
				}
				configPath = filepath.Join(gitDir, "config")
				if _, statErr := os.Stat(configPath); statErr != nil {
					if common, commonErr := os.ReadFile(filepath.Join(gitDir, "commondir")); commonErr == nil {
						configPath = filepath.Join(gitDir, strings.TrimSpace(string(common)), "config")
					}
				}
			}
		}
	}
	file, err := os.Open(configPath)
	if err != nil {
		return ""
	}
	defer file.Close()
	inOrigin := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = strings.EqualFold(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.EqualFold(strings.TrimSpace(key), "url") {
			return projectNameFromRemote(strings.TrimSpace(value))
		}
	}
	return ""
}

func projectNameFromRemote(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), "/")
	if remote == "" {
		return ""
	}
	if index := strings.LastIndex(remote, "/"); index >= 0 {
		remote = remote[index+1:]
	} else if index := strings.LastIndex(remote, ":"); index >= 0 {
		remote = remote[index+1:]
	}
	return strings.TrimSuffix(remote, ".git")
}

func ProjectDirFromSessionPath(filePath string) string {
	dir := filepath.Base(filepath.Dir(filePath))
	if !strings.HasPrefix(dir, "-") {
		return ""
	}
	parts := strings.Split(dir[1:], "-")
	if len(parts) == 0 {
		return ""
	}
	resolved := "/"
	i := 0
	for i < len(parts) {
		found := false
		for j := 1; j <= len(parts)-i; j++ {
			segment := strings.Join(parts[i:i+j], "-")
			candidate := filepath.Join(resolved, segment)
			info, err := os.Stat(candidate)
			if err == nil && info.IsDir() {
				resolved = candidate
				i += j
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	return resolved
}

func GetStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func GetMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key]; ok {
		if mm, ok := v.(map[string]any); ok {
			return mm
		}
	}
	return nil
}

func GetSlice(m map[string]any, key string) []any {
	if v, ok := m[key]; ok {
		if s, ok := v.([]any); ok {
			return s
		}
	}
	return nil
}

func NormalizeAgent(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "claude", "claude-code", "claudecode":
		return "claude"
	case "codex", "openai-codex":
		return "codex"
	case "copilot", "github-copilot", "ghcopilot":
		return "copilot"
	case "pi":
		return "pi"
	case "qoder":
		return "qoder"
	case "qodercli", "qoder-cli":
		return "qodercli"
	case "qoderwork", "qoder-work":
		return "qoderwork"
	case "grok", "grokbuild", "grok-build", "grok-cli":
		return "grokbuild"
	case "antigravity", "agy", "anti-gravity":
		return "antigravity"
	// ATM 自己：它是内置文本模型的 client，用量记在 `store.BuiltinAgent` 名下。这里
	// 认它是为了 `--agent atm` 能过校验；它没有 parser adapter，所以不参与 sync，
	// 也不会出现在活跃面板里。
	case "atm":
		return "atm"
	default:
		return ""
	}
}

func ExtractText(m map[string]any) string {
	return strings.Join(ExtractTextBlocks(m), "\n\n")
}

// ExtractTextBlocks returns every non-empty text block in message order.
// Some Claude/Qoder user messages put IDE context in the first block and the
// actual prompt in a later block, so callers must not stop at the first one.
func ExtractTextBlocks(m map[string]any) []string {
	if s := GetStr(m, "content"); s != "" {
		return []string{s}
	}
	var texts []string
	for _, item := range GetSlice(m, "content") {
		if block, ok := item.(map[string]any); ok {
			if GetStr(block, "type") == "text" {
				if txt := GetStr(block, "text"); txt != "" {
					texts = append(texts, txt)
				}
			}
		}
	}
	return texts
}

func GetFloat(m map[string]any, key string) (float64, bool) {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f, true
		}
	}
	return 0, false
}
