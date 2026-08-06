package store

import (
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
		overview.Summary.Created != 1 || len(overview.Items) != 1 || len(overview.Runs) != 1 {
		t.Fatalf("unexpected overview: %+v", overview)
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
