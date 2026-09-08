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
	dataDir := config.PolicyForExecution(ctx).DataDir
	var lock *executionlock.Lock
	err := config.External(ctx, func(externalCtx context.Context) error {
		var err error
		lock, err = executionlock.Acquire(externalCtx, dataDir, "collection")
		return err
	})
	return lock, err
}
