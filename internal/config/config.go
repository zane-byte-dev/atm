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
	AtmDir            = filepath.Join(Home, ".atm")
	AtmDB             = filepath.Join(Home, ".atm", "atm.db")
	Loc               = time.FixedZone("CST", 8*3600)
	ConfigPath        = filepath.Join(Home, ".atm", "config.json")
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
	// A comma-separated candidate chain: the first command that runs wins, and
	// the next one is tried when the previous exits non-zero, times out or is
	// not installed. That is what keeps collection alive when one CLI is rate
	// limited. A single command remains a valid chain of one.
	CollectionModelCommand = "codex"
	// Synced chat is kept for this many days; 0 keeps it forever. Reading a
	// conversation stores it so it can be searched and read offline, and that
	// archive would otherwise grow for as long as the sources stay enabled.
	CollectionMessageRetentionDays = 90
	// Where a source's daily digest is filed when the source names no knowledge
	// collection of its own.
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
	// CollectionModelRunners describes how to drive a CLI that ATM has no
	// built-in profile for, so a third-party agent CLI can classify chat without
	// a code change. Keys are the names used in CollectionModelCommand; a key
	// that matches a built-in profile overrides it.
	CollectionModelRunners map[string]ModelRunnerConfig
	// TextModelBaseURL and TextModelName configure ATM's narrow built-in text
	// service. Credentials deliberately stay out of config.json: the CLI reads
	// DEEPSEEK_API_KEY and the App supplies its Keychain value to child commands.
	TextModelBaseURL = "https://api.deepseek.com"
	TextModelName    = "deepseek-v4-flash"
	// TodoRefineOnAdd is the desktop default after a human files a todo: run
	// one schema-constrained model pass to polish the card and, when the work
	// is independently trackable, split it. CLI `todo add` never does this
	// unless `--refine` is passed — agents already write structured cards and
	// a network model call would break `id=$(atm todo add ...)`. Default on
	// because that is the whole point of the feature; turn it off to keep
	// messy capture text as typed.
	TodoRefineOnAdd = true
)

// CollectionModelWorkdirPrefix names the scratch directory every model run gets.
// It is a shared constant because the parsers key off it: a CLI that persists a
// session per working directory would otherwise file every classification as a
// real ATM session in a throwaway project.
const CollectionModelWorkdirPrefix = "atm-collection-model-"

// IsCollectionModelWorkdir reports whether a path (or a CLI's URL-encoded
// rendering of one) points inside a classifier scratch directory.
func IsCollectionModelWorkdir(path string) bool {
	return strings.Contains(path, CollectionModelWorkdirPrefix)
}

// CollectionModelCandidates splits the configured command into the ordered
// chain to try. Candidates are separated by commas; each one keeps its own
// arguments, which is why a candidate whose arguments contain a comma has to be
// declared in collection_model_runners instead.
func CollectionModelCandidates(commandLine string) []string {
	if strings.TrimSpace(commandLine) == "" {
		commandLine = CollectionModelCommand
	}
	var candidates []string
	for _, candidate := range strings.Split(commandLine, ",") {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

type CollectionConnectorConfig struct {
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

// ModelRunnerConfig teaches ATM how to call one CLI in headless,
// schema-constrained mode. Args are a template: {{schema_path}},
// {{schema_json}}, {{prompt_path}} and {{workdir}} are substituted per run, and
// a template without a prompt placeholder gets the prompt on stdin.
// OutputField unwraps a CLI that answers with an envelope instead of the bare
// object. A custom runner must carry its own sandbox flags — ATM cannot know
// how a third-party CLI denies network and filesystem writes.
type ModelRunnerConfig struct {
	// Command defaults to the map key, so a key may be either a real binary
	// name or an alias whose command and flags are defined here.
	Command        string   `json:"command,omitempty"`
	Args           []string `json:"args,omitempty"`
	OutputField    string   `json:"output_field,omitempty"`
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
	// Pointer so "absent" (keep default) is distinct from an explicit false.
	GrokLiveQuota             *bool `json:"grok_live_quota,omitempty"`
	CollectionEnabled         *bool `json:"collection_enabled,omitempty"`
	CollectionIntervalMinutes int   `json:"collection_interval_minutes,omitempty"`
	CollectionLookbackMinutes int   `json:"collection_lookback_minutes,omitempty"`
	// Pointer because 0 is a meaningful setting here: keep chat forever.
	CollectionMessageRetentionDays *int                                 `json:"collection_message_retention_days,omitempty"`
	CollectionModelCommand         string                               `json:"collection_model_command,omitempty"`
	CollectionModelRunners         map[string]ModelRunnerConfig         `json:"collection_model_runners,omitempty"`
	TextModelBaseURL               string                               `json:"text_model_base_url,omitempty"`
	TextModelName                  string                               `json:"text_model_name,omitempty"`
	CollectionConnectors           map[string]CollectionConnectorConfig `json:"collection_connectors,omitempty"`
	// Pointer so "absent" (keep the on-by-default) is distinct from false.
	TodoRefineOnAdd *bool                          `json:"todo_refine_on_add,omitempty"`
	QuotaProviders  map[string]QuotaProviderConfig `json:"quota_providers,omitempty"`
	DataDir         string                         `json:"data_dir,omitempty"`
	Pricing         map[string][4]float64          `json:"pricing,omitempty"`
	Subscriptions   map[string]float64             `json:"subscriptions,omitempty"`
	ProjectAliases  map[string]string              `json:"project_aliases,omitempty"`
}

func LoadConfig() {
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
	if cfg.CollectionModelCommand != "" {
		CollectionModelCommand = cfg.CollectionModelCommand
	}
	if strings.TrimSpace(cfg.TextModelBaseURL) != "" {
		TextModelBaseURL = strings.TrimRight(strings.TrimSpace(cfg.TextModelBaseURL), "/")
	}
	if strings.TrimSpace(cfg.TextModelName) != "" {
		TextModelName = strings.TrimSpace(cfg.TextModelName)
	}
	if cfg.TodoRefineOnAdd != nil {
		TodoRefineOnAdd = *cfg.TodoRefineOnAdd
	}
	CollectionModelRunners = cfg.CollectionModelRunners
	CollectionConnectors = cfg.CollectionConnectors
	QuotaProviders = cfg.QuotaProviders
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
	raw := map[string]any{}
	if data, err := os.ReadFile(ConfigPath); err == nil {
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("config file %s is not valid JSON: %w", ConfigPath, err)
		}
	}
	raw[key] = value
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(ConfigPath, b, 0644)
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
		GrokLiveQuota:                  &GrokLiveQuota,
		CollectionEnabled:              &CollectionEnabled,
		CollectionIntervalMinutes:      CollectionIntervalMinutes,
		CollectionLookbackMinutes:      CollectionLookbackMinutes,
		CollectionMessageRetentionDays: &CollectionMessageRetentionDays,
		CollectionModelCommand:         CollectionModelCommand,
		CollectionModelRunners:         CollectionModelRunners,
		TextModelBaseURL:               TextModelBaseURL,
		TextModelName:                  TextModelName,
		CollectionConnectors:           CollectionConnectors,
		TodoRefineOnAdd:                &TodoRefineOnAdd,
		QuotaProviders:                 QuotaProviders,
		DataDir:                        AtmDir,
		Pricing:                        Pricing,
		Subscriptions:                  Subscriptions,
		ProjectAliases:                 ProjectAliases,
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

func InitConfig() error {
	if _, err := os.Stat(ConfigPath); err == nil {
		return fmt.Errorf("config file already exists: %s", ConfigPath)
	}
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
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
		CollectionEnabled:              &CollectionEnabled,
		CollectionIntervalMinutes:      CollectionIntervalMinutes,
		CollectionLookbackMinutes:      CollectionLookbackMinutes,
		CollectionMessageRetentionDays: &CollectionMessageRetentionDays,
		CollectionModelCommand:         CollectionModelCommand,
		TextModelBaseURL:               TextModelBaseURL,
		TextModelName:                  TextModelName,
		TodoRefineOnAdd:                &TodoRefineOnAdd,
		DataDir:                        "~/.atm",
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	b = append(b, '\n')
	return os.WriteFile(ConfigPath, b, 0644)
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
