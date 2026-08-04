package cmd

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/store"
)

func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestDashboardReturnsVersionedAggregateSnapshot(t *testing.T) {
	withIsolatedCommandEnv(t)
	oldJSON, oldAgent, oldSession := jsonOutput, agentFlag, sessionIDFlag
	t.Cleanup(func() {
		jsonOutput, agentFlag, sessionIDFlag = oldJSON, oldAgent, oldSession
	})
	jsonOutput = true
	agentFlag = ""
	sessionIDFlag = ""
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Dashboard contract", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	seedCommandSession(t)

	var runErr error
	raw := captureStdout(t, func() {
		runErr = runDashboard(dashboardCmd, nil)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var snapshot dashboardEnvelope
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatalf("decode dashboard: %v\n%s", err, raw)
	}
	if snapshot.SchemaVersion != contract.DashboardSchemaVersion {
		t.Fatalf("schema version = %d", snapshot.SchemaVersion)
	}
	if len(snapshot.Todos) != 1 || snapshot.Todos[0].ID != "t1" || snapshot.Work.Summary.Open != 1 {
		t.Fatalf("work snapshot = %#v, todos=%#v", snapshot.Work, snapshot.Todos)
	}
	// Ranges are keyed by name, so a calendar period can be asked for by the name
	// people use for it. Every supported window must be present: the app picks one
	// by name and a missing key would render as an empty period rather than an error.
	if len(snapshot.Ranges) != len(config.MetricsRanges) {
		t.Fatalf("ranges = %v, want exactly %v", keysOf(snapshot.Ranges), config.MetricsRanges)
	}
	for _, name := range config.MetricsRanges {
		value, ok := snapshot.Ranges[string(name)]
		if !ok || value.ModelStats == nil || value.Sessions == nil || value.SkillStats == nil ||
			value.ProjectStats == nil {
			t.Fatalf("range %s = %#v, present=%v", name, value, ok)
		}
	}
	// The old day-count keys must be gone, not merely joined: leaving both would
	// let the app keep reading a rolling window while believing it asked for a month.
	for _, stale := range []string{"1", "7", "30"} {
		if _, ok := snapshot.Ranges[stale]; ok {
			t.Errorf("day-count range key %q survived the rename", stale)
		}
	}
	if snapshot.DayStats == nil || snapshot.HourStats == nil ||
		snapshot.ModelDayStats == nil || snapshot.ModelHourStats == nil ||
		snapshot.ProjectDayStats == nil || snapshot.ProjectHourStats == nil {
		t.Fatal("dashboard arrays must encode as [] rather than null")
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawFields); err != nil {
		t.Fatal(err)
	}
	if _, exists := rawFields["today_sessions"]; exists {
		t.Fatal("dashboard must not eagerly include today_sessions")
	}
}

func TestStatsSessionUsageReturnsEventTimeRowsOnDemand(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)
	jsonOutput = true
	statsDaysFlag = 1
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
