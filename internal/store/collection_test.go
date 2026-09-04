package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestCollectionStoreKeepsSourcesCheckpointsAndAuditIdempotent(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-demo",
		Name: "产品群", Project: "atm", ExcludePattern: "机器人通知,日报", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	if source.ID == "" || !source.Enabled || source.Project != "atm" || source.ExcludePattern != "机器人通知,日报" {
		t.Fatalf("unexpected source: %+v", source)
	}
	if source.Strategy != CollectionStrategyTasks || source.IntervalMinutes != 5 {
		t.Fatalf("unexpected default source strategy: %+v", source)
	}
	updated, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-demo",
		Name: "新产品群", Priority: "P2", Enabled: false,
	})
	if err != nil {
		t.Fatalf("update source: %v", err)
	}
	if updated.ID != source.ID || updated.Enabled || updated.Name != "新产品群" {
		t.Fatalf("source update was not stable: %+v", updated)
	}

	if err := SaveCollectionCheckpoint(db, CollectionCheckpoint{SourceID: source.ID, CursorTime: 1234, Cursor: "next"}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	checkpoint, err := GetCollectionCheckpoint(db, source.ID)
	if err != nil || checkpoint.CursorTime != 1234 || checkpoint.Cursor != "next" {
		t.Fatalf("checkpoint = %+v, err=%v", checkpoint, err)
	}

	item := CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-demo", Fingerprint: "same-message-set", MessageIDs: []string{"m1", "m2"},
		Sender: "测试发送人", OccurredAt: 1234, RawContext: "想做自动收集", Action: "create",
		Title: "实现自动收集", Priority: "P1", Status: "processed"}
	first, inserted, err := PutCollectionItem(db, item)
	if err != nil || !inserted {
		t.Fatalf("first item insert = %+v, %v, %v", first, inserted, err)
	}
	second, inserted, err := PutCollectionItem(db, item)
	if err != nil || inserted || second.ID != first.ID || len(second.MessageIDs) != 2 {
		t.Fatalf("duplicate item insert = %+v, %v, %v", second, inserted, err)
	}
	handled, err := HandledCollectionMessageIDs(db, source.ID)
	if err != nil || len(handled) != 2 {
		t.Fatalf("handled message ids = %v, %v", handled, err)
	}
	if _, ok := handled["m1"]; !ok {
		t.Fatalf("handled message ids missing m1: %v", handled)
	}

	now := time.Now().In(config.Loc).Unix()
	if err := SaveCollectionRun(db, CollectionRun{ID: "run-1", Connector: "test",
		SourceID: source.ID, Status: "succeeded", StartedAt: now, FinishedAt: now,
		FetchedCount: 2, CreatedCount: 1}); err != nil {
		t.Fatalf("save run: %v", err)
	}
	overview, err := LoadCollectionOverview(db, 20)
	if err != nil {
		t.Fatalf("load overview: %v", err)
	}
	if overview.Summary.Sources != 1 || overview.Summary.Enabled != 0 || overview.Summary.Fetched != 2 ||
		overview.Summary.Created != 1 || overview.Summary.Unread != 1 ||
		len(overview.Items) != 1 || len(overview.Runs) != 1 {
		t.Fatalf("unexpected overview: %+v", overview)
	}
}

func TestReconcileInterruptedCollectionRunsOnlyClosesRunningRows(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, run := range []CollectionRun{
		{ID: "stale", Connector: "test", SourceID: "source", Status: "running", StartedAt: 10},
		{ID: "done", Connector: "test", SourceID: "source", Status: "succeeded", StartedAt: 9, FinishedAt: 11},
	} {
		if err := SaveCollectionRun(db, run); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := ReconcileInterruptedCollectionRuns(db, 20)
	if err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	runs, err := ListCollectionRuns(db, 20)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]CollectionRun{}
	for _, run := range runs {
		byID[run.ID] = run
	}
	if got := byID["stale"]; got.Status != "failed" || got.FinishedAt != 20 || got.FailedCount != 1 || got.Error == "" {
		t.Fatalf("stale run was not closed: %+v", got)
	}
	if got := byID["done"]; got.Status != "succeeded" || got.FinishedAt != 11 {
		t.Fatalf("terminal run changed: %+v", got)
	}
	changed, err = ReconcileInterruptedCollectionRuns(db, 30)
	if err != nil || changed != 0 {
		t.Fatalf("reconcile was not idempotent: changed=%d err=%v", changed, err)
	}
}

func TestCollectionReadStateCountsOnlyActionableResults(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "read-state", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := []CollectionItem{
		{SourceID: source.ID, Connector: "test", Fingerprint: "create", Action: "create", Status: "processed"},
		{SourceID: source.ID, Connector: "test", Fingerprint: "append", Action: "append", Status: "processed"},
		{SourceID: source.ID, Connector: "test", Fingerprint: "insight", Action: "insight", Status: "processed"},
		{SourceID: source.ID, Connector: "test", Fingerprint: "ignore", Action: "ignore", Status: "processed"},
	}
	stored := map[string]CollectionItem{}
	for _, item := range items {
		item.MessageIDs = []string{"m-" + item.Fingerprint}
		created, _, err := PutCollectionItem(db, item)
		if err != nil {
			t.Fatal(err)
		}
		stored[item.Fingerprint] = created
	}
	overview, err := LoadCollectionOverview(db, 20)
	if err != nil || overview.Summary.Unread != 3 {
		t.Fatalf("initial unread=%d err=%v", overview.Summary.Unread, err)
	}
	changed, err := SetCollectionItemsRead(db,
		[]string{stored["create"].ID, stored["append"].ID}, true)
	if err != nil || len(changed) != 2 || changed[0].ReadAt == 0 || changed[1].ReadAt == 0 {
		t.Fatalf("mark read = %+v err=%v", changed, err)
	}
	overview, _ = LoadCollectionOverview(db, 20)
	if overview.Summary.Unread != 1 {
		t.Fatalf("unread after read = %d", overview.Summary.Unread)
	}
	if _, err := SetCollectionItemsRead(db, []string{stored["create"].ID}, false); err != nil {
		t.Fatal(err)
	}
	overview, _ = LoadCollectionOverview(db, 20)
	if overview.Summary.Unread != 2 {
		t.Fatalf("unread after reopening = %d", overview.Summary.Unread)
	}
	count, err := MarkAllCollectionItemsRead(db)
	if err != nil || count != 2 {
		t.Fatalf("mark all count=%d err=%v", count, err)
	}
	overview, _ = LoadCollectionOverview(db, 20)
	if overview.Summary.Unread != 0 {
		t.Fatalf("unread after mark all = %d", overview.Summary.Unread)
	}
	if _, err := SetCollectionItemsRead(db, []string{"missing"}, true); err == nil {
		t.Fatal("missing item should make the read update fail")
	}
}

func TestCollectionItemsCanBeArchivedAndReopenedWithoutBeingRecollected(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "archive-item", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := []CollectionItem{}
	for _, fingerprint := range []string{"main", "supplement", "retryable-failure"} {
		action, status := "create", "processed"
		if fingerprint == "retryable-failure" {
			action, status = "failed", "failed"
		}
		item, _, err := PutCollectionItem(db, CollectionItem{
			SourceID: source.ID, Connector: "test", Fingerprint: fingerprint,
			MessageIDs: []string{"m-" + fingerprint}, Action: action, Status: status, Attempts: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	ids := []string{items[0].ID, items[1].ID, items[2].ID}
	archived, err := SetCollectionItemsArchived(db, ids, true)
	if err != nil || len(archived) != 3 || archived[0].ArchivedAt == 0 || archived[0].ReadAt == 0 || archived[2].ArchivedAt == 0 {
		t.Fatalf("archive = %+v err=%v", archived, err)
	}
	overview, err := LoadCollectionOverview(db, 20)
	if err != nil || overview.Summary.Unread != 0 {
		t.Fatalf("overview after archive = %+v err=%v", overview.Summary, err)
	}
	handled, err := HandledCollectionMessageIDs(db, source.ID)
	if err != nil || len(handled) != 3 {
		t.Fatalf("archiving released handled messages: %v err=%v", handled, err)
	}
	if _, err := SetCollectionItemsArchived(db, []string{items[0].ID, "missing"}, false); err == nil {
		t.Fatal("stale id should make the archive update fail atomically")
	}
	stillArchived, err := GetCollectionItem(db, items[0].ID)
	if err != nil || stillArchived.ArchivedAt == 0 {
		t.Fatalf("failed batch partially reopened item: %+v err=%v", stillArchived, err)
	}
	reopened, err := SetCollectionItemsArchived(db, ids, false)
	if err != nil || reopened[0].ArchivedAt != 0 || reopened[2].ArchivedAt != 0 || reopened[0].ReadAt == 0 {
		t.Fatalf("reopen = %+v err=%v", reopened, err)
	}
}

func TestRecentlyArchivedCollectionItemRemainsInLimitedOverview(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "archive-window", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var oldest CollectionItem
	for index := 0; index < 201; index++ {
		item, _, err := PutCollectionItem(db, CollectionItem{
			SourceID: source.ID, Connector: "test", Fingerprint: fmt.Sprintf("item-%03d", index),
			MessageIDs: []string{fmt.Sprintf("message-%03d", index)}, Action: "create", Status: "processed",
		})
		if err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			oldest = item
		}
	}
	if _, err := db.Exec(`UPDATE collection_items SET updated_at=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := SetCollectionItemsArchived(db, []string{oldest.ID}, true); err != nil {
		t.Fatal(err)
	}
	items, err := ListCollectionItems(db, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == oldest.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the newly archived item fell outside the limited overview, so the UI cannot reopen it")
	}
}

func TestMigrateV46AddsCollectionItemArchiveState(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_collection_items_archived`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE collection_items DROP COLUMN archived_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version=46`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v46: %v", err)
	}
	defer db.Close()
	var version, columns int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('collection_items') WHERE name='archived_at'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || columns != 1 {
		t.Fatalf("version=%d archived_at columns=%d", version, columns)
	}
}

func TestMigrateV43MarksHistoricalCollectionItemsRead(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "read-migration", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := PutCollectionItem(db, CollectionItem{
		SourceID: source.ID, Connector: "test", Fingerprint: "historical",
		MessageIDs: []string{"m1"}, Action: "insight", Status: "processed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX idx_collection_items_unread`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE collection_items DROP COLUMN read_at`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version=43`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v43: %v", err)
	}
	defer db.Close()
	migrated, err := GetCollectionItem(db, item.ID)
	if err != nil || migrated.ReadAt == 0 {
		t.Fatalf("historical item was not marked read: %+v err=%v", migrated, err)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil || version != SchemaVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func TestMigrateV42AddsExplicitKnowledgeSaveLink(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "migration", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	item, _, err := PutCollectionItem(db, CollectionItem{
		SourceID: source.ID, Connector: "test", Fingerprint: "migration-item",
		MessageIDs: []string{"m1"}, Action: "insight", Status: "processed", Summary: "保留的结论",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE collection_items DROP COLUMN knowledge_document_id`,
		`ALTER TABLE collection_items DROP COLUMN knowledge_collection`,
		`UPDATE schema_version SET version=42`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("prepare v42: %v", err)
		}
	}
	db.Close()

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v42: %v", err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	migrated, err := GetCollectionItem(db, item.ID)
	if err != nil || version != SchemaVersion || migrated.Summary != "保留的结论" ||
		migrated.KnowledgeDocumentID != "" || migrated.KnowledgeCollection != "" {
		t.Fatalf("version=%d item=%+v err=%v", version, migrated, err)
	}
}

func TestCollectionOverviewKeepsLatestRunForEverySource(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	busy, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "busy", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	quiet, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "quiet", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCollectionRun(db, CollectionRun{
		ID: "quiet-latest", Connector: "test", SourceID: quiet.ID,
		Status: "succeeded", StartedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 21; index++ {
		if err := SaveCollectionRun(db, CollectionRun{
			ID: fmt.Sprintf("busy-%02d", index), Connector: "test", SourceID: busy.ID,
			Status: "succeeded", StartedAt: int64(100 + index),
		}); err != nil {
			t.Fatal(err)
		}
	}

	overview, err := LoadCollectionOverview(db, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundQuiet := false
	for _, run := range overview.Runs {
		if run.ID == "quiet-latest" {
			foundQuiet = true
			break
		}
	}
	if !foundQuiet {
		t.Fatalf("quiet source's latest run was dropped from overview: %+v", overview.Runs)
	}
	// Backfilling a quiet source must not leave the page out of order: readers
	// that take "the first run for this connector" rely on newest-first.
	for index := 1; index < len(overview.Runs); index++ {
		previous, current := overview.Runs[index-1], overview.Runs[index]
		if previous.StartedAt < current.StartedAt ||
			(previous.StartedAt == current.StartedAt && previous.ID < current.ID) {
			t.Fatalf("runs are not newest-first at %d: %+v", index, overview.Runs)
		}
	}
}

// The ledger records what collection decided; whether that decision is still
// outstanding belongs to the Todo it wrote to. Nothing writes the Todo's state
// back into the ledger, so every read has to derive it — including for the
// records that were already there before this existed.
func TestCollectionItemsFollowTheirTodoThroughItsLifecycle(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "处理收集到的部署报错"))
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-lifecycle",
		Name: "研发群", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	filed, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-lifecycle", Fingerprint: "filed", MessageIDs: []string{"m1"},
		Action: "create", Title: "处理收集到的部署报错", TodoID: "t1", Status: "processed"})
	if err != nil {
		t.Fatalf("put filed item: %v", err)
	}
	unlinked, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-lifecycle", Fingerprint: "noise", MessageIDs: []string{"m2"},
		Action: "ignore", Status: "processed"})
	if err != nil {
		t.Fatalf("put unlinked item: %v", err)
	}
	if filed.TodoStatus != TodoStatusOpen || filed.TodoArchived || CollectionItemTodoClosed(filed) {
		t.Fatalf("open todo did not reach its record: %+v", filed)
	}
	if unlinked.TodoStatus != "" || CollectionItemTodoClosed(unlinked) {
		t.Fatalf("record without a todo invented one: %+v", unlinked)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		todo := FindTodo(state.Todos, "t1")
		if todo == nil {
			return TodoNotFoundError(state.Todos, "t1")
		}
		todo.Status = TodoStatusDone
		return nil
	}); err != nil {
		t.Fatalf("finish todo: %v", err)
	}

	// Every read path, because the App reads the overview and the collector reads
	// single records — one of them silently missing the join would be worse than
	// none of them having it.
	reread, err := GetCollectionItem(db, filed.ID)
	if err != nil || reread.TodoStatus != TodoStatusDone || !CollectionItemTodoClosed(reread) {
		t.Fatalf("get after done = %+v, %v", reread, err)
	}
	listed, err := ListCollectionItems(db, source.ID, 20)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list items = %+v, %v", listed, err)
	}
	for _, item := range listed {
		if item.ID == filed.ID && item.TodoStatus != TodoStatusDone {
			t.Fatalf("list after done = %+v", item)
		}
	}
	overview, err := LoadCollectionOverview(db, 20)
	if err != nil {
		t.Fatalf("load overview: %v", err)
	}
	if overview.Summary.Followups != 1 || overview.Summary.FollowupsClosed != 1 {
		t.Fatalf("unexpected follow-up counts: %+v", overview.Summary)
	}

	if err := UpdateWorkState(func(state *WorkStateTx) error {
		_, err := state.ArchiveTodos([]string{"t1"})
		return err
	}); err != nil {
		t.Fatalf("archive todo: %v", err)
	}
	archived, err := GetCollectionItem(db, filed.ID)
	if err != nil || archived.TodoStatus != TodoStatusDone || !archived.TodoArchived {
		t.Fatalf("get after archive = %+v, %v", archived, err)
	}
}

// Deleting a record is tidying the ledger, not undoing the work: the Todo it
// filed belongs to whoever has been acting on it. Its messages do come back out
// of the handled set, which is what lets a re-read rebuild the record.
func TestDeleteCollectionItemKeepsItsTodoAndReleasesItsMessages(t *testing.T) {
	withTempStore(t)
	seedTodos(t, openTodo("t1", "处理收集到的部署报错"))
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-delete",
		Name: "研发群", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	filed, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-delete", Fingerprint: "filed", MessageIDs: []string{"m1"},
		Action: "create", Title: "处理收集到的部署报错", TodoID: "t1", Status: "processed"})
	if err != nil {
		t.Fatalf("put item: %v", err)
	}

	deleted, err := DeleteCollectionItem(db, filed.ID)
	if err != nil || deleted.ID != filed.ID || deleted.TodoID != "t1" {
		t.Fatalf("delete item = %+v, %v", deleted, err)
	}
	if _, err := GetCollectionItem(db, filed.ID); err == nil {
		t.Fatalf("record survived its deletion")
	}
	todos, err := LoadTodosReadOnly()
	if err != nil || FindTodo(todos, "t1") == nil {
		t.Fatalf("deleting a record took its Todo with it: %v", err)
	}
	handled, err := HandledCollectionMessageIDs(db, source.ID)
	if err != nil || len(handled) != 0 {
		t.Fatalf("handled message ids = %v, %v", handled, err)
	}
	if _, err := DeleteCollectionItem(db, filed.ID); err == nil {
		t.Fatalf("deleting a missing record reported success")
	}
}

// A duplicated id asks for the same end state as naming it once. Failing the
// batch instead would make a group refuse to clear with nothing wrong with it.
func TestDeleteCollectionItemsIgnoresARepeatedID(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-dup",
		Name: "研发群", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	first, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-dup", Fingerprint: "one", MessageIDs: []string{"m1"},
		Action: "ignore", Status: "processed"})
	if err != nil {
		t.Fatalf("put first item: %v", err)
	}
	second, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-dup", Fingerprint: "two", MessageIDs: []string{"m2"},
		Action: "ignore", Status: "processed"})
	if err != nil {
		t.Fatalf("put second item: %v", err)
	}

	deleted, err := DeleteCollectionItems(db, []string{first.ID, second.ID, first.ID})
	if err != nil {
		t.Fatalf("a repeated id failed the batch: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("deleted %d records, want the 2 distinct ones: %+v", len(deleted), deleted)
	}
	for _, id := range []string{first.ID, second.ID} {
		if _, err := GetCollectionItem(db, id); err == nil {
			t.Fatalf("record %s survived the batch", id)
		}
	}
	// An id that was never there is still a stale snapshot, and still stops
	// everything: the dedup must not have turned "missing" into "fine".
	third, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: "cid-dup", Fingerprint: "three", MessageIDs: []string{"m3"},
		Action: "ignore", Status: "processed"})
	if err != nil {
		t.Fatalf("put third item: %v", err)
	}
	if _, err := DeleteCollectionItems(db, []string{third.ID, "ci-gone"}); err == nil {
		t.Fatal("an unknown id in the batch reported success")
	}
	if _, err := GetCollectionItem(db, third.ID); err != nil {
		t.Fatalf("a failed batch still deleted a record: %v", err)
	}
}

func TestCollectionSourceValidation(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "slack", Kind: "channel", ExternalID: "C123", Priority: "P2",
	}); err != nil || source.Connector != "slack" || source.Kind != "channel" {
		t.Fatalf("connector-defined source kind was rejected: source=%+v err=%v", source, err)
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "Slack Cloud", Kind: "channel", ExternalID: "x", Priority: "P2",
	}); err == nil {
		t.Fatal("invalid connector id was accepted")
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "slack", Kind: "public channel", ExternalID: "x", Priority: "P2",
	}); err == nil {
		t.Fatal("invalid source kind was accepted")
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "x", Priority: "urgent",
	}); err == nil {
		t.Fatal("invalid priority was accepted")
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "x",
		Strategy: "guess", Priority: "P2",
	}); err == nil {
		t.Fatal("invalid strategy was accepted")
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "x",
		Strategy: CollectionStrategyObserve, IntervalMinutes: 2000, Priority: "P2",
	}); err == nil {
		t.Fatal("invalid source interval was accepted")
	}
}

func TestCollectionSourceDueUsesOwnSuccessfulCadence(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-observe",
		Strategy: CollectionStrategyObserve, IntervalMinutes: 60, Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10_000, 0)
	due, err := CollectionSourceDue(db, source, now)
	if err != nil || !due {
		t.Fatalf("new source due=%v err=%v", due, err)
	}
	if err := SaveCollectionRun(db, CollectionRun{ID: "recent", Connector: source.Connector,
		SourceID: source.ID, Status: "succeeded", StartedAt: now.Add(-30 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	due, err = CollectionSourceDue(db, source, now)
	if err != nil || due {
		t.Fatalf("recent observation source due=%v err=%v", due, err)
	}
	due, err = CollectionSourceDue(db, source, now.Add(31*time.Minute))
	if err != nil || !due {
		t.Fatalf("elapsed observation source due=%v err=%v", due, err)
	}
}

// The merged record has to arrive as the members leave, in one transaction: the
// members' message IDs are what marks that chat handled, so a half-applied merge
// would hand those messages back to the next run.
func TestMergeCollectionInsightsSwapsMembersForOneRecord(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-merge",
		Strategy: CollectionStrategyObserve, IntervalMinutes: 60, Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	member := func(fingerprint string, messageIDs ...string) CollectionItem {
		item, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
			ConversationID: source.ExternalID, Fingerprint: fingerprint, MessageIDs: messageIDs,
			OccurredAt: 1234, Action: "insight", Status: "processed",
			Title: "结论 " + fingerprint, Summary: "细节 " + fingerprint, ItemType: "insight"})
		if err != nil {
			t.Fatalf("store member %s: %v", fingerprint, err)
		}
		return item
	}
	first, second := member("fp-1", "m1"), member("fp-2", "m2", "m3")

	// The caller names the union it built; a member ID it left out is still taken
	// on, because the handled bookkeeping is what this invariant protects.
	merged, err := MergeCollectionInsights(db, CollectionItem{SourceID: source.ID, Connector: "test",
		ConversationID: source.ExternalID, Fingerprint: "fp-merged", MessageIDs: []string{"m1"},
		OccurredAt: 1234, Action: "insight", Status: "processed", Title: "本轮 2 条结论",
		Summary: "- 结论 fp-1\n- 结论 fp-2", ItemType: "insight"},
		[]string{first.ID, second.ID})
	if err != nil {
		t.Fatalf("merge insights: %v", err)
	}
	if len(merged.MessageIDs) != 3 {
		t.Fatalf("merged record does not own every message: %+v", merged.MessageIDs)
	}
	items, err := ListCollectionItems(db, source.ID, 10)
	if err != nil || len(items) != 1 || items[0].ID != merged.ID {
		t.Fatalf("members outlived the merge: %+v, %v", items, err)
	}
	handled, err := HandledCollectionMessageIDs(db, source.ID)
	if err != nil || len(handled) != 3 {
		t.Fatalf("merge released handled messages: %v, %v", handled, err)
	}
}

// Nothing a fresh run just produced can already have written itself out, which is
// exactly why a member that did must stop the merge instead of being deleted.
func TestMergeCollectionInsightsRefusesMembersThatWroteSomethingOut(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-refuse", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	saved, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		Fingerprint: "fp-saved", MessageIDs: []string{"m1"}, Action: "insight", Status: "processed",
		Title: "已保存的结论", Summary: "已经进了知识库", ItemType: "insight",
		KnowledgeDocumentID: "kd-1"})
	if err != nil {
		t.Fatalf("store saved insight: %v", err)
	}
	plain, _, err := PutCollectionItem(db, CollectionItem{SourceID: source.ID, Connector: "test",
		Fingerprint: "fp-plain", MessageIDs: []string{"m2"}, Action: "insight", Status: "processed",
		Title: "普通结论", Summary: "还没保存", ItemType: "insight"})
	if err != nil {
		t.Fatalf("store plain insight: %v", err)
	}
	if _, err := MergeCollectionInsights(db, CollectionItem{SourceID: source.ID, Connector: "test",
		Fingerprint: "fp-merged", MessageIDs: []string{"m1", "m2"}, Action: "insight",
		Status: "processed", Title: "本轮 2 条结论", Summary: "拼起来", ItemType: "insight"},
		[]string{saved.ID, plain.ID}); err == nil {
		t.Fatal("a member that already wrote something out was merged away")
	}
	items, err := ListCollectionItems(db, source.ID, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("refused merge still changed the records: %+v, %v", items, err)
	}
}

// One insight is already the one record, and two callers must not disagree about
// that: the store refuses a degenerate merge rather than filing a copy.
func TestMergeCollectionInsightsNeedsTwoRecords(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, err := MergeCollectionInsights(db, CollectionItem{SourceID: "cs-1", Connector: "test",
		Fingerprint: "fp-merged", Action: "insight", Status: "processed"}, []string{"ci-1"}); err == nil {
		t.Fatal("merging a single record was accepted")
	}
}
