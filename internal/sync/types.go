// Package sync owns the derived session index's refresh and freshness. Command
// and Web adapters ask it to run or report; they do not open the index, decide
// which agents to scan, or reproduce the "do not create a database just to look
// at it" rule.
package sync

// RunInput is a refresh request. An empty Agent rescans every source.
type RunInput struct {
	Agent string `json:"agent,omitempty"`
}

// RunResult is what a refresh accomplished. Warnings are the side jobs that
// degraded without failing the refresh — quota history is a convenience, the
// session index is the point.
type RunResult struct {
	SyncedFiles int      `json:"synced"`
	Warnings    []string `json:"warnings,omitempty"`
}

// StatusInput asks how fresh the index is. Scope is the sync scope to report on;
// empty means every source. Sync lets the caller build the index if it is absent,
// which a plain status read deliberately does not do.
type StatusInput struct {
	Scope string `json:"scope,omitempty"`
	Sync  bool   `json:"sync,omitempty"`
}

// StatusIndex describes the index file itself.
type StatusIndex struct {
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	SchemaVersion int    `json:"schema_version"`
	// IndexedSessions counts every session the index holds, including the
	// RetainedSessions whose transcript is no longer on disk. It grows
	// monotonically, so it is not a count of what an agent currently stores.
	IndexedSessions  int `json:"indexed_sessions"`
	RetainedSessions int `json:"retained_sessions"`
}

// StatusState is the last refresh's outcome and how stale it has become.
type StatusState struct {
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

// StatusReport is the published freshness shape the browser workspace decodes.
type StatusReport struct {
	GeneratedAt string      `json:"generated_at"`
	Index       StatusIndex `json:"index"`
	Sync        StatusState `json:"sync"`
}
