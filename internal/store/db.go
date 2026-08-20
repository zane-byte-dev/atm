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
	// The common path must stay read-only. bootstrapSchema deliberately starts
	// with a write so fresh databases and real migrations serialize across
	// processes, but doing that on every Open makes unrelated short-lived writers
	// contend just to rediscover that the schema is already current. In
	// particular, concurrent memory/review commands could exhaust SQLite's busy
	// window before reaching their actual INSERT. A current schema needs no lock.
	var current int
	if err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&current); err == nil && current == SchemaVersion {
		return nil
	}

	version, created, err := bootstrapSchema(db)
	if err != nil || created {
		return err
	}
	switch {
	case version < minUpgradableVersion:
		// Deleting the database is the last step, not the first: sessions rebuild
		// from agent transcripts, but todos, memory, knowledge, the collection
		// ledger and the review cursor are this database's own records and have
		// nowhere to rebuild from. `atm backup` reads a schema this old precisely
		// because it never takes this path.
		return fmt.Errorf("database schema v%d is no longer supported (minimum v%d): "+
			"run `atm backup` first to keep your todos, memory and knowledge, "+
			"then remove %s and run `atm sync` to rebuild the session index",
			version, minUpgradableVersion, config.AtmDB)
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
		case 31:
			if err := migrateV31ToV32(db); err != nil {
				return err
			}
			version = 32
		case 32:
			if err := migrateV32ToV33(db); err != nil {
				return err
			}
			version = 33
		case 33:
			if err := migrateV33ToV34(db); err != nil {
				return err
			}
			version = 34
		case 34:
			if err := migrateV34ToV35(db); err != nil {
				return err
			}
			version = 35
		case 35:
			if err := migrateV35ToV36(db); err != nil {
				return err
			}
			version = 36
		case 36:
			if err := migrateV36ToV37(db); err != nil {
				return err
			}
			version = 37
		case 37:
			if err := migrateV37ToV38(db); err != nil {
				return err
			}
			version = 38
		case 38:
			if err := migrateV38ToV39(db); err != nil {
				return err
			}
			version = 39
		case 39:
			if err := migrateV39ToV40(db); err != nil {
				return err
			}
			version = 40
		case 40:
			if err := migrateV40ToV41(db); err != nil {
				return err
			}
			version = 41
		case 41:
			if err := migrateV41ToV42(db); err != nil {
				return err
			}
			version = 42
		case 42:
			if err := migrateV42ToV43(db); err != nil {
				return err
			}
			version = 43
		case 43:
			if err := migrateV43ToV44(db); err != nil {
				return err
			}
			version = 44
		case 44:
			if err := migrateV44ToV45(db); err != nil {
				return err
			}
			version = 45
		case 45:
			if err := migrateV45ToV46(db); err != nil {
				return err
			}
			version = 46
		case 46:
			if err := migrateV46ToV47(db); err != nil {
				return err
			}
			version = 47
		case 47:
			if err := migrateV47ToV48(db); err != nil {
				return err
			}
			version = 48
		case 48:
			if err := migrateV48ToV49(db); err != nil {
				return err
			}
			version = 49
		case 49:
			if err := migrateV49ToV50(db); err != nil {
				return err
			}
			version = 50
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

// migrateV31ToV32 lets a source say what one decision covers. Batching by "same
// conversation, gaps under fifteen minutes" suits chat, where a request and the
// clarifications after it are one piece of work; a notification feed is the
// opposite, and since a batch yields exactly one decision, grouping silently
// dropped every event but one. Existing sources keep the window behaviour.
func migrateV31ToV32(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hasColumn, err := tableHasColumn(tx, "collection_sources", "decision_unit")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := tx.Exec(`ALTER TABLE collection_sources
			ADD COLUMN decision_unit TEXT NOT NULL DEFAULT 'window'
				CHECK (decision_unit IN ('window','message'))`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 32`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV32ToV33 records who filed a todo. Existing rows keep an empty creator
// rather than a guessed one: the free-text source they carry says why the work
// exists, not who typed it, so reading "me" or "claude" out of it would invent a
// fact the database never had.
func migrateV32ToV33(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hasColumn, err := tableHasColumn(tx, "todos", "creator")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := tx.Exec(`ALTER TABLE todos ADD COLUMN creator TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 33`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV33ToV34 bounds automatic retries of a failed collection batch.
// Existing failed items start at zero attempts rather than an invented count:
// the ledger never recorded how many runs already tried them, and guessing high
// would silently retire items that a working connector would now process. They
// get a full budget from here, which costs at most MaxCollectionAttempts runs
// once.
func migrateV33ToV34(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hasColumn, err := tableHasColumn(tx, "collection_items", "attempts")
	if err != nil {
		return err
	}
	if !hasColumn {
		if _, err := tx.Exec(`ALTER TABLE collection_items
			ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 34`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV34ToV35 drops two columns from todos: lane and feature_path.
//
// The work/personal lane never earned a
// column: nothing branched on it, the value was derived rather than chosen (the
// desktop app filled in whichever lane already dominated the project, the
// collector hardcoded "work"), and the only reader was an optional --lane
// filter on `todo list` and `now`. Todos already have a tag table for
// cross-cutting marks, so the lane was a second, weaker copy of it.
//
// The existing labels are dropped rather than migrated into tags: within the
// one project holding most of the todos they disagreed with each other for the
// same kind of work, so there is no classification in them worth carrying
// forward.
//
// feature_path is the other half: nothing has read or written it for as long as
// it has existed, so every row holds NULL. It outlived its usefulness only
// because dropping one unused column did not justify a schema bump on its own.
//
// Neither column appears in an index, view, or constraint, so both go without
// rewriting the table.
func migrateV34ToV35(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []string{"lane", "feature_path"} {
		hasColumn, err := tableHasColumn(tx, "todos", column)
		if err != nil {
			return err
		}
		if !hasColumn {
			continue
		}
		if _, err := tx.Exec(`ALTER TABLE todos DROP COLUMN ` + column); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 35`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV35ToV36 adds the durable execution record used by `atm todo run`.
// Nothing is backfilled: v35 deliberately had no detached Agent execution, so
// there are no truthful historical runs to invent.
func migrateV35ToV36(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS task_runs (
			id         TEXT PRIMARY KEY,
			todo_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			agent      TEXT NOT NULL,
			project    TEXT NOT NULL DEFAULT '',
			work_dir   TEXT NOT NULL,
			prompt     TEXT NOT NULL DEFAULT '',
			policy     TEXT NOT NULL CHECK (policy IN ('guarded','trusted')),
			log_path   TEXT NOT NULL,
			status     TEXT NOT NULL CHECK (status IN ('starting','running','completed','failed')),
			pid        INTEGER NOT NULL DEFAULT 0,
			start_ts   INTEGER NOT NULL,
			end_ts     INTEGER,
			exit_code  INTEGER,
			message    TEXT NOT NULL DEFAULT '',
			session_id TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_runs_todo_started ON task_runs(todo_id, start_ts DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_task_runs_active_todo ON task_runs(todo_id)
			WHERE status IN ('starting','running')`,
		`UPDATE schema_version SET version = 36`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV36ToV37 adds the explicit collection-to-Agent handoff policy and its
// audit result. Existing sources remain collection-only, preserving the
// authority they had before this feature existed; existing items have no
// dispatch outcome because no automatic dispatch was attempted.
func migrateV36ToV37(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	columns := []struct {
		table, name, statement string
	}{
		{"collection_sources", "auto_dispatch", `ALTER TABLE collection_sources
			ADD COLUMN auto_dispatch INTEGER NOT NULL DEFAULT 0 CHECK (auto_dispatch IN (0,1))`},
		{"collection_items", "dispatch_status", `ALTER TABLE collection_items
			ADD COLUMN dispatch_status TEXT NOT NULL DEFAULT ''
			CHECK (dispatch_status IN ('','pending','dispatched','failed'))`},
		{"collection_items", "dispatch_error", `ALTER TABLE collection_items
			ADD COLUMN dispatch_error TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range columns {
		hasColumn, err := tableHasColumn(tx, column.table, column.name)
		if err != nil {
			return err
		}
		if !hasColumn {
			if _, err := tx.Exec(column.statement); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE collection_sources SET auto_dispatch=0 WHERE strategy='observe'`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 37`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV37ToV38 gives user-requested interruption its own durable outcome.
// SQLite cannot alter a CHECK constraint in place, so rebuild just this table
// while preserving every run and recreating its two indexes.
func migrateV37ToV38(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`ALTER TABLE task_runs RENAME TO task_runs_v37`,
		`CREATE TABLE task_runs (
			id         TEXT PRIMARY KEY,
			todo_id    TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
			agent      TEXT NOT NULL,
			project    TEXT NOT NULL DEFAULT '',
			work_dir   TEXT NOT NULL,
			prompt     TEXT NOT NULL DEFAULT '',
			policy     TEXT NOT NULL CHECK (policy IN ('guarded','trusted')),
			log_path   TEXT NOT NULL,
			status     TEXT NOT NULL CHECK (status IN ('starting','running','completed','failed','interrupted')),
			pid        INTEGER NOT NULL DEFAULT 0,
			start_ts   INTEGER NOT NULL,
			end_ts     INTEGER,
			exit_code  INTEGER,
			message    TEXT NOT NULL DEFAULT '',
			session_id TEXT
		)`,
		`INSERT INTO task_runs
			(id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,end_ts,exit_code,message,session_id)
		 SELECT id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,end_ts,exit_code,message,session_id
		 FROM task_runs_v37`,
		`DROP TABLE task_runs_v37`,
		`CREATE INDEX idx_task_runs_todo_started ON task_runs(todo_id, start_ts DESC)`,
		`CREATE UNIQUE INDEX idx_task_runs_active_todo ON task_runs(todo_id)
			WHERE status IN ('starting','running')`,
		`UPDATE schema_version SET version = 38`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV38ToV39 separates "the thread this run was told to continue" from
// "the session this run turned out to be". Before this column the continuation
// marker lived in task_runs.message, which is display text the controller
// overwrites with the run's outcome — so execution behaviour and a
// human-readable string were the same field.
func migrateV38ToV39(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	exists, err := tableHasColumn(tx, "task_runs", "resume_session_id")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := tx.Exec(`ALTER TABLE task_runs ADD COLUMN resume_session_id TEXT`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 39`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV39ToV40 adds the rebuildable AI Day projection. No existing rows
// need backfilling: `atm day rebuild` derives them from the session mirror on
// demand, and keeping migration free of product logic makes upgrades predictable.
func migrateV39ToV40(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ai_day_features (
			day                 TEXT PRIMARY KEY CHECK (day GLOB '` + datePattern + `'),
			timezone            TEXT NOT NULL,
			session_count       INTEGER NOT NULL DEFAULT 0,
			turn_count          INTEGER NOT NULL DEFAULT 0,
			tool_calls          INTEGER NOT NULL DEFAULT 0,
			source_count        INTEGER NOT NULL DEFAULT 0,
			input_tokens        INTEGER NOT NULL DEFAULT 0,
			output_tokens       INTEGER NOT NULL DEFAULT 0,
			cache_create_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
			generation_seconds  INTEGER NOT NULL DEFAULT 0,
			built_at            INTEGER NOT NULL,
			feature_version     INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_day_results (
			day            TEXT PRIMARY KEY REFERENCES ai_day_features(day) ON DELETE CASCADE,
			state          TEXT NOT NULL CHECK (state IN ('ready','empty')),
			concept_id     TEXT NOT NULL DEFAULT '',
			title          TEXT NOT NULL DEFAULT '',
			explanation    TEXT NOT NULL DEFAULT '',
			tags_json      TEXT NOT NULL DEFAULT '[]',
			evidence_json  TEXT NOT NULL DEFAULT '[]',
			confidence     REAL NOT NULL DEFAULT 0,
			baseline_days  INTEGER NOT NULL DEFAULT 0,
			generated_at   INTEGER NOT NULL,
			engine_version INTEGER NOT NULL
		)`,
		`UPDATE schema_version SET version = 40`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateV40ToV41(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS ai_day_events (event_id TEXT PRIMARY KEY, occurred_at INTEGER NOT NULL, source TEXT NOT NULL, session_hash TEXT NOT NULL, event_type TEXT NOT NULL, quantity INTEGER NOT NULL DEFAULT 1, modality TEXT NOT NULL DEFAULT 'general', execution_mode TEXT NOT NULL DEFAULT 'interactive', input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, cache_create_tokens INTEGER NOT NULL DEFAULT 0, cache_read_tokens INTEGER NOT NULL DEFAULT 0, duration_ms INTEGER NOT NULL DEFAULT 0, semantic_labels_json TEXT NOT NULL DEFAULT '[]', semantic_confidence REAL NOT NULL DEFAULT 0, raw_content_retained INTEGER NOT NULL DEFAULT 0 CHECK (raw_content_retained = 0), schema_version INTEGER NOT NULL, ingested_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_day_events_time ON ai_day_events(occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_day_events_source_time ON ai_day_events(source, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_day_events_session_time ON ai_day_events(session_hash, occurred_at)`,
		`CREATE TABLE IF NOT EXISTS ai_day_session_features (day TEXT NOT NULL CHECK (day GLOB '` + datePattern + `'), session_hash TEXT NOT NULL, source TEXT NOT NULL, modality TEXT NOT NULL DEFAULT 'general', execution_mode TEXT NOT NULL DEFAULT 'interactive', event_count INTEGER NOT NULL DEFAULT 0, turn_count INTEGER NOT NULL DEFAULT 0, tool_calls INTEGER NOT NULL DEFAULT 0, active_seconds INTEGER NOT NULL DEFAULT 0, semantic_counts_json TEXT NOT NULL DEFAULT '{}', built_at INTEGER NOT NULL, feature_version INTEGER NOT NULL, PRIMARY KEY (day, session_hash))`,
		`CREATE TABLE IF NOT EXISTS ai_day_feature_details (day TEXT PRIMARY KEY REFERENCES ai_day_features(day) ON DELETE CASCADE, event_count INTEGER NOT NULL DEFAULT 0, active_seconds INTEGER NOT NULL DEFAULT 0, foreground_seconds INTEGER NOT NULL DEFAULT 0, background_seconds INTEGER NOT NULL DEFAULT 0, semantic_counts_json TEXT NOT NULL DEFAULT '{}', modality_counts_json TEXT NOT NULL DEFAULT '{}')`,
		`CREATE TABLE IF NOT EXISTS ai_day_badge_days (day TEXT NOT NULL REFERENCES ai_day_features(day) ON DELETE CASCADE, badge_id TEXT NOT NULL, qualified INTEGER NOT NULL DEFAULT 0, selected INTEGER NOT NULL DEFAULT 0, level INTEGER NOT NULL DEFAULT 0, score REAL NOT NULL DEFAULT 0, evidence_json TEXT NOT NULL DEFAULT '[]', PRIMARY KEY (day, badge_id))`,
		`CREATE TABLE IF NOT EXISTS ai_day_badge_progress (badge_id TEXT PRIMARY KEY, level INTEGER NOT NULL DEFAULT 0, qualified_days INTEGER NOT NULL DEFAULT 0, last_qualified TEXT NOT NULL DEFAULT '', cooldown_until TEXT NOT NULL DEFAULT '', first_unlocked TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ai_day_feedback (day TEXT PRIMARY KEY, verdict TEXT NOT NULL CHECK (verdict IN ('accurate','inaccurate','corrected')), corrected_badge_id TEXT NOT NULL DEFAULT '', semantic_labels_json TEXT NOT NULL DEFAULT '[]', updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ai_day_sources (source TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 1, semantic_enabled INTEGER NOT NULL DEFAULT 1, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ai_day_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL)`,
		`UPDATE schema_version SET version = 41`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV41ToV42 records where an AI Day conclusion came from, and discards the
// derived projections built by the previous engine.
//
// The reset is deliberate. The v2 engine's rarity term read badge rows for days
// *after* the day being scored, so every rebuild of the same range produced
// different history and the sequence never converged — the stored badge days,
// levels and per-day concepts are output from a non-deterministic process and
// cannot be reconciled with the days a rebuild would now produce. Modality
// counting, tool-to-day attribution and self-source filtering also changed, so
// the features themselves differ. These tables exist to be rebuilt from the
// session mirror; user-authored rows (feedback, source and privacy settings) are
// kept.
func migrateV41ToV42(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []struct{ name, definition string }{
		{"evidence_strength", `ALTER TABLE ai_day_results ADD COLUMN evidence_strength REAL NOT NULL DEFAULT 0`},
		{"origin", `ALTER TABLE ai_day_results ADD COLUMN origin TEXT NOT NULL DEFAULT 'computed'`},
		{"computed_badge_id", `ALTER TABLE ai_day_results ADD COLUMN computed_badge_id TEXT NOT NULL DEFAULT ''`},
	} {
		has, err := tableHasColumn(tx, "ai_day_results", column.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := tx.Exec(column.definition); err != nil {
			return err
		}
	}
	statements := []string{
		`DELETE FROM ai_day_badge_progress`,
		`DELETE FROM ai_day_badge_days`,
		`DELETE FROM ai_day_session_features`,
		`DELETE FROM ai_day_feature_details`,
		`DELETE FROM ai_day_results`,
		`DELETE FROM ai_day_features`,
		`DELETE FROM ai_day_events`,
		`UPDATE schema_version SET version = 42`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV42ToV43 keeps collected insights in their own conclusion until the
// user explicitly saves one. Existing insight records remain unsaved: old daily
// digest documents cannot be mapped back to one item without guessing.
func migrateV42ToV43(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, column := range []struct{ name, definition string }{
		{"knowledge_document_id", `ALTER TABLE collection_items ADD COLUMN knowledge_document_id TEXT NOT NULL DEFAULT ''`},
		{"knowledge_collection", `ALTER TABLE collection_items ADD COLUMN knowledge_collection TEXT NOT NULL DEFAULT ''`},
	} {
		has, err := tableHasColumn(tx, "collection_items", column.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err := tx.Exec(column.definition); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 43`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV43ToV44 adds durable read state to collection results. Existing
// records are marked read at their last update so upgrading does not turn the
// entire historical ledger into a wall of new notifications; only records
// produced after the migration start unread.
func migrateV43ToV44(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	has, err := tableHasColumn(tx, "collection_items", "read_at")
	if err != nil {
		return err
	}
	if !has {
		if _, err := tx.Exec(`ALTER TABLE collection_items ADD COLUMN read_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE collection_items SET read_at=updated_at WHERE read_at=0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_collection_items_unread
		ON collection_items(read_at,updated_at DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 44`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV44ToV45 adds the outbound action gate's ledger. Nothing is backfilled:
// before v45 no command was gated, so there are no historical decisions to
// invent, and inventing one would claim a send had been reviewed when it had not.
func migrateV44ToV45(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS approvals (
			id           TEXT PRIMARY KEY,
			dedup_key    TEXT NOT NULL,
			tool         TEXT NOT NULL,
			rule_id      TEXT NOT NULL DEFAULT '',
			real_bin     TEXT NOT NULL,
			argv         TEXT NOT NULL,
			cwd          TEXT NOT NULL DEFAULT '',
			env_agent    TEXT NOT NULL DEFAULT '',
			label        TEXT NOT NULL DEFAULT '',
			preview_target TEXT NOT NULL DEFAULT '',
			preview_title  TEXT NOT NULL DEFAULT '',
			preview_body   TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL CHECK (status IN ('pending','approved','running','done','denied','expired')),
			stdin_piped  INTEGER NOT NULL DEFAULT 0 CHECK (stdin_piped IN (0,1)),
			gate_pid      INTEGER NOT NULL DEFAULT 0,
			gate_deadline INTEGER NOT NULL DEFAULT 0,
			attach_count INTEGER NOT NULL DEFAULT 1,
			requested_at INTEGER NOT NULL,
			expires_at   INTEGER NOT NULL,
			decided_at   INTEGER,
			decided_by   TEXT NOT NULL DEFAULT '',
			reason       TEXT NOT NULL DEFAULT '',
			ran_by       TEXT NOT NULL DEFAULT '',
			exit_code    INTEGER,
			output       TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approvals_status_requested ON approvals(status, requested_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_approvals_dedup ON approvals(dedup_key, requested_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_approvals_pending_dedup ON approvals(dedup_key)
			WHERE status='pending'`,
		`UPDATE schema_version SET version = 45`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateV45ToV46 lets one source be taken out of the desktop notifications
// without being taken out of collection. Nothing is backfilled: 0 means "still
// notifies", which is what every source did before this column existed, and the
// distinction only ever suppresses a banner — unread counts and badges are the
// same for a muted source as for any other.
func migrateV45ToV46(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	has, err := tableHasColumn(tx, "collection_sources", "muted")
	if err != nil {
		return err
	}
	if !has {
		if _, err := tx.Exec(`ALTER TABLE collection_sources ADD COLUMN muted INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 46`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV46ToV47 gives collection results a recoverable manual end state.
// Existing records remain active: unlike Todo-derived completion there is no
// historical signal from which a manual decision could be reconstructed.
func migrateV46ToV47(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	has, err := tableHasColumn(tx, "collection_items", "archived_at")
	if err != nil {
		return err
	}
	if !has {
		if _, err := tx.Exec(`ALTER TABLE collection_items ADD COLUMN archived_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_collection_items_archived
		ON collection_items(archived_at,updated_at DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 47`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV47ToV48 adds locally managed image attachments. Existing Todos have
// no images, so the relation starts empty and needs no data backfill.
func migrateV47ToV48(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS todo_images (
		todo_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
		position      INTEGER NOT NULL,
		stored_name   TEXT NOT NULL,
		original_name TEXT NOT NULL,
		media_type    TEXT NOT NULL,
		size_bytes    INTEGER NOT NULL CHECK (size_bytes >= 0),
		PRIMARY KEY (todo_id, stored_name)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_todo_images_todo_position
		ON todo_images(todo_id, position)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 48`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV48ToV49 adds the durable Work effect outbox. Historical lifecycle
// changes cannot be reconstructed reliably, so the table intentionally starts
// empty; only transitions performed by v49 and later enqueue effects.
func migrateV48ToV49(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS work_effect_outbox (
		id              TEXT PRIMARY KEY,
		request_id      TEXT NOT NULL,
		todo_id         TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
		kind            TEXT NOT NULL,
		payload_json    TEXT NOT NULL,
		created_at      INTEGER NOT NULL,
		completed_at    INTEGER,
		attempt_count   INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
		last_attempt_at INTEGER,
		last_error      TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_work_effect_outbox_pending
		ON work_effect_outbox(todo_id, created_at, id) WHERE completed_at IS NULL`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 49`); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateV49ToV50 adds the append-only plan stream. Existing Todos start with
// no plan; there is no legacy transient checklist that can be reconstructed.
func migrateV49ToV50(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS todo_plan_revisions (
		todo_id       TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
		revision      INTEGER NOT NULL CHECK (revision >= 1),
		base_revision INTEGER NOT NULL CHECK (base_revision >= 0),
		snapshot_json TEXT NOT NULL,
		snapshot_hash TEXT NOT NULL,
		request_id    TEXT NOT NULL,
		actor_kind    TEXT NOT NULL,
		origin        TEXT NOT NULL,
		session_id    TEXT NOT NULL DEFAULT '',
		binding_id    INTEGER,
		run_id        TEXT NOT NULL DEFAULT '',
		agent         TEXT NOT NULL DEFAULT '',
		created_at    INTEGER NOT NULL,
		PRIMARY KEY (todo_id, revision)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_todo_plan_latest
		ON todo_plan_revisions(todo_id, revision DESC)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE schema_version SET version = 50`); err != nil {
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
