package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"

	_ "modernc.org/sqlite"
)

func Open() (*sql.DB, error) {
	if err := os.MkdirAll(config.AtmDir, 0755); err != nil {
		return nil, err
	}
	// PRAGMAs must ride on the DSN, not a one-off Exec: database/sql pools
	// connections, and a PRAGMA issued via db.Exec applies only to whichever
	// connection served it. Foreign keys in particular have to hold on every
	// connection, because ON DELETE CASCADE is what keeps tags, links and session
	// bindings from outliving their todo.
	dsn := (&url.URL{Scheme: "file", Path: config.AtmDB}).String() +
		"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// ErrDatabaseMissing reports that no database has been created yet. Read paths
// return it instead of creating one, so callers can decide whether an empty
// database means "nothing to show" or "the user needs to run sync".
var ErrDatabaseMissing = errors.New("database does not exist: run `atm sync` first")

// OpenReadOnly opens the existing session database without creating files,
// migrating schemas, changing journal mode, or otherwise mutating state.
// Callers that need fresh session data must explicitly run sync first.
func OpenReadOnly() (*sql.DB, error) {
	if _, err := os.Stat(config.AtmDB); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDatabaseMissing
		}
		return nil, err
	}
	base := (&url.URL{Scheme: "file", Path: config.AtmDB}).String()
	db, err := openReadOnlyDSN(base + "?mode=ro&_pragma=query_only(1)")
	if err == nil {
		return db, nil
	}

	// Sandboxed agents may be allowed to read the database file but not create
	// SQLite lock or shared-memory files in its directory. immutable=1 avoids
	// those side effects and provides a stable snapshot of the last explicit sync.
	// Caveat: immutable=1 ignores the -wal file, so it can serve data from before
	// the most recent (un-checkpointed) commit. Callers in this mode may observe
	// slightly stale todos/bindings until the next checkpoint.
	immutableDB, immutableErr := openReadOnlyDSN(base + "?mode=ro&immutable=1&_pragma=query_only(1)")
	if immutableErr == nil {
		return immutableDB, nil
	}
	return nil, err
}

func openReadOnlyDSN(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	version, created, err := bootstrapSchema(db)
	if err != nil || created {
		return err
	}
	switch {
	case version < minUpgradableVersion:
		return fmt.Errorf("database schema v%d is no longer supported (minimum v%d): "+
			"remove %s and run `atm sync` to rebuild it", version, minUpgradableVersion, config.AtmDB)
	case version > SchemaVersion:
		// An older binary against a newer database: reading it would silently
		// misinterpret columns this build does not know about.
		return fmt.Errorf("database schema v%d is newer than this atm build (v%d): upgrade atm",
			version, SchemaVersion)
	}
	for version < SchemaVersion {
		switch version {
		case 21:
			if err := migrateV21ToV22(db); err != nil {
				return err
			}
			version = 22
		case 22:
			if err := migrateV22ToV23(db); err != nil {
				return err
			}
			version = 23
		case 23:
			if err := migrateV23ToV24(db); err != nil {
				return err
			}
			version = 24
		case 24:
			if err := migrateV24ToV25(db); err != nil {
				return err
			}
			version = 25
		case 25:
			if err := migrateV25ToV26(db); err != nil {
				return err
			}
			version = 26
		case 26:
			if err := migrateV26ToV27(db); err != nil {
				return err
			}
			version = 27
		case 27:
			if err := migrateV27ToV28(db); err != nil {
				return err
			}
			version = 28
		case 28:
			if err := migrateV28ToV29(db); err != nil {
				return err
			}
			version = 29
		case 29:
			if err := migrateV29ToV30(db); err != nil {
				return err
			}
			version = 30
		case 30:
			if err := migrateV30ToV31(db); err != nil {
				return err
			}
			version = 31
		default:
			return fmt.Errorf("missing migration from schema v%d", version)
		}
	}
	return nil
}

// migrateV21ToV22 adds usage_events.duration_ms so request throughput can be
// measured. Existing rows keep 0 — the transcripts they came from were already
// consumed, so their duration is not recoverable and the speed queries skip
// them rather than reporting an invented number.
func migrateV21ToV22(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE usage_events ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0`); err != nil {
		// Idempotent if a previous attempt added the column but failed to bump version.
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 22`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV22ToV23 lets dashboard event-time windows seek directly to the
// requested timestamps. Without it, every refresh scans the full request
// history even when the UI only asks for today's sessions.
func migrateV22ToV23(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_usage_events_ts ON usage_events(ts)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 23`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV23ToV24 adds quota_history. It starts empty: a rate-limit source only
// reports the present, so there is no past to backfill and the first trend
// appears once two syncs have run.
func migrateV23ToV24(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS quota_history (
		agent          TEXT NOT NULL,
		window_minutes INTEGER NOT NULL,
		used_percent   REAL NOT NULL,
		resets_at      INTEGER NOT NULL DEFAULT 0,
		ts             INTEGER NOT NULL,
		PRIMARY KEY (agent, window_minutes, ts)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_quota_history_lookup
		ON quota_history(agent, window_minutes, ts)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 24`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV24ToV25 adds the automatic collection ledger. Nothing is backfilled:
// the first connector run starts from its configured lookback and owns its
// checkpoint from then on.
func migrateV24ToV25(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS collection_sources (
			id TEXT PRIMARY KEY, connector TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('group','user','contact')),
			external_id TEXT NOT NULL, name TEXT NOT NULL DEFAULT '',
			project TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT 'P2' CHECK (priority IN ('P0','P1','P2','P3')),
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			UNIQUE (connector, kind, external_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_sources_connector ON collection_sources(connector,enabled,name)`,
		`CREATE TABLE IF NOT EXISTS collection_checkpoints (
			source_id TEXT PRIMARY KEY REFERENCES collection_sources(id) ON DELETE CASCADE,
			cursor_time INTEGER NOT NULL DEFAULT 0, cursor TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS collection_runs (
			id TEXT PRIMARY KEY, connector TEXT NOT NULL, source_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('running','succeeded','failed')),
			started_at INTEGER NOT NULL, finished_at INTEGER NOT NULL DEFAULT 0,
			fetched_count INTEGER NOT NULL DEFAULT 0, analyzed_count INTEGER NOT NULL DEFAULT 0,
			created_count INTEGER NOT NULL DEFAULT 0, appended_count INTEGER NOT NULL DEFAULT 0,
			ignored_count INTEGER NOT NULL DEFAULT 0, failed_count INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_runs_started ON collection_runs(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS collection_items (
			id TEXT PRIMARY KEY, source_id TEXT NOT NULL, connector TEXT NOT NULL,
			conversation_id TEXT NOT NULL DEFAULT '', fingerprint TEXT NOT NULL,
			message_ids TEXT NOT NULL DEFAULT '[]', sender TEXT NOT NULL DEFAULT '',
			occurred_at INTEGER NOT NULL DEFAULT 0, raw_context TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL DEFAULT 'pending' CHECK (action IN ('pending','create','append','ignore','failed','reverted')),
			title TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '',
			item_type TEXT NOT NULL DEFAULT '', project TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
			confidence REAL NOT NULL DEFAULT 0,
			todo_id TEXT REFERENCES todos(id) ON DELETE SET NULL,
			status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processed','failed')),
			error TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
			UNIQUE (connector, fingerprint)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_items_updated ON collection_items(updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_items_source ON collection_items(source_id,occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_items_todo ON collection_items(todo_id)`,
		`UPDATE schema_version SET version = 25`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV25ToV26 adds source-level message exclusions. Some development v25
// databases were created after the column was added to the fresh schema but
// before the version was bumped, so inspect the table instead of assuming the
// column is absent.
func migrateV25ToV26(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`PRAGMA table_info(collection_sources)`)
	if err != nil {
		return err
	}
	hasExcludePattern := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "exclude_pattern" {
			hasExcludePattern = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasExcludePattern {
		if _, err := tx.Exec(`ALTER TABLE collection_sources
			ADD COLUMN exclude_pattern TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 26`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV26ToV27 adds the synced chat archive. Existing databases start empty:
// past collection runs only kept the excerpt each decision was made from; the
// connector remains the authoritative source for the rest.
func migrateV26ToV27(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Some development v26 databases were created after the table was added to
	// the fresh schema but before the version was bumped.
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS collection_messages (
			connector         TEXT NOT NULL,
			conversation_id   TEXT NOT NULL,
			message_id        TEXT NOT NULL,
			source_id         TEXT NOT NULL DEFAULT '',
			conversation_name TEXT NOT NULL DEFAULT '',
			sender            TEXT NOT NULL DEFAULT '',
			created_at        INTEGER NOT NULL DEFAULT 0,
			content           TEXT NOT NULL DEFAULT '',
			synced_at         INTEGER NOT NULL,
			PRIMARY KEY (connector, conversation_id, message_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_messages_time
			ON collection_messages(conversation_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_messages_created
			ON collection_messages(created_at DESC)`,
		`UPDATE schema_version SET version = 27`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV27ToV28 adds collection_items.proposed_action, which holds what an
// on-demand analysis decided but has not carried out, and
// collection_sources.instruction, which says what a source is watched for.
// Existing rows get ”: every decision made before this column existed was
// applied when it was made, and every source was classified by the generic
// prompt.
func migrateV27ToV28(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Some development v27 databases were created after the column was added to
	// the fresh schema but before the version was bumped.
	hasProposedAction, err := tableHasColumn(tx, "collection_items", "proposed_action")
	if err != nil {
		return err
	}
	if !hasProposedAction {
		// SQLite cannot add a CHECK constraint to an existing table, and rebuilding
		// collection_items would drop its todo_id reference. The write paths in
		// collection.go are the only producers, so they hold the same contract.
		if _, err := tx.Exec(`ALTER TABLE collection_items
			ADD COLUMN proposed_action TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasInstruction, err := tableHasColumn(tx, "collection_sources", "instruction")
	if err != nil {
		return err
	}
	if !hasInstruction {
		if _, err := tx.Exec(`ALTER TABLE collection_sources
			ADD COLUMN instruction TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 28`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV28ToV29 makes collection intent and cost explicit per source.
// Existing sources keep the historical behaviour: task extraction every five
// minutes. People can opt noisy social groups into low-frequency observation.
func migrateV28ToV29(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []struct {
		name string
		sql  string
	}{
		{"strategy", `ALTER TABLE collection_sources ADD COLUMN strategy TEXT NOT NULL DEFAULT 'tasks'`},
		{"interval_minutes", `ALTER TABLE collection_sources ADD COLUMN interval_minutes INTEGER NOT NULL DEFAULT 5`},
	} {
		hasColumn, err := tableHasColumn(tx, "collection_sources", column.name)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := tx.Exec(column.sql); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 29`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV29ToV30 gives collection a second destination. Until now every batch
// either became a Todo or was dropped, so chat that was worth remembering but
// was not work had to be forced into one of those. This adds the 'insight'
// decision and the per-source daily digest it feeds.
//
// Relaxing the action CHECK means rebuilding collection_items: SQLite cannot
// alter a constraint in place. This is the first migration here that does that,
// so it copies every column explicitly and recreates the indexes — the items
// table is the audit trail for why each Todo exists and has nowhere to rebuild
// from if a row is lost. Existing rows keep their decision verbatim; nothing is
// reclassified as an insight retroactively, because that judgement was never
// made for them.
func migrateV29ToV30(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []struct {
		table string
		name  string
		sql   string
	}{
		{"collection_sources", "knowledge_collection",
			`ALTER TABLE collection_sources ADD COLUMN knowledge_collection TEXT NOT NULL DEFAULT ''`},
		{"collection_runs", "insight_count",
			`ALTER TABLE collection_runs ADD COLUMN insight_count INTEGER NOT NULL DEFAULT 0`},
	} {
		hasColumn, err := tableHasColumn(tx, column.table, column.name)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := tx.Exec(column.sql); err != nil {
				return err
			}
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS collection_digests (
			source_id       TEXT NOT NULL,
			digest_date     TEXT NOT NULL CHECK (digest_date GLOB '` + datePattern + `'),
			document_id     TEXT NOT NULL,
			collection      TEXT NOT NULL DEFAULT '',
			title           TEXT NOT NULL DEFAULT '',
			item_count      INTEGER NOT NULL DEFAULT 0,
			covered_through INTEGER NOT NULL DEFAULT 0,
			created_at      INTEGER NOT NULL,
			updated_at      INTEGER NOT NULL,
			PRIMARY KEY (source_id, digest_date)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_collection_digests_updated ON collection_digests(updated_at DESC)`,
	}
	// Skip the rebuild on a database that was created from the fresh v30 schema
	// before this version bump landed.
	relaxed, err := tableDefinitionContains(tx, "collection_items", "'insight'")
	if err != nil {
		return err
	}
	if !relaxed {
		statements = append(statements,
			`CREATE TABLE collection_items_v30 (
				id              TEXT PRIMARY KEY,
				source_id       TEXT NOT NULL,
				connector       TEXT NOT NULL,
				conversation_id TEXT NOT NULL DEFAULT '',
				fingerprint     TEXT NOT NULL,
				message_ids     TEXT NOT NULL DEFAULT '[]',
				sender          TEXT NOT NULL DEFAULT '',
				occurred_at     INTEGER NOT NULL DEFAULT 0,
				raw_context     TEXT NOT NULL DEFAULT '',
				action          TEXT NOT NULL DEFAULT 'pending'
					CHECK (action IN ('pending','create','append','insight','ignore','failed','reverted')),
				proposed_action TEXT NOT NULL DEFAULT ''
					CHECK (proposed_action IN ('','create','append')),
				title           TEXT NOT NULL DEFAULT '',
				summary         TEXT NOT NULL DEFAULT '',
				item_type       TEXT NOT NULL DEFAULT '',
				project         TEXT NOT NULL DEFAULT '',
				priority        TEXT NOT NULL DEFAULT '',
				reason          TEXT NOT NULL DEFAULT '',
				confidence      REAL NOT NULL DEFAULT 0,
				todo_id         TEXT REFERENCES todos(id) ON DELETE SET NULL,
				status          TEXT NOT NULL DEFAULT 'pending'
					CHECK (status IN ('pending','processed','failed')),
				error           TEXT NOT NULL DEFAULT '',
				created_at      INTEGER NOT NULL,
				updated_at      INTEGER NOT NULL,
				UNIQUE (connector, fingerprint)
			)`,
			`INSERT INTO collection_items_v30
				(id,source_id,connector,conversation_id,fingerprint,message_ids,sender,occurred_at,
				 raw_context,action,proposed_action,title,summary,item_type,project,priority,reason,
				 confidence,todo_id,status,error,created_at,updated_at)
			 SELECT id,source_id,connector,conversation_id,fingerprint,message_ids,sender,occurred_at,
				 raw_context,action,proposed_action,title,summary,item_type,project,priority,reason,
				 confidence,todo_id,status,error,created_at,updated_at FROM collection_items`,
			`DROP TABLE collection_items`,
			`ALTER TABLE collection_items_v30 RENAME TO collection_items`,
			`CREATE INDEX IF NOT EXISTS idx_collection_items_updated ON collection_items(updated_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_collection_items_source ON collection_items(source_id,occurred_at DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_collection_items_todo ON collection_items(todo_id)`,
		)
	}
	statements = append(statements, `UPDATE schema_version SET version = 30`)
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	// The rebuild carried todo_id across by value; refuse to commit a ledger that
	// now points at Todos which do not exist.
	if !relaxed {
		var orphans int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM collection_items
			WHERE todo_id IS NOT NULL AND todo_id NOT IN (SELECT id FROM todos)`).Scan(&orphans); err != nil {
			return err
		}
		if orphans > 0 {
			return fmt.Errorf("collection_items rebuild left %d row(s) pointing at missing todos", orphans)
		}
	}
	return tx.Commit()
}

// migrateV30ToV31 lets connectors define their own source kinds. Earlier
// schemas embedded one connector's group/user vocabulary in a CHECK constraint,
// which meant a correctly registered connector could still not persist a
// source such as a Slack channel or GitHub repository.
//
// collection_checkpoints is copied out and rebuilt with the parent table. This
// avoids ON DELETE CASCADE erasing checkpoints while SQLite replaces
// collection_sources to relax the constraint.
func migrateV30ToV31(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	restricted, err := tableDefinitionContains(tx, "collection_sources", "'group','user'")
	if err != nil {
		return err
	}
	if restricted {
		statements := []string{
			`CREATE TABLE collection_checkpoints_v31 (
				source_id   TEXT PRIMARY KEY,
				cursor_time INTEGER NOT NULL DEFAULT 0,
				cursor      TEXT NOT NULL DEFAULT '',
				updated_at  INTEGER NOT NULL DEFAULT 0
			)`,
			`INSERT INTO collection_checkpoints_v31 (source_id,cursor_time,cursor,updated_at)
			 SELECT source_id,cursor_time,cursor,updated_at FROM collection_checkpoints`,
			`DROP TABLE collection_checkpoints`,
			`CREATE TABLE collection_sources_v31 (
				id                   TEXT PRIMARY KEY,
				connector            TEXT NOT NULL,
				kind                 TEXT NOT NULL,
				external_id          TEXT NOT NULL,
				name                 TEXT NOT NULL DEFAULT '',
				project              TEXT NOT NULL DEFAULT '',
				exclude_pattern      TEXT NOT NULL DEFAULT '',
				instruction          TEXT NOT NULL DEFAULT '',
				knowledge_collection TEXT NOT NULL DEFAULT '',
				strategy             TEXT NOT NULL DEFAULT 'tasks'
					CHECK (strategy IN ('tasks','observe')),
				interval_minutes     INTEGER NOT NULL DEFAULT 5
					CHECK (interval_minutes BETWEEN 1 AND 1440),
				priority             TEXT NOT NULL DEFAULT 'P2'
					CHECK (priority IN ('P0','P1','P2','P3')),
				enabled              INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
				created_at           INTEGER NOT NULL,
				updated_at           INTEGER NOT NULL,
				UNIQUE (connector, kind, external_id)
			)`,
			`INSERT INTO collection_sources_v31
				(id,connector,kind,external_id,name,project,exclude_pattern,instruction,
				 knowledge_collection,strategy,interval_minutes,priority,enabled,created_at,updated_at)
			 SELECT id,connector,kind,external_id,name,project,exclude_pattern,instruction,
				 knowledge_collection,strategy,interval_minutes,priority,enabled,created_at,updated_at
			 FROM collection_sources`,
			`DROP TABLE collection_sources`,
			`ALTER TABLE collection_sources_v31 RENAME TO collection_sources`,
			`CREATE INDEX idx_collection_sources_connector ON collection_sources(connector,enabled,name)`,
			`CREATE TABLE collection_checkpoints (
				source_id   TEXT PRIMARY KEY REFERENCES collection_sources(id) ON DELETE CASCADE,
				cursor_time INTEGER NOT NULL DEFAULT 0,
				cursor      TEXT NOT NULL DEFAULT '',
				updated_at  INTEGER NOT NULL DEFAULT 0
			)`,
			`INSERT INTO collection_checkpoints (source_id,cursor_time,cursor,updated_at)
			 SELECT source_id,cursor_time,cursor,updated_at FROM collection_checkpoints_v31`,
			`DROP TABLE collection_checkpoints_v31`,
		}
		for _, statement := range statements {
			if _, err := tx.Exec(statement); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 31`); err != nil {
		return err
	}
	return tx.Commit()
}

// tableHasColumn reports whether a column already exists, so a migration can run
// against a database that was created from the fresh schema mid-development.
func tableHasColumn(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// tableDefinitionContains reports whether a table's stored CREATE statement
// mentions a fragment. Constraints, unlike columns, cannot be inspected through
// pragma_table_info, so this is how a migration recognises a CHECK it has
// already relaxed — or one a fresh-schema database was born with.
func tableDefinitionContains(tx *sql.Tx, table, fragment string) (bool, error) {
	var definition string
	err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&definition)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(definition, fragment), nil
}

// bootstrapSchema reads the schema version and, on an empty database, creates the
// whole schema. Both happen in one transaction whose first statement is a write,
// so the transaction holds SQLite's write lock before it reads the version: two
// processes starting against the same fresh database serialise, instead of both
// deciding it is empty and racing to create the same tables.
func bootstrapSchema(db *sql.DB) (version int, created bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return 0, false, err
	}
	if tx.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version) != nil {
		version = 0
	}
	if version > 0 {
		return version, false, nil
	}
	if err := createSchema(tx); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return SchemaVersion, true, nil
}
