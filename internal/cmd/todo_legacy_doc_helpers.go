package cmd

import (
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"
)

// These helpers remain for Todo adapters that have not yet moved their
// document projections into Work effects. todo lint itself no longer uses them.
func validateTodoLogReferences(tf *store.TodoFile, message string) error {
	if unknown := store.UnknownTodoReferences(tf, message); len(unknown) > 0 {
		return fmt.Errorf("todo log references unknown todo IDs: %s; create and verify structured todos before logging them", strings.Join(unknown, ", "))
	}
	return nil
}

func syncExistingTodoDocs(tf *store.TodoFile, ids ...string) error {
	for _, id := range uniqueStrings(ids) {
		todo := store.FindTodo(tf, id)
		if todo == nil || !store.TodoDocExists(todo.ID) {
			continue
		}
		if err := store.SyncTodoDocMetadata(todo); err != nil {
			return fmt.Errorf("sync todo doc %s: %w", todo.ID, err)
		}
	}
	return nil
}

// ensureTodoDocs creates missing markdown cards and syncs metadata for the
// given todos. Use this on create and agent-handoff paths so `todo doc` always
// has something to return.
func ensureTodoDocs(tf *store.TodoFile, ids ...string) error {
	for _, id := range uniqueStrings(ids) {
		todo := store.FindTodo(tf, id)
		if todo == nil {
			continue
		}
		if _, err := store.EnsureTodoDoc(todo); err != nil {
			return fmt.Errorf("ensure todo doc %s: %w", todo.ID, err)
		}
	}
	return nil
}
