package work

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestReadDocumentDoesNotAssembleTaskDetail(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{ID: "t1", Title: "Document only", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()})
	if err := store.UpdateWorkState(func(state *store.WorkStateTx) error {
		return state.AppendTodoPlanRevision(store.TodoPlanRevision{TodoID: "t1", Revision: 1,
			SnapshotJSON: "invalid JSON", SnapshotHash: "invalid", RequestID: "test",
			ActorKind: "human", Origin: "cli", CreatedAt: 1})
	}); err != nil {
		t.Fatal(err)
	}
	path := store.TodoDocPath("t1")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# independently readable document\n"), 0644); err != nil {
		t.Fatal(err)
	}
	call := bindingCall(application.ActorHuman, "")
	if _, err := Default.Show(context.Background(), call, ShowInput{TodoID: "t1"}); !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("malformed detail fixture must fail Show: %v", err)
	}
	result, err := Default.ReadDocument(context.Background(), call, ShowInput{TodoID: "#T01"})
	if err != nil || !result.Exists || result.Content != "# independently readable document\n" || result.TodoID != "t1" {
		t.Fatalf("ReadDocument = %+v, err=%v", result, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	missing, err := Default.ReadDocument(context.Background(), call, ShowInput{TodoID: "t1"})
	if err != nil || missing.Exists || missing.TodoID != "t1" {
		t.Fatalf("missing document = %+v, err=%v", missing, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read materialized the missing document: %v", err)
	}
}

func TestTaskDetailPreservesArchivedAndCurrentReadContracts(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Live", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Archived", Description: "closed task", Priority: "P1", Status: store.TodoStatusDone,
			Created: store.Today(), Tags: []string{"stored tag"}, Links: []store.TodoLink{{URL: "https://example.test/archive"}},
			WakeCondition: "stored wake", MaintenanceLimit: 2},
	)
	if err := store.UpdateWorkState(func(state *store.WorkStateTx) error {
		if _, err := state.BindSession(store.TodoSessionBinding{SessionID: "legacy", TodoID: "t2"}); err != nil {
			return err
		}
		_, err := state.ArchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Legacy bindings can still point at archived tasks; current lookup must
	// keep its live-task contract even on archive-aware surfaces.
	if _, err := db.Exec(`UPDATE todo_session_bindings SET unbound_at=NULL WHERE session_id='legacy'`); err != nil {
		t.Fatal(err)
	}
	call := bindingCall(application.ActorHuman, "legacy")
	archived, err := Default.ShowIncludingArchived(context.Background(), call, ShowInput{TodoID: "#T02"})
	if err != nil || !archived.Archived || archived.Todo.ID != "t2" {
		t.Fatalf("archived detail = %+v, err=%v", archived, err)
	}
	legacy, err := store.LoadArchivedTodos()
	if err != nil || len(legacy) != 1 || !reflect.DeepEqual(archived.Todo, legacy[0].Todo) {
		t.Fatalf("archive projection changed: detail=%+v legacy=%+v err=%v", archived.Todo, legacy, err)
	}
	for _, id := range []string{"t2", "#T02"} {
		if _, err := Default.Show(context.Background(), call, ShowInput{TodoID: id}); !errors.Is(err, application.ErrNotFound) || !strings.Contains(err.Error(), "todo t2 is archived (done)") {
			t.Fatalf("Show(%q) archived error = %v", id, err)
		}
	}
	for _, id := range []string{"", "current", "CURRENT"} {
		if _, err := Default.ShowIncludingArchived(context.Background(), call, ShowInput{TodoID: id}); !errors.Is(err, application.ErrNotFound) || !strings.Contains(err.Error(), "todo t2 is archived") {
			t.Fatalf("archive-aware current Show(%q) error = %v", id, err)
		}
		if _, err := Default.ReadDocument(context.Background(), call, ShowInput{TodoID: id}); !errors.Is(err, application.ErrNotFound) {
			t.Fatalf("archive-aware current ReadDocument(%q) error = %v", id, err)
		}
	}
	if _, err := Default.ReadDocument(context.Background(), call, ShowInput{TodoID: "#T02"}); err != nil {
		t.Fatalf("explicit archived ReadDocument: %v", err)
	}
	_, err = Default.ShowIncludingArchived(context.Background(), call, ShowInput{TodoID: "#T404"})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeNotFound || appErr.Details["todo_id"] != "t404" || !strings.Contains(err.Error(), "todo not found: #T404") {
		t.Fatalf("missing explicit task error = %#v", err)
	}
	_, err = Default.Show(context.Background(), bindingCall(application.ActorAgent, "unbound"), ShowInput{})
	if !errors.As(err, &appErr) || appErr.Code != application.CodeNotFound || appErr.Details["session_id"] != "unbound" {
		t.Fatalf("missing binding error = %#v", err)
	}
}

func TestRankTodoMatchesScoresEachCandidateOnceAndKeepsStableTies(t *testing.T) {
	todos := []Todo{{ID: "t1"}, {ID: "t2"}, {ID: "t3"}, {ID: "t4"}}
	scores := map[string]int{"t1": 10, "t2": 30, "t3": 30, "t4": -1}
	calls := map[string]int{}
	result := rankTodoMatches(todos, "query", func(todo Todo, query string) int {
		calls[todo.ID]++
		return scores[todo.ID]
	})
	var ids []string
	for _, todo := range result {
		ids = append(ids, todo.ID)
	}
	if !reflect.DeepEqual(ids, []string{"t2", "t3", "t1"}) {
		t.Fatalf("ranked tasks = %v", ids)
	}
	for _, todo := range todos {
		if calls[todo.ID] != 1 {
			t.Fatalf("score reads for %s = %d, want 1", todo.ID, calls[todo.ID])
		}
	}
	rankTodoMatches(todos, " ", func(Todo, string) int {
		t.Fatal("empty queries must not read documents")
		return 0
	})
}

func TestListQueryRankingPreservesFieldWeightsAndPagination(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "needle", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t2", Title: "description match", Description: "needle", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{ID: "t3", Title: "document match", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
	)
	if err := os.MkdirAll(store.TodoDocDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.TodoDocPath("t3"), []byte("needle in Markdown only"), 0644); err != nil {
		t.Fatal(err)
	}
	call := bindingCall(application.ActorHuman, "")
	result, err := Default.List(context.Background(), call, ListInput{Status: "all", Query: "needle", Offset: 1, Limit: 2})
	if err != nil || len(result.Todos) != 2 || result.Todos[0].ID != "t2" || result.Todos[1].ID != "t3" {
		t.Fatalf("weighted query page = %+v, err=%v", result, err)
	}
	result, err = Default.List(context.Background(), call, ListInput{Status: "all", Query: "needle Markdown"})
	if err != nil || len(result.Todos) != 1 || result.Todos[0].ID != "t3" {
		t.Fatalf("AND query = %+v, err=%v", result, err)
	}
}
