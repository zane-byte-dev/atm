package store

import "testing"

func TestMigrateV44ToV45AddsTheOutboundActionGateLedger(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE approvals`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 44`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v44 forward: %v", err)
	}
	defer db.Close()

	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	// Compared against SchemaVersion rather than a literal so a later bump does
	// not fail a test that is really about the approvals table existing.
	if version != SchemaVersion {
		t.Fatalf("version = %d, want %d", version, SchemaVersion)
	}

	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name IN (
			'idx_approvals_status_requested','idx_approvals_dedup','idx_approvals_pending_dedup')`).
		Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 3 {
		t.Fatalf("indexes = %d, want 3", indexes)
	}

	// The migrated table must carry the same claim as the fresh schema, or an
	// upgraded database would let a retrying agent raise a second banner.
	now := int64(1_700_000_000)
	if _, err := CreateApproval(db, sendRequest(now)); err != nil {
		t.Fatalf("create against migrated table: %v", err)
	}
	if _, err := CreateApproval(db, sendRequest(now+1)); err == nil {
		t.Fatal("migrated table accepted a second pending request for one command")
	}
}
