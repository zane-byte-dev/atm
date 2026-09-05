package store

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Todo JSON is consumed by shell one-liners and the browser API. Keys are
// snake_case throughout — closedReason used to be the one camelCase straggler next to
// wake_condition, which is the kind of inconsistency that is only ever discovered
// by a script reading the wrong key and printing nothing.
func TestTodoJSONKeysAreSnakeCase(t *testing.T) {
	closed, reason := "2026-07-29", "shipped"
	start, done := int64(1), int64(2)
	encoded, err := json.Marshal(Todo{
		ID: "t1", Title: "T", Description: "d", Priority: "P1", Status: "done",
		Project: "atm", Tags: []string{"maintenance"},
		WakeCondition: "w", ReviewAt: "2026-07-30", MaintenanceLimit: 1,
		DependsOn: []string{"t2"}, Created: "2026-07-01", Source: "s",
		Closed: &closed, ClosedReason: &reason, OnDone: "o",
		StartTS: &start, DoneTS: &done,
	})
	if err != nil {
		t.Fatal(err)
	}
	var keyed map[string]any
	if err := json.Unmarshal(encoded, &keyed); err != nil {
		t.Fatal(err)
	}
	for key := range keyed {
		if strings.ToLower(key) != key {
			t.Errorf("key %q is not snake_case", key)
		}
	}
	if _, ok := keyed["closed_reason"]; !ok {
		t.Errorf("closed_reason missing from %v", keyed)
	}
	// Todos have one identifier and it is complete; short_id belongs to sessions.
	if _, ok := keyed["short_id"]; ok {
		t.Error("todo JSON grew a short_id, which has no meaning for a todo id like t104")
	}
}

// Read paths must never create or migrate the database: an Agent running
// `atm todo list` in a sandbox should get a clear error, not a half-built file.
func TestLoadTodosReadOnlyDoesNotCreateDatabase(t *testing.T) {
	withTempStore(t)

	_, err := LoadTodosReadOnly()
	if err == nil || !strings.Contains(err.Error(), "atm sync") {
		t.Fatalf("read-only load on a missing database = %v", err)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("read-only load created %s: %v", config.AtmDB, err)
	}

	seedTodos(t, openTodo("t1", "Now it exists"))
	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatalf("LoadTodosReadOnly: %v", err)
	}
	if len(todos.Items) != 1 || todos.Items[0].ID != "t1" {
		t.Fatalf("read-only todos = %#v", todos.Items)
	}
}
