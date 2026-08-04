package cmd

import (
	"testing"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestSessionBindingContextsSeparateObservationFromTodoState(t *testing.T) {
	todos := &store.TodoFile{Items: []store.Todo{
		{ID: "t1", Title: "Active work", Status: store.TodoStatusInProgress},
		{ID: "t2", Title: "Awaiting review", Status: store.TodoStatusReview},
	}}
	bindings := []store.TodoSessionBinding{
		{SessionID: "abcdefgh-full", TodoID: "t1"},
		{SessionID: "review-session", TodoID: "t2"},
		{SessionID: "missing-session", TodoID: "t404"},
	}
	sessions := []parser.Session{
		{SessionID: "abcdefgh"},
		{SessionID: "unbound-live-session"},
	}

	contexts := buildSessionBindingContexts(bindings, todos, sessions,
		matchBindingContextsToSessions(bindings, sessions))
	if len(contexts) != 3 {
		t.Fatalf("contexts = %#v", contexts)
	}
	if contexts[0].State != sessionBindingStateBound || !contexts[0].Observed || contexts[0].ObservedSessionID != "abcdefgh" {
		t.Fatalf("active context = %#v", contexts[0])
	}
	if contexts[1].State != sessionBindingStateTodoNotInProgress || contexts[1].Observed {
		t.Fatalf("review context = %#v", contexts[1])
	}
	if contexts[2].State != sessionBindingStateTodoMissing || contexts[2].Todo != nil {
		t.Fatalf("missing context = %#v", contexts[2])
	}
}

func TestSessionBindingFragmentMatchRequiresStableIDLength(t *testing.T) {
	if sessionIDsShareStableFragment("short", "shorter") {
		t.Fatal("short IDs must not prefix-match")
	}
	if !sessionIDsShareStableFragment("12345678", "12345678-full") {
		t.Fatal("stable 8-character IDs should prefix-match")
	}
	if !sessionIDsShareStableFragment("019f7d37-7220-7020-8952-d4f37ff5ad91", "d4f37ff5") {
		t.Fatal("Codex short IDs should match their stable full-thread fragment")
	}
}

func TestSessionBindingExactMatchWinsOverEarlierFragmentMatch(t *testing.T) {
	bindings := []store.TodoSessionBinding{
		{SessionID: "full-abcdefgh-value", TodoID: "t1"},
		{SessionID: "abcdefgh", TodoID: "t2"},
	}
	sessions := []parser.Session{{SessionID: "abcdefgh"}}

	matches := matchBindingContextsToSessions(bindings, sessions)
	if _, ok := matches[0]; ok {
		t.Fatalf("fragment binding unexpectedly matched: %#v", matches)
	}
	if got := matches[1]; got != 0 {
		t.Fatalf("exact match = %d, want 0; all matches: %#v", got, matches)
	}
}
