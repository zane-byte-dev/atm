package work

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func bindingCall(kind application.ActorKind, sessionID string) application.Call {
	return application.Call{
		RequestID: "binding-request",
		Actor: application.Actor{
			Kind:      kind,
			Origin:    application.OriginCLI,
			SessionID: sessionID,
			Agent:     "codex",
		},
	}
}

func TestBindStartsTodoAndSessionInOneUseCase(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Bind atomically", Priority: "P1", Status: store.TodoStatusWaiting,
		Project: "atm", WakeCondition: "waiting for a worker", ReviewAt: "2026-09-01", Created: store.Today(),
	})

	result, err := Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), BindInput{
		TodoID: "#T01", Agent: "openai-codex", Project: "ATM", CWD: "/tmp/atm", WorkspaceProject: "atm",
	})
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if result.Todo.ID != "t1" || result.Todo.Status != store.TodoStatusInProgress ||
		result.Todo.WakeCondition != "" || result.Todo.ReviewAt != "" || result.Todo.StartTS == nil {
		t.Fatalf("todo = %+v", result.Todo)
	}
	if result.Binding.SessionID != "session-1" || result.Binding.TodoID != "t1" ||
		result.Binding.Agent != "codex" || result.Binding.Project != "ATM" || result.Binding.CWD != "/tmp/atm" {
		t.Fatalf("binding = %+v", result.Binding)
	}
	firstStart, firstBound := *result.Todo.StartTS, result.Binding.BoundAt

	second, err := Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), BindInput{
		TodoID: "t1", Agent: "codex", Project: "atm", CWD: "/tmp/atm", WorkspaceProject: "atm",
	})
	if err != nil {
		t.Fatalf("idempotent Bind: %v", err)
	}
	if second.Todo.StartTS == nil || *second.Todo.StartTS != firstStart || second.Binding.BoundAt != firstBound {
		t.Fatalf("idempotent bind reset timestamps: first=%+v second=%+v", result, second)
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress || todo.StartTS == nil {
		t.Fatalf("persisted todo = %+v", todo)
	}
	binding, err := store.CurrentTodoBinding("session-1")
	if err != nil || binding == nil || binding.TodoID != "t1" {
		t.Fatalf("persisted binding = %+v, err=%v", binding, err)
	}
}

func TestBindOwnsTaskRunLinkAndTodoDocumentAfterCommit(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Bind all effects", Priority: "P1", Status: store.TodoStatusOpen,
		Project: "atm", Created: store.Today(),
	})
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
	db.Close()

	result, err := Default.Bind(
		context.Background(), bindingCall(application.ActorAgent, "actual-session"),
		BindInput{TodoID: "t1", RunID: "run-1", RunTodoID: "#T01"},
	)
	if err != nil || len(result.Warnings) != 0 {
		t.Fatalf("Bind = %+v, err=%v", result, err)
	}
	if !store.TodoDocExists("t1") {
		t.Fatal("Bind did not materialize the Todo document")
	}
	db, err = store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, err := store.GetTaskRun(db, "run-1")
	if err != nil || run == nil || run.SessionID == nil || *run.SessionID != "actual-session" {
		t.Fatalf("run = %+v, err=%v", run, err)
	}
}

func TestBindReportsMissingTaskRunAsNonFatalTypedWarning(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Keep successful binding", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today(),
	})
	result, err := Default.Bind(
		context.Background(), bindingCall(application.ActorAgent, "session-1"),
		BindInput{TodoID: "t1", RunID: "missing-run", RunTodoID: "t1"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Code != BindWarningTaskRunLinkFailed ||
		result.Warnings[0].RunID != "missing-run" || result.Warnings[0].Cause == nil {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	binding, err := store.CurrentTodoBinding("session-1")
	if err != nil || binding == nil || binding.TodoID != "t1" {
		t.Fatalf("binding = %+v, err=%v", binding, err)
	}
}

func TestBindRejectsLifecycleAndWorkspaceConflictsBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		status string
		input  BindInput
	}{
		{
			name: "completed todo", status: store.TodoStatusDone,
			input: BindInput{TodoID: "t1", CWD: "/tmp/atm", WorkspaceProject: "atm"},
		},
		{
			name: "wrong workspace", status: store.TodoStatusOpen,
			input: BindInput{TodoID: "t1", CWD: "/tmp/wanda", WorkspaceProject: "wanda"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Reject safely", Priority: "P1", Status: test.status,
				Project: "atm", WakeCondition: "keep wake", ReviewAt: "2026-09-01", Created: store.Today(),
			})
			_, err := Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), test.input)
			if !errors.Is(err, application.ErrConflict) {
				t.Fatalf("Bind error = %v, want conflict", err)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != test.status ||
				todo.WakeCondition != "keep wake" || todo.ReviewAt != "2026-09-01" || todo.StartTS != nil {
				t.Fatalf("todo mutated after conflict: %+v", todo)
			}
			if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding != nil {
				t.Fatalf("binding after conflict = %+v, err=%v", binding, bindErr)
			}
		})
	}
}

func TestBindForceAndUnknownWorkspaceDoNotInventAConflict(t *testing.T) {
	tests := []struct {
		name  string
		input BindInput
	}{
		{name: "client has no cwd", input: BindInput{TodoID: "t1", WorkspaceProject: "wanda"}},
		{name: "forced", input: BindInput{TodoID: "t1", CWD: "/tmp/wanda", WorkspaceProject: "wanda", Force: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Deliberate cross-project work", Priority: "P1",
				Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
			})
			if _, err := Default.Bind(context.Background(), bindingCall(application.ActorHuman, "session-1"), test.input); err != nil {
				t.Fatalf("Bind: %v", err)
			}
		})
	}
}

func TestBindRollsBackTodoWhenBindingWriteFails(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Rollback together", Priority: "P1", Status: store.TodoStatusOpen,
		WakeCondition: "keep wake", ReviewAt: "2026-09-01", Created: store.Today(),
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_session_bind
		BEFORE INSERT ON todo_session_bindings
		BEGIN SELECT RAISE(ABORT, 'injected bind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), BindInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Bind error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusOpen ||
		todo.WakeCondition != "keep wake" || todo.ReviewAt != "2026-09-01" || todo.StartTS != nil {
		t.Fatalf("todo was committed without binding: %+v", todo)
	}
	if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding != nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
}

func TestCurrentClassifiesUnboundBoundAndStaleBindings(t *testing.T) {
	withTempWorkStore(t)
	call := bindingCall(application.ActorAgent, "session-1")

	// A missing database means this session has never been bound, not that the
	// caller must initialize storage before it can ask.
	current, err := Default.Current(context.Background(), call, CurrentInput{})
	if err != nil || current.Bound || current.State != BindingStateUnbound || current.Context != nil {
		t.Fatalf("fresh Current = %+v, err=%v", current, err)
	}

	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Current work", Priority: "P1", Status: store.TodoStatusInProgress,
		Project: "atm", Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	current, err = Default.Current(context.Background(), call, CurrentInput{})
	if err != nil || !current.Bound || current.State != BindingStateBound || current.Context == nil ||
		current.Context.Todo == nil || current.Context.Todo.ID != "t1" {
		t.Fatalf("bound Current = %+v, err=%v", current, err)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"context"`, `"binding"`, `"todo"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("typed Current JSON omitted %s: %s", field, encoded)
		}
	}

	if err := Default.Mutate(func(transaction *Transaction) error {
		transaction.Todos().Items[0].Status = store.TodoStatusReview
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	current, err = Default.Current(context.Background(), call, CurrentInput{})
	if err != nil || current.Bound || current.State != BindingStateTodoNotInProgress || current.Context == nil {
		t.Fatalf("stale Current = %+v, err=%v", current, err)
	}

	missing := BuildBindingContexts(
		[]store.TodoSessionBinding{{SessionID: "orphan", TodoID: "t404"}},
		&store.TodoFile{},
	)
	if len(missing) != 1 || missing[0].State != string(BindingStateTodoMissing) || missing[0].Todo != nil {
		t.Fatalf("missing context = %+v", missing)
	}
}

func TestUnbindPreservesHistoryAndIsIdempotent(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Preserve binding history", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	call := bindingCall(application.ActorHuman, "session-1")
	result, err := Default.Unbind(context.Background(), call, UnbindInput{Reason: " scope changed "})
	if err != nil || !result.Unbound || result.SessionID != "session-1" {
		t.Fatalf("Unbind = %+v, err=%v", result, err)
	}
	second, err := Default.Unbind(context.Background(), call, UnbindInput{Reason: "manual"})
	if err != nil || second.Unbound {
		t.Fatalf("idempotent Unbind = %+v, err=%v", second, err)
	}
	history, err := store.ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].UnboundAt == nil || history[0].Reason != "scope changed" {
		t.Fatalf("history = %+v", history)
	}
}

func TestBindingUseCasesValidateCallPolicyAndInput(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Validate binding calls", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{
			name: "missing request ID",
			run: func() error {
				_, err := Default.Current(context.Background(), application.Call{Actor: application.Actor{
					Kind: application.ActorHuman, Origin: application.OriginCLI, SessionID: "session-1",
				}}, CurrentInput{})
				return err
			},
			want: application.ErrInvalidArgument,
		},
		{
			name: "missing session ID",
			run: func() error {
				_, err := Default.Current(context.Background(), bindingCall(application.ActorHuman, ""), CurrentInput{})
				return err
			},
			want: application.ErrInvalidArgument,
		},
		{
			name: "controller bind",
			run: func() error {
				_, err := Default.Bind(context.Background(), bindingCall(application.ActorController, "session-1"), BindInput{TodoID: "t1"})
				return err
			},
			want: application.ErrForbidden,
		},
		{
			name: "relative cwd",
			run: func() error {
				_, err := Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), BindInput{TodoID: "t1", CWD: "relative"})
				return err
			},
			want: application.ErrInvalidArgument,
		},
		{
			name: "unknown agent",
			run: func() error {
				_, err := Default.Bind(context.Background(), bindingCall(application.ActorAgent, "session-1"), BindInput{TodoID: "t1", Agent: "robot"})
				return err
			},
			want: application.ErrInvalidArgument,
		},
		{
			name: "empty unbind reason",
			run: func() error {
				_, err := Default.Unbind(context.Background(), bindingCall(application.ActorHuman, "session-1"), UnbindInput{})
				return err
			},
			want: application.ErrInvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
