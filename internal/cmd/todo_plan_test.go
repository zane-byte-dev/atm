package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func TestTodoPlanSetCLIReadsSnapshotFromStdinAndResolvesBinding(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Plan through CLI", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-plan-cli", TodoID: "t1", Agent: "codex"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATM_SESSION_ID", "session-plan-cli")
	t.Setenv("CODEX_THREAD_ID", "session-plan-cli")

	oldFile, oldJSON := todoPlanFile, jsonOutput
	todoPlanFile, jsonOutput = "-", true
	todoPlanSetCmd.SetIn(strings.NewReader(`{
		"base_revision": 0,
		"explanation": "implementation complete",
		"items": [
			{"step":"implement","status":"completed"},
			{"step":"test","status":"in_progress"}
		]
	}`))
	t.Cleanup(func() {
		todoPlanFile, jsonOutput = oldFile, oldJSON
		todoPlanSetCmd.SetIn(nil)
	})

	var runErr error
	out := captureStdout(t, func() {
		runErr = runTodoPlanSet(todoPlanSetCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("runTodoPlanSet: %v", runErr)
	}
	var result workapp.SetPlanResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output %q: %v", out, err)
	}
	if !result.Changed || result.Plan.TodoID != "t1" || result.Plan.Revision != 1 || result.Plan.Agent != "codex" {
		t.Fatalf("result = %+v", result)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, "## 执行计划") {
		t.Fatalf("plan doc = %q, err=%v", doc, err)
	}
}

func TestTodoPlanSetCLIRejectsUnknownAndTrailingJSON(t *testing.T) {
	for name, body := range map[string]string{
		"unknown field":  `{"base_revision":0,"items":[],"action":"done"}`,
		"trailing value": `{"base_revision":0,"items":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			oldFile := todoPlanFile
			todoPlanFile = "-"
			todoPlanSetCmd.SetIn(strings.NewReader(body))
			t.Cleanup(func() {
				todoPlanFile = oldFile
				todoPlanSetCmd.SetIn(nil)
			})
			if err := runTodoPlanSet(todoPlanSetCmd, []string{"t1"}); err == nil {
				t.Fatal("invalid plan JSON was accepted")
			}
		})
	}
}
