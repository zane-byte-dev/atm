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
		t.Fatalf("migrate v39 to v41: %v", err)
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
	if version != 41 || tables != 10 {
		t.Fatalf("version=%d tables=%d, want version=41 tables=10", version, tables)
	}
}
