// Package executionlock serializes long-running jobs across ATM processes.
package executionlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Lock is an exclusive operating-system lock. Close releases it; the operating
// system also releases it when its owner exits, including after a crash.
type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

// Acquire waits for one named job in a data directory. The context controls the
// wait, not the acquired lock's lifetime: callers must defer Close immediately.
// Locks are not reentrant. Acquire once at the outer domain entry point, before
// checking checkpoints or freshness, and leave inner execution primitives free
// of locks. Never remove the lock file: competing processes must share its inode.
func Acquire(ctx context.Context, dataDir, name string) (*Lock, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("execution lock data directory is required")
	}
	if !validName(name) {
		return nil, errors.New("execution lock name must contain only letters, digits, hyphens, or underscores")
	}
	file, err := openLockFile(dataDir, name)
	if err != nil {
		return nil, fmt.Errorf("open %s execution lock: %w", name, err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, fmt.Errorf("secure %s execution lock: %w", name, err)
	}
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		acquired, err := tryLockFile(file)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("acquire %s execution lock: %w", name, err)
		}
		if acquired {
			lock := &Lock{file: file}
			if err := ctx.Err(); err != nil {
				lock.Close()
				return nil, err
			}
			return lock, nil
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// Close is safe to call repeatedly, including from different goroutines.
func (lock *Lock) Close() error {
	if lock == nil {
		return nil
	}
	lock.once.Do(func() {
		lock.err = errors.Join(unlockFile(lock.file), lock.file.Close())
	})
	return lock.err
}
