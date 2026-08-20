package cmd

import (
	"encoding/json"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectItemArchiveAndUnarchiveRoundTrip(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
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

// TestCollectItemArchiveAllSettlesOnlyReadUnsavedConclusions pins the sweep's
// blast radius. --all exists so read conclusions stop piling up in the main
// list, and the reason it is safe to press is that everything still owing an
// action — an unread conclusion, an open follow-up, a saved insight already
// filed away — is outside it. A regression here is silent: the list would just
// look tidier than the work actually is.
func TestCollectItemArchiveAllSettlesOnlyReadUnsavedConclusions(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON := jsonOutput
	oldAll := collectItemArchiveAll
	t.Cleanup(func() { jsonOutput = oldJSON; collectItemArchiveAll = oldAll })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "settle-all", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	put := func(fingerprint string, item store.CollectionItem) store.CollectionItem {
		item.SourceID, item.Connector, item.Fingerprint = source.ID, "test", fingerprint
		item.MessageIDs = []string{fingerprint}
		if item.Status == "" {
			item.Status = "processed"
		}
		stored, _, err := store.PutCollectionItem(db, item)
		if err != nil {
			db.Close()
			t.Fatalf("put %s: %v", fingerprint, err)
		}
		return stored
	}
	readConclusion := put("read-unsaved", store.CollectionItem{Action: "insight", ReadAt: 1000})
	unreadConclusion := put("unread-unsaved", store.CollectionItem{Action: "insight"})
	savedConclusion := put("read-saved", store.CollectionItem{
		Action: "insight", ReadAt: 1000, KnowledgeDocumentID: "kd_1",
	})
	openFollowup := put("read-create", store.CollectionItem{Action: "create", ReadAt: 1000})
	ignored := put("read-ignore", store.CollectionItem{Action: "ignore", ReadAt: 1000})

	overview, err := store.LoadCollectionOverview(db, 50)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if overview.Summary.Settleable != 1 {
		t.Fatalf("settleable count = %d, want 1", overview.Summary.Settleable)
	}

	jsonOutput = true
	collectItemArchiveAll = true
	settledJSON := captureStdout(t, func() {
		if err := collectItemArchiveCmd.RunE(collectItemArchiveCmd, nil); err != nil {
			t.Fatalf("archive --all: %v", err)
		}
	})
	var result struct {
		Archived bool `json:"archived"`
		Count    int  `json:"count"`
	}
	if err := json.Unmarshal([]byte(settledJSON), &result); err != nil {
		t.Fatalf("decode settle: %v\n%s", err, settledJSON)
	}
	if !result.Archived || result.Count != 1 {
		t.Fatalf("unexpected settle result: %+v", result)
	}

	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	settled, err := store.GetCollectionItem(db, readConclusion.ID)
	if err != nil || settled.ArchivedAt == 0 {
		t.Fatalf("read conclusion not settled: %+v err=%v", settled, err)
	}
	// The sweep must not manufacture its own eligibility: read state is the
	// precondition, so an unread record staying unread is part of the contract.
	for _, spared := range []store.CollectionItem{unreadConclusion, savedConclusion, openFollowup, ignored} {
		kept, err := store.GetCollectionItem(db, spared.ID)
		if err != nil {
			t.Fatalf("reload %s: %v", spared.ID, err)
		}
		if kept.ArchivedAt != 0 {
			t.Fatalf("%s (%s) was settled by --all", spared.ID, spared.Action)
		}
		if kept.ReadAt != spared.ReadAt {
			t.Fatalf("%s read state changed: %d -> %d", spared.ID, spared.ReadAt, kept.ReadAt)
		}
	}
	// Pressing it again is a no-op rather than a second sweep of the same rows.
	again, err := store.CountSettleableCollectionItems(db)
	if err != nil || again != 0 {
		t.Fatalf("settleable after sweep = %d err=%v, want 0", again, err)
	}
}

// TestCollectItemArchiveAllRejectsItemIDs keeps --all and explicit IDs from
// being combined, where the flag would silently widen a two-record cleanup into
// the whole ledger.
func TestCollectItemArchiveAllRejectsItemIDs(t *testing.T) {
	oldAll := collectItemArchiveAll
	t.Cleanup(func() { collectItemArchiveAll = oldAll })
	collectItemArchiveAll = true
	if err := collectItemArchiveCmd.Args(collectItemArchiveCmd, []string{"ci_1"}); err == nil {
		t.Fatal("archive --all ci_1 was accepted")
	}
	collectItemArchiveAll = false
	if err := collectItemArchiveCmd.Args(collectItemArchiveCmd, nil); err == nil {
		t.Fatal("archive with no ids and no --all was accepted")
	}
}
