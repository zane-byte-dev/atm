package store

import (
	"database/sql"
	"fmt"
	"reflect"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

type sqlQueryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type sqlWorkStore interface {
	sqlQueryer
	sqlExecer
}

// todoRow is the persisted state of one todo, captured at load time so writes
// can be narrowed to the rows that actually changed.
type todoRow struct {
	position int
	todo     Todo
}

// loadTodos reads the live working set. Archived todos keep their rows but are
// deliberately absent from Items — only their IDs come back, so callers cannot
// accidentally list, transition, or overwrite them.
func loadTodos(q sqlQueryer) (*TodoFile, error) {
	rows, err := q.Query(`SELECT id,title,description,priority,status,project,lane,wake_condition,
		review_at,maintenance_limit,created,source,closed,closed_reason,on_done,start_ts,done_ts
		FROM todos WHERE archived_at IS NULL ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	file := &TodoFile{Items: []Todo{}}
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(
			&todo.ID, &todo.Title, &todo.Description, &todo.Priority, &todo.Status,
			&todo.Project, &todo.Lane, &todo.WakeCondition, &todo.ReviewAt,
			&todo.MaintenanceLimit, &todo.Created, &todo.Source, &todo.Closed,
			&todo.ClosedReason, &todo.OnDone, &todo.StartTS, &todo.DoneTS,
		); err != nil {
			return nil, err
		}
		file.Items = append(file.Items, todo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Child collections load in one query each and are grouped in memory. Doing
	// this per todo would issue 3N+1 queries to assemble a list we always read
	// in full.
	tags, err := loadGroupedStrings(q, `SELECT todo_id,tag FROM todo_tags ORDER BY todo_id,position,tag`)
	if err != nil {
		return nil, err
	}
	dependencies, err := loadGroupedStrings(q, `SELECT todo_id,dependency_id FROM todo_dependencies ORDER BY todo_id,position,dependency_id`)
	if err != nil {
		return nil, err
	}
	links, err := loadGroupedTodoLinks(q)
	if err != nil {
		return nil, err
	}
	for i := range file.Items {
		todo := &file.Items[i]
		todo.Tags = tags[todo.ID]
		todo.DependsOn = dependencies[todo.ID]
		todo.Links = links[todo.ID]
	}
	if file.archived, err = loadArchivedTodoStatuses(q); err != nil {
		return nil, err
	}
	file.baseline = snapshotTodos(file)
	return file, nil
}

// loadArchivedTodoStatuses reads the IDs that are taken but not in the working
// set, with the status each one closed at.
func loadArchivedTodoStatuses(q sqlQueryer) (map[string]string, error) {
	rows, err := q.Query(`SELECT id,status FROM todos WHERE archived_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := map[string]string{}
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			return nil, err
		}
		statuses[id] = status
	}
	return statuses, rows.Err()
}

func loadGroupedStrings(q sqlQueryer, query string) (map[string][]string, error) {
	rows, err := q.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string][]string{}
	for rows.Next() {
		var todoID, value string
		if err := rows.Scan(&todoID, &value); err != nil {
			return nil, err
		}
		grouped[todoID] = append(grouped[todoID], value)
	}
	return grouped, rows.Err()
}

func loadGroupedTodoLinks(q sqlQueryer) (map[string][]TodoLink, error) {
	rows, err := q.Query(`SELECT todo_id,url,kind,title,relation FROM todo_links ORDER BY todo_id,position,url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string][]TodoLink{}
	for rows.Next() {
		var todoID string
		var link TodoLink
		if err := rows.Scan(&todoID, &link.URL, &link.Kind, &link.Title, &link.Relation); err != nil {
			return nil, err
		}
		grouped[todoID] = append(grouped[todoID], link)
	}
	return grouped, rows.Err()
}

// snapshotTodos copies the file's items so later comparisons see the state as
// loaded. Slices are cloned because callers mutate them in place.
func snapshotTodos(file *TodoFile) map[string]todoRow {
	baseline := make(map[string]todoRow, len(file.Items))
	for position, todo := range file.Items {
		todo.Tags = append([]string(nil), todo.Tags...)
		todo.DependsOn = append([]string(nil), todo.DependsOn...)
		todo.Links = append([]TodoLink(nil), todo.Links...)
		baseline[todo.ID] = todoRow{position: position, todo: todo}
	}
	return baseline
}

// writeTodos persists file against the baseline captured when it was loaded,
// touching only the rows that changed. Removed todos are deleted by ID, which
// lets the ON DELETE CASCADE foreign keys take their tags, dependencies, links,
// comments, and session bindings with them — a full table rewrite would delete
// the comments and bindings of every todo, including the surviving ones.
//
// A TodoFile is written at most once: the baseline is not refreshed afterwards,
// because the snapshot's lifetime is the transaction that loaded it.
func writeTodos(store sqlWorkStore, file *TodoFile) error {
	positions := make(map[string]int, len(file.Items))
	for position := range file.Items {
		id := file.Items[position].ID
		if id == "" {
			return fmt.Errorf("todo at position %d has no ID", position)
		}
		if previous, duplicate := positions[id]; duplicate {
			return fmt.Errorf("duplicate todo ID %s at positions %d and %d", id, previous, position)
		}
		positions[id] = position
	}

	for id := range file.baseline {
		if _, kept := positions[id]; kept {
			continue
		}
		if _, err := store.Exec(`DELETE FROM todos WHERE id=?`, id); err != nil {
			return err
		}
	}

	for position := range file.Items {
		todo := &file.Items[position]
		base, existed := file.baseline[todo.ID]
		switch {
		case !existed:
			if err := insertTodo(store, position, todo); err != nil {
				return err
			}
		case base.position != position || !sameTodoScalars(&base.todo, todo):
			if err := updateTodo(store, position, todo); err != nil {
				return err
			}
		}
		if !existed || !reflect.DeepEqual(base.todo.Tags, todo.Tags) {
			if err := writeTodoTags(store, todo); err != nil {
				return err
			}
		}
		if !existed || !reflect.DeepEqual(base.todo.DependsOn, todo.DependsOn) {
			if err := writeTodoDependencies(store, todo); err != nil {
				return err
			}
		}
		if !existed || !reflect.DeepEqual(base.todo.Links, todo.Links) {
			if err := writeTodoLinks(store, todo); err != nil {
				return err
			}
		}
	}
	return nil
}

// sameTodoScalars compares everything stored in the todos row itself. The child
// collections are compared separately so a tag edit does not rewrite the row,
// and a title edit does not rewrite the tags.
func sameTodoScalars(a, b *Todo) bool {
	scalars := func(todo *Todo) Todo {
		copied := *todo
		copied.Tags, copied.DependsOn, copied.Links = nil, nil, nil
		return copied
	}
	// DeepEqual rather than ==: Closed, ClosedReason, StartTS, and DoneTS are
	// pointers, and == would compare addresses instead of values.
	return reflect.DeepEqual(scalars(a), scalars(b))
}

func insertTodo(store sqlExecer, position int, todo *Todo) error {
	_, err := store.Exec(`INSERT INTO todos
		(id,position,title,description,priority,status,project,lane,wake_condition,review_at,
		 maintenance_limit,created,source,closed,closed_reason,on_done,start_ts,done_ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		todo.ID, position, todo.Title, todo.Description, todo.Priority, todo.Status,
		todo.Project, todo.Lane, todo.WakeCondition, todo.ReviewAt, todo.MaintenanceLimit,
		todo.Created, todo.Source, todo.Closed, todo.ClosedReason,
		todo.OnDone, todo.StartTS, todo.DoneTS)
	return err
}

func updateTodo(store sqlExecer, position int, todo *Todo) error {
	_, err := store.Exec(`UPDATE todos SET position=?,title=?,description=?,priority=?,status=?,
		project=?,lane=?,wake_condition=?,review_at=?,maintenance_limit=?,created=?,source=?,
		closed=?,closed_reason=?,on_done=?,start_ts=?,done_ts=? WHERE id=?`,
		position, todo.Title, todo.Description, todo.Priority, todo.Status,
		todo.Project, todo.Lane, todo.WakeCondition, todo.ReviewAt, todo.MaintenanceLimit,
		todo.Created, todo.Source, todo.Closed, todo.ClosedReason,
		todo.OnDone, todo.StartTS, todo.DoneTS, todo.ID)
	return err
}

func writeTodoTags(store sqlWorkStore, todo *Todo) error {
	if _, err := store.Exec(`DELETE FROM todo_tags WHERE todo_id=?`, todo.ID); err != nil {
		return err
	}
	for index, tag := range todo.Tags {
		if _, err := store.Exec(`INSERT INTO todo_tags(todo_id,position,tag) VALUES(?,?,?)`,
			todo.ID, index, tag); err != nil {
			return err
		}
	}
	return nil
}

func writeTodoDependencies(store sqlWorkStore, todo *Todo) error {
	if _, err := store.Exec(`DELETE FROM todo_dependencies WHERE todo_id=?`, todo.ID); err != nil {
		return err
	}
	for index, dependencyID := range todo.DependsOn {
		if _, err := store.Exec(`INSERT INTO todo_dependencies(todo_id,position,dependency_id) VALUES(?,?,?)`,
			todo.ID, index, dependencyID); err != nil {
			return err
		}
	}
	return nil
}

func writeTodoLinks(store sqlWorkStore, todo *Todo) error {
	if _, err := store.Exec(`DELETE FROM todo_links WHERE todo_id=?`, todo.ID); err != nil {
		return err
	}
	for index, link := range todo.Links {
		if _, err := store.Exec(`INSERT INTO todo_links(todo_id,position,url,kind,title,relation)
			VALUES(?,?,?,?,?,?)`, todo.ID, index, link.URL, link.Kind, link.Title, link.Relation); err != nil {
			return err
		}
	}
	return nil
}

// WorkStateTx is the single transaction boundary for Todo lifecycle and Session
// binding changes. Callers mutate Todos in memory and use the binding helpers
// below; UpdateWorkState persists both domains atomically.
type WorkStateTx struct {
	tx    *sql.Tx
	Todos *TodoFile
}

// UpdateWorkState runs fn inside one serialized write transaction: it takes the
// cross-process write lock, reads the todos under that lock, hands them to fn,
// and persists whatever fn changed. Because the snapshot is read after the lock
// is held, no optimistic revision check is needed — concurrent writers queue up
// rather than race and lose each other's edits.
func UpdateWorkState(fn func(*WorkStateTx) error) error {
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := acquireWorkWriteLock(tx); err != nil {
		return err
	}
	todos, err := loadTodos(tx)
	if err != nil {
		return err
	}
	state := &WorkStateTx{tx: tx, Todos: todos}
	if err := fn(state); err != nil {
		return err
	}
	if err := writeTodos(tx, todos); err != nil {
		return err
	}
	return tx.Commit()
}

func (state *WorkStateTx) BindSession(binding TodoSessionBinding) (*TodoSessionBinding, error) {
	if binding.SessionID == "" || binding.TodoID == "" {
		return nil, fmt.Errorf("session ID and todo ID are required")
	}
	now := time.Now().In(config.Loc).Unix()
	current, err := currentTodoBinding(state.tx, binding.SessionID)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if current.TodoID == binding.TodoID {
			if _, err := state.tx.Exec(`UPDATE todo_session_bindings SET agent=?,project=?,cwd=?
				WHERE session_id=? AND unbound_at IS NULL`,
				binding.Agent, binding.Project, binding.CWD, binding.SessionID); err != nil {
				return nil, err
			}
			current.Agent, current.Project, current.CWD = binding.Agent, binding.Project, binding.CWD
			return current, nil
		}
		if _, err := state.tx.Exec(`UPDATE todo_session_bindings SET unbound_at=?,reason='rebound'
			WHERE session_id=? AND unbound_at IS NULL`, now, binding.SessionID); err != nil {
			return nil, err
		}
	}
	binding.BoundAt = now
	binding.UnboundAt = nil
	binding.Reason = ""
	if _, err := insertTodoBinding(state.tx, binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (state *WorkStateTx) UnbindSession(sessionID, reason string) (bool, error) {
	now := time.Now().In(config.Loc).Unix()
	result, err := state.tx.Exec(`UPDATE todo_session_bindings SET unbound_at=?,reason=?
		WHERE session_id=? AND unbound_at IS NULL`, now, reason, sessionID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

// ArchiveTodos moves closed todos out of the working set. The rows stay — an
// archived todo can still be named by a dependency or a progress note, and its
// ID is never reused — so this updates archived_at directly and drops the todos
// from the snapshot, which is what loadTodos would return from now on.
func (state *WorkStateTx) ArchiveTodos(ids []string) ([]string, error) {
	now := time.Now().In(config.Loc).Unix()
	archived := []string{}
	for _, id := range ids {
		todo := FindTodo(state.Todos, id)
		if todo == nil {
			return nil, TodoNotFoundError(state.Todos, id)
		}
		if TodoIsActive(*todo) {
			return nil, fmt.Errorf("cannot archive %s with status %s: finish or drop it first", id, todo.Status)
		}
		if _, err := state.tx.Exec(`UPDATE todos SET archived_at=? WHERE id=?`, now, id); err != nil {
			return nil, err
		}
		state.Todos.forget(id, todo.Status)
		archived = append(archived, id)
	}
	return archived, nil
}

// UnarchiveTodos brings todos back into the working set. They reappear on the
// next load, not in this snapshot.
func (state *WorkStateTx) UnarchiveTodos(ids []string) ([]string, error) {
	restored := []string{}
	for _, id := range ids {
		if _, archived := ArchivedStatus(state.Todos, id); !archived {
			return nil, fmt.Errorf("todo %s is not archived", id)
		}
		if _, err := state.tx.Exec(`UPDATE todos SET archived_at=NULL WHERE id=?`, id); err != nil {
			return nil, err
		}
		delete(state.Todos.archived, id)
		restored = append(restored, id)
	}
	return restored, nil
}

// forget drops a todo from the snapshot without scheduling a delete: the row
// still exists, it just left the working set.
func (file *TodoFile) forget(id, status string) {
	kept := file.Items[:0]
	for _, todo := range file.Items {
		if todo.ID != id {
			kept = append(kept, todo)
		}
	}
	file.Items = kept
	delete(file.baseline, id)
	if file.archived == nil {
		file.archived = map[string]string{}
	}
	file.archived[id] = status
}

func (state *WorkStateTx) UnbindTodoSessions(todoID, reason string) (int, error) {
	now := time.Now().In(config.Loc).Unix()
	result, err := state.tx.Exec(`UPDATE todo_session_bindings SET unbound_at=?,reason=?
		WHERE todo_id=? AND unbound_at IS NULL`, now, reason, todoID)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

// acquireWorkWriteLock promotes the surrounding deferred transaction to a write
// transaction, taking SQLite's write lock before we read the mutable work
// snapshot. This serializes cross-process read/modify/write cycles so two Agent
// clients cannot overwrite each other's Todo changes. The upsert always writes a
// row, so — unlike a conditional UPDATE whose WHERE clause might match nothing —
// it acquires the lock even on a database where the target row does not yet exist.
func acquireWorkWriteLock(exec sqlExecer) error {
	_, err := exec.Exec(`INSERT INTO work_state_meta(key,value) VALUES('write_lock','')
		ON CONFLICT(key) DO UPDATE SET value=''`)
	return err
}
