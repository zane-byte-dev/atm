// Package work owns application-level Todo lifecycle transactions. CLI and
// desktop transports should express intent here instead of coordinating Todo
// and Session binding persistence themselves.
package work

import (
	"github.com/zane-byte-dev/atm/internal/store"
)

type Service struct{}

var Default Service

type Transaction struct {
	state *store.WorkStateTx
}

func (Service) Mutate(fn func(*Transaction) error) error {
	return store.UpdateWorkState(func(state *store.WorkStateTx) error {
		return fn(&Transaction{state: state})
	})
}

func (transaction *Transaction) Todos() *store.TodoFile {
	return transaction.state.Todos
}

func (transaction *Transaction) Todo(id string) (*store.Todo, error) {
	todo := store.FindTodo(transaction.state.Todos, id)
	if todo == nil {
		return nil, store.TodoNotFoundError(transaction.state.Todos, id)
	}
	return todo, nil
}

// ArchiveTodos and UnarchiveTodos move todos between the working set and cold
// storage. The rows never leave the database, so archiving is reversible and IDs
// stay taken.
func (transaction *Transaction) ArchiveTodos(ids []string) ([]string, error) {
	return transaction.state.ArchiveTodos(ids)
}

func (transaction *Transaction) TrashTodos(ids []string) ([]string, error) {
	return transaction.state.TrashTodos(ids)
}

func (transaction *Transaction) UnarchiveTodos(ids []string) ([]string, error) {
	return transaction.state.UnarchiveTodos(ids)
}

func (transaction *Transaction) RestoreTodos(ids []string) ([]string, error) {
	return transaction.state.RestoreTodos(ids)
}

func (transaction *Transaction) PermanentlyDeleteTodos(ids []string) ([]string, error) {
	return transaction.state.PermanentlyDeleteTodos(ids)
}

func (transaction *Transaction) BindSession(binding store.TodoSessionBinding) (*store.TodoSessionBinding, error) {
	return transaction.state.BindSession(binding)
}

func (transaction *Transaction) UnbindSession(sessionID, reason string) (bool, error) {
	return transaction.state.UnbindSession(sessionID, reason)
}

func (transaction *Transaction) UnbindTodoSessions(todoID, reason string) (int, error) {
	return transaction.state.UnbindTodoSessions(todoID, reason)
}
