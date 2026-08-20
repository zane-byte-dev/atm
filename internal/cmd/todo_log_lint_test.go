package cmd

import (
	"encoding/json"
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

func TestRunTodoLogPreservesTextAndJSONSuccessContracts(t *testing.T) {
	withTempAtmDir(t)
	oldSection, oldJSON, oldFile := todoLogSectionFlag, jsonOutput, todoLogMessageFileFlag
	t.Cleanup(func() {
		todoLogSectionFlag, jsonOutput, todoLogMessageFileFlag = oldSection, oldJSON, oldFile
	})
	todoLogSectionFlag, todoLogMessageFileFlag = "", ""
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Service adapter", Priority: "P1",
		Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	jsonOutput = false
	textOutput := captureStdout(t, func() {
		if err := runTodoLog(todoLogCmd, []string{"t1", "结果：服务接入完成"}); err != nil {
			t.Fatalf("text log: %v", err)
		}
	})
	if !strings.HasPrefix(textOutput, "Logged to t1: - [") ||
		!strings.HasSuffix(textOutput, "结果：服务接入完成\n") {
		t.Fatalf("text output = %q", textOutput)
	}

	jsonOutput = true
	jsonText := captureStdout(t, func() {
		if err := runTodoLog(todoLogCmd, []string{"1", "结果：JSON 接口不变"}); err != nil {
			t.Fatalf("JSON log: %v", err)
		}
	})
	var payload struct {
		Success bool   `json:"success"`
		Path    string `json:"path"`
		Entry   string `json:"entry"`
	}
	if err := json.Unmarshal([]byte(jsonText), &payload); err != nil {
		t.Fatalf("decode output %q: %v", jsonText, err)
	}
	if !payload.Success || payload.Path != store.TodoDocPath("t1") ||
		!strings.Contains(payload.Entry, "结果：JSON 接口不变") || strings.HasSuffix(payload.Entry, "\n") {
		t.Fatalf("JSON payload = %+v", payload)
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

func TestTodoLintAdapterPreservesJSONAndTextShapes(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	todo := store.Todo{
		ID: "t1", Title: "Lint adapter", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}
	if err := seedTodos(todo); err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	encoded := captureStdout(t, func() {
		if err := runTodoLint(todoLintCmd, []string{"#T01"}); err != nil {
			t.Fatalf("lint JSON: %v", err)
		}
	})
	var payload struct {
		TodoID  string                `json:"todo_id"`
		Issues  []store.TodoLintIssue `json:"issues"`
		Summary struct {
			Issues int `json:"issues"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		t.Fatalf("decode %q: %v", encoded, err)
	}
	if payload.TodoID != "t1" || payload.Summary.Issues != 1 || len(payload.Issues) != 1 ||
		payload.Issues[0].Code != "doc_missing" {
		t.Fatalf("payload = %+v", payload)
	}

	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	jsonOutput = false
	text := captureStdout(t, func() {
		if err := runTodoLint(todoLintCmd, []string{"t1"}); err != nil {
			t.Fatalf("lint text: %v", err)
		}
	})
	if text != "Todo lint t1: 0 issue(s)\n  clean\n" {
		t.Fatalf("text = %q", text)
	}
}

// `atm todo log t65` used to succeed: the id is optional and defaults to the
// bound todo, so a lone argument was taken as the entry text, and "t65" passed
// every check — it is a valid todo reference and there is no minimum length. The
// result was a progress entry reading "t65" appended to whichever todo the
// session happened to be bound to, reported as success.
func TestRunTodoLogRefusesALoneTodoIDAsTheEntry(t *testing.T) {
	withTempAtmDir(t)
	oldSection, oldJSON, oldFile := todoLogSectionFlag, jsonOutput, todoLogMessageFileFlag
	t.Cleanup(func() {
		todoLogSectionFlag, jsonOutput, todoLogMessageFileFlag = oldSection, oldJSON, oldFile
	})
	jsonOutput, todoLogSectionFlag, todoLogMessageFileFlag = false, "", ""

	if err := seedTodos(store.Todo{ID: "t65", Title: "Bound", Priority: "P1",
		Status: "in_progress", Project: "atm", Created: store.Today()}); err != nil {
		t.Fatal(err)
	}

	// Every spelling the id resolver accepts has to be caught, or the guard just
	// moves the mistake one keystroke away.
	for _, input := range []string{"t65", "65", "#t65", "#65", "T65"} {
		err := runTodoLog(todoLogCmd, []string{input})
		if err == nil {
			t.Fatalf("todo log %q was accepted as an entry", input)
		}
		if !strings.Contains(err.Error(), "looks like a todo id") {
			t.Errorf("todo log %q error = %v", input, err)
		}
		// The message has to be actionable: it names the canonical id so the
		// suggested command can be edited and re-run.
		if !strings.Contains(err.Error(), "atm todo log t65") {
			t.Errorf("todo log %q error does not suggest a runnable command: %v", input, err)
		}
	}

	// An id with an entry after it is the normal call and still works.
	if err := runTodoLog(todoLogCmd, []string{"65", "结果：闸门已落地；证据：单测通过；下一步：接 App"}); err != nil {
		t.Fatalf("id plus entry: %v", err)
	}

	// Prose that happens to be one short word is still an entry, not an id: the
	// guard must not start rejecting real input. It goes on to fail for the right
	// reason — a lone entry needs a bound session to know which todo it belongs to,
	// and this test has none — so the assertion is that the guard let it past.
	err := runTodoLog(todoLogCmd, []string{"完成"})
	if err == nil {
		t.Fatal("expected the unbound-session error")
	}
	if strings.Contains(err.Error(), "looks like a todo id") {
		t.Errorf("a one-word entry was mistaken for an id: %v", err)
	}
	if !strings.Contains(err.Error(), "no todo bound") {
		t.Errorf("unexpected failure for a one-word entry: %v", err)
	}
}
