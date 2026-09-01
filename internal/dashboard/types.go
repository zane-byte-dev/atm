package dashboard

import (
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

// Request selects the independently useful halves of a dashboard snapshot.
// Empty Sections means the complete snapshot. SessionID is optional because the
// desktop app can run without an Agent session in its environment.
type Request struct {
	Sections  []string `json:"sections,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Sync      bool     `json:"sync,omitempty"`
}

type WorkSummary struct {
	Open        int `json:"open"`
	InProgress  int `json:"in_progress"`
	Waiting     int `json:"waiting"`
	Review      int `json:"review"`
	Blocked     int `json:"blocked"`
	Due         int `json:"due"`
	Maintenance int `json:"maintenance"`
}

type WorkView struct {
	GeneratedAt string       `json:"generated_at"`
	Open        []store.Todo `json:"open"`
	Working     []store.Todo `json:"working"`
	Waiting     []store.Todo `json:"waiting"`
	Review      []store.Todo `json:"review"`
	Blocked     []store.Todo `json:"blocked"`
	Due         []store.Todo `json:"due"`
	Summary     WorkSummary  `json:"summary"`
}

type RangeSession struct {
	ID                 string `json:"id"`
	ShortID            string `json:"short_id"`
	Agent              string `json:"agent"`
	Project            string `json:"project"`
	CreatedAt          string `json:"created_at"`
	LastAt             string `json:"last_at,omitempty"`
	QCount             int    `json:"q_count"`
	LocalUserTurnCount int    `json:"local_user_turn_count"`
	Summary            string `json:"summary,omitempty"`
	FirstQ             string `json:"first_q,omitempty"`
	ResumeID           string `json:"resume_id,omitempty"`
	RootSessionID      string `json:"root_session_id,omitempty"`
	ParentSessionID    string `json:"parent_session_id,omitempty"`
	AgentPath          string `json:"agent_path,omitempty"`
	AgentNickname      string `json:"agent_nickname,omitempty"`
	SubagentDepth      int    `json:"subagent_depth,omitempty"`
	IsSubagent         bool   `json:"is_subagent,omitempty"`
	ParserVersion      int    `json:"parser_version"`
	ContentState       string `json:"content_state"`
	ResultStatus       string `json:"result_status"`
	LatestProgress     string `json:"latest_progress,omitempty"`
	FinalResult        string `json:"final_result,omitempty"`
}

type Range struct {
	// StartDate and EndDate are inclusive local calendar dates. The service
	// computes them once so clients never disagree about calendar windows.
	StartDate    string                   `json:"start_date"`
	EndDate      string                   `json:"end_date"`
	ModelStats   []store.ModelStatsResult `json:"model_stats"`
	Sessions     []RangeSession           `json:"sessions"`
	SkillStats   []store.SkillStatsResult `json:"skill_stats"`
	ProjectStats []store.StatsResult      `json:"project_stats"`
	Speed        store.SpeedReport        `json:"speed"`
	Quality      StatsQuality             `json:"quality"`
}

// StatsQuality makes the limits of a dashboard range explicit. It is additive
// to the existing range payload so older desktop clients keep decoding it.
type StatsQuality struct {
	ActiveSessions       int      `json:"active_sessions"`
	TokenSessions        int      `json:"token_sessions"`
	SessionCoveragePct   float64  `json:"session_coverage_percent"`
	ActiveAgents         int      `json:"active_agents"`
	TokenAgents          int      `json:"token_agents"`
	AgentCoveragePct     float64  `json:"agent_coverage_percent"`
	Requests             int      `json:"requests"`
	DetailedRequests     int      `json:"detailed_requests"`
	AggregateRequests    int      `json:"aggregate_requests"`
	RequestCoveragePct   float64  `json:"request_coverage_percent"`
	SpeedRequests        int      `json:"speed_requests"`
	SpeedSampledRequests int      `json:"speed_sampled_requests"`
	SpeedSamplePct       float64  `json:"speed_sample_percent"`
	UntimedRequests      int      `json:"untimed_requests"`
	OutOfWindowRequests  int      `json:"out_of_window_requests"`
	CostUSD              float64  `json:"cost_usd"`
	EstimatedCostUSD     float64  `json:"estimated_cost_usd"`
	EstimatedCostShare   float64  `json:"estimated_cost_share"`
	PricingSources       []string `json:"pricing_sources"`
}

// BindingContext extends the Work binding projection with observation data
// used by the live dashboard. It is still application data: no rendering or
// transport types are embedded in it.
type BindingContext struct {
	State             string                   `json:"state"`
	Binding           store.TodoSessionBinding `json:"binding"`
	Todo              *workapp.TodoSummary     `json:"todo,omitempty"`
	Observed          bool                     `json:"observed"`
	ObservedSessionID string                   `json:"observed_session_id,omitempty"`
}

type LiveSession struct {
	Tool            string                    `json:"tool"`
	SessionID       string                    `json:"session_id,omitempty"`
	ResumeID        string                    `json:"resume_id,omitempty"`
	RootSessionID   string                    `json:"root_session_id,omitempty"`
	ParentSessionID string                    `json:"parent_session_id,omitempty"`
	AgentPath       string                    `json:"agent_path,omitempty"`
	AgentNickname   string                    `json:"agent_nickname,omitempty"`
	SubagentDepth   int                       `json:"subagent_depth,omitempty"`
	Project         string                    `json:"project"`
	Client          string                    `json:"client,omitempty"`
	CWD             string                    `json:"cwd,omitempty"`
	Model           string                    `json:"model,omitempty"`
	Summary         string                    `json:"summary,omitempty"`
	AgeSeconds      int                       `json:"age_seconds"`
	ActivityState   string                    `json:"activity_state"`
	BindingState    string                    `json:"binding_state"`
	Binding         *store.TodoSessionBinding `json:"binding,omitempty"`
	Todo            *workapp.TodoSummary      `json:"todo,omitempty"`
	PID             string                    `json:"pid,omitempty"`
	TTY             string                    `json:"tty,omitempty"`
	TerminalApp     string                    `json:"terminal_app,omitempty"`
	FirstQ          string                    `json:"first_q,omitempty"`
	LastQ           string                    `json:"last_q,omitempty"`
	LastA           string                    `json:"last_a,omitempty"`
	LatestResult    string                    `json:"latest_result,omitempty"`
	Updates         []string                  `json:"updates,omitempty"`
	Tools           []string                  `json:"tools,omitempty"`
	Topics          []string                  `json:"topics,omitempty"`
}

type LiveStatus struct {
	GeneratedAt string           `json:"generated_at"`
	Time        string           `json:"time"`
	Sessions    []LiveSession    `json:"sessions"`
	Bindings    []BindingContext `json:"bindings"`
}

type CurrentSession struct {
	Bound     bool                      `json:"bound"`
	State     string                    `json:"state"`
	SessionID string                    `json:"session_id"`
	Binding   *store.TodoSessionBinding `json:"binding,omitempty"`
	Todo      *workapp.TodoSummary      `json:"todo,omitempty"`
}

type Index struct {
	Path             string `json:"path"`
	Exists           bool   `json:"exists"`
	SchemaVersion    int    `json:"schema_version"`
	IndexedSessions  int    `json:"indexed_sessions"`
	RetainedSessions int    `json:"retained_sessions"`
}

type SyncState struct {
	Scope             string  `json:"scope"`
	Status            string  `json:"status"`
	RunStatus         string  `json:"run_status"`
	LastAttemptAt     *string `json:"last_attempt_at"`
	LastSuccessAt     *string `json:"last_success_at"`
	AgeSeconds        *int64  `json:"age_seconds"`
	StaleAfterSeconds int64   `json:"stale_after_seconds"`
	LastError         string  `json:"last_error"`
	LastSyncedFiles   int     `json:"last_synced_files"`
}

type IndexHealth struct {
	GeneratedAt string    `json:"generated_at"`
	Index       Index     `json:"index"`
	Sync        SyncState `json:"sync"`
}

// Snapshot is the transport-neutral result shared by CLI JSON and typed IPC.
// Its JSON tags are part of the existing desktop dashboard schema.
type Snapshot struct {
	SchemaVersion    int                           `json:"schema_version"`
	GeneratedAt      string                        `json:"generated_at"`
	Work             WorkView                      `json:"work"`
	Todos            []store.Todo                  `json:"todos"`
	DayStats         []store.DayStatsResult        `json:"day_stats"`
	HourStats        []store.DayStatsResult        `json:"hour_stats"`
	ModelDayStats    []store.ModelDayStatsResult   `json:"model_day_stats"`
	ModelHourStats   []store.ModelDayStatsResult   `json:"model_hour_stats"`
	ProjectDayStats  []store.ProjectDayStatsResult `json:"project_day_stats"`
	ProjectHourStats []store.ProjectDayStatsResult `json:"project_hour_stats"`
	TodoCompletions  []store.TodoCompletion        `json:"todo_completions"`
	Ranges           map[string]Range              `json:"ranges"`
	LiveStatus       LiveStatus                    `json:"live_status"`
	CurrentSession   *CurrentSession               `json:"current_session,omitempty"`
	IndexHealth      IndexHealth                   `json:"index_health"`
}
