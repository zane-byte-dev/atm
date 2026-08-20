package stats

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func statsTestCall() application.Call {
	return application.Call{
		RequestID: "stats-test-1",
		Actor:     application.Actor{Kind: application.ActorHuman, Origin: application.OriginCLI},
	}
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestQueryOwnsWindowNormalizationAndQuerySelection(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 4, 5, 0, config.Loc)
	db := testDatabase(t)
	var writable bool
	var got querySpec
	service := Service{
		now:      func() time.Time { return now },
		location: config.Loc,
		open: func(sync bool) (*sql.DB, error) {
			writable = sync
			return db, nil
		},
		query: func(_ context.Context, _ *sql.DB, spec querySpec) (Result, error) {
			got = spec
			return Result{Totals: Totals{CostUSD: 2}}, nil
		},
		subscriptions: func() map[string]float64 { return map[string]float64{"Codex": 20} },
	}

	result, err := service.Query(context.Background(), statsTestCall(), Input{
		Days: 3, Group: "session", Agent: "CODEX", SessionID: " session-1 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if writable {
		t.Fatal("ordinary stats read opened a writable database")
	}
	if got.group != GroupSession || got.agent != "codex" || got.sessionID != "session-1" {
		t.Fatalf("query spec = %+v", got)
	}
	wantStart := time.Date(2026, 8, 18, 0, 0, 0, 0, config.Loc)
	if !got.window.Start.Equal(wantStart) || !got.window.End.Equal(now) || got.window.Label != "last 3 days" {
		t.Fatalf("window = %+v", got.window)
	}
	if result.Group != GroupSession || result.Window.Days != 3 || result.Subscription == nil {
		t.Fatalf("result metadata = %+v", result)
	}
}

func TestQueryNamedRangeUsesClosedCalendarBounds(t *testing.T) {
	now := time.Date(2026, 8, 20, 15, 4, 5, 0, config.Loc)
	db := testDatabase(t)
	var got querySpec
	service := Service{
		now:      func() time.Time { return now },
		location: config.Loc,
		open:     func(bool) (*sql.DB, error) { return db, nil },
		query: func(_ context.Context, _ *sql.DB, spec querySpec) (Result, error) {
			got = spec
			return Result{}, nil
		},
	}
	result, err := service.Query(context.Background(), statsTestCall(), Input{Range: "yesterday", Group: "skill"})
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 8, 19, 0, 0, 0, 0, config.Loc)
	wantEnd := time.Date(2026, 8, 20, 0, 0, 0, 0, config.Loc)
	if !got.window.Start.Equal(wantStart) || !got.window.End.Equal(wantEnd) ||
		result.Window.Label != "yesterday" || result.Window.Days != 1 {
		t.Fatalf("window = %+v", result.Window)
	}
}

func TestQueryRejectsInvalidInputsBeforeOpeningDatabase(t *testing.T) {
	opened := false
	service := Service{open: func(bool) (*sql.DB, error) {
		opened = true
		return nil, errors.New("must not open")
	}}
	tests := []struct {
		name  string
		ctx   context.Context
		call  application.Call
		input Input
		field string
	}{
		{name: "nil context", call: statsTestCall(), field: "context"},
		{name: "invalid call", ctx: context.Background(), call: application.Call{}, field: "request_id"},
		{name: "unknown group", ctx: context.Background(), call: statsTestCall(), input: Input{Group: "model-typo"}, field: "group"},
		{name: "unknown agent", ctx: context.Background(), call: statsTestCall(), input: Input{Agent: "robot"}, field: "agent"},
		{name: "unknown range", ctx: context.Background(), call: statsTestCall(), input: Input{Range: "this_year"}, field: "range"},
		{name: "range and days", ctx: context.Background(), call: statsTestCall(), input: Input{Range: "today", Days: 2}, field: "range"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opened = false
			_, err := service.Query(test.ctx, test.call, test.input)
			if !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("error = %v, want invalid_argument", err)
			}
			var appErr *application.Error
			if !errors.As(err, &appErr) || appErr.Details["field"] != test.field {
				t.Fatalf("typed error = %#v", err)
			}
			if opened {
				t.Fatal("invalid input opened the database")
			}
		})
	}
}

func TestQueryOwnsSyncAndClassifiesInfrastructureErrors(t *testing.T) {
	db := testDatabase(t)
	syncCalls := 0
	service := Service{
		open: func(sync bool) (*sql.DB, error) {
			if !sync {
				t.Fatal("sync request did not choose writable database")
			}
			return db, nil
		},
		sync: func(*sql.DB) (int, error) {
			syncCalls++
			return 4, nil
		},
		query: func(context.Context, *sql.DB, querySpec) (Result, error) { return Result{}, nil },
	}
	result, err := service.Query(context.Background(), statsTestCall(), Input{Sync: true})
	if err != nil {
		t.Fatal(err)
	}
	if syncCalls != 1 || result.SyncedFiles != 4 {
		t.Fatalf("sync calls = %d, files = %d", syncCalls, result.SyncedFiles)
	}

	cause := errors.New("private sqlite detail")
	service = Service{open: func(bool) (*sql.DB, error) { return nil, cause }}
	_, err = service.Query(context.Background(), statsTestCall(), Input{})
	if !errors.Is(err, application.ErrUnavailable) || !errors.Is(err, cause) {
		t.Fatalf("open error = %v", err)
	}
}

func TestAggregationsAreComputedInService(t *testing.T) {
	project := projectResult([]store.StatsResult{
		{Project: "atm", Sessions: 2, Queries: 3, ToolCalls: 4, InputTokens: 10, OutputTokens: 5, CostUSD: 1.5},
		{Project: "other", Sessions: 1, Queries: 2, ToolCalls: 1, InputTokens: 4, OutputTokens: 2, CostUSD: 0.5},
	})
	if project.Totals.Sessions != 3 || project.Totals.Queries != 5 || project.Totals.ToolCalls != 5 || project.Totals.CostUSD != 2 {
		t.Fatalf("project totals = %+v", project.Totals)
	}

	sessions := sessionResult([]store.SessionStatsResult{
		{ShortID: "a", InputTokens: 60, OutputTokens: 20, CacheTokens: 20},
		{ShortID: "b", InputTokens: 20},
	})
	if math.Abs(sessions.Sessions[0].Share-(5.0/6.0)) > 0.0001 || math.Abs(sessions.Sessions[1].Share-(1.0/6.0)) > 0.0001 {
		t.Fatalf("session shares = %+v", sessions.Sessions)
	}

	comparison := buildSubscriptionComparison(10, 5, map[string]float64{"Zed": 20, "Alpha": 10})
	if comparison == nil || comparison.Plans[0].Name != "Alpha" || comparison.APIEquivalentMonthlyUSD != 60 || comparison.ValueRatio != 2 {
		t.Fatalf("subscription comparison = %+v", comparison)
	}
}

func TestDefaultServiceQueriesEveryProjection(t *testing.T) {
	oldDir, oldDB := config.AtmDir, config.AtmDB
	directory := t.TempDir()
	config.AtmDir, config.AtmDB = directory, filepath.Join(directory, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })

	now := time.Date(2026, 8, 20, 14, 0, 0, 0, config.Loc)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	created := now.Add(-time.Hour).Unix()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO sessions (id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"session-1", "session1", "codex", "atm", "fixture.jsonl", "08-20 13:00", created, "fixture", created + 120}},
		{`INSERT INTO messages (session_id, seq, role, content, ts) VALUES (?, ?, ?, ?, ?)`, []any{"session-1", 0, "user", "hello", created + 10}},
		{`INSERT INTO tools (session_id, name, count) VALUES (?, ?, ?)`, []any{"session-1", "exec", 2}},
		{`INSERT INTO skill_events (session_id, name, ts) VALUES (?, ?, ?)`, []any{"session-1", "atm", created + 20}},
		{`INSERT INTO usage (session_id, model, input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, cost_usd, request_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, []any{"session-1", "gpt-5.5", 100, 50, 0, 10, 0.2, 1}},
		{`INSERT INTO usage_events (session_id, model, ts, input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, cost_usd, fingerprint, request_count, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, []any{"session-1", "gpt-5.5", created + 30, 100, 50, 0, 10, 0.2, "fixture", 1, 1000}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			db.Close()
			t.Fatalf("seed stats fixture: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	service := NewService()
	service.now = func() time.Time { return now }
	for _, group := range []Group{
		GroupProject, GroupModel, GroupModelDay, GroupModelHour, GroupSkill,
		GroupSession, GroupSessionUsage, GroupRequest, GroupSpeed, GroupDay,
		GroupHour, GroupWrapped,
	} {
		t.Run(string(group), func(t *testing.T) {
			result, err := service.Query(context.Background(), statsTestCall(), Input{Days: 1, Group: string(group)})
			if err != nil {
				t.Fatalf("Query(%q): %v", group, err)
			}
			if result.Group != group {
				t.Fatalf("group = %q", result.Group)
			}
			if group == GroupWrapped && result.Wrapped == nil {
				t.Fatal("wrapped projection is empty")
			}
		})
	}
}
