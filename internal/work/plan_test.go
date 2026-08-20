package work

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func planCall(requestID, sessionID string) application.Call {
	return application.Call{
		RequestID: requestID,
		Actor: application.Actor{
			Kind: application.ActorAgent, Origin: application.OriginCLI,
			SessionID: sessionID, Agent: "codex",
		},
	}
}

func TestSetPlanAppendsRevisionWithBindingProvenanceAndSyncsReaders(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Implement plan service", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	binding, err := store.BindTodoSession(store.TodoSessionBinding{
		SessionID: "session-plan", TodoID: "t1", Agent: "codex", Project: "atm",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := Default.SetPlan(context.Background(), planCall("plan-1", "session-plan"), SetPlanInput{
		BaseRevision: 0,
		Explanation:  " entering verification ",
		Items: []PlanItem{
			{Step: " finish implementation ", Status: PlanCompleted},
			{Step: "run regression tests", Status: PlanInProgress},
		},
	})
	if err != nil {
		t.Fatalf("SetPlan: %v", err)
	}
	if !result.Changed || result.Plan.TodoID != "t1" || result.Plan.Revision != 1 ||
		result.Plan.Explanation != "entering verification" || result.Plan.BindingID != binding.ID ||
		result.Plan.SessionID != "session-plan" || result.Plan.Agent != "codex" {
		t.Fatalf("result = %+v, binding=%+v", result, binding)
	}
	if result.Plan.ActorKind != application.ActorAgent || result.Plan.Origin != application.OriginCLI {
		t.Fatalf("plan provenance = %+v", result.Plan)
	}

	stored, err := store.LatestTodoPlanRevision("t1")
	if err != nil || stored == nil || stored.Revision != 1 || stored.BindingID == nil || *stored.BindingID != binding.ID {
		t.Fatalf("stored revision = %+v, err=%v", stored, err)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, "## 执行计划") ||
		!strings.Contains(doc, "- [x] finish implementation") ||
		!strings.Contains(doc, "- [>] run regression tests") {
		t.Fatalf("plan document = %q, err=%v", doc, err)
	}

	show, err := Default.Show(context.Background(), planCall("show-plan", "session-plan"), ShowInput{TodoID: "t1"})
	if err != nil || show.LatestPlan == nil || show.LatestPlan.Revision != 1 {
		t.Fatalf("Show latest plan = %+v, err=%v", show.LatestPlan, err)
	}
	contextResult, err := Default.Context(context.Background(), planCall("context-plan", "session-plan"), ContextInput{TodoID: "t1", CWD: t.TempDir()})
	if err != nil || contextResult.LatestPlan == nil || contextResult.LatestPlan.Revision != 1 {
		t.Fatalf("Context latest plan = %+v, err=%v", contextResult.LatestPlan, err)
	}
}

func TestSetPlanIsSnapshotIdempotentBeforeBaseRevisionConflict(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Retry a plan", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today(),
	})
	input := SetPlanInput{TodoID: "t1", BaseRevision: 0, Items: []PlanItem{{Step: "implement", Status: PlanInProgress}}}
	first, err := Default.SetPlan(context.Background(), planCall("plan-first", ""), input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := Default.SetPlan(context.Background(), planCall("plan-retry", ""), input)
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if !first.Changed || retry.Changed || retry.Plan.Revision != first.Plan.Revision {
		t.Fatalf("first=%+v retry=%+v", first, retry)
	}

	changed := input
	changed.Items = []PlanItem{{Step: "implement", Status: PlanCompleted}}
	if _, err := Default.SetPlan(context.Background(), planCall("plan-conflict", ""), changed); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("stale base error = %v, want conflict", err)
	}
	changed.BaseRevision = 1
	second, err := Default.SetPlan(context.Background(), planCall("plan-second", ""), changed)
	if err != nil || !second.Changed || second.Plan.Revision != 2 {
		t.Fatalf("second = %+v, err=%v", second, err)
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusOpen {
		t.Fatalf("plan changed todo lifecycle: %+v", todo)
	}
}

func TestSetPlanRejectsInvalidSnapshotBeforeMutation(t *testing.T) {
	for name, input := range map[string]SetPlanInput{
		"negative revision": {TodoID: "t1", BaseRevision: -1},
		"empty step":        {TodoID: "t1", Items: []PlanItem{{Step: " ", Status: PlanPending}}},
		"unknown status":    {TodoID: "t1", Items: []PlanItem{{Step: "one", Status: "doing"}}},
		"two active": {TodoID: "t1", Items: []PlanItem{
			{Step: "one", Status: PlanInProgress}, {Step: "two", Status: PlanInProgress},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{ID: "t1", Title: "Validate plan", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()})
			if _, err := Default.SetPlan(context.Background(), planCall("invalid-plan", ""), input); !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("SetPlan error = %v, want invalid_argument", err)
			}
			if revision, err := store.LatestTodoPlanRevision("t1"); err != nil || revision != nil {
				t.Fatalf("invalid plan persisted: %+v, err=%v", revision, err)
			}
		})
	}
}

func TestSetPlanConcurrentWritersHaveOneBaseRevisionWinner(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Serialize plan writers", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	type outcome struct {
		result SetPlanResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	for index, step := range []string{"agent A plan", "agent B plan"} {
		go func(index int, step string) {
			result, err := Default.SetPlan(context.Background(), planCall(fmt.Sprintf("concurrent-%d", index), ""), SetPlanInput{
				TodoID: "t1", BaseRevision: 0,
				Items: []PlanItem{{Step: step, Status: PlanInProgress}},
			})
			outcomes <- outcome{result: result, err: err}
		}(index, step)
	}
	successes, conflicts := 0, 0
	for range 2 {
		value := <-outcomes
		switch {
		case value.err == nil:
			successes++
			if value.result.Plan.Revision != 1 {
				t.Fatalf("winning revision = %+v", value.result)
			}
		case errors.Is(value.err, application.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent SetPlan error = %v", value.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	latest, err := store.LatestTodoPlanRevision("t1")
	if err != nil || latest == nil || latest.Revision != 1 {
		t.Fatalf("latest = %+v, err=%v", latest, err)
	}
	show, err := Default.Show(context.Background(), planCall("concurrent-show", ""), ShowInput{TodoID: "t1"})
	if err != nil || show.LatestPlan == nil || len(show.LatestPlan.Items) != 1 {
		t.Fatalf("latest plan read model = %+v, err=%v", show.LatestPlan, err)
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(doc, show.LatestPlan.Items[0].Step) {
		t.Fatalf("document did not converge to latest plan %q: %q, err=%v", show.LatestPlan.Items[0].Step, doc, err)
	}
}
