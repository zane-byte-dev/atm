package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectItemArchiveAndUnarchiveRoundTrip(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	t.Cleanup(func() { jsonOutput = oldJSON })
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "archive-command", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: source.ID, Connector: "test", Fingerprint: "result",
		MessageIDs: []string{"m1"}, Action: "insight", Status: "processed",
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	archivedJSON := captureStdout(t, func() {
		if err := collectItemArchiveCmd.RunE(collectItemArchiveCmd, []string{item.ID}); err != nil {
			t.Fatalf("archive: %v", err)
		}
	})
	var result struct {
		Archived bool                   `json:"archived"`
		Count    int                    `json:"count"`
		Items    []store.CollectionItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(archivedJSON), &result); err != nil {
		t.Fatalf("decode archive: %v\n%s", err, archivedJSON)
	}
	if !result.Archived || result.Count != 1 || result.Items[0].ArchivedAt == 0 {
		t.Fatalf("unexpected archive result: %+v", result)
	}

	if err := collectItemUnarchiveCmd.RunE(collectItemUnarchiveCmd, []string{item.ID}); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened, err := store.GetCollectionItem(db, item.ID)
	if err != nil || reopened.ArchivedAt != 0 {
		t.Fatalf("reopened item = %+v err=%v", reopened, err)
	}
}
