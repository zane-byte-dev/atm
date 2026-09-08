package store

import (
	"context"
	"database/sql"
	"errors"
)

// TodoReadQuery addresses one task. An empty TodoID resolves the current
// binding for SessionID inside the same snapshot as the task itself.
type TodoReadQuery struct {
	TodoID          string
	SessionID       string
	IncludeArchived bool
}

// TodoReference distinguishes an unknown ID, an absent current binding, and an
// archived task without loading its document or any related collections.
type TodoReference struct {
	ID       string
	Status   string
	Found    bool
	Archived bool
}

func (reference TodoReference) NotFoundError(rawID string) error {
	file := &TodoFile{}
	if reference.Archived {
		file.archived = map[string]string{reference.ID: reference.Status}
	}
	return TodoNotFoundError(file, rawID)
}

type TodoDetail struct {
	Reference    TodoReference
	Todo         Todo
	Bindings     []TodoSessionBinding
	Sessions     []TodoBoundSession
	UserMessages map[string][]string
	LatestPlan   *TodoPlanRevision
}

// ReadTodoReference is the bounded lookup used before reading task Markdown.
// The file is deliberately read after this SQLite snapshot has been released.
func ReadTodoReference(ctx context.Context, input TodoReadQuery) (TodoReference, error) {
	var result TodoReference
	err := withTodoReadSnapshot(ctx, func(query sqlQueryer) error {
		var err error
		result, err = readTodoReference(query, input)
		return err
	})
	return result, err
}

// ReadTodoDetail reads only the addressed task and its related rows. Every
// database-backed field, including current-binding resolution and session name
// candidates, belongs to one read transaction. Local files are not read here.
func ReadTodoDetail(ctx context.Context, input TodoReadQuery) (TodoDetail, error) {
	var result TodoDetail
	err := withTodoReadSnapshot(ctx, func(query sqlQueryer) error {
		var err error
		result, err = readTodoDetail(query, input)
		return err
	})
	return result, err
}

func withTodoReadSnapshot(ctx context.Context, read func(sqlQueryer) error) error {
	db, err := OpenReadOnly()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := read(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func readTodoReference(query sqlQueryer, input TodoReadQuery) (TodoReference, error) {
	id := NormalizeTodoID(input.TodoID)
	if id == "" {
		binding, err := currentTodoBinding(query, input.SessionID)
		if err != nil || binding == nil {
			return TodoReference{}, err
		}
		id = binding.TodoID
	}
	result := TodoReference{ID: id}
	var archivedAt sql.NullInt64
	err := query.QueryRow(`SELECT status,archived_at FROM todos WHERE id=?`, id).Scan(&result.Status, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return TodoReference{}, err
	}
	result.Found, result.Archived = true, archivedAt.Valid
	return result, nil
}

func readTodoDetail(query sqlQueryer, input TodoReadQuery) (TodoDetail, error) {
	reference, err := readTodoReference(query, input)
	result := TodoDetail{Reference: reference}
	if err != nil || !reference.Found || reference.Archived && !input.IncludeArchived {
		return result, err
	}
	todo := &result.Todo
	err = query.QueryRow(`SELECT id,title,description,priority,status,project,wake_condition,
		review_at,maintenance_limit,created,source,creator,closed,closed_reason,on_done,start_ts,done_ts,updated_at
		FROM todos WHERE id=?`, reference.ID).Scan(
		&todo.ID, &todo.Title, &todo.Description, &todo.Priority, &todo.Status, &todo.Project,
		&todo.WakeCondition, &todo.ReviewAt, &todo.MaintenanceLimit, &todo.Created, &todo.Source,
		&todo.Creator, &todo.Closed, &todo.ClosedReason, &todo.OnDone, &todo.StartTS, &todo.DoneTS, &todo.UpdatedAt,
	)
	if err != nil {
		return result, err
	}
	if reference.Archived {
		// Preserve the archive read contract: archived cards expose the same
		// scalar projection, links and images as LoadArchivedTodos.
		todo.WakeCondition, todo.ReviewAt, todo.OnDone = "", "", ""
		todo.MaintenanceLimit, todo.StartTS, todo.DoneTS = 0, nil, nil
	} else {
		todo.Tags, err = readTodoStrings(query, `SELECT tag FROM todo_tags WHERE todo_id=? ORDER BY position,tag`, todo.ID)
		if err != nil {
			return result, err
		}
		todo.DependsOn, err = readTodoStrings(query, `SELECT dependency_id FROM todo_dependencies WHERE todo_id=? ORDER BY position,dependency_id`, todo.ID)
		if err != nil {
			return result, err
		}
	}
	if err := readTodoAttachments(query, todo); err != nil {
		return result, err
	}
	result.Bindings, err = queryTodoBindings(query, todo.ID, false)
	if err != nil {
		return result, err
	}
	result.Sessions, err = findSessionsForTodo(query, todo.ID)
	if err != nil {
		return result, err
	}
	var unnamed []string
	for _, session := range result.Sessions {
		if session.Summary == "" && session.IndexedID != "" {
			unnamed = append(unnamed, session.IndexedID)
		}
	}
	result.UserMessages, err = earliestUserMessages(query, unnamed, 8)
	if err != nil {
		return result, err
	}
	result.LatestPlan, err = latestTodoPlanRevision(query, todo.ID)
	return result, err
}

func readTodoStrings(query sqlQueryer, statement, id string) ([]string, error) {
	rows, err := query.Query(statement, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func readTodoAttachments(query sqlQueryer, todo *Todo) error {
	rows, err := query.Query(`SELECT url,kind,title,relation FROM todo_links WHERE todo_id=? ORDER BY position,url`, todo.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var link TodoLink
		if err := rows.Scan(&link.URL, &link.Kind, &link.Title, &link.Relation); err != nil {
			return err
		}
		todo.Links = append(todo.Links, link)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = query.Query(`SELECT stored_name,original_name,media_type,size_bytes FROM todo_images WHERE todo_id=? ORDER BY position,stored_name`, todo.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var image TodoImage
		if err := rows.Scan(&image.StoredName, &image.Name, &image.MediaType, &image.SizeBytes); err != nil {
			return err
		}
		image.Path = TodoImagePath(todo.ID, image.StoredName)
		todo.Images = append(todo.Images, image)
	}
	return rows.Err()
}
