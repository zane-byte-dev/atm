package store

import (
	"database/sql"
	"errors"
	"fmt"
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

// Versions below minUpgradableVersion are hard-rejected. The step immediately
// below SchemaVersion may still be upgradable while migration rungs exist.
func TestMigrateRejectsUnsupportedSchemaVersions(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`UPDATE schema_version SET version = ?`, 8); err != nil {
		t.Fatal(err)
	}
	err := migrate(db)
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("migrate from v8 error = %v", err)
	}
}

func TestMigrateV21ToV22AddsUsageEventDuration(t *testing.T) {
	db := openTempDB(t)
	// Simulate a v21 database: drop the v22 column and pin the version.
	if _, err := db.Exec(`CREATE TABLE usage_events_v21 AS SELECT id, session_id, model, ts,
		input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, cost_usd, fingerprint,
		request_count FROM usage_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE usage_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE usage_events_v21 RENAME TO usage_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 21`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v21→v22: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, file_path) VALUES ('s','s','pi','/tmp/s')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO usage_events (session_id, duration_ms) VALUES ('s', 4200)`); err != nil {
		t.Fatalf("duration_ms column missing: %v", err)
	}
	// Rows migrated from v21 have no measurement, and 0 has to keep meaning
	// "unknown" rather than a zero-length response.
	var carried, added int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(duration_ms),0) FROM usage_events WHERE session_id!='s'`).Scan(&carried); err != nil {
		t.Fatal(err)
	}
	if carried != 0 {
		t.Fatalf("pre-existing rows got a duration: %d", carried)
	}
	if err := db.QueryRow(`SELECT duration_ms FROM usage_events WHERE session_id='s'`).Scan(&added); err != nil || added != 4200 {
		t.Fatalf("duration_ms = %d, err = %v", added, err)
	}
}

func TestMigrateV22ToV23AddsUsageEventTimestampIndex(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`DROP INDEX idx_usage_events_ts`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 22`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate v22→v23: %v", err)
	}

	var version, found int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_usage_events_ts'`,
	).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 1 {
		t.Fatal("idx_usage_events_ts was not created")
	}
}

func TestMigrateV24ToV25AddsCollectionDomain(t *testing.T) {
	db := openTempDB(t)
	for _, statement := range []string{
		`DROP TABLE collection_items`,
		`DROP TABLE collection_runs`,
		`DROP TABLE collection_checkpoints`,
		`DROP TABLE collection_sources`,
		`UPDATE schema_version SET version = 24`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("prepare v24: %v", err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v24→v25: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	source, err := UpsertCollectionSource(db, CollectionSource{Connector: "test",
		Kind: "group", ExternalID: "cid-migrated", ExcludePattern: "robot", Priority: "P2", Enabled: true})
	if err != nil || source.ExcludePattern != "robot" {
		t.Fatalf("migrated collection source = %+v, err=%v", source, err)
	}
}

func TestMigrateV26ToV27AddsSyncedChatArchive(t *testing.T) {
	db := openTempDB(t)
	for _, statement := range []string{
		`DROP TABLE collection_messages`,
		`UPDATE schema_version SET version = 26`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("prepare v26: %v", err)
		}
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v26→v27: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	// The archive starts empty: past runs kept only the excerpt each decision was
	// made from, and the connector is the authoritative place the rest of those chats still exists.
	stats, err := CollectionMessageStatsFor(db)
	if err != nil || stats.Total != 0 {
		t.Fatalf("migrated archive stats = %+v, err=%v", stats, err)
	}
	if _, err := PutCollectionMessages(db, []CollectionMessage{syncedMessage("m1", 1_000, "识衣", "发布完毕")}); err != nil {
		t.Fatalf("write into migrated archive: %v", err)
	}
}

func TestMigrateV25ToV26AddsCollectionSourceExclusion(t *testing.T) {
	db := openTempDB(t)
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-v25",
		Name: "v25 source", Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE collection_sources DROP COLUMN exclude_pattern`); err != nil {
		t.Fatalf("prepare v25 collection_sources: %v", err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 25`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate v25→v26: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	migrated, err := GetCollectionSource(db, source.ID)
	if err != nil || migrated.ExcludePattern != "" {
		t.Fatalf("migrated collection source = %+v, err=%v", migrated, err)
	}

	// A short-lived development v25 schema already contained the column. Its
	// migration must only bump the version and preserve configured exclusions.
	migrated.ExcludePattern = "robot"
	if _, err := UpsertCollectionSource(db, migrated); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 25`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v25 with existing column: %v", err)
	}
	migrated, err = GetCollectionSource(db, source.ID)
	if err != nil || migrated.ExcludePattern != "robot" {
		t.Fatalf("existing exclusion was not preserved: %+v, err=%v", migrated, err)
	}
}

func TestMigrateV28ToV29AddsCollectionSourceStrategy(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`ALTER TABLE collection_sources DROP COLUMN interval_minutes`); err != nil {
		t.Fatalf("drop interval_minutes: %v", err)
	}
	if _, err := db.Exec(`ALTER TABLE collection_sources DROP COLUMN strategy`); err != nil {
		t.Fatalf("drop strategy: %v", err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 28`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v28→v29: %v", err)
	}
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-observe",
		Strategy: CollectionStrategyObserve, IntervalMinutes: 60, Priority: "P2", Enabled: true,
	})
	if err != nil || source.Strategy != CollectionStrategyObserve || source.IntervalMinutes != 60 {
		t.Fatalf("migrated source strategy=%+v err=%v", source, err)
	}
}

// A request must not be counted twice when a forked Codex thread or a continued
// Claude session replays an earlier transcript into a new file.
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
		_, err := state.UnarchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	todos, err = LoadTodosReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if len(todos.Items) != 2 || FindTodo(todos, "t2") == nil {
		t.Fatalf("working set after unarchive = %#v", todos.Items)
	}
}

func TestArchiveRejectsActiveTodos(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Still open"))

	err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "finish or drop it first") {
		t.Fatalf("archiving an open todo = %v", err)
	}
}

func TestTrashAndRestorePreserveActiveTodoAndCloseBinding(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "Recoverable"))

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		if _, err := state.BindSession(TodoSessionBinding{
			SessionID: "session-1", TodoID: "t1", Agent: "codex",
		}); err != nil {
			return err
		}
		trashed, err := state.TrashTodos([]string{"t1"})
		if err != nil {
			return err
		}
		if len(trashed) != 1 || FindTodo(state.Todos, "t1") != nil {
			t.Fatalf("trashed = %#v, todo still live = %v", trashed, FindTodo(state.Todos, "t1") != nil)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	bindings, err := ListTodoSessionBindings("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 || bindings[0].UnboundAt == nil || bindings[0].Reason != "todo moved to trash" {
		t.Fatalf("binding after trash = %#v", bindings)
	}
	archived, err := LoadArchivedTodos()
	if err != nil || len(archived) != 1 || archived[0].Status != TodoStatusOpen {
		t.Fatalf("trash contents = %#v, err=%v", archived, err)
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
		_, err := state.TrashTodos([]string{"t1"})
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
		t.Fatalf("trash after permanent delete = %#v, err=%v", archived, err)
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

func TestArchivedDependencyStaysSatisfied(t *testing.T) {
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "Prerequisite", Priority: "P1", Status: TodoStatusDone, Created: Today()},
		Todo{ID: "t2", Title: "Waiting on it", Priority: "P1", Status: TodoStatusWaiting,
			Created: Today(), DependsOn: []string{"t1"}},
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

	// A dropped dependency still blocks, archived or not.
	withTempStore(t)
	seedTodos(t,
		Todo{ID: "t1", Title: "Abandoned", Priority: "P1", Status: TodoStatusDropped, Created: Today()},
		Todo{ID: "t2", Title: "Waiting on it", Priority: "P1", Status: TodoStatusWaiting,
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
		t.Fatalf("dropped dependency should still block: %#v", unmet)
	}
	issues := AuditTodoDependencies(todos)
	if len(issues) != 1 || issues[0].Code != "dependency_dropped" {
		t.Fatalf("audit issues = %#v", issues)
	}
}

func TestErrorForArchivedTodoPointsAtUnarchive(t *testing.T) {
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
		!strings.Contains(err.Error(), "atm todo unarchive t1") {
		t.Fatalf("archived todo error = %v", err)
	}
	if err := TodoNotFoundError(todos, "t404"); err.Error() != "todo not found: t404" {
		t.Fatalf("missing todo error = %v", err)
	}
}

// The v30 rebuild of collection_items is the first migration here that recreates
// a table rather than adding a column. The items table is the audit trail for why
// each Todo exists, so this asserts the rows, their todo links and the UNIQUE
// constraint all survive — and that the relaxed CHECK actually accepts 'insight'.
func TestMigrateV29ToV30RebuildsCollectionItemsWithoutLosingAudit(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO todos (id, position, title, priority, status, created)
		VALUES ('t1',1,'已有任务','P1','open','2026-08-01')`); err != nil {
		t.Fatal(err)
	}
	// Simulate v29: rebuild collection_items with the pre-insight CHECK, drop the
	// v30 additions, and pin the version.
	for _, statement := range []string{
		`CREATE TABLE collection_items_v29 (
			id TEXT PRIMARY KEY, source_id TEXT NOT NULL, connector TEXT NOT NULL,
			conversation_id TEXT NOT NULL DEFAULT '', fingerprint TEXT NOT NULL,
			message_ids TEXT NOT NULL DEFAULT '[]', sender TEXT NOT NULL DEFAULT '',
			occurred_at INTEGER NOT NULL DEFAULT 0, raw_context TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT 'pending'
				CHECK (action IN ('pending','create','append','ignore','failed','reverted')),
			proposed_action TEXT NOT NULL DEFAULT '' CHECK (proposed_action IN ('','create','append')),
			title TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
			item_type TEXT NOT NULL DEFAULT '', project TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0,
			todo_id TEXT REFERENCES todos(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processed','failed')),
			error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			UNIQUE (connector, fingerprint)
		)`,
		`DROP TABLE collection_items`,
		`ALTER TABLE collection_items_v29 RENAME TO collection_items`,
		`INSERT INTO collection_items (id,source_id,connector,fingerprint,action,title,todo_id,
			status,created_at,updated_at)
		 VALUES ('ci_kept','cs_1','example','fp-kept','create','已建任务','t1','processed',1,2),
		        ('ci_gone','cs_1','example','fp-gone','ignore','闲聊',NULL,'processed',3,4)`,
		`DROP TABLE collection_digests`,
		`ALTER TABLE collection_sources DROP COLUMN knowledge_collection`,
		`ALTER TABLE collection_runs DROP COLUMN insight_count`,
		`UPDATE schema_version SET version = 29`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("simulate v29 (%s): %v", statement, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO collection_items (id,source_id,connector,fingerprint,action,
		created_at,updated_at) VALUES ('ci_reject','cs_1','example','fp-reject','insight',5,6)`); err == nil {
		t.Fatal("v29 CHECK should not accept 'insight'")
	}

	if err := migrate(db); err != nil {
		t.Fatalf("migrate v29→v30: %v", err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	var kept, total int
	var todoID sql.NullString
	if err := db.QueryRow(`SELECT COUNT(*) FROM collection_items`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("rebuild lost audit rows: %d", total)
	}
	if err := db.QueryRow(`SELECT COUNT(*),todo_id FROM collection_items WHERE id='ci_kept'`).
		Scan(&kept, &todoID); err != nil {
		t.Fatal(err)
	}
	if kept != 1 || todoID.String != "t1" {
		t.Fatalf("rebuild dropped the Todo link: count=%d todo=%v", kept, todoID)
	}
	// The relaxed CHECK, the recreated UNIQUE constraint, the new columns and the
	// new table all have to be in place.
	if _, err := db.Exec(`INSERT INTO collection_items (id,source_id,connector,fingerprint,action,
		created_at,updated_at) VALUES ('ci_new','cs_1','example','fp-new','insight',5,6)`); err != nil {
		t.Fatalf("v30 CHECK rejects 'insight': %v", err)
	}
	if _, err := db.Exec(`INSERT INTO collection_items (id,source_id,connector,fingerprint,action,
		created_at,updated_at) VALUES ('ci_dupe','cs_1','example','fp-new','ignore',5,6)`); err == nil {
		t.Fatal("rebuild lost the UNIQUE(connector,fingerprint) constraint")
	}
	if _, err := db.Exec(`INSERT INTO collection_digests (source_id,digest_date,document_id,
		created_at,updated_at) VALUES ('cs_1','2026-08-03','document:1',1,2)`); err != nil {
		t.Fatalf("collection_digests missing: %v", err)
	}
	if _, err := db.Exec(`UPDATE collection_sources SET knowledge_collection='example'`); err != nil {
		t.Fatalf("knowledge_collection missing: %v", err)
	}
	if _, err := db.Exec(`UPDATE collection_runs SET insight_count=1`); err != nil {
		t.Fatalf("insight_count missing: %v", err)
	}
}

func TestMigrateV30ToV31RelaxesSourceKindsAndKeepsCheckpoints(t *testing.T) {
	db := openTempDB(t)
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-v30",
		Name: "existing", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectionCheckpoint(db, CollectionCheckpoint{
		SourceID: source.ID, CursorTime: 123, Cursor: "next",
	}); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE collection_sources_v30 (
			id TEXT PRIMARY KEY, connector TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('group','user','open_example_id')),
			external_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '', project TEXT NOT NULL DEFAULT '',
			exclude_pattern TEXT NOT NULL DEFAULT '', instruction TEXT NOT NULL DEFAULT '',
			knowledge_collection TEXT NOT NULL DEFAULT '',
			strategy TEXT NOT NULL DEFAULT 'tasks' CHECK (strategy IN ('tasks','observe')),
			interval_minutes INTEGER NOT NULL DEFAULT 5 CHECK (interval_minutes BETWEEN 1 AND 1440),
			priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0','P1','P2','P3')),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			UNIQUE (connector,kind,external_id)
		)`,
		// Columns are named, not starred: the live table grows over time (v32
		// added decision_unit) and a star-select would break this simulation
		// every time it does.
		`INSERT INTO collection_sources_v30
			(id,connector,kind,external_id,name,project,exclude_pattern,instruction,
			 knowledge_collection,strategy,interval_minutes,priority,enabled,created_at,updated_at)
		 SELECT id,connector,kind,external_id,name,project,exclude_pattern,instruction,
			 knowledge_collection,strategy,interval_minutes,priority,enabled,created_at,updated_at
		 FROM collection_sources`,
		`DROP TABLE collection_checkpoints`,
		`DROP TABLE collection_sources`,
		`ALTER TABLE collection_sources_v30 RENAME TO collection_sources`,
		`CREATE INDEX idx_collection_sources_connector ON collection_sources(connector,enabled,name)`,
		`CREATE TABLE collection_checkpoints (
			source_id TEXT PRIMARY KEY REFERENCES collection_sources(id) ON DELETE CASCADE,
			cursor_time INTEGER NOT NULL DEFAULT 0, cursor TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		fmt.Sprintf(`INSERT INTO collection_checkpoints (source_id,cursor_time,cursor,updated_at)
			VALUES ('%s',123,'next',124)`, source.ID),
		`UPDATE schema_version SET version=30`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("simulate v30 (%s): %v", statement, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO collection_sources
		(id,connector,kind,external_id,created_at,updated_at) VALUES ('reject','slack','channel','C1',1,1)`); err == nil {
		t.Fatal("v30 source CHECK should reject connector-defined kinds")
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate v30→v31: %v", err)
	}
	checkpoint, err := GetCollectionCheckpoint(db, source.ID)
	if err != nil || checkpoint.CursorTime != 123 || checkpoint.Cursor != "next" {
		t.Fatalf("checkpoint after migration=%+v err=%v", checkpoint, err)
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "slack", Kind: "channel", ExternalID: "C1", Priority: "P2",
	}); err != nil {
		t.Fatalf("v31 rejected connector-defined kind: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM collection_sources WHERE id=?`, source.ID); err != nil {
		t.Fatal(err)
	}
	deletedCheckpoint, err := GetCollectionCheckpoint(db, source.ID)
	if err != nil || deletedCheckpoint.CursorTime != 0 || deletedCheckpoint.Cursor != "" {
		t.Fatalf("checkpoint cascade was not restored: checkpoint=%+v err=%v", deletedCheckpoint, err)
	}
}
