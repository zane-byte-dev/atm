package work

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestMatchReturnsCurrentValidBindingBeforeCandidates(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Current binding", Priority: "P1", Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Another candidate", Priority: "P0", Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Match(
		context.Background(), bindingCall(application.ActorAgent, "session-1"),
		MatchInput{Project: "ATM", Query: "another", Limit: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Bound || result.Binding == nil || result.Binding.TodoID != "t1" ||
		result.Todo == nil || result.Todo.ID != "t1" || result.Candidates != nil {
		t.Fatalf("match result = %+v", result)
	}
}

func TestMatchDedupIgnoresBindingAndSearchesAllProjects(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Current unrelated work", Priority: "P1", Status: store.TodoStatusInProgress, Project: "atm", Created: store.Today()},
		store.Todo{ID: "t2", Title: "Ship lunar dashboard", Priority: "P1", Status: store.TodoStatusOpen, Project: "wanda", Created: store.Today()},
	)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Match(
		context.Background(), bindingCall(application.ActorAgent, "session-1"),
		MatchInput{
			Project: "atm", Query: "ship lunar dashboard", Limit: 3,
			Deduplicate: true, MinQueryScore: DefaultDedupMinQueryScore,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bound || !result.Duplicate || len(result.Candidates) != 1 || result.Candidates[0].ID != "t2" {
		t.Fatalf("dedup result = %+v", result)
	}
}

func TestMatchValidatesLimitAndDedupQuery(t *testing.T) {
	call := bindingCall(application.ActorAgent, "")
	for _, input := range []MatchInput{
		{Limit: 0},
		{Limit: 3, Deduplicate: true, Query: "  "},
	} {
		if _, err := Default.Match(context.Background(), call, input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("Match(%+v) error = %v, want invalid_argument", input, err)
		}
	}
}

func TestMatchOnFreshStoreReturnsStableEmptyCandidates(t *testing.T) {
	withTempWorkStore(t)
	result, err := Default.Match(
		context.Background(), bindingCall(application.ActorHuman, ""),
		MatchInput{Project: "atm", Query: "new work", Limit: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidates == nil || len(result.Candidates) != 0 || result.Bound {
		t.Fatalf("fresh result = %+v", result)
	}
}
