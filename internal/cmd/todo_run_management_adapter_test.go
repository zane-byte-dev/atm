package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/taskrun"
)

func TestTaskRunListAdapterPreservesJSONAndEmptyText(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "List adapter", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: "/tmp/run.log", Status: store.TaskRunStarting, PID: 42, StartTS: 10,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	jsonOutput = true
	var runErr error
	out := captureStdout(t, func() { runErr = runTodoRuns(todoRunsCmd, []string{"#T01"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	var runs []store.TaskRun
	if err := json.Unmarshal([]byte(out), &runs); err != nil || len(runs) != 1 || runs[0].ID != "run-1" || runs[0].PID != 42 {
		t.Fatalf("runs JSON = %q => %+v, err=%v", out, runs, err)
	}

	if err := seedTodos(
		store.Todo{ID: "t1", Title: "List adapter", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		store.Todo{ID: "t2", Title: "No runs", Priority: "P2", Status: store.TodoStatusOpen, Created: store.Today()},
	); err != nil {
		t.Fatal(err)
	}
	jsonOutput = false
	out = captureStdout(t, func() { runErr = runTodoRuns(todoRunsCmd, []string{"t2"}) })
	if runErr != nil || out != "No runs recorded.\n" {
		t.Fatalf("empty runs output=%q err=%v", out, runErr)
	}
}

func TestTaskRunTailAndAgentsAdaptersPreserveOutput(t *testing.T) {
	withTempAtmDir(t)
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Tail adapter", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "run.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("old\n", 20)+"final\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: logPath, Status: store.TaskRunStarting, StartTS: 10,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := store.FinishTaskRun(db, "run-1", 0, 20, "done"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	oldBytes, oldFollow, oldService, oldJSON := todoRunTailBytesFlag, todoRunTailFollowFlag, taskRunManagementService, jsonOutput
	t.Cleanup(func() {
		todoRunTailBytesFlag, todoRunTailFollowFlag, taskRunManagementService, jsonOutput = oldBytes, oldFollow, oldService, oldJSON
	})
	todoRunTailBytesFlag, todoRunTailFollowFlag = 12, false
	var tail bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&tail)
	if err := runTodoRunTail(command, []string{"t1"}); err != nil {
		t.Fatal(err)
	}
	if got := tail.String(); !strings.HasPrefix(got, "[... earlier log truncated ...]\n") || !strings.HasSuffix(got, "final\n") {
		t.Fatalf("tail output = %q", got)
	}

	taskRunManagementService = taskrun.NewService(taskrun.Dependencies{Process: taskRunTestProcess{
		interrupt: func(int) error { return nil },
		lookPath:  func(string) (string, error) { return "/fake/codex", nil },
	}})
	jsonOutput = true
	var agentsErr error
	out := captureStdout(t, func() { agentsErr = runTodoAgents(todoAgentsCmd, nil) })
	if agentsErr != nil {
		t.Fatal(agentsErr)
	}
	var agents []taskrun.AgentInfo
	if err := json.Unmarshal([]byte(out), &agents); err != nil || len(agents) != 1 || agents[0].ID != "codex" || !agents[0].Available {
		t.Fatalf("agents JSON = %q => %+v, err=%v", out, agents, err)
	}
}

func TestTaskRunInterruptAdapterDerivesAgentPolicy(t *testing.T) {
	withTempAtmDir(t)
	withHumanCLI(t)
	t.Setenv("CODEX_THREAD_ID", "taskrun-agent")
	if err := seedTodos(store.Todo{
		ID: "t1", Title: "Agent cannot kill", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, store.TaskRun{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: "/tmp/run.log", Status: store.TaskRunRunning, PID: 55, StartTS: 10,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	called := false
	oldService := taskRunManagementService
	t.Cleanup(func() { taskRunManagementService = oldService })
	taskRunManagementService = taskrun.NewService(taskrun.Dependencies{Process: taskRunTestProcess{
		interrupt: func(int) error {
			called = true
			return nil
		},
	}})
	err = runTodoRunInterrupt(todoRunInterruptCmd, []string{"t1"})
	if !errors.Is(err, application.ErrForbidden) || called {
		t.Fatalf("Agent interrupt err=%v processCalled=%v", err, called)
	}
}
