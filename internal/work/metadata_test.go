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

func metadataTestCall(kind application.ActorKind, agent string) application.Call {
	return application.Call{
		RequestID: "metadata-request",
		Actor: application.Actor{
			Kind:   kind,
			Origin: application.OriginCLI,
			Agent:  agent,
		},
	}
}

func TestAddOwnsNormalizationDocumentAndEffects(t *testing.T) {
	withTempWorkStore(t)
	result, err := Default.Add(context.Background(), metadataTestCall(application.ActorAgent, "openai-codex"), AddInput{
		Title:       "  Create typed metadata  ",
		Description: "Keep the Work service authoritative.\n",
		Priority:    "p0",
		Project:     "  atm  ",
		Source:      " test-suite ",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// A new Todo is always open: reaching review is a transition, not something
	// creation can jump straight to.
	if result.Todo.ID != "t1" || result.Todo.Title != "Create typed metadata" ||
		result.Todo.Priority != "P0" || result.Todo.Status != store.TodoStatusOpen ||
		result.Todo.Project != "atm" || result.Todo.Source != "test-suite" ||
		result.Todo.Creator != "codex" || result.Todo.WakeCondition != "" || result.Todo.ReviewAt != "" {
		t.Fatalf("todo = %+v", result.Todo)
	}
	if len(result.Effects) != 1 || result.Effects[0].Kind != MetadataEffectCreated {
		t.Fatalf("effects = %+v", result.Effects)
	}
	if !store.TodoDocExists("t1") {
		t.Fatal("Add did not materialize the Todo document")
	}
	document, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(document, "Keep the Work service authoritative.") {
		t.Fatalf("document = %q, err=%v", document, err)
	}

	// Results are values, not live pointers into the mutable transaction state.
	result.Todo.Title = "caller mutation"
	persisted, err := store.LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := store.FindTodo(persisted, "t1"); todo == nil || todo.Title != "Create typed metadata" {
		t.Fatalf("persisted todo changed through result: %+v", todo)
	}
}

func TestAddDefaultsOrdinaryWorkToP2AndPreservesExplicitP1(t *testing.T) {
	withTempWorkStore(t)
	call := metadataTestCall(application.ActorHuman, "")
	ordinary, err := Default.Add(context.Background(), call, AddInput{Title: "Ordinary work"})
	if err != nil || ordinary.Todo.Priority != "P2" {
		t.Fatalf("ordinary = %+v, err=%v", ordinary.Todo, err)
	}
	explicit, err := Default.Add(context.Background(), call, AddInput{Title: "Urgent work", Priority: "P1"})
	if err != nil || explicit.Todo.Priority != "P1" {
		t.Fatalf("explicit = %+v, err=%v", explicit.Todo, err)
	}
	batch, err := Default.BatchAdd(context.Background(), call, BatchAddInput{
		Items: []BatchAddItem{{Title: "Ordinary batch work"}},
	})
	if err != nil || len(batch.Todos) != 1 || batch.Todos[0].Priority != "P2" {
		t.Fatalf("batch = %+v, err=%v", batch.Todos, err)
	}
}

func TestAddRejectsInvalidMetadataBeforeMutation(t *testing.T) {
	tests := []struct {
		name  string
		input AddInput
	}{
		{name: "empty title", input: AddInput{Title: "  "}},
		{name: "priority", input: AddInput{Title: "Bad priority", Priority: "P9"}},
		{name: "creator", input: AddInput{Title: "Bad creator", Creator: "unknown-worker"}},
		{name: "description", input: AddInput{Title: "Bad description", Description: "## 分析\nreserved"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t)
			_, err := Default.Add(context.Background(), metadataTestCall(application.ActorHuman, ""), test.input)
			if !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("Add error = %v, want invalid argument", err)
			}
			todos, loadErr := store.LoadTodosReadOnly()
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if len(todos.Items) != 0 {
				t.Fatalf("invalid Add persisted todos: %+v", todos.Items)
			}
		})
	}
}

func TestBatchAddIsAtomicAndAllocatesIDsInOneTransaction(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t)
	call := metadataTestCall(application.ActorHuman, "")
	_, err := Default.BatchAdd(context.Background(), call, BatchAddInput{
		Defaults: BatchAddDefaults{Project: "atm", Priority: "P1"},
		Items: []BatchAddItem{
			{Title: "Would otherwise persist"},
			{Title: "Invalid priority item", Priority: "P9"},
		},
	})
	if !errors.Is(err, application.ErrInvalidArgument) || !strings.Contains(err.Error(), "Invalid priority item") {
		t.Fatalf("BatchAdd error = %v", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(todos.Items) != 0 {
		t.Fatalf("rejected batch partially persisted: %+v", todos.Items)
	}

	result, err := Default.BatchAdd(context.Background(), call, BatchAddInput{
		Defaults: BatchAddDefaults{Project: "atm", Priority: "p1", Creator: "me"},
		Items: []BatchAddItem{
			{Title: "Ready for review"},
			{Title: "Wait for release", Creator: "claude"},
		},
	})
	if err != nil {
		t.Fatalf("valid BatchAdd: %v", err)
	}
	if len(result.Todos) != 2 || result.Todos[0].ID != "t1" || result.Todos[1].ID != "t2" ||
		result.Todos[0].Creator != "me" || result.Todos[1].Creator != "claude" {
		t.Fatalf("todos = %+v", result.Todos)
	}
	// One creation effect per item and nothing else: batch creation cannot put a
	// Todo straight into review any more than single creation can.
	if len(result.Effects) != 2 || result.Effects[0].Kind != MetadataEffectCreated ||
		result.Effects[1].Kind != MetadataEffectCreated {
		t.Fatalf("effects = %+v", result.Effects)
	}
	for _, id := range []string{"t1", "t2"} {
		if !store.TodoDocExists(id) {
			t.Fatalf("batch did not materialize %s document", id)
		}
	}
}

// `todo maintain` was merged into `edit --maintenance-limit`, so its one rule
// has to survive the merge: maintenance is a scope tag on work still being done.
// Clearing the tag off finished work is a different claim and stays allowed.
func TestEditRefusesAMaintenanceLimitOnClosedWorkButStillClearsIt(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Bounded upkeep", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()},
		store.Todo{
			ID: "t2", Title: "Closed upkeep", Priority: "P1", Status: store.TodoStatusDone,
			Tags: []string{store.TodoTagMaintenance}, MaintenanceLimit: 2, Created: store.Today(),
		},
	)
	call := metadataTestCall(application.ActorHuman, "")

	active, err := Default.Edit(context.Background(), call, EditInput{
		TodoID: "t1", Patch: EditPatch{MaintenanceLimit: intPointerForTest(3)},
	})
	if err != nil || active.Todo.MaintenanceLimit != 3 ||
		!store.TodoHasTag(active.Todo, store.TodoTagMaintenance) {
		t.Fatalf("active todo = %+v, err=%v", active.Todo, err)
	}

	if _, err := Default.Edit(context.Background(), call, EditInput{
		TodoID: "t2", Patch: EditPatch{MaintenanceLimit: intPointerForTest(4)},
	}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("closed todo error = %v, want conflict", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t2"); todo == nil || todo.MaintenanceLimit != 2 {
		t.Fatalf("rejected edit changed the closed todo: %+v", todo)
	}

	cleared, err := Default.Edit(context.Background(), call, EditInput{
		TodoID: "t2", Patch: EditPatch{MaintenanceLimit: intPointerForTest(0)},
	})
	if err != nil || cleared.Todo.MaintenanceLimit != 0 ||
		store.TodoHasTag(cleared.Todo, store.TodoTagMaintenance) {
		t.Fatalf("cleared todo = %+v, err=%v", cleared.Todo, err)
	}
}

func TestEditReturnsToOpenAndCommitsBindingPolicyThenSyncsDocument(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{
		ID: "t1", Title: "Old title", Description: "Old requirement", Priority: "P1",
		Status: store.TodoStatusInProgress, Project: "old", Created: store.Today(),
	}
	seedWorkTodos(t, todo)
	if _, err := store.EnsureTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "edit-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Edit(context.Background(), metadataTestCall(application.ActorAgent, "codex"), EditInput{
		TodoID: "#T01",
		Patch: EditPatch{
			Title:       stringPointerForTest("New title"),
			Description: stringPointerForTest("New requirement"),
			Priority:    stringPointerForTest("p0"),
			Project:     stringPointerForTest(" atm "),
			Status:      stringPointerForTest("open"),
		},
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if result.PreviousStatus != store.TodoStatusInProgress || result.Todo.Title != "New title" ||
		result.Todo.Priority != "P0" || result.Todo.Project != "atm" ||
		result.Todo.Status != store.TodoStatusOpen || result.Todo.WakeCondition != "" || result.Todo.ReviewAt != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Effects) != 0 {
		t.Fatalf("effects = %+v", result.Effects)
	}
	if binding, err := store.CurrentTodoBinding("edit-session"); err != nil || binding != nil {
		t.Fatalf("binding after Edit = %+v, err=%v", binding, err)
	}
	history, err := store.ListTodoSessionBindings("t1")
	if err != nil || len(history) != 1 || history[0].Reason != "status-style:open" {
		t.Fatalf("binding history = %+v, err=%v", history, err)
	}
	document, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(document, "# New title") || !strings.Contains(document, "New requirement") {
		t.Fatalf("document = %q, err=%v", document, err)
	}
}

func TestEditRollsBackTodoWhenBindingUnbindFails(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Rollback edit", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	if _, err := store.BindTodoSession(store.TodoSessionBinding{SessionID: "edit-session", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_metadata_edit_unbind
		BEFORE UPDATE OF unbound_at ON todo_session_bindings
		WHEN NEW.reason = 'status-style:open'
		BEGIN SELECT RAISE(ABORT, 'injected metadata unbind failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	_, err = Default.Edit(context.Background(), metadataTestCall(application.ActorHuman, ""), EditInput{
		TodoID: "t1", Patch: EditPatch{Status: stringPointerForTest(store.TodoStatusOpen)},
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Edit error = %v, want unavailable", err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if todo := store.FindTodo(todos, "t1"); todo == nil || todo.Status != store.TodoStatusInProgress {
		t.Fatalf("todo after rollback = %+v", todo)
	}
	if binding, bindErr := store.CurrentTodoBinding("edit-session"); bindErr != nil || binding == nil {
		t.Fatalf("binding after rollback = %+v, err=%v", binding, bindErr)
	}
}

func TestMoveSyncsOnlyAnExistingDocument(t *testing.T) {
	withTempWorkStore(t)
	todo := store.Todo{ID: "t1", Title: "Move me", Priority: "P1", Status: store.TodoStatusOpen, Project: "old", Created: store.Today()}
	seedWorkTodos(t, todo, store.Todo{ID: "t2", Title: "No card", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today()})
	if _, err := store.EnsureTodoDoc(&todo); err != nil {
		t.Fatal(err)
	}

	result, err := Default.Move(context.Background(), metadataTestCall(application.ActorHuman, ""), MoveInput{TodoID: "1", Project: " atm "})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if result.PreviousProject != "old" || result.Todo.Project != "atm" {
		t.Fatalf("result = %+v", result)
	}
	document, err := store.ReadTodoDoc("t1")
	if err != nil || !strings.Contains(document, "- **项目**: atm") {
		t.Fatalf("document = %q, err=%v", document, err)
	}
	if _, err := Default.Move(context.Background(), metadataTestCall(application.ActorHuman, ""), MoveInput{TodoID: "t2", Project: "atm"}); err != nil {
		t.Fatal(err)
	}
	if store.TodoDocExists("t2") {
		t.Fatal("Move materialized a document that did not previously exist")
	}
}

func TestAddCleansImportedImagesWhenDatabaseCommitFails(t *testing.T) {
	withTempWorkStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER fail_metadata_add
		BEFORE INSERT ON todos
		BEGIN SELECT RAISE(ABORT, 'injected add failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "evidence.png")
	if err := os.WriteFile(source, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}

	_, err = Default.Add(context.Background(), metadataTestCall(application.ActorHuman, ""), AddInput{
		Title: "Rollback image import", ImagePaths: []string{source},
	})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Add error = %v, want unavailable", err)
	}
	if _, statErr := os.Stat(store.TodoAssetsDir("t1")); !os.IsNotExist(statErr) {
		t.Fatalf("managed image directory survived rollback: %v", statErr)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(todos.Items) != 0 {
		t.Fatalf("failed Add persisted todos: %+v", todos.Items)
	}
}

func stringPointerForTest(value string) *string {
	return &value
}

func intPointerForTest(value int) *int {
	return &value
}
