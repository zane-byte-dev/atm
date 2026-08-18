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

	// The App's save path is `collect source add`, so an edit reaches the same
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

// An upgraded database must behave exactly as it did before the column existed:
// every source it already had keeps notifying.
func TestMigrateV45ToV46LeavesExistingSourcesNotifying(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertCollectionSource(db, CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-legacy",
		Name: "老来源", Priority: "P2", Enabled: true,
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	// SQLite cannot drop a column inside this schema, so the pre-v46 shape is
	// rebuilt as the migration will find it: rows present, column absent.
	if _, err := db.Exec(`ALTER TABLE collection_sources DROP COLUMN muted`); err != nil {
		t.Fatalf("drop muted column: %v", err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 45`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v45 forward: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}
	sources, err := ListCollectionSources(db, "", false)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(sources))
	}
	if sources[0].Muted {
		t.Fatalf("migration muted an existing source: %+v", sources[0])
	}
	if err := SetCollectionSourceMuted(db, sources[0].ID, true); err != nil {
		t.Fatalf("mute against migrated table: %v", err)
	}
}
