package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/taskrun"
)

type taskRunTestProcess struct {
	interrupt func(int) error
	lookPath  func(string) (string, error)
}

func (process taskRunTestProcess) Interrupt(pid int) error {
	return process.interrupt(pid)
}

func (process taskRunTestProcess) LookPath(binary string) (string, error) {
	if process.lookPath != nil {
		return process.lookPath(binary)
	}
	return "/fake/" + binary, nil
}

type flywheelFetcher struct {
	message collector.Message
}

func (fetcher flywheelFetcher) Fetch(_ context.Context, _ store.CollectionSource, _ int64) ([]collector.Message, int64, error) {
	return []collector.Message{fetcher.message}, fetcher.message.CreatedAt, nil
}

type flywheelExtractor struct{}

func (flywheelExtractor) Extract(_ context.Context, _ collector.MessageBatch, _ []store.Todo) (collector.Decision, error) {
	return collector.Decision{Action: "create", Title: "完成自动飞轮测试",
		Summary: "从采集创建 Todo 并交给 Agent", ItemType: "requirement", Project: "atm",
		Priority: "P1", Reason: "明确可执行需求", Confidence: 0.99}, nil
}

type flywheelDispatcher struct{}

func (flywheelDispatcher) Dispatch(_ context.Context, todoID, _ string) error {
	return runTodoRun(todoRunCmd, []string{todoID})
}

func TestTodoRunClaimsRunAndStartsTodo(t *testing.T) {
	withTempAtmDir(t)
	workDir := t.TempDir()
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Dispatch me", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}

	oldPolicy, oldCWD := todoRunPolicyFlag, todoRunCWDFlag
	oldResolve, oldLaunch, oldJSON := resolveTaskRunAgentBinary, launchTaskRunController, jsonOutput
	t.Cleanup(func() {
		todoRunPolicyFlag, todoRunCWDFlag = oldPolicy, oldCWD
		resolveTaskRunAgentBinary, launchTaskRunController, jsonOutput = oldResolve, oldLaunch, oldJSON
	})
	todoRunPolicyFlag, todoRunCWDFlag = "guarded", workDir
	resolveTaskRunAgentBinary = func(string) (string, error) { return "/fake/codex", nil }
	launchTaskRunController = func(run store.TaskRun) (int, error) { return 4242, nil }
	jsonOutput = false

	var runErr error
	out := captureStdout(t, func() { runErr = runTodoRun(todoRunCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(out, "Dispatched t1 to Codex") || !strings.Contains(out, "PID:    4242") {
		t.Fatalf("output = %q", out)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress {
		t.Fatalf("todo = %#v", todo)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	runs, err := store.ListTaskRuns(db, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != store.TaskRunStarting || runs[0].WorkDir != workDir ||
		!strings.Contains(runs[0].Prompt, "atm todo doc t1") {
		t.Fatalf("runs = %#v", runs)
	}
	jsonOutput = true
	show := captureStdout(t, func() { runErr = runTodoShow(todoShowCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(show, `"latest_run"`) || !strings.Contains(show, runs[0].ID) {
		t.Fatalf("todo show = %q", show)
	}
}

// `--agent` is a persistent root flag, so it reaches `todo run` whether or not
// dispatch has anything to do with it. It selects which agent's data to *read*;
// honouring it as an executor switch would launch a CLI whose sandbox ATM cannot
// enforce, so anything but Codex has to fail loudly instead.
func TestTodoRunRefusesToTreatTheReadFilterAsADispatchTarget(t *testing.T) {
	old := agentFlag
	t.Cleanup(func() { agentFlag = old })

	for _, value := range []string{"grokbuild", "claude", "pi", "copilot"} {
		agentFlag = value
		agent, err := resolveTaskRunDispatchAgent()
		if err == nil {
			t.Fatalf("--agent %s was accepted as a dispatch target: %#v", value, agent)
		}
		if !strings.Contains(err.Error(), "read filter") {
			t.Fatalf("--agent %s error does not say why: %v", value, err)
		}
	}

	// Naming Codex, or naming nothing, both dispatch Codex.
	for _, value := range []string{"", "codex", "Codex", "  codex  "} {
		agentFlag = value
		agent, err := resolveTaskRunDispatchAgent()
		if err != nil || agent.ID != taskRunDispatchAgentID {
			t.Fatalf("--agent %q => %#v, %v", value, agent, err)
		}
	}
}

func TestTaskRunPromptsLeadWithTheUserFacingTodoTitle(t *testing.T) {
	todo := &store.Todo{ID: "t252", Title: "增加前进后退导航"}

	initial := buildTaskRunPrompt(todo)
	if !strings.HasPrefix(initial, "增加前进后退导航 (ATM Todo t252)\n") ||
		strings.HasPrefix(initial, "You are the unattended") {
		t.Fatalf("initial prompt = %q", initial)
	}

	continued := buildTaskRunContinuationPrompt(todo, "把按钮换成图标")
	if !strings.HasPrefix(continued, "增加前进后退导航 (ATM Todo t252)\n") ||
		!strings.Contains(continued, "把按钮换成图标") ||
		strings.HasPrefix(continued, "Continue the existing") {
		t.Fatalf("continuation prompt = %q", continued)
	}
}

func TestTodoRunContinuesLatestCodexSessionWithFollowUp(t *testing.T) {
	withTempAtmDir(t)
	workDir := t.TempDir()
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Continue me", Priority: "P1", Status: store.TodoStatusReview,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	threadID := "019fea8d-a0c4-7130-a984-2c8128705934"
	// The rollout form is what transcript sync writes, so the continuation path
	// has to cope with it rather than only with the id `session bind` reports.
	sessionID := "rollout-2026-08-10T15-19-46-" + threadID
	previous := store.TaskRun{
		ID: "run-previous", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: workDir,
		Prompt: "initial", Policy: "guarded", LogPath: filepath.Join(workDir, "previous.log"),
		Status: store.TaskRunStarting, StartTS: 10, SessionID: &sessionID,
	}
	if err := store.CreateTaskRun(db, previous); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := store.FinishTaskRun(db, previous.ID, 0, 20, "finished"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldPolicy, oldCWD, oldContinue := todoRunPolicyFlag, todoRunCWDFlag, todoRunContinueFlag
	oldResolve, oldLaunch, oldJSON := resolveTaskRunAgentBinary, launchTaskRunController, jsonOutput
	t.Cleanup(func() {
		todoRunPolicyFlag, todoRunCWDFlag, todoRunContinueFlag = oldPolicy, oldCWD, oldContinue
		resolveTaskRunAgentBinary, launchTaskRunController, jsonOutput = oldResolve, oldLaunch, oldJSON
	})
	todoRunPolicyFlag, todoRunCWDFlag = "guarded", workDir
	todoRunContinueFlag = "把按钮文案改清楚，并补上测试"
	resolveTaskRunAgentBinary = func(string) (string, error) { return "/fake/codex", nil }
	var launched store.TaskRun
	launchTaskRunController = func(run store.TaskRun) (int, error) {
		launched = run
		return 4242, nil
	}
	jsonOutput = false

	var runErr error
	out := captureStdout(t, func() { runErr = runTodoRun(todoRunCmd, []string{"t1"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(out, "Continued t1 in its previous Codex session") {
		t.Fatalf("output = %q", out)
	}
	if launched.ResumeSessionID == nil || *launched.ResumeSessionID != threadID ||
		!strings.Contains(launched.Prompt, todoRunContinueFlag) ||
		!strings.Contains(launched.Prompt, "atm session bind t1") {
		t.Fatalf("launched = %#v", launched)
	}
	// Intent is not evidence: this run's own session id stays unknown until it
	// binds, which is also what keeps it visible to transcript sync.
	if launched.SessionID != nil {
		t.Fatalf("continuation pre-claimed a session identity: %#v", launched.SessionID)
	}
}

func TestTodoRunRefusesToContinueAnUnresumableSession(t *testing.T) {
	withTempAtmDir(t)
	workDir := t.TempDir()
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Continue me", Priority: "P1", Status: store.TodoStatusReview,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	// No UUID anywhere in the id, so Codex would read it as a thread name and
	// silently start over rather than reporting a failure.
	sessionID := "codex-desktop-session"
	previous := store.TaskRun{
		ID: "run-previous", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: workDir,
		Prompt: "initial", Policy: "guarded", LogPath: filepath.Join(workDir, "previous.log"),
		Status: store.TaskRunStarting, StartTS: 10, SessionID: &sessionID,
	}
	if err := store.CreateTaskRun(db, previous); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := store.FinishTaskRun(db, previous.ID, 0, 20, "finished"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldPolicy, oldCWD, oldContinue := todoRunPolicyFlag, todoRunCWDFlag, todoRunContinueFlag
	oldResolve, oldLaunch := resolveTaskRunAgentBinary, launchTaskRunController
	t.Cleanup(func() {
		todoRunPolicyFlag, todoRunCWDFlag, todoRunContinueFlag = oldPolicy, oldCWD, oldContinue
		resolveTaskRunAgentBinary, launchTaskRunController = oldResolve, oldLaunch
	})
	todoRunPolicyFlag, todoRunCWDFlag = "guarded", workDir
	todoRunContinueFlag = "再改一版"
	resolveTaskRunAgentBinary = func(string) (string, error) { return "/fake/codex", nil }
	launched := false
	launchTaskRunController = func(store.TaskRun) (int, error) {
		launched = true
		return 4242, nil
	}

	err = runTodoRun(todoRunCmd, []string{"t1"})
	if err == nil || !strings.Contains(err.Error(), "no resumable") {
		t.Fatalf("err = %v", err)
	}
	if launched {
		t.Fatal("an unresumable session must not reach the controller")
	}
}

func TestTodoRunInterruptStopsControllerWithoutChangingTodoLifecycle(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	workDir := t.TempDir()
	logPath := filepath.Join(workDir, "run.log")
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Interrupt me", Priority: "P1", Status: store.TodoStatusInProgress,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: workDir,
		Prompt: "test", Policy: "guarded", LogPath: logPath, Status: store.TaskRunRunning,
		PID: 4321, StartTS: 1,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldService, oldJSON := taskRunManagementService, jsonOutput
	t.Cleanup(func() { taskRunManagementService, jsonOutput = oldService, oldJSON })
	interruptedPID := 0
	taskRunManagementService = taskrun.NewService(taskrun.Dependencies{Process: taskRunTestProcess{
		interrupt: func(pid int) error {
			interruptedPID = pid
			return nil
		},
	}})
	jsonOutput = false

	var interruptErr error
	out := captureStdout(t, func() {
		interruptErr = runTodoRunInterrupt(todoRunInterruptCmd, []string{"t1"})
	})
	if interruptErr != nil {
		t.Fatal(interruptErr)
	}
	if interruptedPID != 4321 || !strings.Contains(out, "Interrupted t1 agent run run-1") {
		t.Fatalf("pid=%d output=%q", interruptedPID, out)
	}
	readDB, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.GetTaskRun(readDB, "run-1")
	readDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.Status != store.TaskRunInterrupted || run.ExitCode != nil {
		t.Fatalf("run = %#v", run)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress {
		t.Fatalf("todo = %#v", todo)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logBody), "ATM run interrupted by user") {
		t.Fatalf("log=%q err=%v", logBody, err)
	}
}

func TestExecuteTaskRunRecordsAgentOutcomeAndOnlySuccessSubmits(t *testing.T) {
	for _, test := range []struct {
		name       string
		helperMode string
		wantStatus string
		wantExit   int
		wantTodo   string
	}{
		{name: "success", helperMode: "success", wantStatus: store.TaskRunCompleted, wantExit: 0, wantTodo: store.TodoStatusReview},
		{name: "failure", helperMode: "failure", wantStatus: store.TaskRunFailed, wantExit: 7, wantTodo: store.TodoStatusInProgress},
	} {
		t.Run(test.name, func(t *testing.T) {
			withTempAtmDir(t)
			t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1")
			workDir := t.TempDir()
			if err := seedTodos(store.Todo{
				ID: "t1", Title: "Execute me", Priority: "P1", Status: store.TodoStatusInProgress,
				Project: "atm", Created: store.Today(),
			}); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(config.AtmDir, "exec", "run-1.log")
			if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
				t.Fatal(err)
			}
			db, err := store.Open()
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CreateTaskRun(db, store.TaskRun{
				ID: "run-1", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: workDir,
				Prompt: "test", Policy: "guarded", LogPath: logPath,
				Status: store.TaskRunStarting, StartTS: 1,
			}); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()

			oldBuild := buildTaskRunAgentCommand
			t.Cleanup(func() { buildTaskRunAgentCommand = oldBuild })
			buildTaskRunAgentCommand = func(store.TaskRun) (*exec.Cmd, error) {
				command := exec.Command(os.Args[0], "-test.run=TestTaskRunHelperProcess", "--")
				command.Env = append(os.Environ(), "ATM_TASK_RUN_TEST_MODE="+test.helperMode)
				return command, nil
			}
			if err := executeTaskRun("run-1"); err != nil {
				t.Fatal(err)
			}

			readDB, err := store.OpenReadOnly()
			if err != nil {
				t.Fatal(err)
			}
			run, err := store.GetTaskRun(readDB, "run-1")
			readDB.Close()
			if err != nil {
				t.Fatal(err)
			}
			if run == nil || run.Status != test.wantStatus || run.ExitCode == nil || *run.ExitCode != test.wantExit {
				t.Fatalf("run = %#v", run)
			}
			todos, err := store.LoadTodosReadOnly()
			if err != nil {
				t.Fatal(err)
			}
			if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != test.wantTodo {
				t.Fatalf("todo = %#v, want status %s", todo, test.wantTodo)
			}
			logBody, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(logBody), "helper "+test.helperMode) {
				t.Fatalf("log = %q", logBody)
			}
		})
	}
}

func TestExecuteCodexTaskRunAttachesThreadWithoutAgentBind(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1")
	workDir := t.TempDir()
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Attach Codex thread", Priority: "P1", Status: store.TodoStatusInProgress,
		Project: "atm", Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(config.AtmDir, "exec", "run-thread.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-thread", TodoID: "t1", Agent: "codex", Project: "atm", WorkDir: workDir,
		Prompt: "test", Policy: "guarded", LogPath: logPath,
		Status: store.TaskRunStarting, StartTS: 1,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	// Reproduce transcript sync racing ahead with a nearby, unrelated Codex
	// session. The controller-owned thread.started event must replace this guess.
	if _, err := db.Exec(`UPDATE task_runs SET session_id='wrong-nearby-session' WHERE id='run-thread'`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldBuild := buildTaskRunAgentCommand
	t.Cleanup(func() { buildTaskRunAgentCommand = oldBuild })
	buildTaskRunAgentCommand = func(store.TaskRun) (*exec.Cmd, error) {
		command := exec.Command(os.Args[0], "-test.run=TestTaskRunHelperProcess", "--")
		command.Env = append(os.Environ(), "ATM_TASK_RUN_TEST_MODE=codex-thread")
		return command, nil
	}

	// The App learns the child is gone from the controller, not from the Agent:
	// a killed or crashed Codex sends no SessionEnd of its own, and its attention
	// signal would then sit in the overlay until the safety TTL expired.
	socketPath := filepath.Join(shortSocketDir(t), "notch.sock")
	t.Setenv(agentevent.SocketEnvVar, socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	delivered := make(chan agentevent.Envelope, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(conn).ReadString('\n')
			conn.Close()
			var envelope agentevent.Envelope
			if json.Unmarshal([]byte(line), &envelope) == nil {
				delivered <- envelope
			}
		}
	}()

	if err := executeTaskRun("run-thread"); err != nil {
		t.Fatal(err)
	}

	const threadID = "019feaa4-f7fd-77e3-b160-18fc1c86e69e"
	select {
	case envelope := <-delivered:
		if envelope.Event != agentevent.KindSessionEnd || envelope.SessionID != threadID ||
			envelope.Source != agentevent.SourceCodex || envelope.CWD != workDir {
			t.Fatalf("envelope = %#v", envelope)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the controller never told the App the thread ended")
	}
	readDB, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.GetTaskRun(readDB, "run-thread")
	readDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.SessionID == nil || *run.SessionID != threadID {
		t.Fatalf("run = %#v", run)
	}
	bindings, err := store.ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].SessionID != threadID || bindings[0].UnboundAt == nil ||
		bindings[0].Reason != "submit:review" {
		t.Fatalf("bindings = %#v", bindings)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logBody), `"type":"thread.started"`) {
		t.Fatalf("log=%q err=%v", logBody, err)
	}
}

func TestCodexThreadStartedWriterHandlesChunkedJSONOnce(t *testing.T) {
	var destination bytes.Buffer
	var linked []string
	writer := newCodexThreadStartedWriter(&destination, func(sessionID string) error {
		linked = append(linked, sessionID)
		return nil
	})
	chunks := []string{
		`{"type":"thr`,
		`ead.started","thread_id":"019feaa4-f7fd-77e3-b160-18fc1c86e69e"}` + "\n",
		`{"type":"thread.started","thread_id":"019feaa5-f7fd-77e3-b160-18fc1c86e69e"}` + "\n",
	}
	for _, chunk := range chunks {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if len(linked) != 1 || linked[0] != "019feaa4-f7fd-77e3-b160-18fc1c86e69e" {
		t.Fatalf("linked = %#v", linked)
	}
	if destination.String() != strings.Join(chunks, "") {
		t.Fatalf("destination = %q", destination.String())
	}
}

func TestCodexThreadStartedWriterRetriesTransientLinkFailure(t *testing.T) {
	var destination bytes.Buffer
	attempts := 0
	writer := newCodexThreadStartedWriter(&destination, func(string) error {
		attempts++
		if attempts == 1 {
			return fmt.Errorf("database is locked")
		}
		return nil
	})
	if _, err := writer.Write([]byte(
		`{"type":"thread.started","thread_id":"019feaa4-f7fd-77e3-b160-18fc1c86e69e"}` + "\n",
	)); err != nil {
		t.Fatal(err)
	}
	if writer.linkErr == nil || writer.linked {
		t.Fatalf("first attempt: linked=%t err=%v", writer.linked, writer.linkErr)
	}
	writer.retryLink()
	if !writer.linked || writer.linkErr != nil || attempts != 2 {
		t.Fatalf("retry: linked=%t err=%v attempts=%d", writer.linked, writer.linkErr, attempts)
	}
}

func TestCollectionToAgentRunToReviewFlywheel(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("ATM_SKIP_LOCAL_NOTIFICATION", "1")
	workDir := t.TempDir()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "flywheel", Project: "atm",
		Priority: "P1", AutoDispatch: true, Enabled: true,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	oldPolicy, oldCWD := todoRunPolicyFlag, todoRunCWDFlag
	oldResolve, oldLaunch := resolveTaskRunAgentBinary, launchTaskRunController
	oldBuild, oldJSON := buildTaskRunAgentCommand, jsonOutput
	t.Cleanup(func() {
		todoRunPolicyFlag, todoRunCWDFlag = oldPolicy, oldCWD
		resolveTaskRunAgentBinary, launchTaskRunController = oldResolve, oldLaunch
		buildTaskRunAgentCommand, jsonOutput = oldBuild, oldJSON
	})
	todoRunPolicyFlag, todoRunCWDFlag, jsonOutput = "guarded", workDir, false
	resolveTaskRunAgentBinary = func(string) (string, error) { return "/fake/codex", nil }
	buildTaskRunAgentCommand = func(store.TaskRun) (*exec.Cmd, error) {
		command := exec.Command(os.Args[0], "-test.run=TestTaskRunHelperProcess", "--")
		command.Env = append(os.Environ(), "ATM_TASK_RUN_TEST_MODE=success")
		return command, nil
	}
	launchTaskRunController = func(run store.TaskRun) (int, error) {
		if err := executeTaskRun(run.ID); err != nil {
			return 0, err
		}
		return os.Getpid(), nil
	}

	service := collector.Service{
		Fetcher: flywheelFetcher{message: collector.Message{ID: "m-flywheel", ConversationID: "flywheel",
			Sender: "测试用户", CreatedAt: 20_000, Content: "把整个自动工作飞轮跑通"}},
		Extractor: flywheelExtractor{}, Dispatcher: flywheelDispatcher{},
		Now: func() time.Time { return time.Unix(30_000, 0) },
	}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("flywheel run: %v", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || todos.Items[0].Status != store.TodoStatusReview {
		t.Fatalf("todo did not reach review: %+v err=%v", todos, err)
	}
	readDB, err := store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer readDB.Close()
	runs, err := store.ListTaskRuns(readDB, todos.Items[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Status != store.TaskRunCompleted ||
		runs[0].ExitCode == nil || *runs[0].ExitCode != 0 {
		t.Fatalf("task runs = %+v err=%v", runs, err)
	}
	items, err := store.ListCollectionItems(readDB, source.ID, 10)
	if err != nil || len(items) != 1 || items[0].DispatchStatus != "dispatched" {
		t.Fatalf("collection items = %+v err=%v", items, err)
	}
}

func TestTaskRunHelperProcess(t *testing.T) {
	mode := os.Getenv("ATM_TASK_RUN_TEST_MODE")
	if mode == "" {
		return
	}
	if mode == "codex-thread" {
		fmt.Println(`{"type":"thread.started","thread_id":"019feaa4-f7fd-77e3-b160-18fc1c86e69e"}`)
		os.Exit(0)
	}
	if mode == "grok-cancelled" {
		// A headless approval request nobody can answer: Grok cancels the turn
		// after the first shell call and still exits 0.
		fmt.Println(`{"type":"tool_call_update","status":"failed","content":[{"type":"content",` +
			`"content":{"type":"text","text":"User cancelled the execution for tool ` + "`run_terminal_command`" + `"}}]}`)
		fmt.Println(`{"type":"end","stopReason":"cancelled","sessionId":"019fea8d-a0c4-7130-a984-2c8128705934"}`)
		os.Exit(0)
	}
	if mode == "grok-end-turn" {
		fmt.Println(`{"type":"end","stopReason":"end_turn","sessionId":"019fea8d-a0c4-7130-a984-2c8128705934"}`)
		os.Exit(0)
	}
	fmt.Println("helper " + mode)
	if mode == "failure" {
		os.Exit(7)
	}
	os.Exit(0)
}

func TestBuildCodexTaskRunCommandKeepsGuardedAndTrustedDistinct(t *testing.T) {
	oldResolve := resolveTaskRunAgentBinary
	t.Cleanup(func() { resolveTaskRunAgentBinary = oldResolve })
	resolveTaskRunAgentBinary = func(string) (string, error) { return "/fake/codex", nil }
	run := store.TaskRun{ID: "r1", TodoID: "t1", WorkDir: "/tmp/work", Prompt: "work", Policy: "guarded"}
	guarded, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	guardedArgs := strings.Join(guarded.Args, " ")
	if !strings.Contains(guardedArgs, "--sandbox workspace-write") ||
		!strings.Contains(guardedArgs, "--add-dir "+config.AtmDir) ||
		strings.Contains(guardedArgs, "dangerously-bypass") {
		t.Fatalf("guarded args = %q", guardedArgs)
	}
	run.Policy = "trusted"
	trusted, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	trustedArgs := strings.Join(trusted.Args, " ")
	if !strings.Contains(trustedArgs, "--dangerously-bypass-approvals-and-sandbox") ||
		strings.Contains(trustedArgs, "--sandbox workspace-write") {
		t.Fatalf("trusted args = %q", trustedArgs)
	}

	resumeID := "019fea8d-a0c4-7130-a984-2c8128705934"
	run.SessionID = &resumeID
	linkedFresh, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(linkedFresh.Args, " "), " resume ") {
		t.Fatalf("a sync-linked fresh run must not resume: %q", strings.Join(linkedFresh.Args, " "))
	}
	// An outcome message must never be able to turn a run into a resume: intent
	// lives in its own column now.
	run.Message = "continuing previous agent session"
	stillFresh, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(stillFresh.Args, " "), " resume ") {
		t.Fatalf("display text must not drive execution: %q", strings.Join(stillFresh.Args, " "))
	}

	run.ResumeSessionID = &resumeID
	resumed, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	resumedArgs := strings.Join(resumed.Args, " ")
	if !strings.Contains(resumedArgs, "resume "+resumeID+" -") {
		t.Fatalf("resumed args = %q", resumedArgs)
	}

	// The rollout form is what transcript sync stores. Codex only accepts the
	// bare thread UUID and treats anything else as a thread name, which silently
	// starts a brand-new session instead of failing.
	rollout := "rollout-2026-08-10T15-19-46-" + resumeID
	run.ResumeSessionID = &rollout
	normalized, err := buildCodexTaskRunCommand(run)
	if err != nil {
		t.Fatal(err)
	}
	normalizedArgs := strings.Join(normalized.Args, " ")
	if !strings.Contains(normalizedArgs, "resume "+resumeID+" -") || strings.Contains(normalizedArgs, "rollout-") {
		t.Fatalf("rollout id was not normalized to a thread UUID: %q", normalizedArgs)
	}
}

// The dispatch prompt must not ask for ATM writes: the run's session is indexed
// with every reply in full and the Todo reads the Agent's closing message from
// that index, so a progress entry duplicates text ATM already owns — and under
// Codex's workspace-write sandbox the write cannot succeed at all.
func TestTaskRunPromptAsksForAnOutcomeInsteadOfATMWrites(t *testing.T) {
	todo := &store.Todo{ID: "t1", Title: "Do the thing"}
	for _, test := range []struct {
		name   string
		prompt string
	}{
		{name: "fresh", prompt: buildTaskRunPrompt(todo)},
		{name: "continuation", prompt: buildTaskRunContinuationPrompt(todo, "also fix the header")},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, forbidden := range []string{"record only meaningful milestones", "milestones with ATM"} {
				if strings.Contains(test.prompt, forbidden) {
					t.Fatalf("prompt still asks the Agent to log progress: %q", test.prompt)
				}
			}
			// The closing message is what the reviewer reads, so the prompt has to
			// ask for one; and the run controller still owns the review transition.
			for _, want := range []string{
				"that closing message is what the person reviewing this Todo reads",
				"Do not record progress with ATM",
				"submitted to review by the run controller",
				"atm todo doc t1",
			} {
				if !strings.Contains(test.prompt, want) {
					t.Fatalf("prompt missing %q:\n%s", want, test.prompt)
				}
			}
		})
	}
}
