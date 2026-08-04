package parser

import (
	"database/sql"
	"net/url"

	_ "modernc.org/sqlite"
)

// openReadOnlySQLite first opens a live read-only database (including WAL).
// Sandboxed clients may be unable to create SQLite shared-memory files beside
// an upstream application's database, so immutable mode is a snapshot fallback.
func openReadOnlySQLite(path string) (*sql.DB, error) {
	base := (&url.URL{Scheme: "file", Path: path}).String()
	var firstErr error
	for _, query := range []string{"?mode=ro&_pragma=query_only(1)", "?mode=ro&immutable=1&_pragma=query_only(1)"} {
		db, err := sql.Open("sqlite", base+query)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			return db, nil
		}
		if db != nil {
			db.Close()
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}
