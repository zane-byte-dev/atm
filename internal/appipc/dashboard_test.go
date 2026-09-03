package appipc

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestDashboardMethodAcceptsSelectedCompactStats(t *testing.T) {
	oldDir, oldDB := config.AtmDir, config.AtmDB
	config.AtmDir = t.TempDir()
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	server := New(Dependencies{Dashboard: dashboard.NewService(nil)})
	var output bytes.Buffer
	if err := server.Serve(context.Background(), "dashboard.snapshot",
		strings.NewReader(`{"sections":["stats"],"ranges":["today"],"compact":true}`), &output); err != nil {
		t.Fatalf("Serve: %v\n%s", err, output.String())
	}
	var envelope struct {
		Data dashboard.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	snapshot := envelope.Data
	if snapshot.SchemaVersion != contract.DashboardSchemaVersion || len(snapshot.Ranges) != 1 {
		t.Fatalf("selected dashboard = %+v", snapshot)
	}
	if _, ok := snapshot.Ranges["today"]; !ok || len(snapshot.DayStats) != 1 {
		t.Fatal("IPC must forward both range selection and compact chart history")
	}
	if snapshot.Todos == nil || snapshot.Work.Open == nil || snapshot.LiveStatus.Sessions == nil {
		t.Fatal("stats-only IPC must retain the existing empty work/live object shapes")
	}
}
