package cmd

import "github.com/zane-byte-dev/atm/internal/store"

// loadTodoByID resolves an ID against live todos and preserves the archived hint
// from TodoNotFoundError so read and write commands report consistent errors.
func loadTodoByID(id string) (*store.TodoFile, *store.Todo, error) {
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return nil, nil, err
	}
	t := store.FindTodo(tf, id)
	if t == nil {
		return nil, nil, store.TodoNotFoundError(tf, id)
	}
	return tf, t, nil
}
