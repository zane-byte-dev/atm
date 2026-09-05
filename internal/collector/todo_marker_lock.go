package collector

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zane-byte-dev/atm/internal/store"
)

// withTodoMarkerLock serializes the marker check and document write across ATM
// processes. A process-local mutex would still allow the server and a CLI retry to
// append the same collection side effect concurrently.
func withTodoMarkerLock(todoID string, run func() error) error {
	directory := store.TodoDocDir()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create Todo document directory for collection marker: %w", err)
	}
	lockPath := filepath.Join(directory, "."+filepath.Base(store.TodoDocPath(todoID))+".collection.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Todo collection marker lock: %w", err)
	}
	defer lockFile.Close()
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure Todo collection marker lock: %w", err)
	}
	if err := lockTodoMarkerFile(lockFile); err != nil {
		return fmt.Errorf("lock Todo collection marker: %w", err)
	}
	defer unlockTodoMarkerFile(lockFile)
	return run()
}
