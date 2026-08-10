package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestSessionBindCreatesMissingTodoDoc(t *testing.T) {
	// GUI-created todos only write the SQLite row. Agent handoff always runs
	// `todo doc` before or after bind; without a card that path looked broken.
	withTempAtmDir(t)
	oldSession, oldJSON := sessionIDFlag, jsonOutput
	oldAgent, oldProject, oldCWD := sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag
	t.Cleanup(func() {
		sessionIDFlag, jsonOutput = oldSession, oldJSON
		sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag = oldAgent, oldProject, oldCWD
	})
	sessionIDFlag = "gui-todo-bind"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = "atm"
	sessionBindCWDFlag = "/tmp/atm"

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "GUI created without markdown card", Priority: "P1",
		Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("seed should not create a markdown card")
	}

	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !store.TodoDocExists("t1") {
		t.Fatal("bind should materialize the markdown card for agent handoff")
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, "GUI created without markdown card") {
		t.Fatalf("doc = %q, err=%v", doc, err)
	}
}

func TestSessionBindAuthoritativelyLinksDispatchedRun(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldJSON := sessionIDFlag, jsonOutput
	oldAgent, oldProject, oldCWD := sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag
	t.Cleanup(func() {
		sessionIDFlag, jsonOutput = oldSession, oldJSON
		sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag = oldAgent, oldProject, oldCWD
	})
	sessionIDFlag = "actual-session"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = "atm"
	sessionBindCWDFlag = "/tmp/atm"
	t.Setenv("ATM_RUN_ID", "run-1")
	t.Setenv("ATM_TODO_ID", "t1")

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Link dispatched run", Priority: "P1",
		Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: "/tmp/atm",
		Policy: "guarded", LogPath: "/tmp/run.log", Status: store.TaskRunRunning, StartTS: 1,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE task_runs SET session_id='wrong-session' WHERE id='run-1'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	db, err = store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := store.GetTaskRun(db, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.SessionID == nil || *run.SessionID != sessionIDFlag {
		t.Fatalf("run = %#v", run)
	}
}

// The dispatched Agent is told to run `atm session bind`, so a run row that has
// been pruned or reconciled must not turn the whole command into a failure the
// Agent has to react to — especially since the binding itself has already been
// committed by the time the link is attempted.
func TestSessionBindSucceedsWhenTheRunLinkCannotBeRecorded(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldJSON := sessionIDFlag, jsonOutput
	oldAgent, oldProject, oldCWD := sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag
	t.Cleanup(func() {
		sessionIDFlag, jsonOutput = oldSession, oldJSON
		sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag = oldAgent, oldProject, oldCWD
	})
	sessionIDFlag = "actual-session"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = "atm"
	sessionBindCWDFlag = "/tmp/atm"
	t.Setenv("ATM_RUN_ID", "run-that-no-longer-exists")
	t.Setenv("ATM_TODO_ID", "t1")

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Bind without a run", Priority: "P1",
		Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	var warnings strings.Builder
	sessionBindCmd.SetErr(&warnings)
	t.Cleanup(func() { sessionBindCmd.SetErr(nil) })
	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !strings.Contains(warnings.String(), "could not link session to task run") {
		t.Fatalf("stderr = %q", warnings.String())
	}

	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bindings, err := store.ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].SessionID != sessionIDFlag {
		t.Fatalf("bindings = %#v", bindings)
	}
}

func TestTodoDocMaterializesMissingCard(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = true

	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Doc on demand", Priority: "P1",
		Status: store.TodoStatusOpen, Description: "requirement from db",
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("seed should not create a markdown card")
	}

	out := captureStdout(t, func() {
		if err := runTodoDoc(todoDocCmd, []string{"t1"}); err != nil {
			t.Fatalf("todo doc: %v", err)
		}
	})
	var result struct {
		Exists  bool   `json:"exists"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !result.Exists || !strings.Contains(result.Content, "requirement from db") {
		t.Fatalf("result = %#v", result)
	}
	if !store.TodoDocExists("t1") {
		t.Fatal("todo doc should persist the card after materializing it")
	}
}

func TestSessionBindingDrivesCurrentTodoCommands(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldJSON := sessionIDFlag, jsonOutput
	oldAgent, oldProject, oldCWD := sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag
	oldMatchProject, oldMatchLimit, oldMatchPrompt := todoMatchProjectFlag, todoMatchLimitFlag, todoMatchPromptFlag
	oldLogSection, oldWake := todoLogSectionFlag, todoWaitWakeFlag
	t.Cleanup(func() {
		sessionIDFlag, jsonOutput = oldSession, oldJSON
		sessionBindAgentFlag, sessionBindProjectFlag, sessionBindCWDFlag = oldAgent, oldProject, oldCWD
		todoMatchProjectFlag, todoMatchLimitFlag, todoMatchPromptFlag = oldMatchProject, oldMatchLimit, oldMatchPrompt
		todoLogSectionFlag, todoWaitWakeFlag = oldLogSection, oldWake
	})
	sessionIDFlag = "session-binding-test"
	jsonOutput = false
	sessionBindAgentFlag = "codex"
	sessionBindProjectFlag = "atm"
	sessionBindCWDFlag = "/tmp/atm"
	todoMatchProjectFlag = "atm"
	todoMatchLimitFlag = 3
	todoMatchPromptFlag = true
	todoLogSectionFlag = ""

	if err := seedTodos(store.Todo{ID: "t1", Title: "Bind agent sessions", Priority: "P1", Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Unrelated project", Priority: "P0", Status: store.TodoStatusInProgress, Project: "wanda", Created: store.Today()}); err != nil {
		t.Fatal(err)
	}

	prompt := captureStdout(t, func() {
		if err := runTodoMatch(todoMatchCmd, []string{"session", "binding"}); err != nil {
			t.Fatalf("match: %v", err)
		}
	})
	if !strings.Contains(prompt, "t1[in_progress]") || strings.Contains(prompt, "t2") {
		t.Fatalf("compact prompt = %q", prompt)
	}

	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	started := store.FindTodo(tf, "t1")
	if started.Status != "in_progress" || started.StartTS == nil {
		t.Fatalf("started todo = %#v", started)
	}
	firstStart := *started.StartTS
	if err := runSessionBind(sessionBindCmd, []string{"t1"}); err != nil {
		t.Fatalf("idempotent bind: %v", err)
	}
	tf, _ = store.LoadTodosReadOnly()
	if got := store.FindTodo(tf, "t1"); got.StartTS == nil || *got.StartTS != firstStart {
		t.Fatalf("bind reset start time: %#v", got)
	}

	if err := runTodoLog(todoLogCmd, []string{"结果：绑定完成；证据：current 写入通过；下一步：等待"}); err != nil {
		t.Fatalf("current log: %v", err)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, "绑定完成") {
		t.Fatalf("current log doc = %q, err=%v", doc, err)
	}

	todoWaitWakeFlag = "external approval"
	if err := runTodoWait(todoWaitCmd, nil); err != nil {
		t.Fatalf("current wait: %v", err)
	}
	current, err := store.CurrentTodoBinding(sessionIDFlag)
	if err != nil || current != nil {
		t.Fatalf("binding after wait = %#v, err=%v", current, err)
	}
}

func TestCurrentTodoRequiresBinding(t *testing.T) {
	withTempAtmDir(t)
	oldSession := sessionIDFlag
	t.Cleanup(func() { sessionIDFlag = oldSession })
	sessionIDFlag = "unbound-session"
	if _, err := optionalTodoID(nil); err == nil || !strings.Contains(err.Error(), "no todo bound") {
		t.Fatalf("unbound current error = %v", err)
	}
}

func TestSessionCurrentSurfacesStaleBindingState(t *testing.T) {
	withTempAtmDir(t)
	oldSession, oldJSON := sessionIDFlag, jsonOutput
	t.Cleanup(func() {
		sessionIDFlag, jsonOutput = oldSession, oldJSON
	})
	sessionIDFlag = "stale-binding-session"
	jsonOutput = true
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Awaiting review", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: sessionIDFlag, TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = runSessionCurrent(sessionCurrentCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("current: %v", runErr)
	}
	var result struct {
		Bound bool   `json:"bound"`
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode current: %v\n%s", err, out)
	}
	if result.Bound || result.State != sessionBindingStateTodoNotInProgress {
		t.Fatalf("current = %#v", result)
	}
}
