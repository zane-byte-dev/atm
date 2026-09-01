package parser

import (
	"path/filepath"
	"time"
)

// Agent abstracts a single AI coding assistant. Adding a new agent only
// requires implementing this interface and registering it; sync and status
// iterate the registry instead of switching on agent names.
type Agent interface {
	Name() string
	Discover() []string
	ParseFile(path string) *ParsedFile
	LiveSessions(maxAge time.Duration) []Session
	// ParseAppend parses only the bytes after offset for append-only transcripts,
	// returning just the new content (Append=true). It returns nil when the agent
	// does not support incremental sync or when a full re-parse is required (e.g.
	// a continuation replay that must be deduped). Callers fall back to ParseFile
	// on nil, so returning nil is always safe.
	ParseAppend(path string, offset int64) *ParsedFile
}

var registry = map[string]Agent{}
var order []string

func register(a Agent) {
	registry[a.Name()] = a
	order = append(order, a.Name())
}

// Get returns the agent by name, or nil if not registered.
func Get(name string) Agent { return registry[name] }

// All returns registered agents in registration order.
func All() []Agent {
	out := make([]Agent, 0, len(order))
	for _, n := range order {
		out = append(out, registry[n])
	}
	return out
}

func init() {
	register(piAgent{})
	register(claudeAgent{})
	register(codexAgent{})
	register(copilotAgent{})
	register(qoderAgent{})
	register(qoderCLIAgent{})
	register(qoderWorkAgent{})
	register(grokBuildAgent{})
	register(antigravityAgent{})
}

type claudeAgent struct{}

func (claudeAgent) Name() string                              { return "claude" }
func (claudeAgent) Discover() []string                        { return DiscoverClaude() }
func (claudeAgent) ParseFile(p string) *ParsedFile            { return ClaudeParseFile(p) }
func (claudeAgent) ParseAppend(p string, o int64) *ParsedFile { return ClaudeParseAppend(p, o) }
func (claudeAgent) LiveSessions(d time.Duration) []Session    { return ClaudeLiveSessions(d) }

type piAgent struct{}

func (piAgent) Name() string                              { return "pi" }
func (piAgent) Discover() []string                        { return DiscoverPi() }
func (piAgent) ParseFile(p string) *ParsedFile            { return PiParseFile(p) }
func (piAgent) ParseAppend(p string, o int64) *ParsedFile { return PiParseAppend(p, o) }
func (piAgent) LiveSessions(d time.Duration) []Session    { return PiLiveSessions(d) }

type codexAgent struct{}

func (codexAgent) Name() string                   { return "codex" }
func (codexAgent) Discover() []string             { return DiscoverCodex() }
func (codexAgent) ParseFile(p string) *ParsedFile { return CodexParseFile(p) }

// ParseAppend is unsupported: Codex token accounting relies on cumulative
// snapshots (previousTotal), so resuming mid-file would lose the baseline and
// mis-count usage. Return nil to force a full re-parse.
func (codexAgent) ParseAppend(string, int64) *ParsedFile  { return nil }
func (codexAgent) LiveSessions(d time.Duration) []Session { return CodexLiveSessions(d) }

type copilotAgent struct{}

type qoderAgent struct{}

type qoderCLIAgent struct{}

type qoderWorkAgent struct{}

func (qoderAgent) Name() string                           { return "qoder" }
func (qoderAgent) Discover() []string                     { return DiscoverQoder() }
func (qoderAgent) ParseFile(p string) *ParsedFile         { return QoderParseFile(p) }
func (qoderAgent) ParseAppend(string, int64) *ParsedFile  { return nil }
func (qoderAgent) LiveSessions(d time.Duration) []Session { return QoderLiveSessions(d) }

func (qoderCLIAgent) Name() string                              { return "qodercli" }
func (qoderCLIAgent) Discover() []string                        { return DiscoverQoderCLI() }
func (qoderCLIAgent) ParseFile(p string) *ParsedFile            { return QoderCLIParseFile(p) }
func (qoderCLIAgent) ParseAppend(p string, o int64) *ParsedFile { return QoderCLIParseAppend(p, o) }
func (qoderCLIAgent) LiveSessions(d time.Duration) []Session    { return QoderCLILiveSessions(d) }

func (qoderWorkAgent) Name() string                           { return "qoderwork" }
func (qoderWorkAgent) Discover() []string                     { return DiscoverQoderWork() }
func (qoderWorkAgent) ParseFile(p string) *ParsedFile         { return QoderWorkParseFile(p) }
func (qoderWorkAgent) ParseAppend(string, int64) *ParsedFile  { return nil }
func (qoderWorkAgent) LiveSessions(d time.Duration) []Session { return QoderWorkLiveSessions(d) }

func (copilotAgent) Name() string { return "copilot" }

// ParseAppend is unsupported for Copilot; full re-parse only.
func (copilotAgent) ParseAppend(string, int64) *ParsedFile { return nil }
func (copilotAgent) Discover() []string {
	var files []string
	for _, t := range FindCopilotTranscripts() {
		files = append(files, t.Path)
	}
	return files
}
func (copilotAgent) ParseFile(p string) *ParsedFile {
	return CopilotParseFile(p, copilotProjectName(filepath.Dir(filepath.Dir(filepath.Dir(p)))))
}
func (copilotAgent) LiveSessions(d time.Duration) []Session { return CopilotLiveSessions(d) }

type grokBuildAgent struct{}

func (grokBuildAgent) Name() string { return "grokbuild" }

// ParseAppend is unsupported: token usage lives in sibling updates.jsonl and is
// aggregated per turn, so a chat_history-only tail parse would under-count.
func (grokBuildAgent) ParseAppend(string, int64) *ParsedFile { return nil }
func (grokBuildAgent) Discover() []string                    { return DiscoverGrok() }
func (grokBuildAgent) ParseFile(p string) *ParsedFile        { return GrokParseFile(p) }
func (grokBuildAgent) LiveSessions(d time.Duration) []Session {
	return GrokLiveSessions(d)
}

// SourceVersion fingerprints chat_history + updates + summary so a late
// turn_completed write still triggers a full re-parse.
func (grokBuildAgent) SourceVersion(path string) (mtime, size int64, ok bool) {
	return GrokSourceVersion(path)
}

type antigravityAgent struct{}

func (antigravityAgent) Name() string                   { return "antigravity" }
func (antigravityAgent) Discover() []string             { return DiscoverAntigravity() }
func (antigravityAgent) ParseFile(p string) *ParsedFile { return AntigravityParseFile(p) }

// ParseAppend is unsupported: the transcript is a SQLite database, not an
// append-only log, so there is no offset a tail parse could resume from.
func (antigravityAgent) ParseAppend(string, int64) *ParsedFile  { return nil }
func (antigravityAgent) LiveSessions(d time.Duration) []Session { return AntigravityLiveSessions(d) }

// SourceVersion folds in the sibling WAL files; see AntigravitySourceVersion for
// why reading the database alone would strand new calls.
func (antigravityAgent) SourceVersion(path string) (mtime, size int64, ok bool) {
	return AntigravitySourceVersion(path)
}
