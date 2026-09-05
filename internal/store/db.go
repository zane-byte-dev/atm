package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"

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
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// ErrDatabaseMissing reports that no database has been created yet. Read paths
// return it instead of creating one, so callers can decide whether an empty
// database means "nothing to show" or "the user needs to run sync".
var ErrDatabaseMissing = errors.New("database does not exist: run `atm sync` first")

var strictReadOnly atomic.Bool

// SetStrictReadOnly disables the immutable fallback for a long-running host.
// Set this once before serving requests: a runtime must see committed WAL data
// or return the read error, never silently serve a pre-checkpoint snapshot.
func SetStrictReadOnly(enabled bool) { strictReadOnly.Store(enabled) }

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
	// WAL recovery and checkpoint transitions can briefly hold an exclusive
	// lock even for a read. Match the writer's bounded busy wait on every pooled
	// connection instead of turning that transition into SQLITE_BUSY_RECOVERY.
	// busy_timeout is connection-local: it neither writes nor migrates the DB.
	db, err := openReadOnlyDSN(base + "?mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err == nil {
		return db, nil
	}
	if strictReadOnly.Load() {
		return nil, err
	}

	// Sandboxed agents may be allowed to read the database file but not create
	// SQLite lock or shared-memory files in its directory. immutable=1 avoids
	// those side effects and provides a stable snapshot of the last explicit sync.
	// Caveat: immutable=1 ignores the -wal file, so it can serve data from before
	// the most recent (un-checkpointed) commit. Callers in this mode may observe
	// slightly stale todos/bindings until the next checkpoint.
	immutableDB, immutableErr := openReadOnlyDSN(base + "?mode=ro&immutable=1&_pragma=busy_timeout(5000)&_pragma=query_only(1)")
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

func ensureSchema(db *sql.DB) error {
	// The common path stays read-only. bootstrapSchema deliberately writes only
	// for a fresh database, so concurrent callers cannot race to create tables.
	var current int
	if err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&current); err == nil && current == SchemaVersion {
		return nil
	}

	version, created, err := bootstrapSchema(db)
	if err != nil || created {
		return err
	}
	if version == SchemaVersion {
		return nil
	}
	if version < MinUpgradableVersion {
		// ATM has one live database. Once it reaches the current baseline, keeping
		// every historical migration is more risk than value. Back up irreplaceable
		// records before rebuilding an unsupported database.
		return fmt.Errorf("database schema v%d is no longer supported (current v%d): "+
			"run `atm backup` first to keep your todos, memory and knowledge, "+
			"then remove %s and run `atm sync` to rebuild the session index",
			version, SchemaVersion, config.AtmDB)
	}
	if version < SchemaVersion {
		return fmt.Errorf("database schema v%d has no registered migration to v%d",
			version, SchemaVersion)
	}
	// An older binary against a newer database must fail before reading columns
	// it may silently misinterpret.
	return fmt.Errorf("database schema v%d is newer than this atm build (v%d): upgrade atm",
		version, SchemaVersion)
}

// bootstrapSchema reads the schema version and, on an empty database, creates
// the whole current schema. Both happen in one transaction whose first statement
// is a write, so two processes starting against a fresh database serialize.
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
