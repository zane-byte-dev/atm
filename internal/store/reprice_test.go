package store

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/zane-byte-dev/atm/internal/parser"
)

func TestRepriceSkipsUnchangedRatesAndNoopUpdates(t *testing.T) {
	db := openTempDB(t)
	withPricingTable(t, map[string][4]float64{"model-a": {1, 0, 0, 0}, "model-b": {2, 0, 0, 0}})
	for _, model := range []string{"model-a", "model-b"} {
		p := &parser.ParsedFile{SessionID: model, Agent: "codex", Usage: parser.Usage{Model: model}}
		for i := 0; i < 500; i++ {
			p.UsageEvents = append(p.UsageEvents, parser.UsageEvent{Model: model, TS: 100, InputTokens: 1000, Fingerprint: fmt.Sprintf("%s-%d", model, i)})
		}
		if err := upsertSession(db, p, "/tmp/"+model, "codex", 1, 10); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"usage_events", "usage"} {
		if _, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_noop_%s BEFORE UPDATE ON %s WHEN OLD.cost_usd IS NEW.cost_usd BEGIN SELECT RAISE(FAIL,'unchanged price was updated'); END`, table, table)); err != nil {
			t.Fatal(err)
		}
	}
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err := db.QueryRow("SELECT revision FROM workspace_changes WHERE domain='usage'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT revision FROM workspace_changes WHERE domain='usage'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("unchanged rates caused %d usage mutations", after-before)
	}
	// Changing an unrelated rate may start a pass, but must not touch these rows.
	SetPricing(map[string][4]float64{"unrelated": {10, 0, 0, 0}})
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	SetPricing(map[string][4]float64{"model-a": {3, 0, 0, 0}})
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	if got := readCost(t, db, "usage_events", "model-a"); got < 1.4999 || got > 1.5001 {
		t.Fatalf("changed rate missed: %v", got)
	}
	if got := readCost(t, db, "usage_events", "model-b"); got < 0.9999 || got > 1.0001 {
		t.Fatalf("unrelated model changed: %v", got)
	}
	if err := db.QueryRow("SELECT revision FROM workspace_changes WHERE domain='usage'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	t.Logf("unchanged repeat: 0 usage mutations; changed one model: %d mutations for 500 events and their rollup", after-before)
}

// readCost sums one session's stored cost in either usage table. table is a test
// literal, never input.
func readCost(t *testing.T, db *sql.DB, table, sessionID string) float64 {
	t.Helper()
	var cost float64
	query := `SELECT COALESCE(SUM(cost_usd), 0) FROM ` + table + ` WHERE session_id = ?`
	if err := db.QueryRow(query, sessionID).Scan(&cost); err != nil {
		t.Fatalf("read %s cost: %v", table, err)
	}
	return cost
}

// TestRepriceUsageMovesStoredCostOntoTheCurrentRate is the difference between
// fixing the rate table and fixing the numbers. Requests are inserted once and
// never updated, so a rate that arrives later — a new table entry, a family
// fallback, a user override — would otherwise apply only to traffic ATM had not
// indexed yet, and every dollar already recorded would keep the old guess.
func TestRepriceUsageMovesStoredCostOntoTheCurrentRate(t *testing.T) {
	db := openTempDB(t)
	withPricingTable(t, map[string][4]float64{})

	parsed := &parser.ParsedFile{
		SessionID: "reprice-events", ShortID: "rp-e", Agent: "codex", Project: "atm",
		CreatedTS: 100, LastTS: 200,
		Usage: parser.Usage{Model: "totally-unknown-vendor-x1", InputTokens: 1e6, RequestCount: 1},
		UsageEvents: []parser.UsageEvent{
			{Model: "totally-unknown-vendor-x1", TS: 150, InputTokens: 1e6, RequestCount: 1, Fingerprint: "rp:1"},
		},
	}
	if err := upsertSession(db, parsed, "/tmp/reprice-events.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	// Nothing matched, so the row was charged at the Opus-tier default.
	if got, want := readCost(t, db, "usage_events", "reprice-events"), defaultPricing[0]; got != want {
		t.Fatalf("initial cost = %v, want the default rate %v", got, want)
	}

	SetPricing(map[string][4]float64{"totally-unknown-vendor-x1": {1.0, 0, 0, 0}})
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	if got, want := readCost(t, db, "usage_events", "reprice-events"), 1.0; got != want {
		t.Fatalf("repriced event cost = %v, want %v", got, want)
	}
	if got, want := readCost(t, db, "usage", "reprice-events"), 1.0; got != want {
		t.Fatalf("repriced rollup cost = %v, want %v", got, want)
	}
}

// A session whose requests span several models has one usage row naming one of
// them. Repricing that row by its own model would charge every request in the
// session at that model's rate, so the rollup is summed from the events instead.
func TestRepriceUsageRollsUpMultiModelSessionsFromEvents(t *testing.T) {
	db := openTempDB(t)
	withPricingTable(t, map[string][4]float64{
		"cheap-model": {1.0, 0, 0, 0},
		"dear-model":  {10.0, 0, 0, 0},
	})

	parsed := &parser.ParsedFile{
		SessionID: "reprice-multi", ShortID: "rp-m", Agent: "codex", Project: "atm",
		CreatedTS: 100, LastTS: 200,
		// The rollup names only the last model the transcript reported.
		Usage: parser.Usage{Model: "dear-model", InputTokens: 2e6, RequestCount: 2},
		UsageEvents: []parser.UsageEvent{
			{Model: "cheap-model", TS: 150, InputTokens: 1e6, RequestCount: 1, Fingerprint: "rp:cheap"},
			{Model: "dear-model", TS: 160, InputTokens: 1e6, RequestCount: 1, Fingerprint: "rp:dear"},
		},
	}
	if err := upsertSession(db, parsed, "/tmp/reprice-multi.jsonl", "codex", 1, 10); err != nil {
		t.Fatal(err)
	}
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	// 1M at $1 plus 1M at $10, not 2M at either rate.
	if got, want := readCost(t, db, "usage", "reprice-multi"), 11.0; got != want {
		t.Fatalf("rollup cost = %v, want %v", got, want)
	}
}

// Sessions whose transcript only ever reported a total have no events, so their
// usage row is the original record and has to be repriced in place.
func TestRepriceUsageHandlesSessionsWithoutEvents(t *testing.T) {
	db := openTempDB(t)
	withPricingTable(t, map[string][4]float64{})

	parsed := &parser.ParsedFile{
		SessionID: "reprice-legacy", ShortID: "rp-l", Agent: "pi", Project: "atm",
		CreatedTS: 100, LastTS: 200,
		Usage: parser.Usage{Model: "totally-unknown-vendor-x1", InputTokens: 1e6, RequestCount: 1},
	}
	if err := upsertSession(db, parsed, "/tmp/reprice-legacy.jsonl", "pi", 1, 10); err != nil {
		t.Fatal(err)
	}
	SetPricing(map[string][4]float64{"totally-unknown-vendor-x1": {2.0, 0, 0, 0}})
	if err := RepriceUsage(db); err != nil {
		t.Fatal(err)
	}
	if got, want := readCost(t, db, "usage", "reprice-legacy"), 2.0; got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}
}
