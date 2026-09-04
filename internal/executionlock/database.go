package executionlock

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
)

// AcquireDatabase resolves an ATM SQLite connection's data directory. The
// supplied database, rather than mutable process configuration, owns the scope.
func AcquireDatabase(ctx context.Context, db *sql.DB, name string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil {
		return nil, fmt.Errorf("execution lock database is required")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, fmt.Errorf("resolve execution lock database: %w", err)
	}
	var path string
	for rows.Next() {
		var sequence int
		var schema, filename string
		if err := rows.Scan(&sequence, &schema, &filename); err != nil {
			rows.Close()
			return nil, err
		}
		if schema == "main" {
			path = filename
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, fmt.Errorf("execution lock requires a file-backed database")
	}
	// A symlink to an index must not create a second independent lock scope.
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve execution lock database path: %w", err)
	}
	return Acquire(ctx, filepath.Dir(path), name)
}
