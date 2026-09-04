package apphost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func seedCollection(t *testing.T) (store.CollectionSource, []store.CollectionItem) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "fixture", Kind: "group", ExternalID: "local-conversation", Name: "Fixture source", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	items := []store.CollectionItem{}
	for i := 0; i < 105; i++ {
		item, _, err := store.PutCollectionItem(db, store.CollectionItem{
			SourceID: source.ID, Connector: source.Connector, ConversationID: source.ExternalID,
			Fingerprint: fmt.Sprintf("item-%d", i), MessageIDs: []string{"m1"}, Title: fmt.Sprintf("Result %03d", i),
			Summary: "saved conclusion", RawContext: "historic source context", Action: "insight", Status: "processed",
			CreatedAt: int64(i + 1), UpdatedAt: int64(i + 1),
		})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	if _, err := store.PutCollectionMessages(db, []store.CollectionMessage{{
		Connector: source.Connector, ConversationID: source.ExternalID, SourceID: source.ID,
		MessageID: "m1", CreatedAt: 1, Content: "old message retained on read", Sender: "fixture sender",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCollectionRun(db, store.CollectionRun{ID: "cr_fixture", Connector: source.Connector, SourceID: source.ID,
		Status: "failed", StartedAt: 20, FinishedAt: 21, Error: "fixture connector unavailable"}); err != nil {
		t.Fatal(err)
	}
	return source, items
}

func TestCollectionSourceManagementUsesRegisteredConnectorWithoutExecutingIt(t *testing.T) {
	h := testHost(t)
	before := config.CollectionConnectors
	t.Cleanup(func() { config.CollectionConnectors = before })
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{"fixture": {Command: "/this-command-must-never-run"}}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	body := `{"connector":"fixture","kind":"group","external_id":"explicit-source","name":"First","enabled":true,"strategy":"tasks","priority":"P1"}`
	created := collectionTestCall(t, h, "collect.source.save", body).(collector.SaveSourceResult)
	if created.Source.ID == "" || created.Source.Name != "First" {
		t.Fatalf("create=%+v", created)
	}
	edited := collectionTestCall(t, h, "collect.source.save", `{"connector":"fixture","kind":"group","external_id":"explicit-source","name":"Edited","enabled":false,"strategy":"observe","decision_unit":"message","interval_minutes":30,"priority":"P2"}`).(collector.SaveSourceResult)
	if edited.Source.ID != created.Source.ID || edited.Source.Name != "Edited" || edited.Source.Enabled || edited.Source.Strategy != "observe" || edited.Source.IntervalMinutes != 30 {
		t.Fatalf("edit=%+v", edited)
	}
	for _, input := range []string{`{"connector":"missing","kind":"group","external_id":"source","enabled":true}`, `{"connector":"fixture","kind":"group","external_id":"source","enabled":true,"command":"sh"}`, `{"connector":"fixture","kind":"group","external_id":"source","enabled":true,"knowledge_collection":"../outside"}`} {
		if _, err := h.callCollection(context.Background(), webCall(), "collect.source.save", json.RawMessage(input)); err == nil {
			t.Fatalf("accepted unsafe source: %s", input)
		}
	}
	agent := webCall()
	agent.Actor.Kind = application.ActorAgent
	if _, err := h.callCollection(context.Background(), agent, "collect.source.save", json.RawMessage(body)); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent changed source: %v", err)
	}
	if _, err := h.callCollection(context.Background(), webCall(), "collect.source.delete", json.RawMessage(fmt.Sprintf(`{"source_id":%q}`, created.Source.ID))); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed deletion accepted: %v", err)
	}
	deleted := collectionTestCall(t, h, "collect.source.delete", fmt.Sprintf(`{"source_id":%q,"confirmed":true}`, created.Source.ID)).(collector.DeleteSourceResult)
	if deleted.Source.ID != created.Source.ID {
		t.Fatalf("delete=%+v", deleted)
	}
	overview := collectionTestCall(t, h, "collect.overview", `{}`).(CollectionOverviewResult)
	if len(overview.Sources) != 0 {
		t.Fatal("deleted source still listed")
	}
}

func collectionTestCall(t *testing.T, h *Host, method, input string) any {
	t.Helper()
	result, err := h.callCollection(context.Background(), webCall(), method, json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func TestCollectionReadsNeverCreateOrMigrateAndHistoryNeverPrunes(t *testing.T) {
	h := testHost(t)
	collectionTestCall(t, h, "collect.overview", `{}`)
	collectionTestCall(t, h, "collect.items", `{}`)
	if _, err := os.Stat(config.AtmDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("collection read created a database: %v", err)
	}
	source, items := seedCollection(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE schema_version SET version=54"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE work_create_idempotency"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, err := os.ReadFile(config.AtmDB)
	if err != nil {
		t.Fatal(err)
	}
	overview := collectionTestCall(t, h, "collect.overview", `{}`).(CollectionOverviewResult)
	if len(overview.Sources) != 1 || len(overview.Runs) != 1 || overview.WorkerOwned || overview.WorkerStatus != "external_status_unknown" {
		t.Fatalf("inaccurate overview: %+v", overview)
	}
	listed := collectionTestCall(t, h, "collect.items", `{"limit":50,"offset":100}`).(CollectionListResult)
	if listed.Total != 105 || len(listed.Items) != 5 {
		t.Fatalf("lost records beyond the old bounded snapshot: %+v", listed)
	}
	shown := collectionTestCall(t, h, "collect.item.show", fmt.Sprintf(`{"item_id":%q}`, items[0].ID)).(CollectionItemResult)
	if shown.Item.RawContext != "historic source context" || shown.Item.ReadAt != 0 {
		t.Fatalf("detail modified or lost item: %+v", shown)
	}
	history := collectionTestCall(t, h, "collect.history", fmt.Sprintf(`{"source_id":%q}`, source.ID)).(CollectionHistoryResult)
	if len(history.Messages) != 1 || history.Messages[0].CreatedAt != 1 || !history.Local {
		t.Fatalf("history lost older local messages: %+v", history)
	}
	for method, input := range map[string]string{
		"collect.item.read":      fmt.Sprintf(`{"item_id":%q,"read":true}`, items[0].ID),
		"collect.item.archive":   fmt.Sprintf(`{"item_id":%q,"archived":true}`, items[0].ID),
		"collect.source.enabled": fmt.Sprintf(`{"source_id":%q,"enabled":false}`, source.ID),
		"collect.source.muted":   fmt.Sprintf(`{"source_id":%q,"muted":true}`, source.ID),
	} {
		_, err := h.callCollection(context.Background(), webCall(), method, json.RawMessage(input))
		if !errors.Is(err, application.ErrForbidden) {
			t.Fatalf("%s upgraded old database: %v", method, err)
		}
	}
	after, err := os.ReadFile(config.AtmDB)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("old database bytes changed during Web reads/rejected writes: %v", err)
	}
}

func TestCollectionItemAndSourceStateUseExplicitHumanActions(t *testing.T) {
	h := testHost(t)
	source, items := seedCollection(t)
	id := items[0].ID
	read := collectionTestCall(t, h, "collect.item.read", fmt.Sprintf(`{"item_id":%q,"read":true}`, id)).(CollectionItemResult)
	if read.Item.ReadAt == 0 {
		t.Fatal("item was not acknowledged")
	}
	unread := collectionTestCall(t, h, "collect.item.read", fmt.Sprintf(`{"item_id":%q,"read":false}`, id)).(CollectionItemResult)
	if unread.Item.ReadAt != 0 {
		t.Fatal("item was not reopened as unread")
	}
	archived := collectionTestCall(t, h, "collect.item.archive", fmt.Sprintf(`{"item_id":%q,"archived":true}`, id)).(CollectionItemResult)
	if archived.Item.ArchivedAt == 0 || archived.Item.ReadAt == 0 {
		t.Fatal("archive must acknowledge the result")
	}
	filtered := collectionTestCall(t, h, "collect.items", `{"state":"archived"}`).(CollectionListResult)
	if filtered.Total != 1 || filtered.Items[0].ID != id {
		t.Fatalf("archive filter=%+v", filtered)
	}
	restored := collectionTestCall(t, h, "collect.item.archive", fmt.Sprintf(`{"item_id":%q,"archived":false}`, id)).(CollectionItemResult)
	if restored.Item.ArchivedAt != 0 || restored.Item.ReadAt == 0 {
		t.Fatal("restore must preserve the acknowledged state")
	}
	disabled := collectionTestCall(t, h, "collect.source.enabled", fmt.Sprintf(`{"source_id":%q,"enabled":false}`, source.ID)).(CollectionSourceResult)
	muted := collectionTestCall(t, h, "collect.source.muted", fmt.Sprintf(`{"source_id":%q,"muted":true}`, source.ID)).(CollectionSourceResult)
	if disabled.Source.Enabled || !muted.Source.Muted || muted.Source.Enabled {
		t.Fatalf("source changes interfered: disabled=%+v muted=%+v", disabled, muted)
	}
	search := collectionTestCall(t, h, "collect.items", fmt.Sprintf(`{"source_id":%q,"query":"Result 104"}`, source.ID)).(CollectionListResult)
	if search.Total != 1 || search.Items[0].Title != "Result 104" {
		t.Fatalf("search returned wrong page: %+v", search)
	}
	agent := webCall()
	agent.Actor.Kind = application.ActorAgent
	_, err := h.callCollection(context.Background(), agent, "collect.item.read", json.RawMessage(fmt.Sprintf(`{"item_id":%q,"read":true}`, id)))
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent Web mutation accepted: %v", err)
	}
}

func TestCollectionTypedWhitelistRejectsExecutionAndAmbiguousWrites(t *testing.T) {
	h := testHost(t)
	for _, test := range []struct{ method, input string }{
		{"collect.run", `{}`}, {"collect.source.save", `{}`}, {"collect.item.delete", `{}`},
		{"collect.history", `{"source_id":"cs_0000000000000000","local":false}`},
		{"collect.history", `{"source_id":"cs_0000000000000000","sync":true}`},
		{"collect.overview", `{"command":"execute"}`}, {"collect.items", `{"limit":101}`},
		{"collect.items", `{"offset":-1}`}, {"collect.items", `{"state":"invalid"}`},
		{"collect.item.show", `{"item_id":"../../private"}`},
		{"collect.item.read", `{"item_id":"ci_0000000000000000"}`},
		{"collect.item.read", `{"item_id":"ci_0000000000000000","read":null}`},
		{"collect.item.archive", `{"item_id":"ci_0000000000000000","archived":true,"all":true}`},
	} {
		if _, err := h.callCollection(context.Background(), webCall(), test.method, json.RawMessage(test.input)); err == nil {
			t.Errorf("accepted %s %s", test.method, test.input)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.callCollection(ctx, webCall(), "collect.overview", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read proceeded: %v", err)
	}
	if _, err := os.Stat(config.AtmDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid requests touched storage: %v", err)
	}
}
