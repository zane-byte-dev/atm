package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestStatsSessionReturnsEventTimeRowsOnDemand(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	createdTS := seedCommandSession(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (
		session_id, model, ts, input_tokens, output_tokens, cost_usd, fingerprint
	) VALUES ('cmd-session-full', 'claude-sonnet-4-6', ?, 1000, 200, 0.006, 'session-event')`, createdTS+90); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	jsonOutput = true
	// Two days, not one: the seed is an hour old, which lands on yesterday for
	// the first hour after midnight and made this assert depend on the clock.
	statsDaysFlag = 2
	statsByFlag = "session"

	var runErr error
	raw := captureStdout(t, func() {
		runErr = runStats(statsCmd, nil)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var rows []store.SessionStatsResult
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("decode session usage: %v\n%s", err, raw)
	}
	if len(rows) != 1 ||
		rows[0].SessionID != "cmd-session-full" ||
		rows[0].TotalTokens != 1200 {
		t.Fatalf("session usage = %#v", rows)
	}
}
