// Package aiday builds a privacy-preserving daily projection from ATM's local
// session index. Raw messages are read transiently for local classification and
// are never copied into AI Day tables or returned by its public contracts.
package aiday

const (
	ContractVersion = 2
	EventVersion    = 1
	FeatureVersion  = 2
	EngineVersion   = 2
)

var SemanticIntents = []string{
	"correction", "retry", "refinement", "question", "directive",
	"acceptance", "brainstorm", "explanation",
}

// Features is the event-time aggregate for one local calendar day.
type Features struct {
	Day               string           `json:"day"`
	Timezone          string           `json:"timezone"`
	SessionCount      int64            `json:"session_count"`
	EventCount        int64            `json:"event_count"`
	TurnCount         int64            `json:"turn_count"`
	ToolCalls         int64            `json:"tool_calls"`
	SourceCount       int64            `json:"source_count"`
	InputTokens       int64            `json:"input_tokens"`
	OutputTokens      int64            `json:"output_tokens"`
	CacheCreateTokens int64            `json:"cache_create_tokens"`
	CacheReadTokens   int64            `json:"cache_read_tokens"`
	GenerationSeconds int64            `json:"generation_seconds"`
	ActiveSeconds     int64            `json:"active_seconds"`
	ForegroundSeconds int64            `json:"foreground_seconds"`
	BackgroundSeconds int64            `json:"background_seconds"`
	SemanticCounts    map[string]int64 `json:"semantic_counts"`
	ModalityCounts    map[string]int64 `json:"modality_counts"`
	BuiltAt           int64            `json:"built_at"`
	FeatureVersion    int              `json:"feature_version"`
}

func (f Features) TotalTokens() int64 {
	return f.InputTokens + f.OutputTokens + f.CacheCreateTokens + f.CacheReadTokens
}

func (f Features) Empty() bool {
	return f.EventCount == 0 && f.SessionCount == 0 && f.TurnCount == 0 && f.ToolCalls == 0 && f.TotalTokens() == 0
}

// Evidence is a structured, content-free reason for selecting a concept.
type Evidence struct {
	Metric     string  `json:"metric"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	Comparison string  `json:"comparison,omitempty"`
}

type Concept struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Explanation string     `json:"explanation"`
	Tags        []string   `json:"tags"`
	Evidence    []Evidence `json:"evidence"`
	Confidence  float64    `json:"confidence"`
}

type Badge struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Family         string     `json:"family"`
	Kind           string     `json:"kind"`
	Level          int        `json:"level"`
	Unlocked       bool       `json:"unlocked"`
	QualifiedDays  int        `json:"qualified_days"`
	QualifiedDates []string   `json:"qualified_dates"`
	NextLevelDays  int        `json:"next_level_days"`
	Progress       float64    `json:"progress"`
	LastQualified  string     `json:"last_qualified,omitempty"`
	CooldownUntil  string     `json:"cooldown_until,omitempty"`
	Score          float64    `json:"score,omitempty"`
	Evidence       []Evidence `json:"evidence,omitempty"`
}

// Result is the versioned public contract returned by `atm day`.
type Result struct {
	SchemaVersion int                `json:"schema_version"`
	Day           string             `json:"day"`
	State         string             `json:"state"`
	Timezone      string             `json:"timezone"`
	Features      Features           `json:"features"`
	Concept       *Concept           `json:"concept,omitempty"`
	Badge         *Badge             `json:"badge,omitempty"`
	Candidates    []Badge            `json:"candidates,omitempty"`
	BaselineDays  int                `json:"baseline_days"`
	Percentiles   map[string]float64 `json:"percentiles,omitempty"`
	GeneratedAt   int64              `json:"generated_at"`
	EngineVersion int                `json:"engine_version"`
}

type Atlas struct {
	SchemaVersion int     `json:"schema_version"`
	GeneratedAt   int64   `json:"generated_at"`
	Unlocked      int     `json:"unlocked"`
	Total         int     `json:"total"`
	Badges        []Badge `json:"badges"`
}

type History struct {
	SchemaVersion int      `json:"schema_version"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Days          []Result `json:"days"`
}

type Dashboard struct {
	SchemaVersion int     `json:"schema_version"`
	Today         Result  `json:"today"`
	Atlas         Atlas   `json:"atlas"`
	History       History `json:"history"`
	Privacy       Privacy `json:"privacy"`
}

type SourceSetting struct {
	Source          string `json:"source"`
	Enabled         bool   `json:"enabled"`
	SemanticEnabled bool   `json:"semantic_enabled"`
	EventCount      int64  `json:"event_count"`
	LastEventAt     int64  `json:"last_event_at"`
}

type Privacy struct {
	SchemaVersion   int             `json:"schema_version"`
	SemanticEnabled bool            `json:"semantic_enabled"`
	RetentionDays   int             `json:"retention_days"`
	RawRetained     bool            `json:"raw_content_retained"`
	Sources         []SourceSetting `json:"sources"`
}

type Feedback struct {
	Day            string   `json:"day"`
	Verdict        string   `json:"verdict"`
	CorrectedBadge string   `json:"corrected_badge_id,omitempty"`
	SemanticLabels []string `json:"semantic_labels,omitempty"`
	UpdatedAt      int64    `json:"updated_at"`
}

type DeleteSummary struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Events      int64  `json:"events_deleted"`
	Projections int64  `json:"projections_deleted"`
	Feedback    int64  `json:"feedback_deleted"`
}

type RebuildSummary struct {
	SchemaVersion int      `json:"schema_version"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Count         int      `json:"count"`
	Days          []Result `json:"days"`
}
