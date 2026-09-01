package parser

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// antigravityGen builds one gen_metadata blob in the shape the real client
// writes, so the field numbers under test are the ones production reads.
type antigravityGen struct {
	model      string
	input      int64
	output     int64
	cacheHit   int64
	responseID string
	startTS    int64
	durationS  int64
	durationNS int64
	errorText  string
}

func (g antigravityGen) encode() []byte {
	tokens := protoEncodeVarint(antigravityTokensInput, g.input)
	tokens = append(tokens, protoEncodeVarint(antigravityTokensOutput, g.output)...)
	tokens = append(tokens, protoEncodeVarint(antigravityTokensCacheHit, g.cacheHit)...)
	if g.responseID != "" {
		tokens = append(tokens, protoEncodeString(antigravityTokensResponseID, g.responseID)...)
	}

	timing := protoEncodeBytes(antigravityTimingStart,
		protoEncodeVarint(antigravityTimestampSeconds, g.startTS))
	if g.durationS != 0 || g.durationNS != 0 {
		duration := protoEncodeVarint(1, g.durationS)
		duration = append(duration, protoEncodeVarint(2, g.durationNS)...)
		timing = append(timing, protoEncodeBytes(antigravityTimingLength, duration)...)
	}

	record := protoEncodeBytes(antigravityGenTokens, tokens)
	record = append(record, protoEncodeBytes(antigravityGenTiming, timing)...)
	record = append(record, protoEncodeString(antigravityGenModelName, g.model)...)

	blob := protoEncodeBytes(antigravityGenRoot, record)
	if g.errorText != "" {
		blob = append(blob, protoEncodeString(antigravityGenError, g.errorText)...)
	}
	return blob
}

// seedAntigravity writes one conversation database and points config at a fake
// Antigravity directory, returning the database path.
func seedAntigravity(t *testing.T, conversationID string, gens ...antigravityGen) string {
	t.Helper()
	oldDir := config.AntigravityDir
	root := t.TempDir()
	config.AntigravityDir = root
	t.Cleanup(func() { config.AntigravityDir = oldDir })

	if err := os.MkdirAll(config.AntigravityConversations(), 0755); err != nil {
		t.Fatalf("mkdir conversations: %v", err)
	}
	path := filepath.Join(config.AntigravityConversations(), conversationID+".db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE gen_metadata (idx INTEGER PRIMARY KEY, data BLOB, size INTEGER NOT NULL DEFAULT 0)"); err != nil {
		t.Fatalf("create gen_metadata: %v", err)
	}
	for index, gen := range gens {
		blob := gen.encode()
		if _, err := db.Exec("INSERT INTO gen_metadata (idx, data, size) VALUES (?, ?, ?)", index, blob, len(blob)); err != nil {
			t.Fatalf("insert gen %d: %v", index, err)
		}
	}
	return path
}

// seedAntigravitySummary writes the index entry that carries a conversation's
// title and workspace, which the database itself does not hold.
func seedAntigravitySummary(t *testing.T, conversationID, title, folderURI string, createdTS, updatedTS int64) {
	t.Helper()
	body := protoEncodeString(antigravityBodyTitle, title)
	body = append(body, protoEncodeBytes(antigravityBodyUpdated,
		protoEncodeVarint(antigravityTimestampSeconds, updatedTS))...)
	body = append(body, protoEncodeBytes(antigravityBodyCreated,
		protoEncodeVarint(antigravityTimestampSeconds, createdTS))...)
	if folderURI != "" {
		workspace := protoEncodeString(antigravityWorkspaceFolder, folderURI)
		workspace = append(workspace, protoEncodeString(antigravityWorkspaceBranch, "main")...)
		body = append(body, protoEncodeBytes(antigravityBodyWorkspace, workspace)...)
	}
	entry := protoEncodeString(antigravityEntryID, conversationID)
	entry = append(entry, protoEncodeBytes(antigravityEntryBody, body)...)

	if err := os.WriteFile(config.AntigravitySummaries(), protoEncodeBytes(antigravitySummaryEntry, entry), 0644); err != nil {
		t.Fatalf("write summaries: %v", err)
	}
}

func TestAntigravityParseFile(t *testing.T) {
	const id = "9d5b8fb4-4502-45f1-ab70-8bf612c731b2"
	path := seedAntigravity(t, id,
		// First call of a conversation: nothing cached yet.
		antigravityGen{model: "Gemini 3.1 Pro (High)", input: 22701, output: 4463,
			responseID: "req-one", startTS: 1786892782, durationS: 15, durationNS: 107273000},
		// Later call: most of the prompt hit cache. input is the part that missed,
		// so it is smaller than the cached count and must not be reduced further.
		antigravityGen{model: "Gemini 3.1 Pro (High)", input: 4461, output: 842, cacheHit: 20452,
			responseID: "req-two", startTS: 1786892819, durationS: 37},
		// A failed call: no tokens, no response id. It must not become an event —
		// it would add a request with no work behind it and nothing to dedupe by.
		antigravityGen{model: "Gemini 3.6 Flash (High)", startTS: 1786892900,
			errorText: "FAILED_PRECONDITION (code 400): User location is not supported"},
	)
	seedAntigravitySummary(t, id, "徽章设计优化", "file://"+t.TempDir(), 1786892766, 1786893113)

	got := AntigravityParseFile(path)
	if got == nil {
		t.Fatal("AntigravityParseFile returned nil")
	}
	if got.SessionID != "antigravity:"+id || got.Agent != "antigravity" || got.ShortID != "9d5b8fb4" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Summary != "徽章设计优化" {
		t.Fatalf("summary = %q", got.Summary)
	}
	if got.CreatedTS != 1786892766 || got.LastTS != 1786893113 {
		t.Fatalf("created=%d last=%d", got.CreatedTS, got.LastTS)
	}
	if len(got.UsageEvents) != 2 {
		t.Fatalf("events = %d, want 2 (the failed call must be skipped)", len(got.UsageEvents))
	}

	first := got.UsageEvents[0]
	if first.Model != "gemini-3.1-pro" {
		t.Fatalf("model = %q, want the reasoning suffix dropped", first.Model)
	}
	if first.InputTokens != 22701 || first.OutputTokens != 4463 || first.CacheReadTokens != 0 {
		t.Fatalf("first event tokens = %#v", first)
	}
	if first.Fingerprint != "antigravity:req-one" {
		t.Fatalf("fingerprint = %q", first.Fingerprint)
	}
	if first.TS != 1786892782 {
		t.Fatalf("ts = %d", first.TS)
	}
	if first.DurationMS != 15107 {
		t.Fatalf("duration = %d ms, want 15107", first.DurationMS)
	}
	if first.RequestCount != 1 {
		t.Fatalf("request count = %d", first.RequestCount)
	}

	// The cached and uncached inputs are two disjoint counts. Subtracting one from
	// the other — correct for endpoints whose prompt total includes hits — would
	// under-report input here, or go negative.
	second := got.UsageEvents[1]
	if second.InputTokens != 4461 || second.CacheReadTokens != 20452 {
		t.Fatalf("second event = in %d / cache %d, want 4461 / 20452 with no subtraction",
			second.InputTokens, second.CacheReadTokens)
	}
	if second.CacheCreateTokens != 0 {
		t.Fatalf("cache create = %d; the upstream reports no such count", second.CacheCreateTokens)
	}

	if got.Usage.InputTokens != 22701+4461 || got.Usage.CacheReadTokens != 20452 ||
		got.Usage.OutputTokens != 4463+842 || got.Usage.RequestCount != 2 {
		t.Fatalf("aggregate usage = %#v", got.Usage)
	}
	if got.Usage.Model != "gemini-3.1-pro" {
		t.Fatalf("aggregate model = %q", got.Usage.Model)
	}
	// Messages are not extracted; see capabilities.go.
	if len(got.Messages) != 0 || len(got.Inputs) != 0 {
		t.Fatalf("messages = %d, inputs = %d; want none", len(got.Messages), len(got.Inputs))
	}
}

// A conversation whose every call failed gets no session at all: without message
// text there would be nothing in the row but a title.
func TestAntigravityParseFileSkipsUnbilledConversation(t *testing.T) {
	path := seedAntigravity(t, "1dda8e88-2923-47cb-929d-396db04e69bb",
		antigravityGen{model: "Gemini 3.1 Pro (High)", startTS: 100, errorText: "boom"},
		antigravityGen{model: "Gemini 3.1 Pro (High)", startTS: 200, errorText: "boom"},
	)
	if got := AntigravityParseFile(path); got != nil {
		t.Fatalf("expected nil for a conversation that billed nothing, got %#v", got)
	}
}

// The index is a separate file and may be missing or stale. Accounting is the
// point of this parser, so it has to survive without a title or a project.
func TestAntigravityParseFileWithoutSummaryIndex(t *testing.T) {
	path := seedAntigravity(t, "eabfd66e-d5ed-40c4-a51e-5eb51f04e0e5",
		antigravityGen{model: "GPT-OSS 120B (Medium)", input: 21541, output: 1350,
			responseID: "req-x", startTS: 1784711112},
	)
	got := AntigravityParseFile(path)
	if got == nil {
		t.Fatal("AntigravityParseFile returned nil without the index")
	}
	if got.Summary != "" || got.Project != "" {
		t.Fatalf("summary = %q, project = %q; both come from the index", got.Summary, got.Project)
	}
	if got.CreatedTS != 1784711112 {
		t.Fatalf("created = %d; want the first call's timestamp as the fallback", got.CreatedTS)
	}
	if len(got.UsageEvents) != 1 || got.UsageEvents[0].Model != "gpt-oss-120b" {
		t.Fatalf("events = %#v", got.UsageEvents)
	}
}

func TestAntigravityModelSlug(t *testing.T) {
	cases := map[string]string{
		"Gemini 3.1 Pro (High)":      "gemini-3.1-pro",
		"Gemini 3.6 Flash (High)":    "gemini-3.6-flash",
		"Gemini 3.6 Flash (Medium)":  "gemini-3.6-flash",
		"GPT-OSS 120B (Medium)":      "gpt-oss-120b",
		"Claude Opus 4.6 (Thinking)": "claude-opus-4.6",
		"gemini-3-pro-preview":       "gemini-3-pro-preview",
		"":                           "",
		"   ":                        "",
		"(High)":                     "",
	}
	for input, want := range cases {
		if got := antigravityModelSlug(input); got != want {
			t.Errorf("antigravityModelSlug(%q) = %q, want %q", input, got, want)
		}
	}
}

// The fingerprint is what stops a forked conversation from billing the same call
// twice, so equal response ids must produce equal fingerprints across databases.
func TestAntigravityFingerprintIsStableAcrossConversations(t *testing.T) {
	gen := antigravityGen{model: "Gemini 3.1 Pro (High)", input: 10, output: 5,
		responseID: "shared-id", startTS: 1786892782}

	first := AntigravityParseFile(seedAntigravity(t, "aaaaaaaa-0000-0000-0000-000000000000", gen))
	second := AntigravityParseFile(seedAntigravity(t, "bbbbbbbb-0000-0000-0000-000000000000", gen))
	if first == nil || second == nil {
		t.Fatal("expected both conversations to parse")
	}
	if first.SessionID == second.SessionID {
		t.Fatal("distinct conversations shared a session id")
	}
	if first.UsageEvents[0].Fingerprint != second.UsageEvents[0].Fingerprint {
		t.Fatalf("fingerprints differ: %q vs %q",
			first.UsageEvents[0].Fingerprint, second.UsageEvents[0].Fingerprint)
	}
}

// -shm must stay out of the fingerprint: its mtime advances whenever any process
// attaches, ATM's own read included, so including it made every sync see a change
// and re-parse everything forever.
func TestAntigravitySourceVersionIgnoresSharedMemoryFile(t *testing.T) {
	path := seedAntigravity(t, "cccccccc-0000-0000-0000-000000000000",
		antigravityGen{model: "Gemini 3.1 Pro (High)", input: 10, output: 5,
			responseID: "req-1", startTS: 100})

	mtime, size, ok := AntigravitySourceVersion(path)
	if !ok || mtime == 0 || size == 0 {
		t.Fatalf("baseline = %d, %d, %v", mtime, size, ok)
	}

	// A newer -shm alone must not move the fingerprint.
	if err := os.WriteFile(path+"-shm", make([]byte, 32768), 0644); err != nil {
		t.Fatalf("write -shm: %v", err)
	}
	if err := os.Chtimes(path+"-shm", time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("touch -shm: %v", err)
	}
	if gotMtime, gotSize, _ := AntigravitySourceVersion(path); gotMtime != mtime || gotSize != size {
		t.Fatalf("-shm changed the fingerprint: %d/%d -> %d/%d", mtime, size, gotMtime, gotSize)
	}

	// A -wal must, since it can hold commits the main file does not.
	if err := os.WriteFile(path+"-wal", make([]byte, 4096), 0644); err != nil {
		t.Fatalf("write -wal: %v", err)
	}
	gotMtime, gotSize, _ := AntigravitySourceVersion(path)
	if gotSize != size+4096 {
		t.Fatalf("-wal size not counted: %d, want %d", gotSize, size+4096)
	}
	if gotMtime < mtime {
		t.Fatalf("-wal mtime went backwards: %d < %d", gotMtime, mtime)
	}
}

func TestDiscoverAntigravityIgnoresSidecarFiles(t *testing.T) {
	path := seedAntigravity(t, "dddddddd-0000-0000-0000-000000000000",
		antigravityGen{model: "Gemini 3.1 Pro (High)", input: 1, output: 1,
			responseID: "req-1", startTS: 100})
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.WriteFile(path+suffix, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", suffix, err)
		}
	}
	got := DiscoverAntigravity()
	if len(got) != 1 || got[0] != path {
		t.Fatalf("DiscoverAntigravity() = %q, want just %q", got, path)
	}
}

func TestAntigravityLiveSessions(t *testing.T) {
	const id = "99b917e1-1d53-4234-94a6-1d3bb206525c"
	path := seedAntigravity(t, id,
		antigravityGen{model: "Gemini 3.1 Pro (High)", input: 10, output: 5,
			responseID: "req-1", startTS: 100},
		antigravityGen{model: "Gemini 3.6 Flash (High)", input: 20, output: 6,
			responseID: "req-2", startTS: 200},
	)
	seedAntigravitySummary(t, id, "Codex URL 参数配置", "file:///tmp/does-not-matter", 100, 200)

	got := AntigravityLiveSessions(time.Hour)
	if len(got) != 1 {
		t.Fatalf("live sessions = %d, want 1", len(got))
	}
	if got[0].SessionID != id || got[0].Tool != "antigravity" {
		t.Fatalf("session = %#v", got[0])
	}
	if got[0].Summary != "Codex URL 参数配置" {
		t.Fatalf("summary = %q", got[0].Summary)
	}
	// The newest row is what a live session is currently on.
	if got[0].Model != "gemini-3.6-flash" {
		t.Fatalf("model = %q, want the latest call's", got[0].Model)
	}

	// Backdating the database past the window drops it.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	if got := AntigravityLiveSessions(time.Hour); len(got) != 0 {
		t.Fatalf("stale conversation still live: %#v", got)
	}

	// A fresh -wal must not resurrect it. Reading a WAL database is enough to
	// move that file's mtime, ATM's own sync included, so counting it as activity
	// reported every conversation on disk as live with the same age.
	if err := os.WriteFile(path+"-wal", nil, 0644); err != nil {
		t.Fatalf("write -wal: %v", err)
	}
	if err := os.WriteFile(path+"-shm", make([]byte, 32768), 0644); err != nil {
		t.Fatalf("write -shm: %v", err)
	}
	if got := AntigravityLiveSessions(time.Hour); len(got) != 0 {
		t.Fatalf("a touched -wal resurrected a stale conversation: %#v", got)
	}
}

// CapabilitiesFor and the parser have to agree, or doctor reports the upstream's
// design as a regression.
func TestAntigravityCapabilitiesMatchExtraction(t *testing.T) {
	claims := CapabilitiesFor("antigravity")
	if claims.Messages {
		t.Fatal("antigravity claims message extraction it does not do")
	}
	if !claims.Usage {
		t.Fatal("antigravity extracts usage but does not claim it")
	}
}
