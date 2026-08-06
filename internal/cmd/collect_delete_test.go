package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

// The App deletes with --yes because it already asked, so the flag has to be the
// only thing standing between the command and the row. Without stdin a prompt
// would otherwise fail, and the record would look undeletable from the desktop —
// which is the complaint this command answers.
func TestCollectItemDeleteRemovesTheRecordAndKeepsItsTodo(t *testing.T) {
	withTempAtmDir(t)
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
	var deleted struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
		TodoID  string `json:"todo_id"`
	}
	if err := json.Unmarshal([]byte(deletedJSON), &deleted); err != nil {
		t.Fatalf("decode delete: %v\n%s", err, deletedJSON)
	}
	if deleted.ID != item.ID || !deleted.Deleted || deleted.TodoID != "t1" {
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
