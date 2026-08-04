package parser

// durationTracker derives how long the model spent producing one response, for
// the agents that do not say.
//
// Grok is the exception: it logs apiDurationMs and its parser uses that instead
// of this. Everywhere else the transcript carries no duration at all — Claude's
// `usage.speed` is the tier it ran in ("standard"/"fast"), not a measurement — so
// the only available signal is the record timestamps. The window measured here
// runs from the last input the request carried (a human message, a tool result)
// to the last output record the response produced (text, reasoning, a tool call).
// Tool execution falls between two windows rather than inside one, which is what
// keeps the number about the model instead of the work around it — and what makes
// it comparable to Grok's own figure.
//
// Agents disagree about where the usage record sits. Claude reports usage on the
// response records themselves, so its window is still open when the event is
// built. Codex writes `token_count` alongside the tool output that the *next*
// request starts from, so by then the window has already closed — pendingMS
// holds it for exactly one reader rather than letting it be discarded.
//
// Transcripts do not always describe a window. An incremental parse can start in
// the middle of a response, and Codex's sub-agent rollups report usage for
// requests whose output records live in another file. Those measure as 0, which
// readers must treat as "unknown" rather than "instant".
type durationTracker struct {
	inputEndMS  int64
	outputEndMS int64
	// pendingMS is a finished window whose usage record has not arrived yet.
	pendingMS int64
	// claimed records that a usage event already reported the current window. A
	// second event may only report it once more output has arrived — Codex logs
	// runs of usage records for requests whose responses were written to another
	// file, and without this they would all inherit the last measured window and
	// read as measured when they are not.
	claimed bool
}

// Input marks a record the model reads. It closes the open window — the next
// output belongs to the next request — and keeps it for one Measure call.
func (d *durationTracker) Input(ms int64) {
	if ms <= 0 {
		return
	}
	if open := d.open(); open > 0 && !d.claimed {
		d.pendingMS = open
	}
	d.inputEndMS = ms
	d.outputEndMS = 0
}

// Output marks a record the model wrote. A response split across several records
// keeps the latest of them as its end.
func (d *durationTracker) Output(ms int64) {
	if ms <= 0 {
		return
	}
	if ms > d.outputEndMS {
		d.outputEndMS = ms
	}
	// The next response has started, so a window still waiting to be claimed
	// belongs to nothing that is coming — and the window now growing is worth
	// reporting again, even if an earlier record of it already was.
	d.pendingMS = 0
	d.claimed = false
}

// Measure returns this response's window in milliseconds, or 0 when the records
// do not bound one. The open window wins over a closed one, and either is handed
// out only until the next output record arrives: no two responses may be timed by
// the same window.
func (d *durationTracker) Measure() int64 {
	if d.claimed {
		return 0
	}
	d.claimed = true
	if open := d.open(); open > 0 {
		return open
	}
	pending := d.pendingMS
	d.pendingMS = 0
	return pending
}

func (d *durationTracker) open() int64 {
	if d.inputEndMS <= 0 || d.outputEndMS <= d.inputEndMS {
		return 0
	}
	return d.outputEndMS - d.inputEndMS
}
