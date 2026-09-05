package store

import "testing"

func TestFreshSchemaUsesCurrentBaselineWithoutUsageRollups(t *testing.T) {
	db := openTempDB(t)
	defer db.Close()

	var version, rollupTables int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name IN
		('usage_hourly_rollups','usage_daily_rollups','usage_rollup_state')`).Scan(&rollupTables); err != nil {
		t.Fatal(err)
	}
	if version != SchemaVersion || rollupTables != 0 {
		t.Fatalf("version=%d rollup tables=%d, want %d/0", version, rollupTables, SchemaVersion)
	}
}
