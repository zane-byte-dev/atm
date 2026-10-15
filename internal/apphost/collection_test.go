package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
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
	created := collectionTestCall(t, h, "collect.source.save", body).(CollectionSourceResult)
	if created.Source.ID == "" || created.Source.Name != "First" {
		t.Fatalf("create=%+v", created)
	}
	edited := collectionTestCall(t, h, "collect.source.save", `{"connector":"fixture","kind":"group","external_id":"explicit-source","name":"Edited","enabled":false,"strategy":"observe","decision_unit":"message","interval_minutes":30,"priority":"P2"}`).(CollectionSourceResult)
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
	deleted := collectionTestCall(t, h, "collect.source.delete", fmt.Sprintf(`{"source_id":%q,"confirmed":true}`, created.Source.ID)).(CollectionSourceResult)
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

func TestCollectionReadsNeverCreateAndHistoryNeverPrunes(t *testing.T) {
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
	ignored, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: source.ID, Connector: source.Connector, ConversationID: source.ExternalID,
		Fingerprint: "ignored-result", MessageIDs: []string{"m1"}, Title: "Ignored result",
		Action: "ignore", Status: "processed", CreatedAt: 200, UpdatedAt: 200,
	})
	db.Close()
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
	all := collectionTestCall(t, h, "collect.items", `{"state":"all","limit":100}`).(CollectionListResult)
	foundIgnored := false
	for _, item := range all.Items {
		foundIgnored = foundIgnored || item.ID == ignored.ID
	}
	if all.Total != 106 || len(all.Items) != 100 || !foundIgnored {
		t.Fatalf("all records did not retain ignored audit item: %+v", all)
	}
	shown := collectionTestCall(t, h, "collect.item.show", fmt.Sprintf(`{"item_id":%q}`, items[0].ID)).(CollectionItemResult)
	if shown.Item.RawContext != "historic source context" || shown.Item.ReadAt != 0 {
		t.Fatalf("detail modified or lost item: %+v", shown)
	}
	history := collectionTestCall(t, h, "collect.history", fmt.Sprintf(`{"source_id":%q}`, source.ID)).(CollectionHistoryResult)
	if len(history.Messages) != 1 || history.Messages[0].CreatedAt != 1 || !history.Local {
		t.Fatalf("history lost older local messages: %+v", history)
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

func TestInboxUnreadFilterComposesWithArchiveAndPagination(t *testing.T) {
	h := testHost(t)
	_, items := seedCollection(t)
	collectionTestCall(t, h, "collect.item.read", fmt.Sprintf(`{"item_id":%q,"read":true}`, items[0].ID))
	collectionTestCall(t, h, "collect.item.archive", fmt.Sprintf(`{"item_id":%q,"archived":true}`, items[1].ID))
	active := collectionTestCall(t, h, "collect.items", `{"state":"active","unread_only":true,"limit":100,"offset":100}`).(CollectionListResult)
	if active.Total != 103 || len(active.Items) != 3 {
		t.Fatalf("unread pagination: %+v", active)
	}
	collectionTestCall(t, h, "collect.item.read", fmt.Sprintf(`{"item_id":%q,"read":false}`, items[1].ID))
	archived := collectionTestCall(t, h, "collect.items", `{"state":"archived","unread_only":true}`).(CollectionListResult)
	if archived.Total != 1 || archived.Items[0].ID != items[1].ID {
		t.Fatalf("archived unread: %+v", archived)
	}
}

// The browser's collection read has to carry the same connector projection
// `atm collect status` prints, plus the run volume behind it. Health without
// volume says whether a connector is broken this minute but not whether it has
// been working; volume without health leaves an expired login undiagnosed. For
// the dingtalk connector this is the only place the `dws` CLI it drives is
// visible outside a terminal, so both have to travel together.
func TestCollectionOverviewCarriesConnectorHealthAndRunVolume(t *testing.T) {
	h := testHost(t)
	before := config.CollectionConnectors
	t.Cleanup(func() { config.CollectionConnectors = before })
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{
		"dingtalk": {Command: "/this-command-must-never-run", LoginCommand: "~/bin/dws auth login"},
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "dingtalk", Kind: "group", ExternalID: "g1", Name: "Project Wanda", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, run := range []store.CollectionRun{
		{ID: "today", Connector: "dingtalk", SourceID: source.ID, Status: "succeeded",
			StartedAt: now, FinishedAt: now, FetchedCount: 4},
		{ID: "this-week", Connector: "dingtalk", SourceID: source.ID, Status: "failed",
			StartedAt: now - 3*86400, FinishedAt: now - 3*86400, Error: "collection connector dingtalk timed out after 45s"},
		{ID: "older", Connector: "dingtalk", SourceID: source.ID, Status: "failed",
			StartedAt: now - 10*86400, FinishedAt: now - 10*86400, Error: "dws returned an error: 未登录"},
	} {
		if err := store.SaveCollectionRun(db, run); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	overview := collectionTestCall(t, h, "collect.overview", `{}`).(CollectionOverviewResult)
	if len(overview.ConnectorUsage) != 1 {
		t.Fatalf("connector usage = %+v", overview.ConnectorUsage)
	}
	usage := overview.ConnectorUsage[0]
	// The newest run succeeded, so nothing is broken right now — but the window
	// still has to admit that two of the last three attempts failed.
	if usage.Health.Connector != "dingtalk" || usage.Health.Status != "ready" {
		t.Errorf("health = %+v", usage.Health)
	}
	if usage.Health.RecentRuns != 3 || usage.Health.RecentFailures != 2 {
		t.Errorf("health window = %+v", usage.Health)
	}
	if want := config.Home + "/bin/dws auth login"; usage.Health.LoginCommand != want {
		t.Errorf("login command = %q, want %q", usage.Health.LoginCommand, want)
	}
	if usage.Today.Runs != 1 || usage.Today.Failed != 0 || usage.Today.Fetched != 4 {
		t.Errorf("today = %+v", usage.Today)
	}
	if usage.Week.Runs != 2 || usage.Week.Failed != 1 || usage.Week.LastFailedAt != now-3*86400 {
		t.Errorf("week = %+v", usage.Week)
	}
}
