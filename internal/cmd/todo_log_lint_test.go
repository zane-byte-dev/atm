package cmd

import (
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestRunTodoLogEnforcesProgressContract(t *testing.T) {
	withTempAtmDir(t)
	oldSection, oldJSON := todoLogSectionFlag, jsonOutput
	t.Cleanup(func() {
		todoLogSectionFlag = oldSection
		jsonOutput = oldJSON
	})
	jsonOutput = false
	todoLogSectionFlag = ""

	tf := &store.TodoFile{Items: []store.Todo{
		{ID: "t1", Title: "Main", Priority: "P1", Status: "in_progress", Project: "atm", Created: store.Today()},
		{ID: "t2", Title: "Dependency", Priority: "P1", Status: "open", Project: "atm", Created: store.Today()},
	}}
	if err := seedTodos(tf.Items...); err != nil {
		t.Fatal(err)
	}

	if err := runTodoLog(todoLogCmd, []string{"t1", strings.Repeat("界", store.TodoProgressMaxRunes+1)}); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("long progress error = %v", err)
	}
	if err := runTodoLog(todoLogCmd, []string{"t1", "结果\n证据"}); err == nil || !strings.Contains(err.Error(), "one paragraph") {
		t.Fatalf("multiline progress error = %v", err)
	}
	if err := runTodoLog(todoLogCmd, []string{"t1", "拆出 t99"}); err == nil || !strings.Contains(err.Error(), "unknown todo IDs: t99") {
		t.Fatalf("unknown reference error = %v", err)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("invalid progress should not create a markdown card")
	}

	if err := runTodoLog(todoLogCmd, []string{"t1", "结果：t2 已确认；证据：测试通过；下一步：收尾"}); err != nil {
		t.Fatalf("valid progress: %v", err)
	}
	todoLogSectionFlag = "分析"
	if err := runTodoLog(todoLogCmd, []string{"t1", strings.Repeat("detail\n", 100)}); err != nil {
		t.Fatalf("analysis detail: %v", err)
	}

	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "结果：t2 已确认") || !strings.Contains(doc, "detail") {
		t.Fatalf("todo doc missing accepted entries:\n%s", doc)
	}
}

func TestTodoLintCommandIsRegistered(t *testing.T) {
	found := false
	for _, command := range todoCmd.Commands() {
		if command.Name() == "lint" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("todo lint command is not registered")
	}
}
