package store

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"
)

func TestReadQuickUsageSummaryCountsCrossDaySessionOnceAndMatchesDayStats(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	start := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	if _, err := db.Exec(`INSERT INTO sessions
		(id,short_id,agent,file_path,created_ts,last_ts)
		VALUES('overnight','overnigh','codex','overnight.jsonl',?,?)`,
		start.Add(time.Hour).Unix(), end.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for seq, row := range []struct {
		ts      int64
		content string
	}{
		{start.Add(2 * time.Hour).Unix(), "day one"},
		{start.Add(26 * time.Hour).Unix(), "day two"},
	} {
		if _, err := db.Exec(`INSERT INTO messages
			(session_id,seq,role,content,ts,scope,kind)
			VALUES('overnight',?,'user',?,?,'local','conversation')`, seq, row.content, row.ts); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		ts                          int64
		input, output, create, read int64
		cost                        float64
	}{
		{start.Add(3 * time.Hour).Unix(), 10, 2, 3, 4, 0.10},
		{start.Add(27 * time.Hour).Unix(), 20, 5, 1, 6, 0.20},
	} {
		if _, err := db.Exec(`INSERT INTO usage_events
			(session_id,model,ts,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,cost_usd)
			VALUES('overnight','model-a',?,?,?,?,?,?)`, row.ts, row.input, row.output,
			row.create, row.read, row.cost); err != nil {
			t.Fatal(err)
		}
	}
	// Detailed events are authoritative; the aggregate row must not be added a
	// second time.
	if _, err := db.Exec(`INSERT INTO usage
		(session_id,model,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,cost_usd)
		VALUES('overnight','model-a',900,900,900,900,900)`); err != nil {
		t.Fatal(err)
	}

	got, err := ReadQuickUsageSummary(context.Background(), db, start.Unix(), end.Unix(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := ListSessions(db, start.Unix(), end.Unix(), "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	days, err := GetDayStats(db, start.Unix(), end.Unix(), "codex", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var want QuickUsageSummary
	want.Sessions = len(listed)
	for _, day := range days {
		want.Queries += day.Queries
		want.InputTokens += day.InputTokens
		want.OutputTokens += day.OutputTokens
		want.CacheReadTokens += day.CacheReadTokens
		want.CostUSD += day.CostUSD
	}
	if got.Sessions != 1 || got.Sessions != want.Sessions {
		t.Fatalf("sessions=%d, ListSessions=%d; cross-day session must be counted once", got.Sessions, want.Sessions)
	}
	if len(days) != 2 || days[0].Sessions != 1 || days[1].Sessions != 1 {
		t.Fatalf("day stats did not span both days: %#v", days)
	}
	if got.Queries != want.Queries || got.InputTokens != want.InputTokens ||
		got.OutputTokens != want.OutputTokens || got.CacheReadTokens != want.CacheReadTokens ||
		math.Abs(got.CostUSD-want.CostUSD) > 1e-12 {
		t.Fatalf("quick=%+v, summed day stats=%+v", got, want)
	}
	if got.Queries != 2 || got.InputTokens != 44 || got.OutputTokens != 7 ||
		got.CacheReadTokens != 10 || math.Abs(got.CostUSD-0.30) > 1e-12 {
		t.Fatalf("unexpected totals: %+v", got)
	}
}

func TestReadQuickUsageSummaryUsesAggregateFallbackAndEventTimeBounds(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, row := range []struct {
		id, agent string
		created   int64
		last      int64
	}{
		{"fallback", "codex", 110, 190},
		{"evented", "codex", 120, 200},
		{"other-agent", "claude", 130, 180},
	} {
		if _, err := db.Exec(`INSERT INTO sessions
			(id,short_id,agent,file_path,created_ts,last_ts) VALUES(?,?,?,?,?,?)`,
			row.id, row.id, row.agent, row.id+".jsonl", row.created, row.last); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO usage
		(session_id,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,cost_usd)
		VALUES('fallback',10,4,2,3,0.4),('evented',500,500,500,500,500)`); err != nil {
		t.Fatal(err)
	}
	// The start boundary is included. Once any detailed event exists, the
	// aggregate row is never used, even when another event lies outside the
	// selected range. The end boundary remains excluded.
	if _, err := db.Exec(`INSERT INTO usage_events
		(session_id,ts,input_tokens,output_tokens,cache_read_tokens,cost_usd)
		VALUES('evented',100,20,5,7,0.2),('evented',200,900,900,900,900),
		('other-agent',150,30,6,8,0.3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages(session_id,seq,role,content,ts)
		VALUES('fallback',0,'user','inside',100),('fallback',1,'user','end',200),
		('other-agent',0,'user','other',150)`); err != nil {
		t.Fatal(err)
	}

	got, err := ReadQuickUsageSummary(context.Background(), db, 100, 200, "codex")
	if err != nil {
		t.Fatal(err)
	}
	days, err := GetDayStats(db, 100, 200, "codex", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	var input, output, cacheRead int64
	var queries int
	var cost float64
	for _, day := range days {
		queries += day.Queries
		input += day.InputTokens
		output += day.OutputTokens
		cacheRead += day.CacheReadTokens
		cost += day.CostUSD
	}
	if got.Queries != queries || got.InputTokens != input || got.OutputTokens != output ||
		got.CacheReadTokens != cacheRead || math.Abs(got.CostUSD-cost) > 1e-12 {
		t.Fatalf("quick=%+v day totals queries=%d input=%d output=%d cache=%d cost=%f",
			got, queries, input, output, cacheRead, cost)
	}
	if got.Sessions != 2 || got.Queries != 1 || got.InputTokens != 42 ||
		got.OutputTokens != 9 || got.CacheReadTokens != 10 || math.Abs(got.CostUSD-0.6) > 1e-12 {
		t.Fatalf("filtered summary=%+v", got)
	}
}

func TestReadQuickUsageSummaryPropagatesSchemaAndContextErrors(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := ReadQuickUsageSummary(context.Background(), db, 0, 1, ""); err == nil {
		t.Fatal("read succeeded without schema")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadQuickUsageSummary(ctx, db, 0, 1, ""); err == nil {
		t.Fatal("read ignored canceled context")
	}
}
