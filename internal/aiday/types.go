// Package aiday builds a privacy-preserving daily projection from ATM's local
// session index. Raw messages are read transiently for local classification and
// are never copied into AI Day tables or returned by its public contracts.
package aiday

const (
	ContractVersion = 3
	EventVersion    = 2
	FeatureVersion  = 3
	EngineVersion   = 3
)

// SelfSources are ATM's own model calls. They are recorded in usage_events like
// any agent's, but they are ATM working on the user's behalf rather than the
// user working with an AI, so counting them would invent a second "AI source"
// the user never chose.
var SelfSources = map[string]bool{"atm": true}

func IsSelfSource(source string) bool { return SelfSources[source] }

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

// WorkTokens excludes cache reads. A cached prompt is re-read on every turn, so
// cache_read grows with context size rather than with work done and dwarfs the
// rest by one to two orders of magnitude. Thresholds, percentiles and anything
// shown as "how much happened today" use this instead of TotalTokens.
func (f Features) WorkTokens() int64 {
	return f.InputTokens + f.OutputTokens + f.CacheCreateTokens
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
	// Confidence is how much to trust this specific conclusion. It combines
	// baseline length, the selected badge's evidence strength and source
	// coverage. It is never raised by user feedback: a correction changes what
	// the day is, not how certain the engine is.
	Confidence float64 `json:"confidence"`
	// EvidenceStrength is the selected badge's own normalized signal, split out
	// so a card can say "weak evidence, long history" instead of one blended number.
	EvidenceStrength float64 `json:"evidence_strength"`
	// Origin is "computed" or "user_corrected".
	Origin string `json:"origin"`
	// ComputedID/ComputedTitle preserve what the engine chose before a
	// correction, so the UI can show both instead of erasing its own answer.
	ComputedID    string `json:"computed_id,omitempty"`
	ComputedTitle string `json:"computed_title,omitempty"`
}

// Coverage reports whether the day's inputs look complete. AI Day reads a
// session mirror that other processes fill in as sessions flush, so a day being
// viewed can legitimately be missing a source that was active all week.
type Coverage struct {
	Complete       bool     `json:"complete"`
	ExpectedSource int      `json:"expected_sources"`
	PresentSource  int      `json:"present_sources"`
	MissingSources []string `json:"missing_sources,omitempty"`
	DataThrough    int64    `json:"data_through"`
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
	SchemaVersion int      `json:"schema_version"`
	Day           string   `json:"day"`
	State         string   `json:"state"`
	Timezone      string   `json:"timezone"`
	Features      Features `json:"features"`
	Concept       *Concept `json:"concept,omitempty"`
	Badge         *Badge   `json:"badge,omitempty"`
	Candidates    []Badge  `json:"candidates,omitempty"`
	BaselineDays  int      `json:"baseline_days"`
	// Percentiles is omitted while Provisional is true: ranking a partial day
	// against complete days puts every in-progress day at the bottom.
	Percentiles map[string]float64 `json:"percentiles,omitempty"`
	// Provisional marks a day that has not finished in the user's timezone. Its
	// conclusion is the best read of the data so far and is expected to change.
	Provisional bool      `json:"provisional"`
	Coverage    *Coverage `json:"coverage,omitempty"`
	Feedback    *Feedback `json:"feedback,omitempty"`
	GeneratedAt   int64 `json:"generated_at"`
	EngineVersion int   `json:"engine_version"`
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
