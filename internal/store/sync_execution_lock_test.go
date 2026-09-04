package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/executionlock"
)

func TestSyncEntrypointsShareDatabaseScopeAndCancelWaits(t *testing.T) {
	db := openTempDB(t)
	databaseDir := config.AtmDir
	lock, err := executionlock.Acquire(context.Background(), databaseDir, "sync")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	// The database argument is authoritative even when global config points at
	// a different workspace. Using config here would silently bypass its owner.
	config.AtmDir = t.TempDir()
	for _, test := range []struct {
		name string
		run  func(context.Context, *sql.DB) (int, error)
	}{
		{"all", SyncAllContext},
		{"agent", func(ctx context.Context, db *sql.DB) (int, error) {
			return SyncAgentContext(ctx, db, "claude")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if n, err := test.run(ctx, db); n != 0 || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("sync escaped shared lock: count=%d error=%v", n, err)
			}
		})
	}
}
