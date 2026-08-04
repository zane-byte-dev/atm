package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestTodoLifecycleIsMirroredToMarkdownDoc(t *testing.T) {
	withTempAtmDir(t)
	oldReason, oldJSON := todoReasonFlag, jsonOutput
	t.Cleanup(func() {
		todoReasonFlag = oldReason
		jsonOutput = oldJSON
	})

	todo := store.Todo{
		ID:          "t1",
		Title:       "Ship lifecycle metadata",
		Description: "Keep this requirement intact.",
		Priority:    "P1",
		Status:      "open",
		Project:     "atm",
		Created:     store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatalf("save todos: %v", err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatalf("init todo doc: %v", err)
	}

	todoReasonFlag = "Lifecycle metadata is now durable."
	jsonOutput = true
	if err := runTodoDone(todoDoneCmd, []string{"t1"}); err != nil {
		t.Fatalf("done todo: %v", err)
	}

	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatalf("read todo doc: %v", err)
	}
	for _, want := range []string{
		"- **状态**: done（已完成）",
		"- **完结日期**: " + store.Today(),
		"- **完结原因**: Lifecycle metadata is now durable.",
		"Keep this requirement intact.",
		"[done] Lifecycle metadata is now durable.",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("todo doc does not contain %q:\n%s", want, doc)
		}
	}
}
