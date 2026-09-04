package store

import (
	"database/sql"
	"fmt"
	"sort"
)

// InstallWorkspaceChangeTracking records content changes in the same transaction
// as every writer, including standalone CLI processes. CLI telemetry is not a
// reason to invalidate unrelated workspaces.
func InstallWorkspaceChangeTracking(tx *sql.Tx) error {
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS workspace_changes (domain TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return err
	}
	domains := map[string][]string{
		"todos":      {"todos", "todo_tags", "todo_dependencies", "todo_links", "todo_images", "todo_session_bindings", "todo_plan_revisions"},
		"sessions":   {"sessions", "messages", "tools", "sync_state", "sync_health", "session_reviews"},
		"usage":      {"usage", "usage_events", "skill_events", "quota_history"},
		"collection": {"collection_sources", "collection_checkpoints", "collection_runs", "collection_items", "collection_digests", "collection_messages"},
		"day":        {"ai_day_features", "ai_day_results", "ai_day_events", "ai_day_session_features", "ai_day_feature_details", "ai_day_badge_days", "ai_day_badge_progress", "ai_day_feedback", "ai_day_sources", "ai_day_settings"},
		"memory":     {"memory_events", "memory_event_tags", "memory_event_metadata"},
		"knowledge":  {"knowledge_feedback"},
		"jobs":       {"background_jobs"},
	}
	keys := make([]string, 0, len(domains))
	for key := range domains {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, domain := range keys {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO workspace_changes(domain,revision) VALUES (?,0)`, domain); err != nil {
			return err
		}
		for _, table := range domains[domain] {
			var exists int
			if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				continue
			}
			for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
				// All identifiers here are compile-time constants, never caller input.
				statement := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS workspace_%s_%s AFTER %s ON %s BEGIN UPDATE workspace_changes SET revision=revision+1 WHERE domain='%s'; END`, table, operation, operation, table, domain)
				if _, err := tx.Exec(statement); err != nil {
					return fmt.Errorf("track %s: %w", table, err)
				}
			}
		}
	}
	return nil
}
