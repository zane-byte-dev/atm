package store

import (
	"path/filepath"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func withTempStore(t *testing.T) string {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})
	return dir
}

// seedTodos installs items as the whole todos table through the production write
// path, so fixtures are subject to the same constraints as real writes.
func seedTodos(t *testing.T, items ...Todo) {
	t.Helper()
	if err := UpdateWorkState(func(state *WorkStateTx) error {
		state.Todos.Items = items
		return nil
	}); err != nil {
		t.Fatalf("seed todos: %v", err)
	}
}

// openTodo is a valid minimal todo: comments and session bindings reference
// todos(id), so most fixtures need one to exist first.
func openTodo(id, title string) Todo {
	return Todo{ID: id, Title: title, Priority: "P1", Status: TodoStatusOpen, Created: Today()}
}
