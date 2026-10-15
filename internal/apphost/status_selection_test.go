package apphost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestStatusSelectionLifecycleAdapters(t *testing.T) {
	for _, method := range []string{"todo.submit", "todo.return_to_open"} {
		t.Run(method, func(t *testing.T) {
			h := testHost(t)
			h.SetWorkEffects(&countingEffects{})
			seed(t, card("t1", "status selection", "in_progress", "atm"))
			if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "bound-session", TodoID: "t1"}); err != nil {
				t.Fatal(err)
			}
			detail, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t1"})
			if err != nil {
				t.Fatal(err)
			}
			input := map[string]string{"todo_id": "t1"}
			expected := "review"
			if method == "todo.return_to_open" {
				input["expected_etag"] = detail.ETag
				expected = "open"
			}
			raw, _ := json.Marshal(input)
			agent := webCall()
			agent.Actor.Kind = application.ActorAgent
			agent.Actor.Agent = "codex"
			if _, err := h.Call(context.Background(), agent, method, raw, ""); err == nil {
				t.Fatal("agent Web mutation accepted")
			}
			result, err := h.Call(context.Background(), webCall(), method, raw, "")
			if err != nil {
				t.Fatal(err)
			}
			if result.(MutationResult).Todo.Status != expected {
				t.Fatalf("unexpected status: %+v", result)
			}
			if binding, err := store.CurrentTodoBinding("bound-session"); err != nil || binding != nil {
				t.Fatalf("binding not released: %+v %v", binding, err)
			}
		})
	}
}

func TestReturnToOpenRejectsStaleRevisionAndReview(t *testing.T) {
	h := testHost(t)
	h.SetWorkEffects(&countingEffects{})
	seed(t, card("t1", "in progress", "in_progress", "atm"), card("t2", "review", "review", "atm"))
	if _, err := h.ReturnTodoToOpen(context.Background(), webCall(), ReturnToOpenInput{TodoID: "t1", ExpectedETag: "stale"}); err == nil {
		t.Fatal("stale revision accepted")
	}
	detail, err := h.ShowTodo(context.Background(), webCall(), TodoInput{TodoID: "t2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.ReturnTodoToOpen(context.Background(), webCall(), ReturnToOpenInput{TodoID: "t2", ExpectedETag: detail.ETag}); err == nil {
		t.Fatal("review bypassed reopening")
	}
	if _, err := h.SubmitTodo(context.Background(), webCall(), DoneInput{TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
}
