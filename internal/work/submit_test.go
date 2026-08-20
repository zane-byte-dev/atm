package work

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func withTempWorkStore(t *testing.T) {
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

func seedWorkTodos(t *testing.T, todos ...store.Todo) {
	t.Helper()
	if err := Default.Mutate(func(transaction *Transaction) error {
		transaction.Todos().Items = append([]store.Todo(nil), todos...)
		return nil
	}); err != nil {
		t.Fatalf("seed todos: %v", err)
	}
}

func submitCall(kind application.ActorKind, origin application.Origin) application.Call {
	return application.Call{
		RequestID: "submit-request",
		Actor: application.Actor{
			Kind:   kind,
			Origin: origin,
		},
	}
}

func TestSubmitCommitsReviewAndUnbindTogether(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Submit atomically", Priority: "P1", Status: store.TodoStatusInProgress,
		WakeCondition: "after CI", ReviewAt: "2026-09-01", Created: store.Today(),
	})
	for _, sessionID := range []string{"session-1", "session-2"} {
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: sessionID, TodoID: "t1"}); err != nil {
			t.Fatal(err)
		}
	}

	call := submitCall(application.ActorAgent, application.OriginCLI)
	call.Actor.Agent = "codex"
	call.Actor.SessionID = "session-1"
	result, err := Default.Submit(context.Background(), call, SubmitInput{
		TodoID: "t1", Reason: "implementation verified",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if result.AlreadyReview || result.UnboundSessions != 2 {
		t.Fatalf("result = %+v", result)
	}
	if result.Todo.Status != store.TodoStatusReview || result.Todo.WakeCondition != "" || result.Todo.ReviewAt != "" {
		t.Fatalf("submitted todo = %+v", result.Todo)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != EffectTodoSubmitted ||
		result.Effects[0].Message != "[submit] implementation verified" {
		t.Fatalf("effects = %+v", result.Effects)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("service performed Markdown work before the adapter handled its effect")
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusReview ||
		todo.WakeCondition != "" || todo.ReviewAt != "" {
		t.Fatalf("persisted todo = %+v", todo)
	}
	for _, sessionID := range []string{"session-1", "session-2"} {
		if binding, err := store.CurrentTodoBinding(sessionID); err != nil || binding != nil {
			t.Fatalf("binding %s = %+v, err=%v", sessionID, binding, err)
		}
	}
}

func TestSubmitAcceptsEveryAuthorizedActorKind(t *testing.T) {
	tests := []struct {
		name string
		call application.Call
	}{
		{name: "human CLI", call: submitCall(application.ActorHuman, application.OriginCLI)},
		{name: "agent CLI", call: application.Call{
			RequestID: "agent-submit",
			Actor: application.Actor{Kind: application.ActorAgent, Origin: application.OriginCLI,
				SessionID: "session-1", Agent: "codex"},
		}},
		{name: "controller", call: application.Call{
			RequestID: "controller-submit",
			Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController,
				SessionID: "session-1", Agent: "codex"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Actor may submit", Priority: "P1",
				Status: store.TodoStatusInProgress, Created: store.Today(),
			})
			result, err := Default.Submit(context.Background(), test.call, SubmitInput{TodoID: "t1"})
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			if result.Todo.Status != store.TodoStatusReview {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestSubmitReviewIsIdempotentWithoutAnotherEvent(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Already submitted", Priority: "P1",
		Status: store.TodoStatusReview, Created: store.Today(),
	})
	result, err := Default.Submit(context.Background(), submitCall(application.ActorHuman, application.OriginCLI), SubmitInput{
		TodoID: "t1",
		// Idempotency wins before log validation: the original submission already
		// supplied the only progress entry this transition owns.
		Reason: "this\nwould be invalid for a new submission",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !result.AlreadyReview || len(result.Effects) != 0 || result.UnboundSessions != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestSubmitRejectsInvalidCallStatusAndLogBeforeMutation(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		call      application.Call
		reason    string
		wantError error
	}{
		{
			name: "invalid call", status: store.TodoStatusInProgress,
			call:      application.Call{Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginCLI}},
			wantError: application.ErrInvalidArgument,
		},
		{
			name: "not started", status: store.TodoStatusOpen,
			call:      submitCall(application.ActorHuman, application.OriginCLI),
			wantError: application.ErrConflict,
		},
		{
			name: "multiline reason", status: store.TodoStatusInProgress,
			call: submitCall(application.ActorAgent, application.OriginCLI), reason: "result\nevidence",
			wantError: application.ErrInvalidArgument,
		},
		{
			name: "unknown todo reference", status: store.TodoStatusInProgress,
			call: submitCall(application.ActorAgent, application.OriginCLI), reason: "completed t999",
			wantError: application.ErrInvalidArgument,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Reject safely", Priority: "P1", Status: test.status,
				WakeCondition: "keep", ReviewAt: "2026-09-01", Created: store.Today(),
			})
			if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
				t.Fatal(err)
			}
			_, err := Default.Submit(context.Background(), test.call, SubmitInput{TodoID: "t1", Reason: test.reason})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Submit error = %v, want %v", err, test.wantError)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != test.status ||
				todo.WakeCondition != "keep" || todo.ReviewAt != "2026-09-01" {
				t.Fatalf("todo mutated after rejection: %+v", todo)
			}
			if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding == nil {
				t.Fatalf("binding after rejection = %+v, err=%v", binding, bindErr)
			}
		})
	}
}

func TestSubmitRollsBackStatusWhenUnbindFails(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Rollback together", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_submit_unbind
		BEFORE UPDATE OF unbound_at ON todo_session_bindings
		WHEN NEW.reason = 'submit:review'
		BEGIN SELECT RAISE(ABORT, 'injected unbind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	result, err := Default.Submit(context.Background(), submitCall(application.ActorController, application.OriginController), SubmitInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Submit error = %v, want unavailable", err)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("failed transaction returned effects: %+v", result.Effects)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress {
		t.Fatalf("todo after rollback = %+v", todo)
	}
	if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding == nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
	if pending, pendingErr := store.ListPendingWorkEffects("t1"); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("outbox after rollback = %+v, err=%v", pending, pendingErr)
	}
}

func TestSubmitKnownTodoReferenceIsAccepted(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Main", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependency", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
	)
	result, err := Default.Submit(context.Background(), submitCall(application.ActorAgent, application.OriginCLI), SubmitInput{
		TodoID: "t1", Reason: "verified against t2",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(result.Effects) != 1 || !strings.Contains(result.Effects[0].Message, "t2") {
		t.Fatalf("effects = %+v", result.Effects)
	}
}

func TestSubmitRetryReturnsStablePendingEffectUntilAcknowledged(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Recover durable effect", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "outbox-submit-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	firstCall := submitCall(application.ActorAgent, application.OriginCLI)
	firstCall.Actor.SessionID = "outbox-submit-session"
	firstCall.Actor.Agent = "codex"
	first, err := Default.Submit(context.Background(), firstCall, SubmitInput{
		TodoID: "t1", Reason: "state committed before projection",
	})
	if err != nil || len(first.Effects) != 1 {
		t.Fatalf("first Submit = %+v, err=%v", first, err)
	}

	retryCall := submitCall(application.ActorAgent, application.OriginCLI)
	retryCall.RequestID = "different-request-id"
	retryCall.Actor.SessionID = "outbox-submit-session"
	retryCall.Actor.Agent = "codex"
	retry, err := Default.Submit(context.Background(), retryCall, SubmitInput{})
	if err != nil {
		t.Fatalf("retry Submit: %v", err)
	}
	if !retry.AlreadyReview || len(retry.Effects) != 1 || retry.Effects[0].ID != first.Effects[0].ID ||
		retry.Effects[0].RequestID != first.Effects[0].RequestID {
		t.Fatalf("retry effects = %+v, first = %+v", retry.Effects, first.Effects)
	}

	if err := Default.CompleteEffect(context.Background(), retryCall, CompleteEffectInput{EffectID: retry.Effects[0].ID}); err != nil {
		t.Fatalf("CompleteEffect: %v", err)
	}
	afterAck, err := Default.Submit(context.Background(), retryCall, SubmitInput{TodoID: "t1"})
	if err != nil || !afterAck.AlreadyReview || len(afterAck.Effects) != 0 {
		t.Fatalf("Submit after ack = %+v, err=%v", afterAck, err)
	}
}
