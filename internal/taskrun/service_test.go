package taskrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func taskRunCall(kind application.ActorKind, requestID string) application.Call {
	agent := ""
	if kind == application.ActorAgent {
		agent = "codex"
	}
	return application.Call{
		RequestID: requestID,
		Actor:     application.Actor{Kind: kind, Origin: application.OriginCLI, Agent: agent},
	}
}

func withTempTaskRunStore(t *testing.T) {
	t.Helper()
	oldDir, oldDB, oldConfig := config.AtmDir, config.AtmDB, config.ConfigPath
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	config.ConfigPath = filepath.Join(dir, "config.json")
	t.Cleanup(func() {
		config.AtmDir, config.AtmDB, config.ConfigPath = oldDir, oldDB, oldConfig
	})
}

func seedTaskRunTodo(t *testing.T, todo store.Todo) {
	t.Helper()
	if err := store.UpdateWorkState(func(state *store.WorkStateTx) error {
		state.Todos.Items = []store.Todo{todo}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func createTaskRun(t *testing.T, run Run) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.CreateTaskRun(db, run); err != nil {
		t.Fatal(err)
	}
}

type fakeProcess struct {
	available   bool
	interrupt   error
	lookups     []string
	interrupted []int
}

func (process *fakeProcess) LookPath(binary string) (string, error) {
	process.lookups = append(process.lookups, binary)
	if !process.available {
		return "", errors.New("missing")
	}
	return "/fake/" + binary, nil
}

func (process *fakeProcess) Interrupt(pid int) error {
	process.interrupted = append(process.interrupted, pid)
	return process.interrupt
}

type memoryLogReader struct {
	*bytes.Reader
}

func (memoryLogReader) Close() error { return nil }

func (reader memoryLogReader) Size() (int64, error) {
	return reader.Reader.Size(), nil
}

type fakeLogs struct {
	bodies   map[string][]byte
	opened   []string
	appended map[string][]byte
	openErr  error
}

func (logs *fakeLogs) Open(path string) (LogReader, error) {
	logs.opened = append(logs.opened, path)
	if logs.openErr != nil {
		return nil, logs.openErr
	}
	body, found := logs.bodies[path]
	if !found {
		return nil, os.ErrNotExist
	}
	return memoryLogReader{Reader: bytes.NewReader(append([]byte(nil), body...))}, nil
}

func (logs *fakeLogs) Append(path string, data []byte) error {
	if logs.appended == nil {
		logs.appended = map[string][]byte{}
	}
	logs.appended[path] = append(logs.appended[path], data...)
	return nil
}

type fakeEvents struct {
	runs  []Run
	times []time.Time
}

func (events *fakeEvents) ReportEnded(run Run, at time.Time) error {
	events.runs = append(events.runs, run)
	events.times = append(events.times, at)
	return nil
}

type fakeClock struct {
	now   time.Time
	wait  func(context.Context, time.Duration) error
	waits int
}

func (clock *fakeClock) Now() time.Time { return clock.now }

func (clock *fakeClock) Wait(ctx context.Context, duration time.Duration) error {
	clock.waits++
	if clock.wait != nil {
		return clock.wait(ctx, duration)
	}
	return nil
}

func TestListAndAgentsReturnTypedSnapshots(t *testing.T) {
	withTempTaskRunStore(t)
	seedTaskRunTodo(t, store.Todo{
		ID: "t1", Title: "Inspect runs", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	createTaskRun(t, Run{
		ID: "run-old", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "old", Policy: "guarded",
		LogPath: "/tmp/old.log", Status: store.TaskRunStarting, StartTS: 10,
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishTaskRun(db, "run-old", 0, 20, "complete"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := store.CreateTaskRun(db, Run{
		ID: "run-new", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "new", Policy: "guarded",
		LogPath: "/tmp/new.log", Status: store.TaskRunStarting, StartTS: 30,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	process := &fakeProcess{available: true}
	service := NewService(Dependencies{Process: process})
	runs, err := service.List(context.Background(), taskRunCall(application.ActorAgent, "list"), ListInput{TodoID: "#T01"})
	if err != nil || len(runs.Runs) != 2 || runs.Runs[0].ID != "run-new" || runs.Runs[1].ID != "run-old" {
		t.Fatalf("List = %+v, err=%v", runs, err)
	}
	agents, err := service.Agents(context.Background(), taskRunCall(application.ActorAgent, "agents"), AgentsInput{})
	if err != nil || len(agents.Agents) != 1 || agents.Agents[0].ID != DispatchAgentID || !agents.Agents[0].Available {
		t.Fatalf("Agents = %+v, err=%v", agents, err)
	}
	if len(process.lookups) != 1 || process.lookups[0] != "codex" {
		t.Fatalf("lookups = %v", process.lookups)
	}
}

func TestInterruptRequiresHumanAndHumanCommitsStatusBeforeBestEffortProjections(t *testing.T) {
	withTempTaskRunStore(t)
	seedTaskRunTodo(t, store.Todo{
		ID: "t1", Title: "Controlled stop", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	sessionID := "019fea8d-a0c4-7130-a984-2c8128705934"
	createTaskRun(t, Run{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp/work", Prompt: "work", Policy: "guarded",
		LogPath: "/logs/run-1.log", Status: store.TaskRunRunning, PID: 4321, StartTS: 10, SessionID: &sessionID,
	})
	process := &fakeProcess{available: true}
	logs := &fakeLogs{bodies: map[string][]byte{}}
	events := &fakeEvents{}
	clock := &fakeClock{now: time.Date(2026, 8, 20, 13, 0, 0, 0, time.FixedZone("CST", 8*3600))}
	service := NewService(Dependencies{Process: process, Logs: logs, Events: events, Clock: clock})

	_, err := service.Interrupt(context.Background(), taskRunCall(application.ActorAgent, "agent-stop"), InterruptInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrForbidden) || len(process.interrupted) != 0 {
		t.Fatalf("Agent Interrupt err=%v process=%v", err, process.interrupted)
	}
	db, openErr := store.OpenReadOnly()
	if openErr != nil {
		t.Fatal(openErr)
	}
	active, queryErr := store.ActiveTaskRun(db, "t1")
	db.Close()
	if queryErr != nil || active == nil {
		t.Fatalf("forbidden interrupt changed active run: %+v, err=%v", active, queryErr)
	}

	result, err := service.Interrupt(context.Background(), taskRunCall(application.ActorHuman, "human-stop"), InterruptInput{TodoID: "#T01"})
	if err != nil || result.Run.Status != store.TaskRunInterrupted || result.Run.ExitCode != nil {
		t.Fatalf("human Interrupt = %+v, err=%v", result, err)
	}
	if len(process.interrupted) != 1 || process.interrupted[0] != 4321 {
		t.Fatalf("interrupted = %v", process.interrupted)
	}
	if got := string(logs.appended["/logs/run-1.log"]); !strings.Contains(got, "ATM run interrupted by user at 2026-08-20T13:00:00+08:00") {
		t.Fatalf("appended log = %q", got)
	}
	if len(events.runs) != 1 || events.runs[0].ID != "run-1" || !events.times[0].Equal(clock.now) {
		t.Fatalf("events = %+v at %v", events.runs, events.times)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusInProgress {
		t.Fatalf("Todo lifecycle changed: %+v, err=%v", todos, err)
	}
}

func TestInterruptProcessFailureLeavesRunActive(t *testing.T) {
	withTempTaskRunStore(t)
	seedTaskRunTodo(t, store.Todo{
		ID: "t1", Title: "Failed stop", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	createTaskRun(t, Run{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: "/tmp/run.log", Status: store.TaskRunRunning, PID: 999, StartTS: 10,
	})
	process := &fakeProcess{interrupt: errors.New("permission denied")}
	service := NewService(Dependencies{Process: process})
	_, err := service.Interrupt(context.Background(), taskRunCall(application.ActorHuman, "failed-stop"), InterruptInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Interrupt error = %v, want unavailable", err)
	}
	db, openErr := store.OpenReadOnly()
	if openErr != nil {
		t.Fatal(openErr)
	}
	active, queryErr := store.ActiveTaskRun(db, "t1")
	db.Close()
	if queryErr != nil || active == nil || active.Status != store.TaskRunRunning {
		t.Fatalf("run after process failure = %+v, err=%v", active, queryErr)
	}
}

func TestTailBoundsUTF8ThroughInjectedLogPort(t *testing.T) {
	withTempTaskRunStore(t)
	seedTaskRunTodo(t, store.Todo{
		ID: "t1", Title: "Read log", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today(),
	})
	createTaskRun(t, Run{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: "/logs/run.log", Status: store.TaskRunStarting, StartTS: 10,
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishTaskRun(db, "run-1", 0, 20, "done"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	body := strings.Repeat("old event\n", 40) + "最新事件：任务仍在执行\n"
	logs := &fakeLogs{bodies: map[string][]byte{"/logs/run.log": []byte(body)}}
	service := NewService(Dependencies{Logs: logs})
	var output bytes.Buffer
	result, err := service.Tail(context.Background(), taskRunCall(application.ActorAgent, "tail"), TailInput{
		TodoID: "t1", MaxBytes: 37,
	}, &output)
	if err != nil || result.RunID != "run-1" {
		t.Fatalf("Tail = %+v, err=%v", result, err)
	}
	got := output.String()
	if !strings.HasPrefix(got, logTruncatedNotice) || !strings.HasSuffix(got, "最新事件：任务仍在执行\n") || !utf8.ValidString(got) {
		t.Fatalf("tail output = %q", got)
	}
	if len(logs.opened) != 1 || logs.opened[0] != "/logs/run.log" {
		t.Fatalf("opened = %v", logs.opened)
	}
	if _, err := service.Tail(context.Background(), taskRunCall(application.ActorAgent, "bad-tail"), TailInput{
		TodoID: "t1", MaxBytes: -1,
	}, io.Discard); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("negative Tail error = %v", err)
	}
}

func TestTailFollowNormalizesNilContextAndReadsFinalAppend(t *testing.T) {
	withTempTaskRunStore(t)
	seedTaskRunTodo(t, store.Todo{
		ID: "t1", Title: "Follow log", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	logPath := filepath.Join(config.AtmDir, "run.log")
	if err := os.WriteFile(logPath, []byte("start\n"), 0600); err != nil {
		t.Fatal(err)
	}
	createTaskRun(t, Run{
		ID: "run-1", TodoID: "t1", Agent: "codex", WorkDir: "/tmp", Prompt: "work", Policy: "guarded",
		LogPath: logPath, Status: store.TaskRunRunning, PID: 123, StartTS: 10,
	})
	clock := &fakeClock{}
	clock.wait = func(ctx context.Context, _ time.Duration) error {
		if ctx == nil {
			t.Fatal("Tail passed a nil context to Clock.Wait")
		}
		file, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		if _, err := file.WriteString("finish\n"); err != nil {
			file.Close()
			return err
		}
		file.Close()
		db, err := store.Open()
		if err != nil {
			return err
		}
		defer db.Close()
		return store.FinishTaskRun(db, "run-1", 0, 20, "done")
	}
	service := NewService(Dependencies{Clock: clock})
	var output bytes.Buffer
	if _, err := service.Tail(nil, taskRunCall(application.ActorAgent, "follow"), TailInput{
		TodoID: "t1", Follow: true,
	}, &output); err != nil {
		t.Fatal(err)
	}
	if clock.waits != 1 || output.String() != "start\nfinish\n" {
		t.Fatalf("waits=%d output=%q", clock.waits, output.String())
	}
}
