package store

import (
	"database/sql"
	"errors"
)

// TodoSessionBinding records which Agent session is working on which todo.
// BoundAt/UnboundAt are epoch seconds; an open binding has UnboundAt == nil, and
// a unique partial index keeps at most one of those per session.
type TodoSessionBinding struct {
	SessionID string `json:"session_id"`
	TodoID    string `json:"todo_id"`
	Agent     string `json:"agent,omitempty"`
	Project   string `json:"project,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	BoundAt   int64  `json:"bound_at"`
	UnboundAt *int64 `json:"unbound_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// BindTodoSession and UnbindTodoSession wrap a single binding change in its own
// transaction. No command uses them — the commands bundle the binding with the
// todo's status change through work.Transaction — but tests across two packages
// set up bindings with them, and Go cannot share a test helper across packages.
// Inlining UpdateWorkState at those twenty call sites would cost more than these
// two functions do.
func BindTodoSession(binding TodoSessionBinding) (*TodoSessionBinding, error) {
	var result *TodoSessionBinding
	err := UpdateWorkState(func(state *WorkStateTx) error {
		var err error
		result, err = state.BindSession(binding)
		return err
	})
	return result, err
}

func CurrentTodoBinding(sessionID string) (*TodoSessionBinding, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return currentTodoBinding(db, sessionID)
}

func ListActiveTodoSessionBindings() ([]TodoSessionBinding, error) {
	return listTodoBindings("", true)
}

func ListTodoSessionBindings(todoID string) ([]TodoSessionBinding, error) {
	return listTodoBindings(todoID, false)
}

func UnbindTodoSession(sessionID, reason string) (bool, error) {
	var changed bool
	err := UpdateWorkState(func(state *WorkStateTx) error {
		var err error
		changed, err = state.UnbindSession(sessionID, reason)
		return err
	})
	return changed, err
}

func UnbindTodoSessions(todoID, reason string) (int, error) {
	var changed int
	err := UpdateWorkState(func(state *WorkStateTx) error {
		var err error
		changed, err = state.UnbindTodoSessions(todoID, reason)
		return err
	})
	return changed, err
}

func insertTodoBinding(exec sqlExecer, binding TodoSessionBinding) (sql.Result, error) {
	return exec.Exec(`INSERT INTO todo_session_bindings
		(session_id,todo_id,agent,project,cwd,bound_at,unbound_at,reason)
		VALUES(?,?,?,?,?,?,?,?)`,
		binding.SessionID, binding.TodoID, binding.Agent, binding.Project, binding.CWD,
		binding.BoundAt, binding.UnboundAt, binding.Reason)
}

func currentTodoBinding(q sqlQueryer, sessionID string) (*TodoSessionBinding, error) {
	var binding TodoSessionBinding
	err := q.QueryRow(`SELECT session_id,todo_id,agent,project,cwd,bound_at,unbound_at,reason
		FROM todo_session_bindings WHERE session_id=? AND unbound_at IS NULL
		ORDER BY id DESC LIMIT 1`, sessionID).Scan(
		&binding.SessionID, &binding.TodoID, &binding.Agent, &binding.Project,
		&binding.CWD, &binding.BoundAt, &binding.UnboundAt, &binding.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func listTodoBindings(todoID string, activeOnly bool) ([]TodoSessionBinding, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return queryTodoBindings(db, todoID, activeOnly)
}

func queryTodoBindings(q sqlQueryer, todoID string, activeOnly bool) ([]TodoSessionBinding, error) {
	query := `SELECT session_id,todo_id,agent,project,cwd,bound_at,unbound_at,reason
		FROM todo_session_bindings WHERE 1=1`
	args := []any{}
	if todoID != "" {
		query += ` AND todo_id=?`
		args = append(args, todoID)
	}
	if activeOnly {
		query += ` AND unbound_at IS NULL`
	}
	// Insertion order, not bound_at: bindings created within the same second
	// must still come back in the order they happened.
	query += ` ORDER BY id`
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bindings := []TodoSessionBinding{}
	for rows.Next() {
		var binding TodoSessionBinding
		if err := rows.Scan(&binding.SessionID, &binding.TodoID, &binding.Agent, &binding.Project,
			&binding.CWD, &binding.BoundAt, &binding.UnboundAt, &binding.Reason); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}
