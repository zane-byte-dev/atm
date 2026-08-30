// Package stats owns usage-report queries and their transport-neutral
// aggregation. Command and IPC adapters choose how to render these values; they
// do not open the session index or reproduce reporting policy.
package stats

import "time"

// Group is one supported usage-report projection. Project is represented by an
// empty CLI value for compatibility with `atm stats`, but is explicit inside the
// application layer.
type Group string

const (
	GroupProject      Group = ""
	GroupModel        Group = "model"
	GroupModelDay     Group = "model-day"
	GroupModelHour    Group = "model-hour"
	GroupSkill        Group = "skill"
	GroupSession      Group = "session"
	GroupSessionUsage Group = "session-usage"
	GroupRequest      Group = "request"
	GroupSpeed        Group = "speed"
	GroupDay          Group = "day"
	GroupHour         Group = "hour"
	GroupWrapped      Group = "wrapped"
)

// Input is the complete application request. Range is the public named range;
// an empty value selects the rolling Days window. Sync asks the service to
// refresh the derived session index before querying it.
type Input struct {
	Days      int
	Range     string
	Group     string
	Agent     string
	SessionID string
	Sync      bool
}

type Window struct {
	Start time.Time
	End   time.Time
	Label string
	Days  int
}

type ProjectRow struct {
	Project             string  `json:"project"`
	Agent               string  `json:"agent"`
	Sessions            int     `json:"sessions"`
	TokenSessions       int     `json:"token_sessions"`
	Queries             int     `json:"queries"`
	ToolCalls           int     `json:"tool_calls"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Requests            int     `json:"requests"`
	DetailedRequests    int     `json:"detailed_requests"`
	AggregateRequests   int     `json:"aggregate_requests"`
	RequestCoveragePct  float64 `json:"request_coverage_percent"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
}

type ModelRow struct {
	Client              string  `json:"client"`
	Model               string  `json:"model"`
	Sessions            int     `json:"sessions"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Requests            int     `json:"requests"`
	DetailedRequests    int     `json:"detailed_requests"`
	AggregateRequests   int     `json:"aggregate_requests"`
	RequestCoveragePct  float64 `json:"request_coverage_percent"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
}

type ModelPeriodRow struct {
	Date                 string  `json:"date"`
	Client               string  `json:"client"`
	Model                string  `json:"model"`
	Sessions             int     `json:"sessions"`
	InputTokens          int64   `json:"input_tokens"`
	FreshInputTokens     int64   `json:"fresh_input_tokens"`
	OutputTokens         int64   `json:"output_tokens"`
	CacheCreateTokens    int64   `json:"cache_create_tokens"`
	CacheReadTokens      int64   `json:"cache_read_tokens"`
	TotalInputTokens     int64   `json:"total_input_tokens"`
	TotalTokens          int64   `json:"total_tokens"`
	Requests             int     `json:"requests"`
	DetailedRequests     int     `json:"detailed_requests"`
	AggregateRequests    int     `json:"aggregate_requests"`
	RequestCoveragePct   float64 `json:"request_coverage_percent"`
	SampledRequests      int     `json:"sampled_requests"`
	UntimedRequests      int     `json:"untimed_requests"`
	OutOfWindowRequests  int     `json:"out_of_window_requests"`
	CostUSD              float64 `json:"cost_usd"`
	CostEstimated        bool    `json:"cost_estimated"`
	EstimatedCostUSD     float64 `json:"estimated_cost_usd"`
	PricingSource        string  `json:"pricing_source"`
	MeasuredOutputTokens int64   `json:"measured_output_tokens"`
	MeasuredDurationMS   int64   `json:"measured_duration_ms"`
}

type SkillRow struct {
	Skill    string `json:"skill"`
	Calls    int    `json:"calls"`
	Sessions int    `json:"sessions"`
	Agents   int    `json:"agents"`
}

type SessionRow struct {
	ShortID             string  `json:"short_id"`
	Project             string  `json:"project"`
	Model               string  `json:"model"`
	Queries             int     `json:"queries"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheTokens         int64   `json:"cache_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	DetailedRequests    int     `json:"detailed_requests"`
	AggregateRequests   int     `json:"aggregate_requests"`
	RequestCoveragePct  float64 `json:"request_coverage_percent"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
	// Share is an application aggregate used by the text view. It is deliberately
	// excluded from JSON to preserve the legacy Session row contract.
	Share float64 `json:"-"`
}

type SessionUsageRow struct {
	SessionID           string  `json:"session_id"`
	ShortID             string  `json:"short_id"`
	Agent               string  `json:"agent"`
	Project             string  `json:"project"`
	Model               string  `json:"model"`
	StartedTS           int64   `json:"started_ts"`
	LastTS              int64   `json:"last_ts"`
	Requests            int     `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	DetailedRequests    int     `json:"detailed_requests"`
	AggregateRequests   int     `json:"aggregate_requests"`
	RequestCoveragePct  float64 `json:"request_coverage_percent"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
	Share               float64 `json:"share"`
}

type RequestRow struct {
	SessionID           string  `json:"session_id"`
	Agent               string  `json:"agent"`
	Project             string  `json:"project"`
	Model               string  `json:"model"`
	TS                  int64   `json:"ts"`
	RequestCount        int     `json:"request_count"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheTokens         int64   `json:"cache_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
}

type SpeedModelRow struct {
	Client                  string  `json:"client"`
	Model                   string  `json:"model"`
	Requests                int     `json:"requests"`
	Sampled                 int     `json:"sampled"`
	TokensPerSecondP50      float64 `json:"tokens_per_second_p50"`
	TokensPerSecondP90      float64 `json:"tokens_per_second_p90"`
	TokensPerSecondWeighted float64 `json:"tokens_per_second_weighted"`
	DurationP50Seconds      float64 `json:"duration_p50_seconds"`
	DurationP90Seconds      float64 `json:"duration_p90_seconds"`
	OutputTokens            int64   `json:"output_tokens"`
	SampledSeconds          float64 `json:"sampled_seconds"`
}

type TurnWaitRow struct {
	Agent           string  `json:"agent"`
	Turns           int     `json:"turns"`
	WaitP50Seconds  float64 `json:"wait_p50_seconds"`
	WaitP90Seconds  float64 `json:"wait_p90_seconds"`
	WaitMaxSeconds  float64 `json:"wait_max_seconds"`
	RequestsPerTurn float64 `json:"requests_per_turn"`
}

type SpeedReport struct {
	Models      []SpeedModelRow `json:"models"`
	Turns       []TurnWaitRow   `json:"turns"`
	Untimed     int             `json:"untimed_requests"`
	OutOfWindow int             `json:"out_of_window_requests"`
}

type PeriodRow struct {
	Date                string  `json:"date"`
	Sessions            int     `json:"sessions"`
	Queries             int     `json:"queries"`
	InputTokens         int64   `json:"input_tokens"`
	FreshInputTokens    int64   `json:"fresh_input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreateTokens   int64   `json:"cache_create_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalInputTokens    int64   `json:"total_input_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	Requests            int     `json:"requests"`
	DetailedRequests    int     `json:"detailed_requests"`
	AggregateRequests   int     `json:"aggregate_requests"`
	RequestCoveragePct  float64 `json:"request_coverage_percent"`
	SampledRequests     int     `json:"sampled_requests"`
	UntimedRequests     int     `json:"untimed_requests"`
	OutOfWindowRequests int     `json:"out_of_window_requests"`
	CostUSD             float64 `json:"cost_usd"`
	CostEstimated       bool    `json:"cost_estimated"`
	EstimatedCostUSD    float64 `json:"estimated_cost_usd"`
	PricingSource       string  `json:"pricing_source"`
}

type Totals struct {
	Sessions            int
	TokenSessions       int
	ActiveAgents        int
	TokenAgents         int
	Queries             int
	ToolCalls           int
	Requests            int
	InputTokens         int64
	FreshInputTokens    int64
	OutputTokens        int64
	CacheCreateTokens   int64
	CacheReadTokens     int64
	TotalInputTokens    int64
	TotalTokens         int64
	CacheTokens         int64
	DetailedRequests    int
	AggregateRequests   int
	SampledRequests     int
	UntimedRequests     int
	OutOfWindowRequests int
	CostUSD             float64
	EstimatedCostUSD    float64
	AnyEstimated        bool
	MaxCostUSD          float64
}

// Quality is additive report metadata. It keeps coverage and estimate caveats
// next to totals in typed IPC without changing the legacy CLI JSON row arrays.
type Quality struct {
	ActiveSessions      int      `json:"active_sessions"`
	TokenSessions       int      `json:"token_sessions"`
	SessionCoveragePct  float64  `json:"session_coverage_percent"`
	ActiveAgents        int      `json:"active_agents"`
	TokenAgents         int      `json:"token_agents"`
	AgentCoveragePct    float64  `json:"agent_coverage_percent"`
	Requests            int      `json:"requests"`
	DetailedRequests    int      `json:"detailed_requests"`
	AggregateRequests   int      `json:"aggregate_requests"`
	RequestCoveragePct  float64  `json:"request_coverage_percent"`
	SampledRequests     int      `json:"sampled_requests"`
	UntimedRequests     int      `json:"untimed_requests"`
	OutOfWindowRequests int      `json:"out_of_window_requests"`
	SpeedSamplePct      float64  `json:"speed_sample_percent"`
	CostUSD             float64  `json:"cost_usd"`
	EstimatedCostUSD    float64  `json:"estimated_cost_usd"`
	EstimatedCostShare  float64  `json:"estimated_cost_share"`
	PricingSources      []string `json:"pricing_sources"`
}

type SubscriptionPlan struct {
	Name       string
	MonthlyUSD float64
}

type SubscriptionComparison struct {
	Plans                   []SubscriptionPlan
	SubscriptionMonthlyUSD  float64
	APIEquivalentMonthlyUSD float64
	ValueRatio              float64
}

// Wrapped is the stable summary object returned by `--by wrapped`.
type Wrapped struct {
	Period         string  `json:"period"`
	Days           int     `json:"days"`
	ActiveDays     int     `json:"active_days"`
	Sessions       int     `json:"sessions"`
	Queries        int     `json:"queries"`
	ToolCalls      int     `json:"tool_calls"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CostUSD        float64 `json:"cost_usd"`
	TopModel       string  `json:"top_model"`
	TopProject     string  `json:"top_project"`
	PeakDay        string  `json:"peak_day"`
	PeakCost       float64 `json:"peak_cost"`
	TopProjectCost float64 `json:"-"`
}

// Result is a discriminated application result: Group identifies which row set
// is populated. Totals and Subscription are computed once here so every adapter
// uses the same aggregation policy.
type Result struct {
	Group       Group
	Window      Window
	SyncedFiles int

	Projects     []ProjectRow
	Models       []ModelRow
	ModelPeriods []ModelPeriodRow
	Skills       []SkillRow
	Sessions     []SessionRow
	SessionUsage []SessionUsageRow
	Requests     []RequestRow
	Speed        SpeedReport
	Periods      []PeriodRow
	Wrapped      *Wrapped

	Totals       Totals
	Quality      Quality
	Subscription *SubscriptionComparison
}
