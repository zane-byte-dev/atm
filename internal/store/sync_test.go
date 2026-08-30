package store

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
)

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})

	db, err := Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	return db
}

func TestUpsertSessionPersistsMessagesToolsAndUsage(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "session-1",
		ShortID:   "session",
		Agent:     "claude",
		Project:   "atm",
		CreatedAt: "06-27 12:00",
		CreatedTS: 100,
		LastTS:    200,
		Summary:   "Test session",
		Inputs: []parser.Message{
			{Content: "First question", TS: 101},
			{Content: "Second question", TS: 103},
		},
		Outputs: []parser.Message{
			{Content: "First answer", TS: 102},
		},
		Tools: map[string]int{"Edit": 2},
		Usage: parser.Usage{
			Model:        "claude-sonnet-4-6",
			InputTokens:  1_000_000,
			OutputTokens: 100_000,
		},
	}

	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "session.jsonl"), "claude", 1, 2); err != nil {
		t.Fatalf("upsert session: %v", err)
	}

	got, err := GetSession(db, "session")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Agent != "Claude Code" || got.Project != "atm" || got.FullID != "session-1" {
		t.Fatalf("session meta = %#v", got)
	}
	if len(got.Inputs) != 2 || got.Inputs[0] != "First question" || got.Inputs[1] != "Second question" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0] != "First answer" {
		t.Fatalf("outputs = %#v", got.Outputs)
	}
	if got.Tools["Edit"] != 2 {
		t.Fatalf("tools = %#v", got.Tools)
	}

	var cost float64
	if err := db.QueryRow("SELECT cost_usd FROM usage WHERE session_id = ?", "session-1").Scan(&cost); err != nil {
		t.Fatalf("usage row: %v", err)
	}
	if math.Abs(cost-4.5) > 0.000001 {
		t.Fatalf("cost = %f, want 4.5", cost)
	}
}

func TestSearchMessagesFindsPersistedContent(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "search-session",
		ShortID:   "search",
		Agent:     "codex",
		Project:   "atm",
		CreatedAt: "06-27",
		CreatedTS: 100,
		LastTS:    100,
		Inputs: []parser.Message{
			{Content: "Find the buried keyword", TS: 100},
		},
		Outputs: []parser.Message{
			{Content: "The keyword is here too", TS: 101},
		},
		Tools: map[string]int{},
	}

	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "search.jsonl"), "codex", 1, 2); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	hits, err := SearchMessages(db, "keyword", "codex")
	if err != nil {
		t.Fatalf("search messages: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %#v", hits)
	}
	for _, hit := range hits {
		if hit.ShortID != "search" || hit.Agent != "codex" || hit.Project != "atm" {
			t.Fatalf("hit = %#v", hit)
		}
	}

	fallbackHits, err := SearchMessages(db, "buried key", "codex")
	if err != nil {
		t.Fatalf("fallback search messages: %v", err)
	}
	if len(fallbackHits) != 1 || fallbackHits[0].Role != "user" {
		t.Fatalf("fallback hits = %#v", fallbackHits)
	}

	partial := &parser.ParsedFile{
		SessionID: "partial-session",
		ShortID:   "partial",
		Agent:     "codex",
		Project:   "atm",
		CreatedAt: "06-27",
		CreatedTS: 110,
		LastTS:    110,
		Inputs: []parser.Message{
			{Content: "alpha token", TS: 110},
		},
		Outputs: []parser.Message{
			{Content: "alphabet soup", TS: 111},
		},
		Tools: map[string]int{},
	}
	if err := upsertSession(db, partial, filepath.Join(t.TempDir(), "partial.jsonl"), "codex", 3, 4); err != nil {
		t.Fatalf("upsert partial session: %v", err)
	}
	partialHits, err := SearchMessages(db, "alpha", "codex")
	if err != nil {
		t.Fatalf("partial search messages: %v", err)
	}
	if len(partialHits) != 2 {
		t.Fatalf("partial hits = %#v", partialHits)
	}
}

func TestCoverageCapsInconsistentRequestCountsAtOneHundredPercent(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "coverage-session", ShortID: "coverage", Agent: "pi", Project: "atm",
		CreatedTS: 100, LastTS: 100,
		Usage: parser.Usage{Model: "test-model", InputTokens: 2, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "test-model", TS: 100, InputTokens: 1},
			{Model: "test-model", TS: 101, InputTokens: 1},
		},
	}
	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "coverage.jsonl"), "pi", 1, 2); err != nil {
		t.Fatalf("upsert session: %v", err)
	}
	// syncUsage derives the rollup from the events, so a sync cannot produce this
	// drift any more. Databases written before it could, which is what the
	// capped-at-100% branch is still there for, so seed the drift directly.
	if _, err := db.Exec(`UPDATE usage SET request_count = 1 WHERE session_id = 'coverage-session'`); err != nil {
		t.Fatalf("understate request count: %v", err)
	}
	coverage, err := GetCoverage(db)
	if err != nil {
		t.Fatalf("get coverage: %v", err)
	}
	if len(coverage) != 1 || coverage[0].CoveragePercent != 100 || coverage[0].CoverageStatus != "inconsistent" || coverage[0].DetailedExcess != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestCoverageWindowUsesRequestTimeAndMarksAggregateFallback(t *testing.T) {
	db := openTempDB(t)
	detailed := &parser.ParsedFile{
		SessionID: "coverage-window-detailed", ShortID: "covdetail", Agent: "codex", Project: "atm",
		CreatedTS: 50, LastTS: 160,
		UsageEvents: []parser.UsageEvent{
			{Model: "gpt-5.5", TS: 80, InputTokens: 10, Fingerprint: "coverage:old"},
			{Model: "gpt-5.5", TS: 150, InputTokens: 20, DurationMS: 1000, Fingerprint: "coverage:new"},
		},
	}
	legacy := &parser.ParsedFile{
		SessionID: "coverage-window-legacy", ShortID: "covlegacy", Agent: "pi", Project: "atm",
		CreatedTS: 160, LastTS: 160,
		Usage: parser.Usage{Model: "legacy", InputTokens: 7, RequestCount: 2},
	}
	if err := upsertSession(db, detailed, "/tmp/coverage-window-detailed.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := upsertSession(db, legacy, "/tmp/coverage-window-legacy.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}
	coverage, err := GetCoverageWindow(db, 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	byAgent := map[string]Coverage{}
	for _, row := range coverage {
		byAgent[row.Agent] = row
	}
	if got := byAgent["codex"]; got.ReportedRequests != 1 || got.DetailedRequests != 1 ||
		got.TimedRequests != 1 || got.CoverageStatus != "complete" {
		t.Fatalf("codex coverage = %+v", got)
	}
	if got := byAgent["pi"]; got.ReportedRequests != 2 || got.DetailedRequests != 0 ||
		got.CoverageStatus != "partial" {
		t.Fatalf("pi coverage = %+v", got)
	}
}

func TestStatsMergeConfiguredProjectAliases(t *testing.T) {
	db := openTempDB(t)
	oldAliases := config.ProjectAliases
	config.ProjectAliases = nil
	t.Cleanup(func() { config.ProjectAliases = oldAliases })
	for index, project := range []string{"atm-worktree", "atm"} {
		parsed := &parser.ParsedFile{
			SessionID: fmt.Sprintf("alias-session-%d", index), ShortID: fmt.Sprintf("alias-%d", index), Agent: "codex", Project: project,
			CreatedTS: 100, LastTS: 100,
			Inputs: []parser.Message{{Content: "question", TS: 100}},
		}
		if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), fmt.Sprintf("alias-%d.jsonl", index)), "codex", 1, 2); err != nil {
			t.Fatalf("upsert %s: %v", project, err)
		}
	}
	config.ProjectAliases = map[string]string{"atm-worktree": "atm"}
	stats, err := GetStats(db, 0, 200, "codex")
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if len(stats) != 1 || stats[0].Project != "atm" || stats[0].Sessions != 2 || stats[0].Queries != 2 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStatsUsesActivityTimeForResumedSessions(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "resumed-stats", ShortID: "resumed", Agent: "codex", Project: "atm",
		CreatedTS: 50,
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "old question", TS: 60},
			{Role: "user", Content: "new question", TS: 150},
		},
		Usage: parser.Usage{Model: "gpt-5.6-sol", InputTokens: 30, OutputTokens: 3, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "gpt-5.6-sol", TS: 70, InputTokens: 10, OutputTokens: 1},
			{Model: "gpt-5.6-sol", TS: 160, InputTokens: 20, OutputTokens: 2},
		},
	}
	if err := upsertSession(db, parsed, "/tmp/resumed-stats.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	stats, err := GetStats(db, 100, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	modelStats, err := GetModelStats(db, 100, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || len(modelStats) != 1 || stats[0].Sessions != 1 || stats[0].Queries != 1 ||
		stats[0].InputTokens != 20 || stats[0].OutputTokens != 2 || stats[0].CostUSD != modelStats[0].CostUSD {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestSessionUsageStatsUsesEventWindowAndRanksPrimaryModel(t *testing.T) {
	db := openTempDB(t)
	resumed := &parser.ParsedFile{
		SessionID: "resumed-session-usage", ShortID: "resumed-u", Agent: "codex", Project: "atm",
		CreatedTS: 50, LastTS: 170,
		Usage: parser.Usage{
			Model: "model-b", InputTokens: 69, OutputTokens: 9,
			CacheCreateTokens: 5, CacheReadTokens: 30, RequestCount: 10,
		},
		UsageEvents: []parser.UsageEvent{
			{Model: "model-a", TS: 70, InputTokens: 40, OutputTokens: 4, RequestCount: 1, Fingerprint: "session-usage:old"},
			{Model: "model-a", TS: 150, InputTokens: 20, OutputTokens: 2, CacheReadTokens: 10, RequestCount: 4, Fingerprint: "session-usage:a"},
			{Model: "model-b", TS: 170, InputTokens: 9, OutputTokens: 3, CacheCreateTokens: 5, CacheReadTokens: 20, RequestCount: 5, Fingerprint: "session-usage:b"},
		},
	}
	if err := upsertSession(db, resumed, "/tmp/resumed-session-usage.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	legacy := &parser.ParsedFile{
		SessionID: "legacy-session-usage", ShortID: "legacy-u", Agent: "pi", Project: "atm-worktree",
		CreatedTS: 160, LastTS: 160,
		Usage: parser.Usage{
			Model: "legacy-model", InputTokens: 7, OutputTokens: 1,
			CacheReadTokens: 2, RequestCount: 2,
		},
	}
	if err := upsertSession(db, legacy, "/tmp/legacy-session-usage.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}

	oldAliases := config.ProjectAliases
	config.ProjectAliases = map[string]string{"atm-worktree": "atm"}
	t.Cleanup(func() { config.ProjectAliases = oldAliases })

	stats, err := GetSessionUsageStats(db, 100, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	got := stats[0]
	expectedCost := CalcCost("model-a", 20, 2, 0, 10) +
		CalcCost("model-b", 9, 3, 5, 20)
	if got.ShortID != "resumed-u" || got.Model != "model-b" ||
		got.StartedTS != 150 || got.LastTS != 170 || got.Requests != 9 ||
		got.InputTokens != 29 || got.OutputTokens != 5 ||
		got.CacheCreateTokens != 5 || got.CacheReadTokens != 30 ||
		got.TotalTokens != 69 || got.CostUSD != expectedCost {
		t.Fatalf("resumed stats = %#v", got)
	}
	if stats[1].ShortID != "legacy-u" || stats[1].Project != "atm" ||
		stats[1].Requests != 2 || stats[1].TotalTokens != 10 {
		t.Fatalf("legacy stats = %#v", stats[1])
	}
	if got.Share < 0.873 || got.Share > 0.874 {
		t.Fatalf("share = %f, want 69/79", got.Share)
	}

	filtered, err := GetSessionUsageStats(db, 100, 200, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ShortID != "legacy-u" || filtered[0].Share != 1 {
		t.Fatalf("filtered stats = %#v", filtered)
	}
}

func TestRequestAndLegacySessionStatsUseRequestEventWindow(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "event-window-session", ShortID: "eventwin", Agent: "codex", Project: "atm",
		CreatedTS: 50, LastTS: 170,
		Usage: parser.Usage{
			Model: "gpt-5.5", InputTokens: 30, OutputTokens: 5,
			CacheCreateTokens: 3, CacheReadTokens: 11, RequestCount: 2,
		},
		UsageEvents: []parser.UsageEvent{
			{Model: "gpt-5.5", TS: 70, InputTokens: 10, OutputTokens: 1, CacheReadTokens: 4, Fingerprint: "event-window:old"},
			{Model: "gpt-5.5", TS: 160, InputTokens: 20, OutputTokens: 4, CacheCreateTokens: 3, CacheReadTokens: 7, Fingerprint: "event-window:new"},
		},
	}
	if err := upsertSession(db, parsed, "/tmp/event-window.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	requests, err := GetRequestStats(db, 100, 200, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].TS != 160 || requests[0].FreshInputTokens != 20 ||
		requests[0].CacheCreateTokens != 3 || requests[0].CacheReadTokens != 7 ||
		requests[0].TotalInputTokens != 30 || requests[0].TotalTokens != 34 {
		t.Fatalf("request stats = %#v", requests)
	}

	sessions, err := GetSessionStats(db, 100, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Queries != 1 || sessions[0].FreshInputTokens != 20 ||
		sessions[0].CacheCreateTokens != 3 || sessions[0].CacheReadTokens != 7 ||
		sessions[0].TotalInputTokens != 30 || sessions[0].TotalTokens != 34 {
		t.Fatalf("session stats = %#v", sessions)
	}
}

func TestStatsCollapseSubagentUsageIntoRootTaskCounts(t *testing.T) {
	db := openTempDB(t)
	root := &parser.ParsedFile{
		SessionID: "root-rollout", ResumeID: "root-thread", ShortID: "root",
		Agent: "codex", Project: "atm", CreatedTS: 100, LastTS: 180,
		Messages:    []parser.TranscriptMessage{{Role: "user", Content: "do it", TS: 110}},
		Tools:       map[string]int{"exec": 1},
		UsageEvents: []parser.UsageEvent{{Model: "gpt-5.5", TS: 120, InputTokens: 10, OutputTokens: 2, Fingerprint: "root:req"}},
	}
	child := &parser.ParsedFile{
		SessionID: "child-rollout", ResumeID: "child-thread", RootSessionID: "root-thread",
		ParentSessionID: "root-thread", IsSubagent: true, ShortID: "child",
		Agent: "codex", Project: "child-project", CreatedTS: 130, LastTS: 170,
		Tools:       map[string]int{"exec": 2},
		UsageEvents: []parser.UsageEvent{{Model: "gpt-5.5", TS: 160, InputTokens: 20, OutputTokens: 3, Fingerprint: "child:req"}},
	}
	if err := upsertSession(db, root, "/tmp/root-rollout.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := upsertSession(db, child, "/tmp/child-rollout.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	stats, err := GetStats(db, 90, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Project != "atm" || stats[0].Sessions != 1 || stats[0].TokenSessions != 1 ||
		stats[0].Queries != 1 || stats[0].DetailedRequests != 2 ||
		stats[0].FreshInputTokens != 30 || stats[0].ToolCalls != 3 {
		t.Fatalf("root-collapsed stats = %#v", stats)
	}
}

func TestSearchMessagesUsesLiteralCaseInsensitiveSubstring(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "literal-search-session",
		ShortID:   "literal",
		Agent:     "pi",
		Project:   "mox",
		CreatedAt: "07-13",
		CreatedTS: 200,
		LastTS:    200,
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "修复路径 packages/agent/src/index.ts", TS: 200},
			{Role: "assistant", Content: "Use FOO_Bar% exactly", TS: 201},
		},
	}
	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "literal.jsonl"), "pi", 1, 2); err != nil {
		t.Fatalf("upsert literal session: %v", err)
	}

	for _, test := range []struct {
		keyword string
		role    string
	}{
		{keyword: "路径 packages/agent", role: "user"},
		{keyword: "foo_bar%", role: "assistant"},
	} {
		hits, err := SearchMessages(db, test.keyword, "pi")
		if err != nil {
			t.Fatalf("search %q: %v", test.keyword, err)
		}
		if len(hits) != 1 || hits[0].Role != test.role {
			t.Fatalf("search %q hits = %#v", test.keyword, hits)
		}
	}

	var ftsObjects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('messages_fts', 'messages_ai', 'messages_ad')`).Scan(&ftsObjects); err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if ftsObjects != 0 {
		t.Fatalf("found %d legacy FTS objects", ftsObjects)
	}
}

// A fresh database is built by createSchema in one shot rather than by replaying
// migrations, so this asserts that bootstrap really produces the current shape.
func TestFreshDatabaseHasCurrentSchema(t *testing.T) {
	db := openTempDB(t)

	var version int
	if err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	for _, table := range []string{
		"sessions", "messages", "tools", "usage", "usage_events", "skill_events",
		"sync_state", "sync_health",
		"todos", "todo_tags", "todo_dependencies", "todo_links", "todo_images",
		"todo_session_bindings", "work_state_meta",
		"memory_events", "memory_event_tags", "memory_event_metadata",
		"knowledge_feedback", "session_reviews",
	} {
		var found int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if found != 1 {
			t.Fatalf("table %s missing from a fresh database", table)
		}
	}
	// The FTS mirror was dropped long ago; nothing should recreate it.
	var ftsObjects int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('messages_fts', 'messages_ai', 'messages_ad')`).Scan(&ftsObjects); err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if ftsObjects != 0 {
		t.Fatalf("found %d legacy FTS objects", ftsObjects)
	}
}

func TestListAndExportIncludeSessionsOverlappingRange(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "overnight-session",
		ShortID:   "overnigh",
		Agent:     "codex",
		Project:   "atm",
		CreatedAt: "06-26 23:59",
		CreatedTS: 100,
		LastTS:    200,
		Inputs: []parser.Message{
			{Content: "Before midnight", TS: 100},
			{Content: "After midnight", TS: 200},
		},
		Outputs: []parser.Message{
			{Content: "Still working", TS: 201},
		},
		Tools: map[string]int{},
	}
	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "overnight.jsonl"), "codex", 1, 2); err != nil {
		t.Fatalf("upsert overnight session: %v", err)
	}

	list, err := ListSessions(db, 150, 250, "codex", "")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 1 || list[0].ShortID != "overnigh" {
		t.Fatalf("list = %#v", list)
	}

	rows, err := ExportMessages(db, 150, 250, "codex")
	if err != nil {
		t.Fatalf("export messages: %v", err)
	}
	if len(rows) != 3 || rows[0].SessionID != "overnigh" {
		t.Fatalf("export rows = %#v", rows)
	}
}

// Forking a Codex thread, or continuing a Claude session, writes a second
// transcript that reports requests the first one already did. The fingerprint is
// what stops those from being counted twice, and the rollup has to agree with the
// events that survived rather than with what the parser handed over.
func TestFingerprintedRequestIsCountedOnceAcrossSessions(t *testing.T) {
	db := openTempDB(t)
	shared := []parser.UsageEvent{
		{Model: "gpt-5.6-sol", TS: 101, InputTokens: 10, OutputTokens: 2, Fingerprint: "codex:a"},
		{Model: "gpt-5.6-sol", TS: 102, InputTokens: 20, OutputTokens: 3, Fingerprint: "codex:b"},
	}
	parent := &parser.ParsedFile{SessionID: "parent", ShortID: "parent", Agent: "codex", Project: "atm",
		CreatedTS: 100, LastTS: 102,
		Inputs:      []parser.Message{{Content: "question", TS: 100}},
		Usage:       parser.Usage{Model: "gpt-5.6-sol", InputTokens: 30, OutputTokens: 5, RequestCount: 2},
		UsageEvents: shared}
	if err := upsertSession(db, parent, "/tmp/parent.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	// The fork replays both of the parent's requests, then makes one of its own.
	fork := &parser.ParsedFile{SessionID: "fork", ShortID: "fork", Agent: "codex", Project: "atm",
		CreatedTS: 200, LastTS: 202,
		Inputs: []parser.Message{{Content: "follow up", TS: 200}},
		Usage:  parser.Usage{Model: "gpt-5.6-sol", InputTokens: 35, OutputTokens: 6, RequestCount: 3},
		UsageEvents: append(append([]parser.UsageEvent{}, shared...),
			parser.UsageEvent{Model: "gpt-5.6-sol", TS: 202, InputTokens: 5, OutputTokens: 1, Fingerprint: "codex:c"})}
	if err := upsertSession(db, fork, "/tmp/fork.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	stats, err := GetStats(db, 0, 300, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].InputTokens != 35 || stats[0].OutputTokens != 6 {
		t.Fatalf("stats = %#v, want the three distinct requests only", stats)
	}

	// The rollup follows the events, so the fork reports only the request it made.
	var forkRequests, forkInput int64
	if err := db.QueryRow(`SELECT request_count, input_tokens FROM usage WHERE session_id = 'fork'`).
		Scan(&forkRequests, &forkInput); err != nil {
		t.Fatalf("read fork rollup: %v", err)
	}
	if forkRequests != 1 || forkInput != 5 {
		t.Fatalf("fork rollup = %d requests, %d input tokens", forkRequests, forkInput)
	}

	// Re-parsing the parent frees its fingerprints and takes them back, rather
	// than leaving the requests attributed to whichever file was seen last.
	if err := upsertSession(db, parent, "/tmp/parent.jsonl", "codex", 2, 11); err != nil {
		t.Fatal(err)
	}
	var parentRequests int64
	if err := db.QueryRow(`SELECT request_count FROM usage WHERE session_id = 'parent'`).Scan(&parentRequests); err != nil {
		t.Fatalf("read parent rollup: %v", err)
	}
	if parentRequests != 2 {
		t.Fatalf("parent rollup = %d requests, want 2", parentRequests)
	}
	stats, err = GetStats(db, 0, 300, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].InputTokens != 35 {
		t.Fatalf("stats after reparse = %#v", stats)
	}
}

// Unfingerprinted events come from transcripts that offer no request identity;
// they must keep flowing through untouched, duplicate-looking or not.
func TestUnfingerprintedRequestsAreAllCounted(t *testing.T) {
	db := openTempDB(t)
	p := &parser.ParsedFile{SessionID: "plain", ShortID: "plain", Agent: "pi", Project: "atm",
		CreatedTS: 100, LastTS: 102,
		Inputs: []parser.Message{{Content: "question", TS: 100}},
		Usage:  parser.Usage{Model: "model-a", InputTokens: 20, OutputTokens: 4, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "model-a", TS: 101, InputTokens: 10, OutputTokens: 2},
			{Model: "model-a", TS: 102, InputTokens: 10, OutputTokens: 2},
		}}
	if err := upsertSession(db, p, "/tmp/plain.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}
	stats, err := GetStats(db, 0, 300, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].InputTokens != 20 || stats[0].OutputTokens != 4 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestModelStatsUsesPerRequestUsageEvents(t *testing.T) {
	db := openTempDB(t)
	p := &parser.ParsedFile{SessionID: "pi-models", ShortID: "pi-models", Agent: "pi", Project: "atm", CreatedTS: 100,
		Inputs:      []parser.Message{{Content: "question", TS: 101}},
		Usage:       parser.Usage{Model: "model-b", InputTokens: 30, OutputTokens: 12, CacheReadTokens: 9, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{{Model: "model-a", TS: 102, InputTokens: 10, OutputTokens: 4, CacheReadTokens: 3}, {Model: "model-b", TS: 103, InputTokens: 20, OutputTokens: 8, CacheReadTokens: 6}}}
	if err := upsertSession(db, p, "/tmp/pi-models.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}
	got, err := GetModelStats(db, 0, 200, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("model stats = %#v", got)
	}
	byModel := map[string]ModelStatsResult{}
	for _, r := range got {
		if r.Client != "pi" {
			t.Fatalf("model client = %q", r.Client)
		}
		byModel[r.Model] = r
	}
	if byModel["model-a"].InputTokens != 13 || byModel["model-b"].OutputTokens != 8 || byModel["model-b"].CacheReadTokens != 6 {
		t.Fatalf("model stats = %#v", got)
	}
}

func TestModelStatsSeparatesSameModelByClient(t *testing.T) {
	db := openTempDB(t)
	for _, item := range []struct {
		session string
		client  string
		input   int64
	}{
		{session: "same-model-codex", client: "codex", input: 10},
		{session: "same-model-pi", client: "pi", input: 20},
	} {
		parsed := &parser.ParsedFile{
			SessionID: item.session, ShortID: item.session[:8], Agent: item.client, Project: "atm", CreatedTS: 100,
			Usage:       parser.Usage{Model: "shared-model", InputTokens: item.input, RequestCount: 1},
			UsageEvents: []parser.UsageEvent{{Model: "shared-model", TS: 110, InputTokens: item.input}},
		}
		if err := upsertSession(db, parsed, "/tmp/"+item.session+".jsonl", item.client, 1, 10); err != nil {
			t.Fatal(err)
		}
	}

	got, err := GetModelStats(db, 0, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 ||
		got[0].Client != "pi" || got[0].Model != "shared-model" || got[0].InputTokens != 20 ||
		got[1].Client != "codex" || got[1].Model != "shared-model" || got[1].InputTokens != 10 {
		t.Fatalf("model stats = %#v", got)
	}

	period, err := GetModelDayStats(db, 0, 200, "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(period) != 2 ||
		period[0].Client != "codex" || period[0].Model != "shared-model" ||
		period[1].Client != "pi" || period[1].Model != "shared-model" {
		t.Fatalf("model day stats = %#v", period)
	}
}

func TestModelStatsUsesUsageEventTime(t *testing.T) {
	db := openTempDB(t)
	p := &parser.ParsedFile{SessionID: "resumed-model", ShortID: "resumed", Agent: "codex", Project: "atm", CreatedTS: 50,
		Inputs:      []parser.Message{{Content: "old question", TS: 51}},
		Usage:       parser.Usage{Model: "model-a", InputTokens: 25, OutputTokens: 3, CacheReadTokens: 5, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{{Model: "model-a", TS: 90, InputTokens: 10, OutputTokens: 1}, {Model: "model-a", TS: 150, InputTokens: 15, OutputTokens: 2, CacheReadTokens: 5}}}
	if err := upsertSession(db, p, "/tmp/resumed-model.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	got, err := GetModelStats(db, 100, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].InputTokens != 20 || got[0].OutputTokens != 2 || got[0].CacheReadTokens != 5 {
		t.Fatalf("model stats = %#v", got)
	}
}

func TestModelDayStatsGroupsByRequestDateAndIncludesAggregateFallback(t *testing.T) {
	db := openTempDB(t)
	loc := time.FixedZone("CST", 8*3600)
	dayOne := time.Date(2026, 7, 12, 10, 0, 0, 0, loc)
	dayTwo := dayOne.AddDate(0, 0, 1)

	detailed := &parser.ParsedFile{
		SessionID: "model-day-detailed", ShortID: "detailed", Agent: "codex", Project: "atm",
		CreatedTS: dayOne.Unix(),
		Usage: parser.Usage{
			Model: "model-b", InputTokens: 30, OutputTokens: 1, CacheReadTokens: 3, RequestCount: 2,
		},
		UsageEvents: []parser.UsageEvent{
			{Model: "model-a", TS: dayOne.Add(time.Hour).Unix(), InputTokens: 10, OutputTokens: 1, CacheReadTokens: 3},
			{Model: "model-b", TS: dayTwo.Add(time.Hour).Unix(), InputTokens: 20},
		},
	}
	if err := upsertSession(db, detailed, "/tmp/model-day-detailed.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	aggregate := &parser.ParsedFile{
		SessionID: "model-day-aggregate", ShortID: "aggregate", Agent: "codex", Project: "atm",
		CreatedTS: dayOne.Add(2 * time.Hour).Unix(),
		Usage: parser.Usage{
			Model: "model-a", InputTokens: 5, OutputTokens: 2, CacheReadTokens: 2, RequestCount: 1,
		},
	}
	if err := upsertSession(db, aggregate, "/tmp/model-day-aggregate.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	got, err := GetModelDayStats(db, dayOne.Add(-time.Hour).Unix(), dayTwo.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("model day stats = %#v", got)
	}
	if got[0].Date != "2026-07-12" || got[0].Client != "codex" || got[0].Model != "model-a" ||
		got[0].Sessions != 2 || got[0].InputTokens != 20 || got[0].OutputTokens != 3 ||
		got[0].CacheReadTokens != 5 {
		t.Fatalf("first model day = %#v", got[0])
	}
	if got[1].Date != "2026-07-13" || got[1].Model != "model-b" ||
		got[1].Sessions != 1 || got[1].InputTokens != 20 {
		t.Fatalf("second model day = %#v", got[1])
	}

	hourly, err := GetModelHourStats(db, dayOne.Add(-time.Hour).Unix(), dayTwo.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) != 3 ||
		hourly[0].Date != "2026-07-12 11:00" ||
		hourly[1].Date != "2026-07-12 12:00" ||
		hourly[2].Date != "2026-07-13 11:00" {
		t.Fatalf("model hour stats = %#v", hourly)
	}
}

func TestProjectDayStatsGroupsByProjectAndKeepsModellessUsage(t *testing.T) {
	db := openTempDB(t)
	loc := time.FixedZone("CST", 8*3600)
	dayOne := time.Date(2026, 7, 12, 10, 0, 0, 0, loc)
	dayTwo := dayOne.AddDate(0, 0, 1)

	atm := &parser.ParsedFile{
		SessionID: "project-day-atm", ShortID: "atmsess", Agent: "codex", Project: "atm",
		CreatedTS: dayOne.Unix(),
		UsageEvents: []parser.UsageEvent{
			{Model: "model-a", TS: dayOne.Add(time.Hour).Unix(), InputTokens: 10, OutputTokens: 1, CacheReadTokens: 3},
			// A request the transcript never named a model for still spent tokens
			// on this project, so the project series must count it.
			{TS: dayOne.Add(2 * time.Hour).Unix(), InputTokens: 5, OutputTokens: 1},
			{Model: "model-b", TS: dayTwo.Add(time.Hour).Unix(), InputTokens: 20},
		},
	}
	if err := upsertSession(db, atm, "/tmp/project-day-atm.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	wanda := &parser.ParsedFile{
		SessionID: "project-day-wanda", ShortID: "wandases", Agent: "codex", Project: "wanda",
		CreatedTS: dayOne.Unix(),
		UsageEvents: []parser.UsageEvent{
			{Model: "model-a", TS: dayOne.Add(time.Hour).Unix(), InputTokens: 7, OutputTokens: 2},
		},
	}
	if err := upsertSession(db, wanda, "/tmp/project-day-wanda.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	got, err := GetProjectDayStats(db, dayOne.Add(-time.Hour).Unix(), dayTwo.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("project day stats = %#v", got)
	}
	if got[0].Date != "2026-07-12" || got[0].Client != "codex" || got[0].Project != "atm" ||
		// 10 + 5 input plus the 3 cached tokens the query folds into input.
		got[0].Sessions != 1 || got[0].InputTokens != 18 || got[0].OutputTokens != 2 ||
		got[0].CacheReadTokens != 3 {
		t.Fatalf("first project day = %#v", got[0])
	}
	if got[1].Project != "wanda" || got[1].InputTokens != 7 {
		t.Fatalf("second project day = %#v", got[1])
	}
	if got[2].Date != "2026-07-13" || got[2].Project != "atm" || got[2].InputTokens != 20 {
		t.Fatalf("third project day = %#v", got[2])
	}

	hourly, err := GetProjectHourStats(db, dayOne.Add(-time.Hour).Unix(), dayTwo.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) != 4 ||
		hourly[0].Date != "2026-07-12 11:00" || hourly[0].Project != "atm" ||
		hourly[1].Date != "2026-07-12 11:00" || hourly[1].Project != "wanda" ||
		hourly[2].Date != "2026-07-12 12:00" ||
		hourly[3].Date != "2026-07-13 11:00" {
		t.Fatalf("project hour stats = %#v", hourly)
	}
}

func TestDayStatsUsesActivityTimeAcrossMidnight(t *testing.T) {
	db := openTempDB(t)
	loc := time.FixedZone("CST", 8*3600)
	created := time.Date(2026, 7, 12, 23, 30, 0, 0, loc).Unix()
	dayStart := time.Date(2026, 7, 13, 0, 0, 0, 0, loc)
	beforeMidnight := dayStart.Add(-10 * time.Minute).Unix()
	afterMidnight := dayStart.Add(time.Hour).Unix()

	p := &parser.ParsedFile{
		SessionID: "overnight-usage", ShortID: "overnigh", Agent: "codex", Project: "atm",
		CreatedTS: created,
		Messages: []parser.TranscriptMessage{
			{Role: "user", Content: "before midnight", TS: beforeMidnight},
			{Role: "user", Content: "after midnight", TS: afterMidnight},
		},
		Usage: parser.Usage{Model: "gpt-5.1-codex", InputTokens: 60, OutputTokens: 7, CacheReadTokens: 30, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "gpt-5.1-codex", TS: beforeMidnight, InputTokens: 10, OutputTokens: 2},
			{Model: "gpt-5.1-codex", TS: afterMidnight, InputTokens: 20, OutputTokens: 5, CacheReadTokens: 30},
		},
	}
	if err := upsertSession(db, p, "/tmp/overnight-usage.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	got, err := GetDayStats(db, dayStart.Unix(), dayStart.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("day stats = %#v", got)
	}
	if got[0].Date != "2026-07-13" || got[0].Sessions != 1 || got[0].Queries != 1 ||
		got[0].InputTokens != 50 || got[0].OutputTokens != 5 || got[0].CacheReadTokens != 30 {
		t.Fatalf("day stats = %#v", got[0])
	}

	hourly, err := GetHourStats(db, dayStart.Unix(), dayStart.Add(12*time.Hour).Unix(), "codex", loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hourly) != 12 {
		t.Fatalf("hour stats = %#v", hourly)
	}
	if hourly[0].Date != "2026-07-13 00:00" || hourly[0].InputTokens != 0 {
		t.Fatalf("zero hour = %#v", hourly[0])
	}
	if hourly[1].Date != "2026-07-13 01:00" || hourly[1].Sessions != 1 ||
		hourly[1].Queries != 1 || hourly[1].InputTokens != 50 ||
		hourly[1].OutputTokens != 5 || hourly[1].CacheReadTokens != 30 {
		t.Fatalf("active hour = %#v", hourly[1])
	}
}

func TestSyncedSessionExposesTimelineAndRequests(t *testing.T) {
	db := openTempDB(t)
	p := &parser.ParsedFile{SessionID: "linked-session", ShortID: "linked", Agent: "pi", Project: "atm", CreatedTS: 101,
		Messages:    []parser.TranscriptMessage{{Role: "user", Content: "do it", TS: 102}, {Role: "assistant", Content: "done", TS: 104}},
		Usage:       parser.Usage{Model: "model-a", InputTokens: 10, OutputTokens: 2, RequestCount: 1},
		UsageEvents: []parser.UsageEvent{{Model: "model-a", TS: 103, InputTokens: 10, OutputTokens: 2}}}
	if err := upsertSession(db, p, "/tmp/linked.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}
	timeline, err := GetSessionTimeline(db, "linked")
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) != 3 || timeline[0].Kind != "message" || timeline[1].Kind != "request" || timeline[2].Content != "done" {
		t.Fatalf("timeline = %#v", timeline)
	}
	requests, err := GetRequestStats(db, 0, 200, "pi", "linked")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].Model != "model-a" || requests[0].RequestCount != 1 {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestGetRequestStatsExposesAggregatedRequestCount(t *testing.T) {
	db := openTempDB(t)
	p := &parser.ParsedFile{
		SessionID: "agg-req", ShortID: "agg-req", Agent: "grokbuild", Project: "atm",
		CreatedTS: 100, LastTS: 110,
		Usage: parser.Usage{Model: "grok-4.5-build", InputTokens: 30, OutputTokens: 3, RequestCount: 9},
		UsageEvents: []parser.UsageEvent{
			{Model: "grok-4.5-build", TS: 105, InputTokens: 10, OutputTokens: 1, RequestCount: 4, Fingerprint: "g:a"},
			{Model: "grok-4.5-build", TS: 110, InputTokens: 20, OutputTokens: 2, RequestCount: 5, Fingerprint: "g:b"},
		},
	}
	if err := upsertSession(db, p, filepath.Join(t.TempDir(), "agg.jsonl"), "grokbuild", 1, 2); err != nil {
		t.Fatal(err)
	}
	requests, err := GetRequestStats(db, 0, 200, "grokbuild", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %#v", requests)
	}
	// Newest first.
	if requests[0].RequestCount != 5 || requests[1].RequestCount != 4 {
		t.Fatalf("request counts = %#v", requests)
	}
	if requests[0].InputTokens != 20 || requests[1].InputTokens != 10 {
		t.Fatalf("tokens = %#v", requests)
	}
}

// Two syncs appending the same session at once used to abort one of them:
// reading MAX(seq) first takes a snapshot, and SQLite then refuses the write with
// SQLITE_BUSY_SNAPSHOT instead of waiting. Taking the write lock first makes the
// second one queue.
func TestConcurrentAppendSessionQueuesInsteadOfFailing(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO sessions (id,short_id,agent,project,file_path,created_at,created_ts)
		VALUES ('s1','s1','pi','atm','/tmp/s1.jsonl','',0)`); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for index := 0; index < len(errs); index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs[index] = appendSession(db, &parser.ParsedFile{
				SessionID: "s1", ShortID: "s1", Agent: "pi", Project: "atm",
				Messages: []parser.TranscriptMessage{
					{Role: "user", Content: fmt.Sprintf("tail %d", index), TS: int64(index)},
				},
			}, "/tmp/s1.jsonl", "pi", int64(index), int64(index))
		}(index)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("append %d failed: %v", index, err)
		}
	}
	var messages, distinctSeq int
	if err := db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT seq) FROM messages WHERE session_id='s1'`).
		Scan(&messages, &distinctSeq); err != nil {
		t.Fatal(err)
	}
	if messages != 4 || distinctSeq != 4 {
		t.Fatalf("messages = %d with %d distinct seq, want 4 and 4", messages, distinctSeq)
	}
}

// The agents rotate their own transcripts, so a file leaving the disk is not the
// user deleting a session. The index has to keep the tokens either way, or spend
// that already happened stops being counted a month later.
func TestRemovedSourceKeepsSessionAndUsage(t *testing.T) {
	db := openTempDB(t)
	fp := "/tmp/rotated.jsonl"
	parsed := &parser.ParsedFile{SessionID: "rotated", ShortID: "rotated", Agent: "codex", Project: "atm",
		CreatedTS: 100, LastTS: 102,
		Inputs:  []parser.Message{{Content: "question", TS: 100}},
		Outputs: []parser.Message{{Content: "answer", TS: 101}},
		Tools:   map[string]int{"Edit": 1},
		Usage:   parser.Usage{Model: "gpt-5.6-sol", InputTokens: 30, OutputTokens: 5, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "gpt-5.6-sol", TS: 101, InputTokens: 10, OutputTokens: 2, Fingerprint: "codex:a"},
			{Model: "gpt-5.6-sol", TS: 102, InputTokens: 20, OutputTokens: 3, Fingerprint: "codex:b"},
		}}
	if err := upsertSession(db, parsed, fp, "codex", 1, 10); err != nil {
		t.Fatal(err)
	}

	before, err := GetModelStats(db, 0, 300, "codex")
	if err != nil {
		t.Fatal(err)
	}

	// The transcript is gone: nothing on disk for this agent any more.
	if err := forgetRemovedSources(db, "codex", map[string]bool{}); err != nil {
		t.Fatalf("forget removed sources: %v", err)
	}

	var syncStates int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_state WHERE file_path = ?", fp).Scan(&syncStates); err != nil {
		t.Fatal(err)
	}
	if syncStates != 0 {
		t.Fatalf("sync_state rows = %d, want the bookkeeping dropped", syncStates)
	}

	var sessions, messages, usageRows, events int
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM sessions WHERE id = 'rotated'),
		(SELECT COUNT(*) FROM messages WHERE session_id = 'rotated'),
		(SELECT COUNT(*) FROM usage WHERE session_id = 'rotated'),
		(SELECT COUNT(*) FROM usage_events WHERE session_id = 'rotated')`).
		Scan(&sessions, &messages, &usageRows, &events); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || messages != 2 || usageRows != 1 || events != 2 {
		t.Fatalf("after removal: %d sessions, %d messages, %d usage, %d events; want everything kept",
			sessions, messages, usageRows, events)
	}

	after, err := GetModelStats(db, 0, 300, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || len(after) != 1 ||
		after[0].InputTokens != before[0].InputTokens || after[0].OutputTokens != before[0].OutputTokens {
		t.Fatalf("model stats = %#v, want %#v unchanged by the source removal", after, before)
	}

	retained, err := GetRetainedSessionCounts(db)
	if err != nil {
		t.Fatal(err)
	}
	if retained["codex"] != 1 {
		t.Fatalf("retained counts = %#v, want codex 1", retained)
	}
}

// Session ids are derived from the transcript filename, so moving a checkout or
// repointing a source path re-presents the same id under a new path. That must
// not collide on sessions.id: the insert error aborts the agent's whole sync.
func TestMovedTranscriptDoesNotCollideOnSessionID(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{SessionID: "moved", ShortID: "moved", Agent: "claude", Project: "atm",
		CreatedTS: 100, LastTS: 102,
		Inputs:  []parser.Message{{Content: "question", TS: 100}},
		Outputs: []parser.Message{{Content: "answer", TS: 101}},
		Usage:   parser.Usage{Model: "claude-sonnet-4-6", InputTokens: 10, OutputTokens: 2}}
	if err := upsertSession(db, parsed, "/old/path/moved.jsonl", "claude", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := upsertSession(db, parsed, "/new/path/moved.jsonl", "claude", 1, 10); err != nil {
		t.Fatalf("upsert at new path: %v", err)
	}

	var sessions, messages int
	var filePath string
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM sessions WHERE id = 'moved'),
		(SELECT COUNT(*) FROM messages WHERE session_id = 'moved'),
		(SELECT file_path FROM sessions WHERE id = 'moved')`).
		Scan(&sessions, &messages, &filePath); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || messages != 2 || filePath != "/new/path/moved.jsonl" {
		t.Fatalf("after move: %d sessions, %d messages, path %s", sessions, messages, filePath)
	}
}

// Retained history is history: it belongs in the window the caller asked for and
// nowhere else. `atm session list` defaults to a day, and must not fill up with
// sessions whose transcripts rotated away years ago.
func TestRetainedSessionStaysOutOfRecentWindow(t *testing.T) {
	db := openTempDB(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC).Unix()
	old := now - 90*24*3600
	parsed := &parser.ParsedFile{SessionID: "ancient", ShortID: "ancient", Agent: "claude", Project: "atm",
		CreatedTS: old, LastTS: old + 60,
		Inputs: []parser.Message{{Content: "question", TS: old}}}
	if err := upsertSession(db, parsed, "/tmp/ancient.jsonl", "claude", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := forgetRemovedSources(db, "claude", map[string]bool{}); err != nil {
		t.Fatal(err)
	}

	recent, err := ListSessions(db, now-24*3600, now, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 0 {
		t.Fatalf("recent list = %#v, want the rotated session excluded", recent)
	}

	historical, err := ListSessions(db, old-3600, now, "claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(historical) != 1 || historical[0].ShortID != "ancient" {
		t.Fatalf("historical list = %#v, want the rotated session kept", historical)
	}
}

func TestGrokSiblingVersionTriggersResync(t *testing.T) {
	db := openTempDB(t)
	old := config.GrokSessions
	t.Cleanup(func() { config.GrokSessions = old })
	root := t.TempDir()
	config.GrokSessions = root
	sessionDir := filepath.Join(root, "%2Ftmp%2Fproj", "sess-sibling")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	chat := filepath.Join(sessionDir, "chat_history.jsonl")
	updates := filepath.Join(sessionDir, "updates.jsonl")
	summary := filepath.Join(sessionDir, "summary.json")
	writeFile := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(summary, `{"info":{"cwd":"/tmp/proj"},"created_at":"2026-07-28T08:00:00Z","updated_at":"2026-07-28T08:00:00Z","current_model_id":"grok-4.5","generated_title":"Sib"}`)
	writeFile(chat, `{"type":"user","prompt_index":0,"content":[{"type":"text","text":"<user_query>\nhello\n</user_query>"}]}`+"\n")
	writeFile(updates, "")
	// First sync with no usage yet.
	n, err := SyncAgent(db, "grokbuild")
	if err != nil || n != 1 {
		t.Fatalf("first sync n=%d err=%v", n, err)
	}
	var requests int
	if err := db.QueryRow(`SELECT COALESCE(u.request_count,0) FROM sessions s LEFT JOIN usage u ON u.session_id=s.id WHERE s.agent='grokbuild'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("requests before usage = %d", requests)
	}
	// Chat unchanged; updates gains a turn_completed with modelCalls=5.
	writeFile(updates, `{"timestamp":1785225600,"method":"_x.ai/session/update","params":{"sessionId":"sess-sibling","update":{"sessionUpdate":"turn_completed","prompt_id":"p1","usage":{"inputTokens":100,"outputTokens":10,"cachedReadTokens":0,"modelCalls":5,"modelUsage":{"grok-4.5-build":{"inputTokens":100,"outputTokens":10,"cachedReadTokens":0,"modelCalls":5}}}},"_meta":{"agentTimestampMs":1785225600000}}}`+"\n")
	// Ensure updates mtime/size change while chat stays byte-identical.
	n, err = SyncAgent(db, "grokbuild")
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected sibling change to resync, n=%d", n)
	}
	if err := db.QueryRow(`SELECT COALESCE(u.request_count,0) FROM sessions s LEFT JOIN usage u ON u.session_id=s.id WHERE s.agent='grokbuild'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 5 {
		t.Fatalf("requests after usage = %d, want 5 (modelCalls)", requests)
	}
	// Unchanged siblings should not resync.
	n, err = SyncAgent(db, "grokbuild")
	if err != nil || n != 0 {
		t.Fatalf("third sync should be no-op, n=%d err=%v", n, err)
	}
}

func TestUsageEventRequestCountRollsUp(t *testing.T) {
	db := openTempDB(t)
	parsed := &parser.ParsedFile{
		SessionID: "req-count", ShortID: "reqcount", Agent: "grokbuild", Project: "atm",
		CreatedTS: 100, LastTS: 100,
		Usage: parser.Usage{Model: "grok-4.5-build", InputTokens: 30, OutputTokens: 3, RequestCount: 9},
		UsageEvents: []parser.UsageEvent{
			{Model: "grok-4.5-build", TS: 100, InputTokens: 10, OutputTokens: 1, RequestCount: 4, Fingerprint: "g:1"},
			{Model: "grok-4.5-build", TS: 101, InputTokens: 20, OutputTokens: 2, RequestCount: 5, Fingerprint: "g:2"},
		},
	}
	if err := upsertSession(db, parsed, filepath.Join(t.TempDir(), "req.jsonl"), "grokbuild", 1, 2); err != nil {
		t.Fatal(err)
	}
	var requests int
	if err := db.QueryRow(`SELECT request_count FROM usage WHERE session_id='req-count'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 9 {
		t.Fatalf("rollup request_count = %d, want 9", requests)
	}
	coverage, err := GetCoverage(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage) != 1 || coverage[0].DetailedRequests != 9 || coverage[0].ReportedRequests != 9 || coverage[0].CoverageStatus != "complete" {
		t.Fatalf("coverage = %#v", coverage)
	}
}
