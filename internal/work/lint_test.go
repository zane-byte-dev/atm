package work

import (
	"context"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func lintTestCall(sessionID string) application.Call {
	return application.Call{
		RequestID: "lint-request",
		Actor: application.Actor{
			Kind: application.ActorAgent, Origin: application.OriginCLI,
			SessionID: sessionID, Agent: "codex",
		},
	}
}

func TestLintReportsMissingDocumentAndResolvesCurrentBinding(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Lint current", Priority: "P2", Status: store.TodoStatusInProgress,
		Creator: store.TodoCreatorMe, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "lint-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Lint(context.Background(), lintTestCall("lint-session"), LintInput{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if result.TodoID != "t1" || result.Summary.Issues != 1 || len(result.Issues) != 1 ||
		result.Issues[0].Code != "doc_missing" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLintReturnsCleanGeneratedDocument(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{
		ID: "t1", Title: "Clean projection", Description: "Keep metadata aligned.",
		Priority: "P2", Status: store.TodoStatusOpen, Project: "atm", Creator: store.TodoCreatorMe, Created: store.Today(),
	}
	seedWorkTodos(t, todo)
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Lint(context.Background(), lintTestCall(""), LintInput{TodoID: "#T01"})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if result.TodoID != "t1" || result.Summary.Issues != 0 || result.Issues == nil || len(result.Issues) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestLintReadsDurableSubmitAndReopenHistory(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{
		ID: "t1", Title: "Lifecycle history", Priority: "P2", Status: store.TodoStatusInProgress,
		Creator: store.TodoCreatorMe, Created: store.Today(),
	}
	seedWorkTodos(t, todo)
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	call := lifecycleCall(application.ActorHuman, "lint-history-submit-1")
	if _, err := Default.Submit(context.Background(), call, SubmitInput{
		TodoID: "t1", Reason: "first implementation verified",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Default.Start(context.Background(), lifecycleCall(application.ActorHuman, "lint-history-reopen"), StartInput{
		TodoID: "t1", ReopenReason: "review requested another boundary",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Default.Submit(context.Background(), lifecycleCall(application.ActorHuman, "lint-history-submit-2"), SubmitInput{
		TodoID: "t1", Reason: "review correction verified",
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Lint(context.Background(), lintTestCall(""), LintInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, issue := range result.Issues {
		codes[issue.Code] = true
	}
	if !codes["multiple_submit"] || !codes["post_submit_work"] {
		t.Fatalf("issues = %+v", result.Issues)
	}
}
