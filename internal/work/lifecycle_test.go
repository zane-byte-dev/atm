package work

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func lifecycleCall(kind application.ActorKind, requestID string) application.Call {
	agent := ""
	if kind == application.ActorAgent {
		agent = "codex"
	}
	return application.Call{
		RequestID: requestID,
		Actor: application.Actor{
			Kind:   kind,
			Origin: application.OriginCLI,
			Agent:  agent,
		},
	}
}

func TestStartReopensOnceAndRecoversPendingProjection(t *testing.T) {
	withTempWorkStore(t)
	closed, reason := "2026-08-01", "first attempt"
	oldStart, oldDone := int64(10), int64(20)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Reopen through service", Priority: "P1", Status: store.TodoStatusDone,
		Created: store.Today(), Closed: &closed, ClosedReason: &reason, StartTS: &oldStart, DoneTS: &oldDone,
	})

	first, err := Default.Start(context.Background(), lifecycleCall(application.ActorAgent, "start-1"), StartInput{
		TodoID: "#T01", ReopenReason: "acceptance found a regression",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if first.AlreadyStarted || first.Todo.Status != store.TodoStatusInProgress || first.Todo.StartTS == nil ||
		*first.Todo.StartTS == oldStart || first.Todo.DoneTS != nil || first.Todo.Closed != nil || first.Todo.ClosedReason != nil {
		t.Fatalf("first result = %+v", first)
	}
	if len(first.Effects) != 1 || first.Effects[0].Kind != EffectTodoStarted {
		t.Fatalf("first effects = %+v", first.Effects)
	}

	second, err := Default.Start(context.Background(), lifecycleCall(application.ActorAgent, "start-2"), StartInput{TodoID: "t1"})
	if err != nil {
		t.Fatalf("idempotent Start: %v", err)
	}
	if !second.AlreadyStarted || second.Todo.StartTS == nil || *second.Todo.StartTS != *first.Todo.StartTS ||
		len(second.Effects) != 1 || second.Effects[0].ID != first.Effects[0].ID {
		t.Fatalf("second result = %+v", second)
	}
}

func TestStartRequiresExplicitReasonToReopenReview(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Submitted", Priority: "P1", Status: store.TodoStatusReview,
		Creator: store.TodoCreatorMe, Created: store.Today(),
	})
	_, err := Default.Start(context.Background(), lifecycleCall(application.ActorHuman, "reopen-missing"), StartInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "--reopen-reason") {
		t.Fatalf("Start error = %v", err)
	}
	result, err := Default.Start(context.Background(), lifecycleCall(application.ActorHuman, "reopen-explicit"), StartInput{
		TodoID: "t1", ReopenReason: "review found an unhandled boundary",
	})
	if err != nil || !result.Reopened || result.Todo.Status != store.TodoStatusInProgress ||
		len(result.Effects) != 1 || result.Effects[0].Message != "[reopen] review found an unhandled boundary" {
		t.Fatalf("Start = %+v, err=%v", result, err)
	}
}

func TestReturnToOpenIsExplicitAndCannotEraseCompletionAudit(t *testing.T) {
	withTempWorkStore(t)
	startTS := int64(10)
	closed, reason, doneTS := "2026-09-01", "accepted after review", int64(20)
	seedWorkTodos(t,
		store.Todo{
			ID: "t1", Title: "Back to backlog", Priority: "P1", Status: store.TodoStatusInProgress,
			WakeCondition: "external release", ReviewAt: "2026-09-10", StartTS: &startTS, Created: store.Today(),
		},
		store.Todo{
			ID: "t2", Title: "Accepted", Priority: "P1", Status: store.TodoStatusDone,
			Closed: &closed, ClosedReason: &reason, StartTS: &startTS, DoneTS: &doneTS, Created: store.Today(),
		},
		store.Todo{
			ID: "t3", Title: "Stale open audit", Priority: "P1", Status: store.TodoStatusOpen,
			Closed: &closed, ClosedReason: &reason, StartTS: &startTS, DoneTS: &doneTS, Created: store.Today(),
		},
		store.Todo{
			ID: "t4", Title: "Under review", Priority: "P1", Status: store.TodoStatusReview,
			StartTS: &startTS, Created: store.Today(),
		},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "return-open", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	first, err := Default.ReturnToOpen(context.Background(), lifecycleCall(application.ActorAgent, "return-open-1"), ReturnToOpenInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.AlreadyOpen || first.UnboundSessions != 1 || first.Todo.Status != store.TodoStatusOpen ||
		first.Todo.WakeCondition != "" || first.Todo.ReviewAt != "" || first.Todo.StartTS != nil ||
		len(first.Effects) != 1 || first.Effects[0].Kind != EffectTodoUpdated {
		t.Fatalf("ReturnToOpen = %+v", first)
	}
	retry, err := Default.ReturnToOpen(context.Background(), lifecycleCall(application.ActorAgent, "return-open-2"), ReturnToOpenInput{TodoID: "t1"})
	if err != nil || !retry.AlreadyOpen || len(retry.Effects) != 1 || retry.Effects[0].ID != first.Effects[0].ID {
		t.Fatalf("ReturnToOpen retry = %+v, err=%v", retry, err)
	}

	_, err = Default.ReturnToOpen(context.Background(), lifecycleCall(application.ActorAgent, "return-done"), ReturnToOpenInput{TodoID: "t2"})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("completed ReturnToOpen error = %v, want conflict", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	completed := store.FindTodo(todos, "t2")
	if completed.Status != store.TodoStatusDone || completed.ClosedReason == nil || *completed.ClosedReason != reason ||
		completed.DoneTS == nil || *completed.DoneTS != doneTS {
		t.Fatalf("completed audit changed: %+v", completed)
	}
	_, err = Default.ReturnToOpen(context.Background(), lifecycleCall(application.ActorAgent, "return-review"), ReturnToOpenInput{TodoID: "t4"})
	if !errors.Is(err, application.ErrConflict) || !strings.Contains(err.Error(), "--reopen-reason") {
		t.Fatalf("review ReturnToOpen error = %v, want reopen conflict", err)
	}
	repaired, err := Default.ReturnToOpen(context.Background(), lifecycleCall(application.ActorAgent, "repair-open"), ReturnToOpenInput{TodoID: "t3"})
	if err != nil || repaired.AlreadyOpen || repaired.Todo.Closed != nil || repaired.Todo.ClosedReason != nil ||
		repaired.Todo.StartTS != nil || repaired.Todo.DoneTS != nil {
		t.Fatalf("stale open repair = %+v, err=%v", repaired, err)
	}
}

func TestDoneRequiresAcceptanceEvidenceButRetryRemainsIdempotent(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Await evidence", Priority: "P1", Status: store.TodoStatusReview,
		Creator: store.TodoCreatorMe, Created: store.Today(),
	})
	call := lifecycleCall(application.ActorHuman, "done-evidence")
	for _, reason := range []string{"", store.TodoGUICompletionReceipt} {
		_, err := Default.Done(context.Background(), call, CloseInput{TodoID: "t1", Reason: reason})
		if !errors.Is(err, application.ErrInvalidArgument) || !strings.Contains(err.Error(), "evidence") {
			t.Fatalf("Done(%q) error = %v", reason, err)
		}
	}
	result, err := Default.Done(context.Background(), call, CloseInput{TodoID: "t1", Reason: "reviewed output and reran tests"})
	if err != nil || result.Todo.ClosedReason == nil || *result.Todo.ClosedReason != "reviewed output and reran tests" {
		t.Fatalf("Done = %+v, err=%v", result, err)
	}
	if _, err := Default.Done(context.Background(), lifecycleCall(application.ActorHuman, "done-retry-evidence"), CloseInput{TodoID: "t1"}); err != nil {
		t.Fatalf("idempotent retry required evidence again: %v", err)
	}
}

func TestDoneFromGUIAcceptsOptionalNotesAndKeepsAudit(t *testing.T) {
	for _, status := range []string{store.TodoStatusOpen, store.TodoStatusInProgress, store.TodoStatusReview} {
		for _, note := range []struct{ name, input, want string }{
			{"empty", "", store.TodoGUICompletionReceipt},
			{"whitespace", " \n\t ", store.TodoGUICompletionReceipt},
			{"short", "完成", "完成"},
			{"custom", "  关键路径已验证  ", "关键路径已验证"},
		} {
			t.Run(status+"/"+note.name, func(t *testing.T) {
				withTempWorkStore(t)
				seedWorkTodos(t, store.Todo{
					ID: "t1", Title: "Human GUI acceptance", Status: status,
					Priority: "P2", Creator: store.TodoCreatorMe, Created: store.Today(),
				})
				call := lifecycleCall(application.ActorHuman, "gui-done")
				call.Actor.Origin = application.OriginWeb
				result, err := Default.Done(context.Background(), call, CloseInput{TodoID: "t1", Reason: note.input})
				if err != nil {
					t.Fatal(err)
				}
				if result.Todo.Status != store.TodoStatusDone || result.Todo.ClosedReason == nil ||
					*result.Todo.ClosedReason != note.want || result.Todo.DoneTS == nil {
					t.Fatalf("GUI Done = %+v", result)
				}
				if len(result.Effects) != 1 || result.Effects[0].Kind != EffectTodoClosed ||
					result.Effects[0].Message != "[done] "+note.want {
					t.Fatalf("GUI audit = %+v", result.Effects)
				}
				call.RequestID = "gui-done-retry"
				retry, err := Default.Done(context.Background(), call, CloseInput{TodoID: "t1"})
				if err != nil || !retry.AlreadyClosed || retry.Todo.ClosedReason == nil ||
					*retry.Todo.ClosedReason != note.want || len(retry.Effects) != 1 ||
					retry.Effects[0].ID != result.Effects[0].ID || *retry.Todo.DoneTS != *result.Todo.DoneTS {
					t.Fatalf("GUI retry = %+v, err=%v", retry, err)
				}
			})
		}
	}
}

func TestDoneFromWebStillRejectsNonHumanActors(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Needs human acceptance", Status: store.TodoStatusReview, Created: store.Today(),
	})
	for _, kind := range []application.ActorKind{application.ActorAgent, application.ActorController} {
		call := lifecycleCall(kind, "nonhuman-web-done")
		call.Actor.Origin = application.OriginWeb
		_, err := Default.Done(context.Background(), call, CloseInput{TodoID: "t1", Reason: store.TodoGUICompletionReceipt})
		if !errors.Is(err, application.ErrForbidden) {
			t.Fatalf("Done(%s@web) = %v, want forbidden", kind, err)
		}
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t1").Status != store.TodoStatusReview {
		t.Fatalf("non-human Web request changed state: %+v, err=%v", todos, err)
	}
}

func TestDoneRequiresHumanAndAtomicallyWakesDependencies(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Await acceptance", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Dependent work", Priority: "P1", Status: store.TodoStatusInProgress,
			WakeCondition: "waiting for todos: t1", DependsOn: []string{"t1"}, Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "done-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	_, err := Default.Done(context.Background(), lifecycleCall(application.ActorAgent, "done-agent"), CloseInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent Done error = %v, want forbidden", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if store.FindTodo(todos, "t1").Status != store.TodoStatusReview || store.FindTodo(todos, "t2").Status != store.TodoStatusInProgress {
		t.Fatalf("Agent Done mutated todos: %+v", todos.Items)
	}

	result, err := Default.Done(context.Background(), lifecycleCall(application.ActorHuman, "done-human"), CloseInput{
		TodoID: "t1", Reason: "accepted after verification",
	})
	if err != nil {
		t.Fatalf("human Done: %v", err)
	}
	if result.AlreadyClosed || result.Todo.Status != store.TodoStatusDone || result.UnboundSessions != 1 ||
		len(result.Awakened) != 1 || result.Awakened[0].TodoID != "t2" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Effects) != 2 || result.Effects[0].Kind != EffectTodoDependencyAwakened ||
		result.Effects[0].CauseTodoID != "t1" || result.Effects[1].Kind != EffectTodoClosed {
		t.Fatalf("effects = %+v", result.Effects)
	}
	if binding, err := store.CurrentTodoBinding("done-session"); err != nil || binding != nil {
		t.Fatalf("binding after Done = %+v, err=%v", binding, err)
	}
	todos, err = store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t2").Status != store.TodoStatusInProgress {
		t.Fatalf("todos after Done = %+v, err=%v", todos, err)
	}

	doneTS := *result.Todo.DoneTS
	retry, err := Default.Done(context.Background(), lifecycleCall(application.ActorHuman, "done-retry"), CloseInput{TodoID: "t1"})
	if err != nil {
		t.Fatalf("retry Done: %v", err)
	}
	if !retry.AlreadyClosed || retry.Todo.DoneTS == nil || *retry.Todo.DoneTS != doneTS || len(retry.Effects) != 2 ||
		retry.Effects[0].ID != result.Effects[0].ID || retry.Effects[1].ID != result.Effects[1].ID {
		t.Fatalf("retry = %+v", retry)
	}
}

func TestDoneRollsBackLifecycleWakeAndOutboxWhenUnbindFails(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Rollback acceptance", Priority: "P1", Status: store.TodoStatusReview, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Must stay waiting", Priority: "P1", Status: store.TodoStatusInProgress,
			WakeCondition: "waiting for todos: t1", DependsOn: []string{"t1"}, Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "rollback-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_done_unbind
		BEFORE UPDATE OF unbound_at ON todo_session_bindings
		WHEN NEW.reason = 'done'
		BEGIN SELECT RAISE(ABORT, 'injected done unbind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Default.Done(context.Background(), lifecycleCall(application.ActorHuman, "done-rollback"), CloseInput{
		TodoID: "t1", Reason: "verified before injected rollback",
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Done error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if store.FindTodo(todos, "t1").Status != store.TodoStatusReview || store.FindTodo(todos, "t2").Status != store.TodoStatusInProgress {
		t.Fatalf("rolled-back todos = %+v", todos.Items)
	}
	if binding, bindErr := store.CurrentTodoBinding("rollback-session"); bindErr != nil || binding == nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
	if pending, pendingErr := store.ListPendingWorkEffects(""); pendingErr != nil || len(pending) != 0 {
		t.Fatalf("outbox after rollback = %+v, err=%v", pending, pendingErr)
	}
}

func TestWakeIsValidatedTransactionalAndRetryable(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Wake externally", Priority: "P1", Status: store.TodoStatusInProgress,
		WakeCondition: "external approval", ReviewAt: "2026-09-01", Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "stale-wake-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	_, err := Default.Wake(context.Background(), lifecycleCall(application.ActorAgent, "wake-invalid"), WakeInput{
		TodoID: "t1", Reason: "t999 approved",
	})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid Wake error = %v", err)
	}

	first, err := Default.Wake(context.Background(), lifecycleCall(application.ActorAgent, "wake-1"), WakeInput{
		TodoID: "#T01", Reason: "external approval arrived",
	})
	if err != nil {
		t.Fatalf("Wake: %v", err)
	}
	if first.Todo.Status != store.TodoStatusInProgress || first.Todo.WakeCondition != "" || first.Todo.ReviewAt != "" ||
		first.UnboundSessions != 1 || len(first.Effects) != 1 || first.Effects[0].Kind != EffectTodoAwakened {
		t.Fatalf("first = %+v", first)
	}
	if binding, err := store.CurrentTodoBinding("stale-wake-session"); err != nil || binding != nil {
		t.Fatalf("binding after Wake = %+v, err=%v", binding, err)
	}
	retry, err := Default.Wake(context.Background(), lifecycleCall(application.ActorAgent, "wake-2"), WakeInput{
		TodoID: "t1", Reason: "external approval arrived",
	})
	if err != nil {
		t.Fatalf("retry Wake: %v", err)
	}
	if !retry.AlreadyAwake || len(retry.Effects) != 1 || retry.Effects[0].ID != first.Effects[0].ID {
		t.Fatalf("retry = %+v", retry)
	}
}

func TestRetentionBatchPolicyIsAtomicAndIdempotent(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Closed card", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Active card", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "trash-session", TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	call := lifecycleCall(application.ActorHuman, "retention")
	archivedBatch, err := Default.Archive(context.Background(), call, RetentionInput{TodoIDs: []string{"t1", "t2"}})
	if err != nil || len(archivedBatch.Moved) != 2 {
		t.Fatalf("Archive = %+v, err=%v", archivedBatch, err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil || len(todos.Items) != 0 {
		t.Fatalf("Archive left working todos: %+v, err=%v", todos, loadErr)
	}
	if binding, err := store.CurrentTodoBinding("trash-session"); err != nil || binding != nil {
		t.Fatalf("binding after Archive = %+v, err=%v", binding, err)
	}
	// Archiving what is already archived reports it as unchanged rather than
	// failing the batch.
	retry, err := Default.Archive(context.Background(), call, RetentionInput{TodoIDs: []string{"t2"}})
	if err != nil || len(retry.Moved) != 0 || len(retry.Unchanged) != 1 {
		t.Fatalf("idempotent Archive = %+v, err=%v", retry, err)
	}
	restored, err := Default.Restore(context.Background(), call, RetentionInput{TodoIDs: []string{"t2"}})
	if err != nil || len(restored.Moved) != 1 {
		t.Fatalf("Restore = %+v, err=%v", restored, err)
	}
	// Restore takes any lifecycle state back out of the archive, including the
	// closed card that only `unarchive` used to accept.
	reopened, err := Default.Restore(context.Background(), call, RetentionInput{TodoIDs: []string{"t1"}})
	if err != nil || len(reopened.Moved) != 1 {
		t.Fatalf("Restore closed card = %+v, err=%v", reopened, err)
	}
}

func TestDeleteRequiresConfirmedStablePlanAndCleansArtifacts(t *testing.T) {
	withTempWorkStore(t)
	source := filepath.Join(t.TempDir(), "evidence.png")
	if err := os.WriteFile(source, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	images, cleanup, err := store.ImportTodoImages("t1", []string{source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	t1 := store.Todo{ID: "t1", Title: "Delete with assets", Priority: "P1", Status: store.TodoStatusOpen, Project: "atm", Images: images, Created: store.Today()}
	t2 := store.Todo{ID: "t2", Title: "Delete with project", Priority: "P1", Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()}
	other := store.Todo{ID: "t3", Title: "Keep other project", Priority: "P1", Status: store.TodoStatusOpen, Project: "other", Created: store.Today()}
	seedWorkTodos(t, t1, t2, other)
	for _, todo := range []*store.Todo{&t1, &t2} {
		if _, err := store.EnsureTodoDoc(todo); err != nil {
			t.Fatal(err)
		}
	}
	call := lifecycleCall(application.ActorHuman, "delete")
	plan, err := Default.PlanDelete(context.Background(), call, DeleteSelector{Project: "atm"})
	if err != nil || len(plan.TodoIDs) != 2 {
		t.Fatalf("PlanDelete = %+v, err=%v", plan, err)
	}
	if _, err := Default.Delete(context.Background(), call, DeleteInput{Plan: plan}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("unconfirmed Delete error = %v", err)
	}
	if err := Default.Mutate(func(transaction *Transaction) error {
		transaction.Todos().Items = append(transaction.Todos().Items, store.Todo{
			ID: "t4", Title: "Created after prompt", Priority: "P1", Status: store.TodoStatusOpen, Project: "atm", Created: store.Today(),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Default.Delete(context.Background(), call, DeleteInput{Plan: plan, Confirmed: true}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("stale Delete error = %v, want conflict", err)
	}

	plan, err = Default.PlanDelete(context.Background(), call, DeleteSelector{Project: "atm"})
	if err != nil || len(plan.TodoIDs) != 3 {
		t.Fatalf("refreshed PlanDelete = %+v, err=%v", plan, err)
	}
	deleted, err := Default.Delete(context.Background(), call, DeleteInput{Plan: plan, Confirmed: true})
	if err != nil || len(deleted.Deleted) != 3 {
		t.Fatalf("Delete = %+v, err=%v", deleted, err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || todos.Items[0].ID != "t3" {
		t.Fatalf("todos after Delete = %+v, err=%v", todos, err)
	}
	for _, id := range []string{"t1", "t2"} {
		if store.TodoDocExists(id) {
			t.Fatalf("document %s survived Delete", id)
		}
	}
	if _, err := os.Stat(store.TodoAssetsDir("t1")); !os.IsNotExist(err) {
		t.Fatalf("assets survived Delete: %v", err)
	}
}
