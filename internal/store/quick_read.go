package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

const quickReadBusyWait = 200 * time.Millisecond

// OpenQuickReadOnly opens the existing index with a short lock wait suited to
// optional menu projections. Normal Web reads retain OpenReadOnly's longer wait.
func OpenQuickReadOnly() (*sql.DB, error) {
	if _, err := os.Stat(config.AtmDB); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDatabaseMissing
		}
		return nil, err
	}
	base := (&url.URL{Scheme: "file", Path: config.AtmDB}).String()
	dsn := fmt.Sprintf("%s?mode=ro&_pragma=busy_timeout(%d)&_pragma=query_only(1)", base, quickReadBusyWait.Milliseconds())
	db, err := openReadOnlyDSN(dsn)
	if err == nil || strictReadOnly.Load() {
		return db, err
	}
	immutable := fmt.Sprintf("%s?mode=ro&immutable=1&_pragma=busy_timeout(%d)&_pragma=query_only(1)", base, quickReadBusyWait.Milliseconds())
	immutableDB, immutableErr := openReadOnlyDSN(immutable)
	if immutableErr == nil {
		return immutableDB, nil
	}
	return nil, err
}

// BoundReadWait caps SQLite's lock wait for a latency-sensitive, optional
// projection. The database handle must be dedicated to that read. Limiting it
// to one connection ensures the connection-local PRAGMA applies to every query.
func BoundReadWait(ctx context.Context, db *sql.DB, wait time.Duration) error {
	if wait <= 0 {
		wait = 200 * time.Millisecond
	}
	millis := wait.Milliseconds()
	if millis < 1 {
		millis = 1
	}
	if millis > 1000 {
		millis = 1000
	}
	db.SetMaxOpenConns(1)
	_, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", millis))
	return err
}
