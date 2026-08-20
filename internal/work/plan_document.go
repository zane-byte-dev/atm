package work

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zane-byte-dev/atm/internal/store"
)

// syncTodoDocumentWithLatestPlan serializes plan projection writers across ATM
// processes and reloads the authoritative latest revision while holding that
// lock. Therefore an older SetPlan call cannot finish after a newer one and
// leave the Markdown card on the older snapshot.
func syncTodoDocumentWithLatestPlan(todo *store.Todo) (string, error) {
	if todo == nil {
		return "", fmt.Errorf("todo is nil")
	}
	var path string
	err := withPlanDocumentLock(todo.ID, func() error {
		var err error
		path, err = store.EnsureTodoDoc(todo)
		if err != nil {
			return err
		}
		plan, err := latestPlanSnapshot(todo.ID)
		if err != nil {
			return err
		}
		if plan == nil {
			return nil
		}
		items := make([]store.TodoPlanDocumentItem, len(plan.Items))
		for index, item := range plan.Items {
			items[index] = store.TodoPlanDocumentItem{Step: item.Step, Status: item.Status}
		}
		return store.SyncTodoDocPlan(todo.ID, plan.Explanation, items)
	})
	return path, err
}

func withPlanDocumentLock(todoID string, run func() error) error {
	directory := store.TodoDocDir()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create Todo document directory for plan: %w", err)
	}
	lockPath := filepath.Join(directory, "."+filepath.Base(store.TodoDocPath(todoID))+".plan.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Todo plan document lock: %w", err)
	}
	defer lockFile.Close()
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure Todo plan document lock: %w", err)
	}
	if err := lockPlanDocumentFile(lockFile); err != nil {
		return fmt.Errorf("lock Todo plan document: %w", err)
	}
	defer unlockPlanDocumentFile(lockFile)
	return run()
}
