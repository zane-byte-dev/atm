package parser

import "time"

func truncateText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type Session struct {
	Tool          string
	Project       string
	SessionID     string
	ResumeID      string
	Client        string
	CWD           string
	StartedAt     time.Time
	Model         string
	AgeSeconds    int
	Summary       string
	FirstQ        string
	LastUserMsg   string
	RecentTools   []string
	LastAssistant string
	LatestResult  string
	RecentUpdates []string
	Topics        []string
}

type Message struct {
	Content string
	TS      int64
}

type TranscriptMessage struct {
	Role    string
	Content string
	TS      int64
}

type UsageEvent struct {
	Model             string
	TS                int64
	InputTokens       int64
	OutputTokens      int64
	CacheCreateTokens int64
	CacheReadTokens   int64
	// RequestCount is how many model calls this event represents. Zero means 1.
	// Grok turn_completed rows aggregate several modelCalls into one usage blob;
	// storing the count here keeps token totals accurate while request stats and
	// doctor coverage still see every call.
	RequestCount int
	// Fingerprint identifies the model request this event reports, stably enough
	// that two transcript files describing the same request produce the same
	// value. Resuming or forking a session copies the earlier transcript into a
	// new file, so without it the copied requests are counted a second time.
	// Empty means the transcript offers no usable identity; such events are
	// always counted.
	Fingerprint string
	// DurationMS is how long the model spent generating, covering every call this
	// event represents. Grok reads it straight from its own log; the other agents
	// derive it from record timestamps via durationTracker. 0 means the transcript
	// does not bound the window, not that the response was instant.
	DurationMS int64
}

// EventRequestCount returns how many model requests an event represents.
func EventRequestCount(u UsageEvent) int {
	if u.RequestCount > 0 {
		return u.RequestCount
	}
	return 1
}

type SkillEvent struct {
	Name string
	TS   int64
}

type Usage struct {
	Model             string
	InputTokens       int64
	OutputTokens      int64
	CacheCreateTokens int64
	CacheReadTokens   int64
	RequestCount      int
}

type ParsedFile struct {
	SessionID   string
	ShortID     string
	Agent       string
	Project     string
	CreatedAt   string
	CreatedTS   int64
	LastTS      int64
	Summary     string
	Inputs      []Message
	Outputs     []Message
	Messages    []TranscriptMessage
	Tools       map[string]int
	Skills      []SkillEvent
	Usage       Usage
	UsageEvents []UsageEvent
	// EndOffset is the byte size consumed; stored so subsequent syncs of an
	// append-only file can resume from here. Append reports only new content.
	EndOffset int64
	Append    bool
}
