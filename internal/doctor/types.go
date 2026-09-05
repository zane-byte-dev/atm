// Package doctor owns ATM's self-check: what it can see, how much of it it
// understood, and every way the install can be quietly broken. Command and Web
// adapters render the report; they do not open the session index or derive
// findings of their own.
package doctor

import "github.com/zane-byte-dev/atm/internal/store"

// Input selects an optional recent event-time coverage window. Zero preserves
// the historical all-time doctor behavior.
type Input struct {
	Days int `json:"days,omitempty"`
}

type CoverageWindow struct {
	Mode  string `json:"mode"`
	Days  int    `json:"days,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Source is one agent's data source as ATM currently finds it.
type Source struct {
	Agent           string `json:"agent"`
	Path            string `json:"path"`
	Exists          bool   `json:"exists"`
	Files           int    `json:"files"`
	IndexedSessions int    `json:"indexed_sessions"`
	// RetainedSessions is the part of IndexedSessions whose transcript is no
	// longer on disk. ATM keeps those on purpose so rotated logs don't erase
	// spend and history, which also means IndexedSessions counts every session
	// ever seen rather than the files discovered now.
	RetainedSessions int    `json:"retained_sessions"`
	Status           string `json:"status"`
}

// Issue is one finding. Code is the stable identifier a consumer matches on;
// Detail and Suggestion are what a person reads.
type Issue struct {
	Severity   string `json:"severity"`
	Domain     string `json:"domain"`
	Code       string `json:"code"`
	Subject    string `json:"subject"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

// Summary is the count consumers show without reading the list. It is a struct
// rather than a computed field so the CLI and browser workspace publish the same
// shape.
type Summary struct {
	Issues int `json:"issues"`
}

// Report is everything a check found. The JSON tags are the published contract:
// `atm doctor --json` and the browser workspace both serialize this value, so a
// field added here changes both on purpose rather than by accident.
type Report struct {
	CoverageWindow CoverageWindow              `json:"coverage_window"`
	Sources        []Source                    `json:"sources"`
	Coverage       []store.Coverage            `json:"coverage"`
	ModelPricing   []store.ModelPricing        `json:"model_pricing"`
	TodoIssues     []store.TodoDependencyIssue `json:"todo_dependency_issues"`
	Issues         []Issue                     `json:"issues"`
	Summary        Summary                     `json:"summary"`
}
