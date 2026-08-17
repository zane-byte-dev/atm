package store

import "testing"

func TestMigrateV39ToV41AddsCompleteAIDayProjection(t *testing.T) {
	withTempStore(t)
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE ai_day_results`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE ai_day_features`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 39`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open()
	if err != nil {
		t.Fatalf("migrate v39 forward: %v", err)
	}
	defer db.Close()
	var version, tables int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name IN ('ai_day_features','ai_day_results','ai_day_events','ai_day_session_features','ai_day_feature_details','ai_day_badge_days','ai_day_badge_progress','ai_day_feedback','ai_day_sources','ai_day_settings')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	// Compared against SchemaVersion rather than a literal so a later bump does
	// not fail a test that is really about the AI Day tables existing.
	if version != SchemaVersion || tables != 10 {
		t.Fatalf("version=%d tables=%d, want version=%d tables=10", version, tables, SchemaVersion)
	}
	// The projection carries concept provenance from v42 on.
	for _, column := range []string{"evidence_strength", "origin", "computed_badge_id"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('ai_day_results') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("ai_day_results is missing column %q", column)
		}
	}
}
