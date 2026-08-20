package work

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestShowNormalizesTodoIDAndResolvesCurrentBinding(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Read one Todo", Priority: "P1", Status: store.TodoStatusInProgress,
		Project: "atm", Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-read", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	call := bindingCall(application.ActorAgent, "session-read")
	for _, input := range []ShowInput{{TodoID: "#T01"}, {TodoID: "current"}, {}} {
		result, err := Default.Show(context.Background(), call, input)
		if err != nil {
			t.Fatalf("Show(%+v): %v", input, err)
		}
		if result.Todo.ID != "t1" || result.LatestPlan != nil || len(result.Bindings) != 1 {
			t.Fatalf("Show(%+v) = %+v", input, result)
		}
	}
	if _, err := Default.Show(context.Background(), bindingCall(application.ActorHuman, ""), ShowInput{}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("Show(current without session) error = %v, want invalid_argument", err)
	}
}

func TestListOwnsDocumentQueryFilteringAndValidation(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{ID: "t1", Title: "Ordinary title", Priority: "P1", Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()}
	seedWorkTodos(t, todo,
		store.Todo{ID: "t2", Title: "Other project", Priority: "P2", Status: store.TodoStatusOpen, Project: "wanda", Created: store.Today()},
	)
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendTodoLog(&todo, "only-in-the-task-document", "分析"); err != nil {
		t.Fatal(err)
	}
	result, err := Default.List(context.Background(), bindingCall(application.ActorHuman, ""), ListInput{
		Status: "all", Project: "ATM", Query: "only-in-the-task-document", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != ListKindWorking || len(result.Todos) != 1 || result.Todos[0].ID != "t1" || !result.DocumentExists["t1"] {
		t.Fatalf("List = %+v", result)
	}
	for _, input := range []ListInput{{Status: "mystery"}, {Creator: "robot"}, {Offset: -1}, {Limit: -1}} {
		if _, err := Default.List(context.Background(), bindingCall(application.ActorHuman, ""), input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("List(%+v) error = %v, want invalid_argument", input, err)
		}
	}
}

func TestDocMaterializesMissingCardAndClassifiesExistingInit(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Materialize the card", Description: "The current requirement.",
		Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})
	call := bindingCall(application.ActorHuman, "")
	result, err := Default.Doc(context.Background(), call, DocInput{TodoID: "01"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Exists || result.Created || !strings.Contains(result.Content, "The current requirement.") || !store.TodoDocExists("t1") {
		t.Fatalf("Doc = %+v", result)
	}
	if _, err := Default.Doc(context.Background(), call, DocInput{TodoID: "t1", Initialize: true}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("initialize existing doc error = %v, want conflict", err)
	}
}

func TestContextOwnsBindingAndDocumentReadModel(t *testing.T) {
	withTempWorkStore(t)
	cwd := t.TempDir()
	todo := store.Todo{ID: "t1", Title: "Context facts", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()}
	seedWorkTodos(t, todo)
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "session-context", TodoID: "t1", CWD: cwd}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InitTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	result, err := Default.Context(
		context.Background(), bindingCall(application.ActorAgent, "session-context"), ContextInput{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.WorkState.ID != "t1" || result.Trace.BindingCount != 1 || len(result.Trace.RecentBindings) != 1 ||
		result.TaskDocument.Path == "" || !result.TaskDocument.Exists || result.Verification.Status != VerificationNotRun ||
		result.Implementation.WorkspaceSource != "active_binding" || result.LatestPlan != nil {
		t.Fatalf("Context = %+v", result)
	}
}
