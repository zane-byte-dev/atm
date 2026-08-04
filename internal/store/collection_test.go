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
