package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

type collectionDeletion struct {
	Count   int `json:"count"`
	Deleted []struct {
		ID     string `json:"id"`
		TodoID string `json:"todo_id"`
	} `json:"deleted"`
}

func decodeCollectionDeletion(payload string) (collectionDeletion, error) {
	var deletion collectionDeletion
	err := json.Unmarshal([]byte(payload), &deletion)
	return deletion, err
}

// The browser deletes with --yes because it already asked, so the flag has to be the
// only thing standing between the command and the row. Without stdin a prompt
// would otherwise fail, and the record would look undeletable from the browser —
// which is the complaint this command answers.
func TestCollectItemDeleteRemovesTheRecordAndKeepsItsTodo(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON, oldYes := jsonOutput, collectYes
	t.Cleanup(func() { jsonOutput, collectYes = oldJSON, oldYes })
	if err := seedTodos(store.Todo{ID: "t1", Title: "修部署脚本", Priority: "P1",
		Status: store.TodoStatusOpen, Created: store.Today()}); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-delete", Priority: "P2", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{SourceID: source.ID,
		Connector: "test", Fingerprint: "filed", MessageIDs: []string{"m1"},
		Action: "create", Title: "修部署脚本", TodoID: "t1", Status: "processed"})
	if err != nil {
		db.Close()
		t.Fatalf("put item: %v", err)
	}
	db.Close()

	jsonOutput, collectYes = true, true
	deletedJSON := captureStdout(t, func() {
		if err := collectItemDeleteCmd.RunE(collectItemDeleteCmd, []string{item.ID}); err != nil {
			t.Fatalf("collect item delete: %v", err)
		}
	})
	deleted, err := decodeCollectionDeletion(deletedJSON)
	if err != nil {
		t.Fatalf("decode delete: %v\n%s", err, deletedJSON)
	}
	if deleted.Count != 1 || len(deleted.Deleted) != 1 ||
		deleted.Deleted[0].ID != item.ID || deleted.Deleted[0].TodoID != "t1" {
		t.Fatalf("unexpected delete result: %+v", deleted)
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil || store.FindTodo(todos, "t1") == nil {
		t.Fatalf("deleting a record took its Todo with it: %v", err)
	}
	err = collectItemDeleteCmd.RunE(collectItemDeleteCmd, []string{item.ID})
	if err == nil || !strings.Contains(err.Error(), "collection item not found") {
		t.Fatalf("deleting a missing record = %v", err)
	}
}

// Clearing a group in the browser hands the whole batch to one command, so the ids
// have to go together: a per-record loop would spawn a process per row, and a
// batch that stops halfway would leave a group nobody can tell apart from one
// that was never cleared.
func TestCollectItemDeleteClearsAGroupInOneTransaction(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON, oldYes := jsonOutput, collectYes
	t.Cleanup(func() { jsonOutput, collectYes = oldJSON, oldYes })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-clear", Priority: "P2", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	ids := []string{}
	for _, fingerprint := range []string{"one", "two", "three"} {
		item, _, err := store.PutCollectionItem(db, store.CollectionItem{SourceID: source.ID,
			Connector: "test", Fingerprint: fingerprint, MessageIDs: []string{"m-" + fingerprint},
			Action: "ignore", Title: fingerprint, Status: "processed"})
		if err != nil {
			db.Close()
			t.Fatalf("put item %s: %v", fingerprint, err)
		}
		ids = append(ids, item.ID)
	}
	db.Close()

	jsonOutput, collectYes = true, true
	// A stale id anywhere in the batch leaves every other record in place.
	err = collectItemDeleteCmd.RunE(collectItemDeleteCmd, []string{ids[0], "nope", ids[1]})
	if err == nil || !strings.Contains(err.Error(), "collection item not found") {
		t.Fatalf("clearing with a stale id = %v", err)
	}
	if remaining := countCollectionItems(t); remaining != 3 {
		t.Fatalf("a failed clear deleted %d of 3 records", 3-remaining)
	}

	clearedJSON := captureStdout(t, func() {
		if err := collectItemDeleteCmd.RunE(collectItemDeleteCmd, ids); err != nil {
			t.Fatalf("collect item delete: %v", err)
		}
	})
	cleared, err := decodeCollectionDeletion(clearedJSON)
	if err != nil {
		t.Fatalf("decode clear: %v\n%s", err, clearedJSON)
	}
	if cleared.Count != 3 || len(cleared.Deleted) != 3 {
		t.Fatalf("unexpected clear result: %+v", cleared)
	}
	if remaining := countCollectionItems(t); remaining != 0 {
		t.Fatalf("clearing the group left %d records", remaining)
	}
}

func countCollectionItems(t *testing.T) int {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := store.ListCollectionItems(db, "", 500)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	return len(items)
}
