package store

import (
	"database/sql"
	"errors"
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
	rows, err := q.Query(`SELECT id,title,description,priority,status,project,wake_condition,
		review_at,maintenance_limit,created,source,creator,closed,closed_reason,on_done,start_ts,done_ts
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
			&todo.Project, &todo.WakeCondition, &todo.ReviewAt,
			&todo.MaintenanceLimit, &todo.Created, &todo.Source, &todo.Creator, &todo.Closed,
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
	images, err := loadGroupedTodoImages(q)
	if err != nil {
		return nil, err
	}
	for i := range file.Items {
		todo := &file.Items[i]
		todo.Tags = tags[todo.ID]
		todo.DependsOn = dependencies[todo.ID]
		todo.Links = links[todo.ID]
		todo.Images = images[todo.ID]
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

func loadGroupedTodoImages(q sqlQueryer) (map[string][]TodoImage, error) {
	rows, err := q.Query(`SELECT todo_id,stored_name,original_name,media_type,size_bytes
		FROM todo_images ORDER BY todo_id,position,stored_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grouped := map[string][]TodoImage{}
	for rows.Next() {
		var todoID string
		var image TodoImage
		if err := rows.Scan(&todoID, &image.StoredName, &image.Name, &image.MediaType, &image.SizeBytes); err != nil {
			return nil, err
		}
		image.Path = TodoImagePath(todoID, image.StoredName)
		grouped[todoID] = append(grouped[todoID], image)
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
		todo.Images = append([]TodoImage(nil), todo.Images...)
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

	// Insert and update the todo rows first. Dependencies are a foreign key
	// onto todos.id, so a parent that gains children in the same transaction
	// (todo refine splitting a card) would fail if we wrote its edges before
	// the new rows existed. Collections are a second pass against that
	// already-complete set of ids.
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
	}
	for position := range file.Items {
		todo := &file.Items[position]
		base, existed := file.baseline[todo.ID]
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
		if !existed || !reflect.DeepEqual(base.todo.Images, todo.Images) {
			if err := writeTodoImages(store, todo); err != nil {
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
		copied.Tags, copied.DependsOn, copied.Links, copied.Images = nil, nil, nil, nil
		return copied
	}
	// DeepEqual rather than ==: Closed, ClosedReason, StartTS, and DoneTS are
	// pointers, and == would compare addresses instead of values.
	return reflect.DeepEqual(scalars(a), scalars(b))
}

func insertTodo(store sqlExecer, position int, todo *Todo) error {
	_, err := store.Exec(`INSERT INTO todos
		(id,position,title,description,priority,status,project,wake_condition,review_at,
		 maintenance_limit,created,source,creator,closed,closed_reason,on_done,start_ts,done_ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		todo.ID, position, todo.Title, todo.Description, todo.Priority, todo.Status,
		todo.Project, todo.WakeCondition, todo.ReviewAt, todo.MaintenanceLimit,
		todo.Created, todo.Source, todo.Creator, todo.Closed, todo.ClosedReason,
		todo.OnDone, todo.StartTS, todo.DoneTS)
	return err
}

func updateTodo(store sqlExecer, position int, todo *Todo) error {
	_, err := store.Exec(`UPDATE todos SET position=?,title=?,description=?,priority=?,status=?,
		project=?,wake_condition=?,review_at=?,maintenance_limit=?,created=?,source=?,creator=?,
		closed=?,closed_reason=?,on_done=?,start_ts=?,done_ts=? WHERE id=?`,
		position, todo.Title, todo.Description, todo.Priority, todo.Status,
		todo.Project, todo.WakeCondition, todo.ReviewAt, todo.MaintenanceLimit,
		todo.Created, todo.Source, todo.Creator, todo.Closed, todo.ClosedReason,
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

func writeTodoImages(store sqlWorkStore, todo *Todo) error {
	if _, err := store.Exec(`DELETE FROM todo_images WHERE todo_id=?`, todo.ID); err != nil {
		return err
	}
	for index, image := range todo.Images {
		if _, err := store.Exec(`INSERT INTO todo_images
			(todo_id,position,stored_name,original_name,media_type,size_bytes)
			VALUES(?,?,?,?,?,?)`, todo.ID, index, image.StoredName, image.Name,
			image.MediaType, image.SizeBytes); err != nil {
			return err
		}
	}
	return nil
}

// WorkStateTx is the single transaction boundary for Todo lifecycle, Session
// bindings and the durable effects those changes require. Callers mutate Todos
// in memory and use the helpers below; UpdateWorkState persists all three
// atomically.
type WorkStateTx struct {
	tx    *sql.Tx
	Todos *TodoFile
}

// WorkEffectRecord is one durable request to update a projection outside the
// WorkStateTx database (currently Todo Markdown and desktop notifications).
// Consumers must acknowledge a row only after applying its whole payload. A
// failed or crashed delivery remains pending and may therefore run more than
// once.
type WorkEffectRecord struct {
	ID            string
	RequestID     string
	TodoID        string
	Kind          string
	PayloadJSON   string
	CreatedAt     int64
	CompletedAt   *int64
	AttemptCount  int
	LastAttemptAt *int64
	LastError     string
}

var ErrWorkEffectNotFound = errors.New("work effect not found")

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

// EnqueueWorkEffect inserts an effect into the same transaction as the Todo and
// binding changes that require it. The caller owns the stable ID; retries read
// that persisted row instead of generating another one.
func (state *WorkStateTx) EnqueueWorkEffect(effect WorkEffectRecord) error {
	if effect.ID == "" || effect.RequestID == "" || effect.TodoID == "" ||
		effect.Kind == "" || effect.PayloadJSON == "" || effect.CreatedAt == 0 {
		return fmt.Errorf("work effect id, request ID, todo ID, kind, payload and creation time are required")
	}
	_, err := state.tx.Exec(`INSERT INTO work_effect_outbox
		(id,request_id,todo_id,kind,payload_json,created_at)
		VALUES(?,?,?,?,?,?)`, effect.ID, effect.RequestID, effect.TodoID,
		effect.Kind, effect.PayloadJSON, effect.CreatedAt)
	return err
}

// UpdatePendingWorkEffectPayload coalesces a projection update that has not yet
// been delivered. Its ID, originating request and attempt history remain stable.
func (state *WorkStateTx) UpdatePendingWorkEffectPayload(id, payloadJSON string) error {
	if id == "" || payloadJSON == "" {
		return fmt.Errorf("work effect ID and payload are required")
	}
	result, err := state.tx.Exec(`UPDATE work_effect_outbox SET payload_json=?
		WHERE id=? AND completed_at IS NULL`, payloadJSON, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: %s", ErrWorkEffectNotFound, id)
	}
	return nil
}

func (state *WorkStateTx) PendingWorkEffects(todoID string) ([]WorkEffectRecord, error) {
	return pendingWorkEffects(state.tx, todoID)
}

// ListPendingWorkEffects reads undelivered effects in creation order. Passing
// an empty Todo ID returns every pending effect, which lets a future background
// drainer reuse the same durable contract as command retries.
func ListPendingWorkEffects(todoID string) ([]WorkEffectRecord, error) {
	db, err := Open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return pendingWorkEffects(db, todoID)
}

// ListWorkEffects returns the complete lifecycle-effect history for one Todo.
// Unlike ListPendingWorkEffects this is a read model: completed rows are kept
// because lint needs to distinguish a first submit from work that was reopened
// and submitted again. Callers must not infer delivery state from absence here.
func ListWorkEffects(todoID string) ([]WorkEffectRecord, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	statement := `SELECT id,request_id,todo_id,kind,payload_json,created_at,
		completed_at,attempt_count,last_attempt_at,last_error
		FROM work_effect_outbox`
	args := []any{}
	if todoID != "" {
		statement += ` WHERE todo_id=?`
		args = append(args, todoID)
	}
	statement += ` ORDER BY created_at,id`
	rows, err := db.Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	effects := []WorkEffectRecord{}
	for rows.Next() {
		effect, err := scanWorkEffect(rows)
		if err != nil {
			return nil, err
		}
		effects = append(effects, effect)
	}
	return effects, rows.Err()
}

func pendingWorkEffects(query sqlQueryer, todoID string) ([]WorkEffectRecord, error) {
	statement := `SELECT id,request_id,todo_id,kind,payload_json,created_at,
		completed_at,attempt_count,last_attempt_at,last_error
		FROM work_effect_outbox WHERE completed_at IS NULL`
	args := []any{}
	if todoID != "" {
		statement += ` AND todo_id=?`
		args = append(args, todoID)
	}
	statement += ` ORDER BY created_at,id`
	rows, err := query.Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	effects := []WorkEffectRecord{}
	for rows.Next() {
		effect, err := scanWorkEffect(rows)
		if err != nil {
			return nil, err
		}
		effects = append(effects, effect)
	}
	return effects, rows.Err()
}

type workEffectScanner interface {
	Scan(dest ...any) error
}

func scanWorkEffect(scanner workEffectScanner) (WorkEffectRecord, error) {
	var effect WorkEffectRecord
	var completedAt, lastAttemptAt sql.NullInt64
	err := scanner.Scan(&effect.ID, &effect.RequestID, &effect.TodoID, &effect.Kind,
		&effect.PayloadJSON, &effect.CreatedAt, &completedAt, &effect.AttemptCount,
		&lastAttemptAt, &effect.LastError)
	if err != nil {
		return WorkEffectRecord{}, err
	}
	if completedAt.Valid {
		value := completedAt.Int64
		effect.CompletedAt = &value
	}
	if lastAttemptAt.Valid {
		value := lastAttemptAt.Int64
		effect.LastAttemptAt = &value
	}
	return effect, nil
}

// CompleteWorkEffect acknowledges successful delivery. It is idempotent: a
// second acknowledgement of an already completed row succeeds, while an
// unknown ID remains an error so adapters cannot silently ack the wrong item.
func CompleteWorkEffect(id string) error {
	return updateWorkEffectAttempt(id, "", true)
}

// FailWorkEffect records one failed delivery without removing it from the
// pending set. The next Submit/Wait retry will receive the same effect ID.
func FailWorkEffect(id, message string) error {
	return updateWorkEffectAttempt(id, message, false)
}

func updateWorkEffectAttempt(id, message string, complete bool) error {
	if id == "" {
		return fmt.Errorf("work effect ID is required")
	}
	db, err := Open()
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC().UnixNano()
	var result sql.Result
	if complete {
		result, err = db.Exec(`UPDATE work_effect_outbox SET completed_at=?,
			attempt_count=attempt_count+1,last_attempt_at=?,last_error=''
			WHERE id=? AND completed_at IS NULL`, now, now, id)
	} else {
		result, err = db.Exec(`UPDATE work_effect_outbox SET
			attempt_count=attempt_count+1,last_attempt_at=?,last_error=?
			WHERE id=? AND completed_at IS NULL`, now, message, id)
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM work_effect_outbox WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %s", ErrWorkEffectNotFound, id)
	}
	// Completed rows deliberately ignore duplicate Complete/Fail calls. This
	// makes acknowledgement safe when a transport retries after losing its reply.
	return nil
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
	result, err := insertTodoBinding(state.tx, binding)
	if err != nil {
		return nil, err
	}
	binding.ID, err = result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func (state *WorkStateTx) CurrentSessionBinding(sessionID string) (*TodoSessionBinding, error) {
	return currentTodoBinding(state.tx, sessionID)
}

func (state *WorkStateTx) LatestSessionBinding(sessionID string) (*TodoSessionBinding, error) {
	return latestTodoSessionBinding(state.tx, sessionID)
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

// ArchiveTodos moves todos out of the working set. The rows stay — an
// archived todo can still be named by a dependency or a progress note, and its
// ID is never reused — so this updates archived_at directly and drops the todos
// from the snapshot, which is what loadTodos would return from now on.
func (state *WorkStateTx) ArchiveTodos(ids []string) ([]string, error) {
	return state.moveTodosOutOfWorkingSet(ids, false, "todo archived")
}

// TrashTodos moves todos of any lifecycle status out of the working set. Unlike
// ArchiveTodos this is the recoverable counterpart of Delete: the lifecycle
// state is preserved so RestoreTodos can put the exact task back. Any live
// session binding is closed because an invisible todo must not remain the
// session's current focus.
func (state *WorkStateTx) TrashTodos(ids []string) ([]string, error) {
	return state.ArchiveTodos(ids)
}

func (state *WorkStateTx) moveTodosOutOfWorkingSet(ids []string, requireClosed bool, unbindReason string) ([]string, error) {
	now := time.Now().In(config.Loc).Unix()
	archived := []string{}
	for _, id := range ids {
		todo := FindTodo(state.Todos, id)
		if todo == nil {
			return nil, TodoNotFoundError(state.Todos, id)
		}
		if requireClosed && TodoIsActive(*todo) {
			return nil, fmt.Errorf("cannot archive %s with status %s: finish or drop it first", id, todo.Status)
		}
		if _, err := state.UnbindTodoSessions(id, unbindReason); err != nil {
			return nil, err
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

// RestoreTodos is the product-language counterpart of UnarchiveTodos. Both
// restore the same retained row; keeping the alias lets existing archive users
// continue to use the older command vocabulary.
func (state *WorkStateTx) RestoreTodos(ids []string) ([]string, error) {
	return state.UnarchiveTodos(ids)
}

// PermanentlyDeleteTodos removes live or archived rows. Callers are responsible
// for obtaining explicit confirmation before reaching this irreversible layer.
func (state *WorkStateTx) PermanentlyDeleteTodos(ids []string) ([]string, error) {
	deleted := []string{}
	for _, id := range ids {
		if FindTodo(state.Todos, id) == nil {
			if _, archived := ArchivedStatus(state.Todos, id); !archived {
				return nil, TodoNotFoundError(state.Todos, id)
			}
		}
		if _, err := state.tx.Exec(`DELETE FROM todos WHERE id=?`, id); err != nil {
			return nil, err
		}
		state.Todos.forgetPermanently(id)
		deleted = append(deleted, id)
	}
	return deleted, nil
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

func (file *TodoFile) forgetPermanently(id string) {
	kept := file.Items[:0]
	for _, todo := range file.Items {
		if todo.ID != id {
			kept = append(kept, todo)
		}
	}
	file.Items = kept
	delete(file.baseline, id)
	delete(file.archived, id)
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
