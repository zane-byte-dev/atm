package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// rebuildableTables hold the derived session mirror: every row in them came from
// an agent's own transcript files and `atm sync` puts it back. Emptying them is
// what keeps a backup small enough to be worth taking regularly — on a real
// database they are the overwhelming majority of the bytes.
//
// Everything not listed here is treated as this database's own record and is
// carried into the backup. That direction matters: a table added later is
// preserved by default, and the failure mode of a wrong guess is a slightly
// larger archive rather than silently dropped history.
var rebuildableTables = []string{
	"messages",
	"sessions",
	"skill_events",
	"sync_health",
	"sync_state",
	"tools",
	"usage",
	"usage_events",
}

// RebuildableTables lists the tables a backup carries empty, so the command
// layer can tell the user what restoring will not bring back until the next sync.
func RebuildableTables() []string {
	out := append([]string(nil), rebuildableTables...)
	sort.Strings(out)
	return out
}

// ReadSchemaVersionAt reports a database file's schema version without opening
// it through Open, which would migrate it. Backup has to work on exactly the
// databases Open refuses — one below minUpgradableVersion is the case that needs
// an escape hatch most — so it must never take the migrating path.
//
// A file with no schema_version table reports 0 rather than an error: that is
// what a database created by some other tool, or an empty file, looks like, and
// the caller decides whether that is fatal.
//
// "No such table" is asked of the catalogue rather than inferred from a failed
// read of the table itself. Treating every read error as version 0 would fold a
// corrupt file, an unreadable one and a locked one into the same answer as a
// foreign database — and 0 is a claim, not a shrug: `atm backup` writes it into
// the manifest a restore checks, and `atm diagnose` prints it in place of the
// error field it keeps for exactly this.
func ReadSchemaVersionAt(dbPath string) (int, error) {
	db, err := openNoMigrate(dbPath, true)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var name string
	switch err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'`,
	).Scan(&name); {
	case err == sql.ErrNoRows:
		return 0, nil
	case err != nil:
		// The catalogue itself did not read: not a database, truncated, or locked.
		return 0, err
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&version); err != nil {
		if err == sql.ErrNoRows {
			// The table exists with nothing in it, which is a database caught
			// mid-creation. Same answer as no table at all: no version recorded.
			return 0, nil
		}
		return 0, err
	}
	return version, nil
}

// openNoMigrate opens a database file directly, skipping migrate(). Read-only
// mode deliberately omits query_only: VACUUM INTO writes only to its
// destination, and the pragma would refuse the statement anyway.
func openNoMigrate(dbPath string, readOnly bool) (*sql.DB, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDatabaseMissing
		}
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: dbPath}).String() + "?_pragma=busy_timeout(5000)"
	if readOnly {
		dsn += "&mode=ro"
	}
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

// SnapshotOwnRecords writes a consistent copy of the live database to destPath
// with the rebuildable session mirror emptied.
//
// VACUUM INTO does the copying because it is the only way to get a
// transactionally consistent single file out of a live WAL database without
// stopping the writers, and because it needs no knowledge of the schema — the
// same call works on a database too old for this build to migrate.
//
// The session tables are emptied rather than dropped. Dropping them would leave
// a restored database missing tables at a schema version that claims to have
// them, and nothing rebuilds a table: bootstrapSchema only creates the schema
// when the version is 0, so the first command to touch sessions would fail
// instead of reporting an empty index. Emptied tables restore into something
// `atm doctor` can read immediately and `atm sync` refills.
//
// destPath must not exist; SQLite refuses to overwrite, and so does this.
func SnapshotOwnRecords(destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("snapshot destination already exists: %s", destPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0700); err != nil {
		return err
	}

	source, err := openNoMigrate(config.AtmDB, true)
	if err != nil {
		return err
	}
	defer source.Close()

	// SQLite has no placeholder for a VACUUM target, so the path is inlined.
	// Single quotes are doubled to keep a path containing one from ending the
	// literal — this is a local file path, but the escaping is not optional.
	quoted := strings.ReplaceAll(destPath, "'", "''")
	if _, err := source.Exec(`VACUUM INTO '` + quoted + `'`); err != nil {
		return fmt.Errorf("snapshot database: %w", err)
	}

	if err := clearRebuildable(destPath); err != nil {
		// A half-pruned snapshot is worse than none: it looks like a backup but
		// carries rows the restore path claims it does not.
		os.Remove(destPath)
		return err
	}
	return nil
}

func clearRebuildable(dbPath string) error {
	db, err := openNoMigrate(dbPath, false)
	if err != nil {
		return err
	}
	defer db.Close()
	// Order does not matter: openNoMigrate leaves foreign_keys at SQLite's default
	// of off, so each DELETE stands alone, and if they were on, the ON DELETE
	// CASCADE from messages to sessions would remove the same rows anyway.
	for _, table := range rebuildableTables {
		exists, err := tableExists(db, table)
		if err != nil {
			return err
		}
		// A schema old enough to need the escape hatch may predate some of these
		// tables. Missing is not an error; there is nothing to empty.
		if !exists {
			continue
		}
		if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("prune %s from snapshot: %w", table, err)
		}
	}
	// Reclaim the pages those rows occupied. Without this the snapshot keeps them
	// as free pages and a backup of a 30 MB database stays roughly 30 MB, which
	// defeats the point of pruning.
	if _, err := db.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("compact snapshot: %w", err)
	}
	return nil
}

func tableExists(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
