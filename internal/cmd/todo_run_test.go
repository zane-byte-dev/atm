package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

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
	if !strings.Contains(out, "Dispatched t1 to codex") || !strings.Contains(out, "PID:    4242") {
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
}

func TestCopyTaskRunLogTailBoundsOutputAndPreservesUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	body := strings.Repeat("old event\n", 40) + "最新事件：任务仍在执行\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var output bytes.Buffer
	if err := copyTaskRunLogTail(&output, file, 37); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.HasPrefix(got, taskRunLogTruncatedNotice) {
		t.Fatalf("missing truncation notice: %q", got)
	}
	if !strings.HasSuffix(got, "最新事件：任务仍在执行\n") {
		t.Fatalf("tail lost latest event: %q", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("tail is not valid UTF-8: %q", got)
	}
}

func TestCopyTaskRunLogTailKeepsSmallLogWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var output bytes.Buffer
	if err := copyTaskRunLogTail(&output, file, 64); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "one\ntwo\n" {
		t.Fatalf("tail = %q", got)
	}
}
