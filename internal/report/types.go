// Package report owns the daily activity report: which day, which sessions are
// worth listing, and what of each session's transcript is readable. Command and
// IPC adapters decide column widths and truncation; they do not open the session
// index or reproduce the selection rules.
package report

// Input is the complete application request. Date accepts today, yesterday, or
// YYYY-MM-DD. Verbose asks for the full question-and-answer text rather than a
// prompt count, and widens which sessions qualify.
type Input struct {
	Date    string `json:"date,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Verbose bool   `json:"verbose,omitempty"`
}

// Report is one day's activity, grouped by agent in ATM's fixed display order.
type Report struct {
	// Date is the day reported on, normalized to YYYY-MM-DD. A caller that passed
	// "yesterday" gets the resolved date back rather than its own word.
	Date   string         `json:"date"`
	Agents []AgentSection `json:"agents"`
}

// Empty reports whether the day has nothing to show, so an adapter does not have
// to reproduce that test by summing sections.
func (report Report) Empty() bool { return len(report.Agents) == 0 }

// AgentSection is one agent's sessions for the day. It only exists when that
// agent has at least one session worth listing.
type AgentSection struct {
	Agent string `json:"agent"`
	// DisplayName is the heading — "Claude Code" rather than "claude". It belongs
	// with the data because the same mapping names agents everywhere else in ATM.
	DisplayName string    `json:"display_name"`
	Sessions    []Session `json:"sessions"`
}

// Session is one session's readable activity. Prompts and Exchanges are already
// cleaned of the harness noise that surrounds a person's actual words; what is
// left is what a human typed.
type Session struct {
	Project string `json:"project"`
	ShortID string `json:"short_id"`
	// Summary is the session's stored summary, or its first prompt when there is
	// none — an unsummarized session is still worth naming by what it opened with.
	Summary   string         `json:"summary"`
	Prompts   []string       `json:"prompts"`
	Tools     map[string]int `json:"tools"`
	ToolCalls int            `json:"tool_calls"`
	// Exchanges is populated only for a verbose report, which is the one that
	// shows transcript text rather than counts.
	Exchanges []Exchange `json:"exchanges,omitempty"`
}

// Exchange is one prompt and the reply that followed it, if there was one.
type Exchange struct {
	Question string `json:"question"`
	Answer   string `json:"answer,omitempty"`
}
