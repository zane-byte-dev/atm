package store

import "testing"

// Muting is a notification setting, not a collection setting: it must survive the
// upsert that `collect source add` runs for every edit, and it must leave the
// unread ledger alone. Both are what makes it different from disabling a source.
func TestCollectionSourceMuteOnlySilencesNotifications(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	source, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-mute",
		Name: "吵闹群", Project: "atm", Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatalf("upsert source: %v", err)
	}
	if source.Muted {
		t.Fatalf("a new source must notify: %+v", source)
	}

	if err := SetCollectionSourceMuted(db, source.ID, true); err != nil {
		t.Fatalf("mute source: %v", err)
	}
	muted, err := GetCollectionSource(db, source.ID)
	if err != nil {
		t.Fatalf("read muted source: %v", err)
	}
	if !muted.Muted {
		t.Fatalf("mute did not persist: %+v", muted)
	}
	if !muted.Enabled {
		t.Fatalf("mute must not pause collection: %+v", muted)
	}

	// The browser's save path is `collect source add`, so an edit reaches the same
	// upsert. If it carried muted through excluded.muted, changing an interval
	// would put a silenced group back into the notifications.
	edited, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-mute",
		Name: "吵闹群", Project: "atm", Priority: "P1", Enabled: true, IntervalMinutes: 30,
	})
	if err != nil {
		t.Fatalf("edit source: %v", err)
	}
	if !edited.Muted {
		t.Fatalf("editing a source cleared its mute: %+v", edited)
	}
	if edited.IntervalMinutes != 30 || edited.Priority != "P1" {
		t.Fatalf("edit did not apply: %+v", edited)
	}

	// Unread is deliberately untouched by muting: the banner is suppressed, the
	// work is not.
	item, _, err := PutCollectionItem(db, CollectionItem{
		SourceID: source.ID, Connector: "test", Fingerprint: "fp-muted",
		Action: "insight", Title: "结论", Status: "processed",
	})
	if err != nil {
		t.Fatalf("put item: %v", err)
	}
	if item.ReadAt != 0 {
		t.Fatalf("a new result must start unread: %+v", item)
	}
	overview, err := LoadCollectionOverview(db, 10)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if overview.Summary.Unread != 1 {
		t.Fatalf("unread = %d, want 1 for a muted source", overview.Summary.Unread)
	}

	if err := SetCollectionSourceMuted(db, source.ID, false); err != nil {
		t.Fatalf("unmute source: %v", err)
	}
	restored, err := GetCollectionSource(db, source.ID)
	if err != nil {
		t.Fatalf("read unmuted source: %v", err)
	}
	if restored.Muted {
		t.Fatalf("unmute did not persist: %+v", restored)
	}
	if err := SetCollectionSourceMuted(db, "cs_missing", true); err == nil {
		t.Fatal("muting an unknown source must report not found")
	}
}
