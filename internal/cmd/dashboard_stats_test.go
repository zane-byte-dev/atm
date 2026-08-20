package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestStatsSessionUsageReturnsEventTimeRowsOnDemand(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)
	jsonOutput = true
	// Two days, not one: the seed is an hour old, which lands on yesterday for
	// the first hour after midnight and made this assert depend on the clock.
	statsDaysFlag = 2
	statsByFlag = "session-usage"

	var runErr error
	raw := captureStdout(t, func() {
		runErr = runStats(statsCmd, nil)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var rows []store.SessionUsageStatsResult
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatalf("decode session usage: %v\n%s", err, raw)
	}
	if len(rows) != 1 ||
		rows[0].SessionID != "cmd-session-full" ||
		rows[0].TotalTokens != 1200 {
		t.Fatalf("session usage = %#v", rows)
	}
}
