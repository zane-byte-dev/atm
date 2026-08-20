package work

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func waitCall(kind application.ActorKind, origin application.Origin) application.Call {
	return application.Call{
		RequestID: "wait-request",
		Actor: application.Actor{
			Kind:   kind,
			Origin: origin,
		},
	}
}

func TestWaitCommitsConditionsAndUnbindsTogether(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Wait atomically", Priority: "P1", Status: store.TodoStatusInProgress,
		WakeCondition: "keep existing wake", Created: store.Today(),
	})
	for _, sessionID := range []string{"session-1", "session-2"} {
		if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: sessionID, TodoID: "t1"}); err != nil {
			t.Fatal(err)
		}
	}

	call := waitCall(application.ActorAgent, application.OriginCLI)
	call.Actor.SessionID = "session-1"
	call.Actor.Agent = "codex"
	result, err := Default.Wait(context.Background(), call, WaitInput{
		TodoID: "#T01", ReviewAt: "2026-09-01",
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.UnboundSessions != 2 {
		t.Fatalf("unbound sessions = %d", result.UnboundSessions)
	}
	if result.Todo.Status != store.TodoStatusInProgress ||
		result.Todo.WakeCondition != "keep existing wake" || result.Todo.ReviewAt != "2026-09-01" {
		t.Fatalf("waiting todo = %+v", result.Todo)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != EffectTodoWaiting ||
		result.Effects[0].Todo.ID != "t1" {
		t.Fatalf("effects = %+v", result.Effects)
	}
	for _, sessionID := range []string{"session-1", "session-2"} {
		if binding, bindErr := store.CurrentTodoBinding(sessionID); bindErr != nil || binding != nil {
			t.Fatalf("binding %s = %+v, err=%v", sessionID, binding, bindErr)
		}
	}
}

func TestWaitRetainsExistingConditionWhenInputIsOmitted(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Retain review date", Priority: "P1", Status: store.TodoStatusReview,
		ReviewAt: "2026-08-30", Created: store.Today(),
	})
	result, err := Default.Wait(context.Background(), waitCall(application.ActorHuman, application.OriginCLI), WaitInput{TodoID: "1"})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if result.Todo.Status != store.TodoStatusInProgress || result.Todo.ReviewAt != "2026-08-30" {
		t.Fatalf("result = %+v", result)
	}
}

func TestWaitAcceptsEveryAuthorizedActorKind(t *testing.T) {
	tests := []struct {
		name string
		call application.Call
	}{
		{name: "human CLI", call: waitCall(application.ActorHuman, application.OriginCLI)},
		{name: "agent CLI", call: application.Call{
			RequestID: "agent-wait",
			Actor: application.Actor{Kind: application.ActorAgent, Origin: application.OriginCLI,
				SessionID: "session-1", Agent: "codex"},
		}},
		{name: "controller", call: application.Call{
			RequestID: "controller-wait",
			Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController,
				SessionID: "session-1", Agent: "codex"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Actor may wait", Priority: "P1",
				Status: store.TodoStatusInProgress, Created: store.Today(),
			})
			result, err := Default.Wait(context.Background(), test.call, WaitInput{
				TodoID: "t1", WakeCondition: "external condition",
			})
			if err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if result.Todo.Status != store.TodoStatusInProgress {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestWaitRejectsTypedValidationAndLifecycleErrorsBeforeMutation(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		call      application.Call
		input     WaitInput
		wantError error
	}{
		{
			name: "invalid call", status: store.TodoStatusInProgress,
			call:  application.Call{Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginCLI}},
			input: WaitInput{TodoID: "t1", WakeCondition: "condition"}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "missing todo ID", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{WakeCondition: "condition"}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "malformed todo ID", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "work", WakeCondition: "condition"}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "blank wake", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "t1", WakeCondition: "  "}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "invalid review date", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "t1", ReviewAt: "2026-02-30"}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "no condition", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "t1"}, wantError: application.ErrInvalidArgument,
		},
		{
			name: "closed todo", status: store.TodoStatusDone,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "t1", WakeCondition: "condition"}, wantError: application.ErrConflict,
		},
		{
			name: "unknown todo", status: store.TodoStatusInProgress,
			call:  waitCall(application.ActorHuman, application.OriginCLI),
			input: WaitInput{TodoID: "t2", WakeCondition: "condition"}, wantError: application.ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Reject safely", Priority: "P1", Status: test.status,
				Created: store.Today(),
			})
			if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
				t.Fatal(err)
			}
			_, err := Default.Wait(context.Background(), test.call, test.input)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Wait error = %v, want %v", err, test.wantError)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != test.status ||
				todo.WakeCondition != "" || todo.ReviewAt != "" {
				t.Fatalf("todo mutated after rejection: %+v", todo)
			}
			if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding == nil {
				t.Fatalf("binding after rejection = %+v, err=%v", binding, bindErr)
			}
		})
	}
}

func TestWaitRollsBackTodoWhenUnbindFails(t *testing.T) {
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
	_, err = db.Exec(`CREATE TRIGGER fail_wait_unbind
		BEFORE UPDATE OF unbound_at ON todo_session_bindings
		WHEN NEW.reason = 'waiting'
		BEGIN SELECT RAISE(ABORT, 'injected unbind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	result, err := Default.Wait(context.Background(), waitCall(application.ActorController, application.OriginController), WaitInput{
		TodoID: "t1", WakeCondition: "external condition",
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Wait error = %v, want unavailable", err)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("failed transaction returned effects: %+v", result.Effects)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress ||
		todo.WakeCondition != "" || todo.ReviewAt != "" {
		t.Fatalf("todo after rollback = %+v", todo)
	}
	if binding, bindErr := store.CurrentTodoBinding("session-1"); bindErr != nil || binding == nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
	if pending, pendingErr := store.ListPendingWorkEffects("t1"); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("outbox after rollback = %+v, err=%v", pending, pendingErr)
	}
}

func TestWaitRetryCoalescesPendingEffectAndStopsAfterAcknowledgement(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Coalesce document sync", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "outbox-wait-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	firstCall := waitCall(application.ActorAgent, application.OriginCLI)
	firstCall.Actor.SessionID = "outbox-wait-session"
	firstCall.Actor.Agent = "codex"
	first, err := Default.Wait(context.Background(), firstCall, WaitInput{
		TodoID: "t1", WakeCondition: "first condition",
	})
	if err != nil || len(first.Effects) != 1 {
		t.Fatalf("first Wait = %+v, err=%v", first, err)
	}

	retryCall := waitCall(application.ActorAgent, application.OriginCLI)
	retryCall.RequestID = "wait-retry-request"
	retryCall.Actor.SessionID = "outbox-wait-session"
	retryCall.Actor.Agent = "codex"
	retry, err := Default.Wait(context.Background(), retryCall, WaitInput{
		WakeCondition: "first condition",
	})
	if err != nil || len(retry.Effects) != 1 || retry.Effects[0].ID != first.Effects[0].ID {
		t.Fatalf("retry Wait = %+v, err=%v", retry, err)
	}
	updated, err := Default.Wait(context.Background(), retryCall, WaitInput{
		WakeCondition: "updated condition",
	})
	if err != nil || len(updated.Effects) != 1 || updated.Effects[0].ID != first.Effects[0].ID ||
		updated.Effects[0].Todo.WakeCondition != "updated condition" {
		t.Fatalf("updated Wait = %+v, err=%v", updated, err)
	}

	if err := Default.CompleteEffect(context.Background(), retryCall, CompleteEffectInput{EffectID: first.Effects[0].ID}); err != nil {
		t.Fatalf("CompleteEffect: %v", err)
	}
	afterAck, err := Default.Wait(context.Background(), retryCall, WaitInput{
		TodoID: "t1", WakeCondition: "updated condition",
	})
	if err != nil || len(afterAck.Effects) != 0 {
		t.Fatalf("Wait after ack = %+v, err=%v", afterAck, err)
	}
}

func TestValidateReviewAtPreservesDateContract(t *testing.T) {
	for _, value := range []string{"", "2026-08-20"} {
		if err := ValidateReviewAt(value); err != nil {
			t.Errorf("ValidateReviewAt(%q): %v", value, err)
		}
	}
	for _, value := range []string{"2026-2-3", "2026-02-30", " 2026-08-20"} {
		if err := ValidateReviewAt(value); err == nil {
			t.Errorf("ValidateReviewAt(%q) succeeded", value)
		}
	}
}
