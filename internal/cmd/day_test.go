package cmd

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestDayTodayEmitsVersionedJSONAndShowReplaysIt(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)
	jsonOutput = true
	dayTodayCmd.SetContext(context.Background())
	dayShowCmd.SetContext(context.Background())

	var commandErr error
	todayJSON := captureStdout(t, func() {
		commandErr = dayTodayCmd.RunE(dayTodayCmd, nil)
	})
	if commandErr != nil {
		t.Fatalf("day today: %v", commandErr)
	}
	var today aiday.Result
	if err := json.Unmarshal([]byte(todayJSON), &today); err != nil {
		t.Fatalf("decode day today JSON: %v\n%s", err, todayJSON)
	}
	if today.SchemaVersion != aiday.ContractVersion || today.State != "ready" || today.Concept == nil {
		t.Fatalf("today contract = %+v", today)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	var featureDays int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_day_features`).Scan(&featureDays); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if featureDays != 31 {
		t.Fatalf("auto-built feature days = %d, want 31", featureDays)
	}

	showJSON := captureStdout(t, func() {
		commandErr = dayShowCmd.RunE(dayShowCmd, []string{today.Day})
	})
	if commandErr != nil {
		t.Fatalf("day show: %v", commandErr)
	}
	var shown aiday.Result
	if err := json.Unmarshal([]byte(showJSON), &shown); err != nil {
		t.Fatalf("decode day show JSON: %v\n%s", err, showJSON)
	}
	if shown.Day != today.Day || shown.Concept == nil || shown.Concept.ID != today.Concept.ID {
		t.Fatalf("shown result = %+v, today = %+v", shown, today)
	}
}

func TestDayDashboardReturnsCompleteDesktopContract(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)
	jsonOutput = true
	dayDashboardDays = 180
	dayDashboardCmd.SetContext(context.Background())
	var commandErr error
	raw := captureStdout(t, func() { commandErr = dayDashboardCmd.RunE(dayDashboardCmd, nil) })
	if commandErr != nil {
		t.Fatalf("day dashboard: %v", commandErr)
	}
	var dashboard aiday.Dashboard
	if err := json.Unmarshal([]byte(raw), &dashboard); err != nil {
		t.Fatalf("decode dashboard: %v\n%s", err, raw)
	}
	if dashboard.SchemaVersion != aiday.ContractVersion || dashboard.Today.Day == "" {
		t.Fatalf("dashboard=%+v", dashboard)
	}
	if dashboard.Atlas.Total != 12 || len(dashboard.Atlas.Badges) != 12 {
		t.Fatalf("atlas=%+v", dashboard.Atlas)
	}
	if dashboard.Privacy.RawRetained || dashboard.Privacy.RetentionDays != 90 {
		t.Fatalf("privacy=%+v", dashboard.Privacy)
	}
}
