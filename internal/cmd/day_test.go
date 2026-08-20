package cmd

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
)

func TestDayPublicCommandSurfaceIsRepairAndDiagnosticsOnly(t *testing.T) {
	want := []string{"badge", "export", "rebuild", "sources"}
	if got := visibleDayChildNames(dayCmd); !reflect.DeepEqual(got, want) {
		t.Fatalf("day commands = %v, want %v", got, want)
	}
	if got, want := visibleDayChildNames(daySourcesCmd), []string{"list"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("day sources commands = %v, want %v", got, want)
	}
}

func TestDayRebuildEmitsVersionedServiceResult(t *testing.T) {
	withIsolatedCommandEnv(t)
	withCommandFlags(t)
	seedCommandSession(t)

	oldFrom, oldTo := dayFromFlag, dayToFlag
	t.Cleanup(func() { dayFromFlag, dayToFlag = oldFrom, oldTo })
	dayFromFlag, dayToFlag = "", ""
	jsonOutput = true
	dayRebuildCmd.SetContext(context.Background())

	var commandErr error
	raw := captureStdout(t, func() {
		commandErr = dayRebuildCmd.RunE(dayRebuildCmd, nil)
	})
	if commandErr != nil {
		t.Fatalf("day rebuild: %v", commandErr)
	}
	var summary aiday.RebuildSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatalf("decode day rebuild JSON: %v\n%s", err, raw)
	}
	if summary.SchemaVersion != aiday.ContractVersion || summary.Count != 1 || len(summary.Days) != 1 {
		t.Fatalf("rebuild contract = %+v", summary)
	}
}

func visibleDayChildNames(parent *cobra.Command) []string {
	names := make([]string, 0, len(parent.Commands()))
	for _, child := range parent.Commands() {
		if child.IsAvailableCommand() && child.Name() != "help" && child.Name() != "completion" {
			names = append(names, child.Name())
		}
	}
	sort.Strings(names)
	return names
}
