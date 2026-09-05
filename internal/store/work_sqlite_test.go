package store

import (
	"errors"
	"strings"
	"testing"
)

func TestUpdateWorkStateRollsBackTodoAndBindingTogether(t *testing.T) {
	withTempStore(t)
	seedTodos(t, Todo{
		ID: "t1", Title: "Atomic transition", Priority: "P1",
		Status: TodoStatusInProgress, Created: Today(),
	})
	if _, err := BindTodoSession(TodoSessionBinding{SessionID: "s1", TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("injected transition failure")
	err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").Status = TodoStatusReview
		if _, err := state.UnbindTodoSessions("t1", "review"); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("UpdateWorkState error = %v", err)
	}
	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if todo := FindTodo(todos, "t1"); todo == nil || todo.Status != TodoStatusInProgress {
		t.Fatalf("todo after rollback = %#v", todo)
	}
	if binding, err := CurrentTodoBinding("s1"); err != nil || binding == nil {
		t.Fatalf("binding after rollback = %#v, err=%v", binding, err)
	}
}

func TestSchemaRejectsUnknownStatusAndPriority(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Valid"))

	for field, mutate := range map[string]func(*Todo){
		"status":   func(todo *Todo) { todo.Status = "snoozing" },
		"priority": func(todo *Todo) { todo.Priority = "P9" },
	} {
		err := UpdateWorkState(func(state *WorkStateTx) error {
			mutate(FindTodo(state.Todos, "t1"))
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "constraint failed") {
			t.Fatalf("invalid %s error = %v", field, err)
		}
	}
}

func TestSchemaRejectsRemovedLifecycleStatuses(t *testing.T) {
	for _, status := range []string{"waiting", "blocked", "dropped"} {
		t.Run(status, func(t *testing.T) {
			withTempStore(t)
			seedTodos(t, openTodo("t1", "Valid"))
			err := UpdateWorkState(func(state *WorkStateTx) error {
				FindTodo(state.Todos, "t1").Status = status
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), "constraint failed") {
				t.Fatalf("removed status %q error = %v", status, err)
			}
		})
	}
}

func TestSchemaRejectsMalformedDates(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Valid"))

	err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").ReviewAt = "next tuesday"
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "constraint failed") {
		t.Fatalf("malformed review_at error = %v", err)
	}
}

func TestWriteTodosRejectsDuplicateAndEmptyIDs(t *testing.T) {
	withTempStore(t)

	err := UpdateWorkState(func(state *WorkStateTx) error {
		state.Todos.Items = []Todo{openTodo("t1", "One"), openTodo("t1", "Also one")}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate todo ID") {
		t.Fatalf("duplicate ID error = %v", err)
	}

	err = UpdateWorkState(func(state *WorkStateTx) error {
		state.Todos.Items = []Todo{openTodo("", "Nameless")}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "no ID") {
		t.Fatalf("empty ID error = %v", err)
	}
}

func TestEnsureSchemaRejectsUnsupportedVersions(t *testing.T) {
	for _, test := range []struct {
		name, want string
		version    int
	}{
		{name: "previous", version: SchemaVersion - 1, want: "no longer supported"},
		{name: "newer", version: SchemaVersion + 1, want: "newer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := openTempDB(t)
			if _, err := db.Exec(`UPDATE schema_version SET version = ?`, test.version); err != nil {
				t.Fatal(err)
			}
			err := ensureSchema(db)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("schema v%d error = %v, want %q", test.version, err, test.want)
			}
		})
	}
}

func TestFreshSchemaIncludesWorkEffectOutbox(t *testing.T) {
	db := openTempDB(t)
	var version, tables, columns int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='work_effect_outbox'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('work_effect_outbox')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || tables != 1 || columns != 10 {
		t.Fatalf("fresh outbox: version=%d tables=%d columns=%d", version, tables, columns)
	}
}

func TestFreshSchemaIncludesTodoPlanRevisions(t *testing.T) {
	db := openTempDB(t)
	var version, tables, columns int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='todo_plan_revisions'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('todo_plan_revisions')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || tables != 1 || columns != 12 {
		t.Fatalf("plan schema: version=%d tables=%d columns=%d", version, tables, columns)
	}
}

func TestWorkEffectOutboxTracksFailureAndAcknowledgement(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Durable effect"))
	effect := WorkEffectRecord{
		ID: "we_test", RequestID: "request-1", TodoID: "t1", Kind: "todo_waiting",
		PayloadJSON: `{"todo":{"id":"t1"}}`, CreatedAt: 1,
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		return state.EnqueueWorkEffect(effect)
	}); err != nil {
		t.Fatalf("enqueue effect: %v", err)
	}
	if err := FailWorkEffect(effect.ID, "injected projection failure"); err != nil {
		t.Fatalf("FailWorkEffect: %v", err)
	}
	pending, err := ListPendingWorkEffects("t1")
	if err != nil || len(pending) != 1 || pending[0].AttemptCount != 1 ||
		pending[0].LastAttemptAt == nil || pending[0].LastError != "injected projection failure" {
		t.Fatalf("pending after failure = %+v, err=%v", pending, err)
	}
	if err := CompleteWorkEffect(effect.ID); err != nil {
		t.Fatalf("CompleteWorkEffect: %v", err)
	}
	if err := CompleteWorkEffect(effect.ID); err != nil {
		t.Fatalf("idempotent CompleteWorkEffect: %v", err)
	}
	if pending, err = ListPendingWorkEffects("t1"); err != nil || len(pending) != 0 {
		t.Fatalf("pending after acknowledgement = %+v, err=%v", pending, err)
	}
	if err := CompleteWorkEffect("we_missing"); !errors.Is(err, ErrWorkEffectNotFound) {
		t.Fatalf("unknown CompleteWorkEffect error = %v", err)
	}
}

func TestUsageEventFingerprintIsUnique(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, file_path) VALUES ('s2','s2','codex','/tmp/s2.jsonl')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (session_id, fingerprint) VALUES ('s2','codex:a')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (session_id, fingerprint) VALUES ('s2','codex:a')`); err == nil {
		t.Fatal("duplicate fingerprint was accepted")
	}
	// The index is partial: rows that carry no fingerprint are not deduped.
	for range 2 {
		if _, err := db.Exec(`INSERT INTO usage_events (session_id, input_tokens) VALUES ('s2', 10)`); err != nil {
			t.Fatalf("fingerprint-less event rejected: %v", err)
		}
	}
}

func TestArchiveRemovesFromWorkingSetButKeepsTheRow(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Live"), Todo{
		ID: "t2", Title: "Finished", Priority: "P1", Status: TodoStatusDone, Created: Today(),
	})

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		archived, err := state.ArchiveTodos([]string{"t2"})
		if err != nil {
			return err
		}
		if len(archived) != 1 || FindTodo(state.Todos, "t2") != nil {
			t.Fatalf("archived = %#v, snapshot still holds t2 = %v", archived, FindTodo(state.Todos, "t2") != nil)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 1 || todos.Items[0].ID != "t1" {
		t.Fatalf("working set = %#v", todos.Items)
	}
	archived, err := LoadArchivedTodos()
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 1 || archived[0].ID != "t2" || archived[0].ArchivedAt == 0 {
		t.Fatalf("archived todos = %#v", archived)
	}

	// The row is still there, so its ID stays taken.
	if next := NextTodoID(todos); next != "t3" {
		t.Fatalf("next ID = %s, want t3 (t2 is archived, not free)", next)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.RestoreTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	todos, err = LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 2 || FindTodo(todos, "t2") == nil {
		t.Fatalf("working set after restore = %#v", todos.Items)
	}
}

func TestArchiveAcceptsActiveTodos(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Still open"))

	err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	})
	if err != nil {
		t.Fatalf("archiving an open todo = %v", err)
	}
}

func TestArchiveAndRestorePreserveActiveTodoAndCloseBinding(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Recoverable"))

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		if _, err := state.BindSession(TodoSessionBinding{
			SessionID: "session-1", TodoID: "t1", Agent: "codex",
		}); err != nil {
			return err
		}
		archived, err := state.ArchiveTodos([]string{"t1"})
		if err != nil {
			return err
		}
		if len(archived) != 1 || FindTodo(state.Todos, "t1") != nil {
			t.Fatalf("archived = %#v, todo still live = %v", archived, FindTodo(state.Todos, "t1") != nil)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	bindings, err := ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].UnboundAt == nil || bindings[0].Reason != "todo archived" {
		t.Fatalf("binding after archive = %#v", bindings)
	}
	archived, err := LoadArchivedTodos()
	if err != nil || len(archived) != 1 || archived[0].Status != TodoStatusOpen {
		t.Fatalf("archive contents = %#v, err=%v", archived, err)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.RestoreTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	todos, err := LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || todos.Items[0].Status != TodoStatusOpen {
		t.Fatalf("restored todos = %#v, err=%v", todos, err)
	}
}

func TestPermanentlyDeleteArchivedTodo(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Disposable"))
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.PermanentlyDeleteTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	archived, err := LoadArchivedTodos()
	if err != nil || len(archived) != 0 {
		t.Fatalf("archive after permanent delete = %#v, err=%v", archived, err)
	}
	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if next := NextTodoID(todos); next != "t1" {
		t.Fatalf("next todo after permanent delete = %s, want t1", next)
	}
}

// Archived todos are absent from the snapshot, so an edit of the live set must
// not be mistaken for their deletion.
func TestEditingLiveTodosDoesNotDeleteArchivedOnes(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Live"), Todo{
		ID: "t2", Title: "Finished", Priority: "P1", Status: TodoStatusDone, Created: Today(),
	})
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").Title = "Edited"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	archived, err := LoadArchivedTodos()
	if err != nil || len(archived) != 1 {
		t.Fatalf("archived after unrelated edit = %#v, err=%v", archived, err)
	}
}

// A dependency may name an archived todo; the foreign key only rejects a target
// that does not exist at all.
func TestDependencyMayPointAtArchivedTodoButNotAtNothing(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Depends"), Todo{
		ID: "t2", Title: "Finished", Priority: "P1", Status: TodoStatusDone, Created: Today(),
	})
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").DependsOn = []string{"t2"}
		return nil
	}); err != nil {
		t.Fatalf("dependency on an archived todo: %v", err)
	}

	err := UpdateWorkState(func(state *WorkStateTx) error {
		FindTodo(state.Todos, "t1").DependsOn = []string{"t404"}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("dependency on a missing todo = %v", err)
	}
}

func TestNewTodoAndParentDependencyCanBeWrittenTogether(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Parent"))
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		state.Todos.Items = append(state.Todos.Items, openTodo("t2", "Child"))
		FindTodo(state.Todos, "t1").DependsOn = []string{"t2"}
		return nil
	}); err != nil {
		t.Fatalf("parent + new child in one write: %v", err)
	}
	loaded, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	parent := FindTodo(loaded, "t1")
	if parent == nil || len(parent.DependsOn) != 1 || parent.DependsOn[0] != "t2" {
		t.Fatalf("parent = %+v", parent)
	}
}

func TestArchivedDependencyStaysSatisfied(t *testing.T) {
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: TodoStatusDone, Created: Today()},
		Todo{ID: "t2", Title: "Waiting on it", Priority: "P1", Status: TodoStatusInProgress,
			Created: Today(), WakeCondition: "waiting for todos: t1", DependsOn: []string{"t1"}},
	)
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	waiting := FindTodo(todos, "t2")
	if unmet := UnmetTodoDependencies(todos, *waiting); len(unmet) != 0 {
		t.Fatalf("unmet dependencies after archiving a done one = %#v", unmet)
	}
	for _, issue := range AuditTodoDependencies(todos) {
		t.Fatalf("audit reported %s for an archived dependency: %#v", issue.Code, issue)
	}
	if events := ReconcileTodoDependencies(todos); len(events) != 1 || events[0].TodoID != "t2" {
		t.Fatalf("wake events = %#v", events)
	}

	// An archived open dependency still blocks.
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "Archived backlog", Priority: "P1", Status: TodoStatusOpen, Created: Today()},
		Todo{ID: "t2", Title: "Waiting on it", Priority: "P1", Status: TodoStatusInProgress,
			Created: Today(), DependsOn: []string{"t1"}},
	)
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	todos, err = LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if unmet := UnmetTodoDependencies(todos, *FindTodo(todos, "t2")); len(unmet) != 1 {
		t.Fatalf("archived open dependency should still block: %#v", unmet)
	}
}

func TestErrorForArchivedTodoPointsAtRestore(t *testing.T) {
	withTempStore(t)
	seedTodos(t, Todo{ID: "t1", Title: "Finished", Priority: "P1", Status: TodoStatusDone, Created: Today()})
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	todos, err := LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if err := TodoNotFoundError(todos, "t1"); !strings.Contains(err.Error(), "archived (done)") ||
		!strings.Contains(err.Error(), "atm todo restore t1") {
		t.Fatalf("archived todo error = %v", err)
	}
	if err := TodoNotFoundError(todos, "t404"); err.Error() != "todo not found: t404" {
		t.Fatalf("missing todo error = %v", err)
	}
}
