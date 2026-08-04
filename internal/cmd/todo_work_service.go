package cmd

import (
	"fmt"

	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

// finishTodoMutation is the tail every todo mutation shares: refresh the doc if
// the todo has one, then report the todo as JSON or as a single human line. It
// takes the message already rendered because a few callers decide what to say
// from the old and new value, not from the todo alone.
func finishTodoMutation(tf *store.TodoFile, t *store.Todo, message string) error {
	if err := syncExistingTodoDocs(tf, t.ID); err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(t)
		return nil
	}
	fmt.Println(message)
	return nil
}

// loadTodoByID is the read-only counterpart of mutateTodo: it resolves an ID
// against the live todos and reports the archived hint TodoNotFoundError adds,
// so a read command cannot drift into a barer "not found" than a write one.
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

func mutateTodo(
	id string,
	fn func(todo *store.Todo, todos *store.TodoFile, transaction *workapp.Transaction) error,
) (*store.TodoFile, *store.Todo, error) {
	var file *store.TodoFile
	var result store.Todo
	err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		file = transaction.Todos()
		todo, err := transaction.Todo(id)
		if err != nil {
			return err
		}
		if err := fn(todo, file, transaction); err != nil {
			return err
		}
		result = *todo
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return file, &result, nil
}
