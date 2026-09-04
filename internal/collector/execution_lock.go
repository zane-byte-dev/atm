package collector

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/executionlock"
)

// Collection checkpoints, classification decisions, and their Todo/document
// side effects form one job even though they span several transactions. All
// top-level executors share this lock; inner batch methods must not acquire it.
func acquireCollectionLock(ctx context.Context) (*executionlock.Lock, error) {
	return executionlock.Acquire(ctx, config.AtmDir, "collection")
}
