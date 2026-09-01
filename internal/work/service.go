// Package work owns application-level Todo reads and lifecycle transactions.
// CLI and desktop transports should express intent here instead of coordinating
// Todo, document, and Session binding persistence themselves.
package work

import (
	"github.com/zane-byte-dev/atm/internal/store"
)

type Service struct {
	// RefinementModel is the outbound model port used by Refine. Nil selects
	// ATM's built-in text-model adapter; tests and alternate compositions can
	// inject a deterministic implementation without changing the use case.
	RefinementModel RefinementModel
}

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

func (transaction *Transaction) currentSessionBinding(sessionID string) (*store.TodoSessionBinding, error) {
	return transaction.state.CurrentSessionBinding(sessionID)
}

func (transaction *Transaction) latestSessionBinding(sessionID string) (*store.TodoSessionBinding, error) {
	return transaction.state.LatestSessionBinding(sessionID)
}

func (transaction *Transaction) UnbindSession(sessionID, reason string) (bool, error) {
	return transaction.state.UnbindSession(sessionID, reason)
}

func (transaction *Transaction) UnbindTodoSessions(todoID, reason string) (int, error) {
	return transaction.state.UnbindTodoSessions(todoID, reason)
}
