package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

type serviceFixture struct {
	service        Service
	now            time.Time
	createdTS      int64
	transcriptPath string
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})
	location := time.FixedZone("session-test", 8*60*60)
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, location)
	created := now.Add(-time.Hour)
	oldCreated := now.AddDate(0, -6, 0)
	transcriptPath := filepath.Join(dir, "recent.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(
		`{"type":"reasoning","summary":[{"type":"summary_text","text":"inspect deployment carefully"}]}`+"\n"+
			`{"type":"assistant","content":"Deployment keyword answer from assistant"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO sessions (id,short_id,agent,project,file_path,created_at,created_ts,summary,last_ts)
				VALUES (?,?,?,?,?,?,?,?,?)`,
			args: []any{"session-recent-full", "recent", "codex", "atm", transcriptPath,
				created.Format("01-02 15:04"), created.Unix(), "Recent session", created.Unix() + 120},
		},
		{
			query: `INSERT INTO sessions (id,short_id,agent,project,file_path,created_at,created_ts,summary,last_ts)
				VALUES (?,?,?,?,?,?,?,?,?)`,
			args: []any{"session-old-full", "old", "claude", "other", "",
				oldCreated.Format("01-02 15:04"), oldCreated.Unix(), "Old session", oldCreated.Unix() + 60},
		},
		{query: `INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`,
			args: []any{"session-recent-full", 0, "user", "Find deployment keyword", created.Unix() + 30}},
		{query: `INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`,
			args: []any{"session-recent-full", 1, "assistant", "Deployment keyword answer from assistant", created.Unix() + 60}},
		{query: `INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`,
			args: []any{"session-old-full", 0, "user", "Old question", oldCreated.Unix() + 10}},
		{query: `INSERT INTO tools(session_id,name,count) VALUES(?,?,?)`,
			args: []any{"session-recent-full", "exec_command", 2}},
		{
			query: `INSERT INTO usage_events(session_id,model,ts,input_tokens,output_tokens,
				cache_create_tokens,cache_read_tokens,cost_usd,fingerprint,request_count)
				VALUES(?,?,?,?,?,?,?,?,?,?)`,
			args: []any{"session-recent-full", "gpt-5", created.Unix() + 45,
				int64(100), int64(20), int64(5), int64(7), 0.01, "session-service-request", 1},
		},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			db.Close()
			t.Fatalf("seed session service: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSessionReview(store.SessionReview{
		SessionID: "session-recent-full", Outcome: "memory", Note: "kept",
		ReviewedAt: now.Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	return serviceFixture{
		service: NewService(ServiceOptions{Now: func() time.Time { return now }, Location: location}),
		now:     now, createdTS: created.Unix(), transcriptPath: transcriptPath,
	}
}

func TestServiceListOwnsWindowReviewOrderAndPagination(t *testing.T) {
	fixture := newServiceFixture(t)
	result, err := fixture.service.List(context.Background(), ListInput{
		All: true, Review: "all", Order: "desc", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || len(result.Sessions) != 1 || result.Sessions[0].ID != "session-recent-full" {
		t.Fatalf("first page = %#v", result)
	}
	recent := result.Sessions[0]
	if recent.Review == nil || recent.Review.Outcome != "memory" ||
		recent.FirstQuestion != "Find deployment keyword" {
		t.Fatalf("recent session = %#v", recent)
	}
	if _, err := time.Parse(time.RFC3339, recent.CreatedAt); err != nil {
		t.Fatalf("created_at = %q: %v", recent.CreatedAt, err)
	}

	second, err := fixture.service.List(context.Background(), ListInput{
		All: true, Review: "all", Order: "desc", Limit: 1, Offset: 1,
	})
	if err != nil || len(second.Sessions) != 1 || second.Sessions[0].ID != "session-old-full" {
		t.Fatalf("second page = %#v, err = %v", second, err)
	}
	pending, err := fixture.service.List(context.Background(), ListInput{
		All: true, Review: "pending", Order: "asc",
	})
	if err != nil || len(pending.Sessions) != 1 || pending.Sessions[0].ID != "session-old-full" {
		t.Fatalf("pending = %#v, err = %v", pending, err)
	}
	recentWindow, err := fixture.service.List(context.Background(), ListInput{Days: 1, Review: "all"})
	if err != nil || recentWindow.Total != 1 || recentWindow.Sessions[0].ID != "session-recent-full" {
		t.Fatalf("recent window = %#v, err = %v", recentWindow, err)
	}
}

func TestServiceSearchOwnsFiltersRankingAndUnicodeSnippet(t *testing.T) {
	fixture := newServiceFixture(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for seq, content := range []string{
		"Another deployment mention with enough surrounding content",
		"<system-reminder>deployment</system-reminder>",
	} {
		if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`,
			"session-recent-full", seq+10, "assistant", content, fixture.createdTS+int64(seq)+70); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()

	result, err := fixture.service.Search(context.Background(), SearchInput{
		Keyword: " deployment ", Agent: "codex", Project: "ATM", Role: "assistant",
		Days: 2, Limit: 1, Snippet: 18,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Returned != 1 || !result.Truncated || len(result.Matches) != 1 {
		t.Fatalf("search result = %#v", result)
	}
	match := result.Matches[0]
	if match.Role != "assistant" || !match.SnippetTruncated ||
		len([]rune(match.Content)) > 18 || !strings.Contains(strings.ToLower(match.Content), "deployment") {
		t.Fatalf("search match = %#v", match)
	}

	snippet, truncated := matchSnippet("开头甲乙丙丁关键字戊己庚辛结尾", "关键字", 8)
	if !truncated || len([]rune(snippet)) > 8 || !strings.Contains(snippet, "关键字") {
		t.Fatalf("unicode snippet = %q, truncated = %v", snippet, truncated)
	}
}

func TestServiceShowOwnsThinkingRoutingTurnSelectionAndBudgets(t *testing.T) {
	fixture := newServiceFixture(t)
	withThinking, err := fixture.service.Show(context.Background(), ShowInput{
		SessionID: "recent", IncludeThinking: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withThinking.QA) != 1 || !strings.Contains(withThinking.QA[0].Thinking, "inspect deployment") ||
		withThinking.Tools["exec_command"] != 2 || withThinking.ThinkingAbsent {
		t.Fatalf("show with thinking = %#v", withThinking)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		seq     int
		role    string
		content string
	}{
		{2, "user", "Second question has enough text to hit the budget"},
		{3, "assistant", "Second answer"},
		{4, "user", "Third question"},
		{5, "assistant", "Third answer"},
	} {
		if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts) VALUES(?,?,?,?,?)`,
			"session-recent-full", row.seq, row.role, row.content, fixture.createdTS+int64(row.seq)+80); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	ranged, err := fixture.service.Show(context.Background(), ShowInput{
		SessionID: "recent", Turns: "2-3", MaxChars: 20,
	})
	if err != nil || ranged.TotalTurns != 3 || ranged.ReturnedTurns != 1 ||
		!ranged.Truncated || ranged.QA[0].Turn != 2 || len([]rune(ranged.QA[0].Q)) > 20 {
		t.Fatalf("ranged show = %#v, err = %v", ranged, err)
	}
	last, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent", Last: 1})
	if err != nil || len(last.QA) != 1 || last.QA[0].Turn != 3 {
		t.Fatalf("last show = %#v, err = %v", last, err)
	}

	if err := os.Remove(fixture.transcriptPath); err != nil {
		t.Fatal(err)
	}
	missing, err := fixture.service.Show(context.Background(), ShowInput{
		SessionID: "recent", IncludeThinking: true,
	})
	if err != nil || !missing.ThinkingSourceMissing || missing.ThinkingAbsent {
		t.Fatalf("missing thinking source = %#v, err = %v", missing, err)
	}
}

func TestServiceTimelineReturnsOrderedPresentationNeutralEvents(t *testing.T) {
	fixture := newServiceFixture(t)
	result, err := fixture.service.Timeline(context.Background(), TimelineInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 3 || result.Events[0].Kind != "message" ||
		result.Events[1].Kind != "request" || result.Events[2].Kind != "message" {
		t.Fatalf("timeline = %#v", result.Events)
	}
	if result.Events[1].InputTokens != 100 || result.Events[1].CacheTokens != 12 {
		t.Fatalf("request event = %#v", result.Events[1])
	}
}

func TestServiceQueriesReturnTypedValidationNotFoundUnavailableAndContextErrors(t *testing.T) {
	fixture := newServiceFixture(t)
	invalidCalls := []func() error{
		func() error {
			_, err := fixture.service.List(context.Background(), ListInput{Order: "sideways"})
			return err
		},
		func() error { _, err := fixture.service.List(context.Background(), ListInput{Offset: -1}); return err },
		func() error {
			_, err := fixture.service.Search(context.Background(), SearchInput{Limit: 1, Snippet: 1})
			return err
		},
		func() error {
			_, err := fixture.service.Search(context.Background(), SearchInput{Keyword: "x", Role: "tool", Limit: 1, Snippet: 1})
			return err
		},
		func() error {
			_, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "recent", Turns: "2-3", Last: 1})
			return err
		},
		func() error { _, err := fixture.service.Timeline(context.Background(), TimelineInput{}); return err },
	}
	for index, call := range invalidCalls {
		if err := call(); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("invalid call %d error = %v", index, err)
		}
	}
	if _, err := fixture.service.Show(context.Background(), ShowInput{SessionID: "missing"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing show error = %v", err)
	}
	if _, err := fixture.service.Timeline(context.Background(), TimelineInput{SessionID: "missing"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing timeline error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.List(ctx, ListInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list error = %v", err)
	}
	if _, err := fixture.service.Search(ctx, SearchInput{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search error = %v", err)
	}

	oldDB := config.AtmDB
	missingPath := filepath.Join(t.TempDir(), "missing", "atm.db")
	config.AtmDB = missingPath
	_, err := fixture.service.List(context.Background(), ListInput{})
	config.AtmDB = oldDB
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("unavailable database error = %v", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("unavailable database error type = %T", err)
	}
	if appErr.Message != store.ErrDatabaseMissing.Error() {
		t.Fatalf("unavailable message = %q, want stable guidance %q", appErr.Message, store.ErrDatabaseMissing.Error())
	}
	if strings.Contains(appErr.Message, missingPath) {
		t.Fatalf("unavailable message leaked database path: %q", appErr.Message)
	}
}

func TestThinkingSelectionGroupsEveryReasoningBlockForOneTurn(t *testing.T) {
	blocks := []parser.ThinkingBlock{
		{Thinking: "inspect", Response: "intermediate"},
		{Thinking: "change", Response: "done"},
		{Thinking: "next", Response: "second answer"},
	}
	thinking, next := collectTurnThinking(blocks, 0, "done")
	if !strings.Contains(thinking, "inspect") || !strings.Contains(thinking, "change") ||
		strings.Contains(thinking, "next") || next != 2 {
		t.Fatalf("thinking = %q, next = %d", thinking, next)
	}
	thinking, next = collectTurnThinking(blocks, 2, "unclaimed")
	if thinking != "next" || next != 3 {
		t.Fatalf("fallback thinking = %q, next = %d", thinking, next)
	}
}
