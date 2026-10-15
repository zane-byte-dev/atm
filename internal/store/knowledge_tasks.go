package store

import (
	"context"
	"errors"
)

type LinkedKnowledgeTask struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Archived bool   `json:"archived"`
}

// Read only the matching link rows, without opening task documents or scanning
// the collected session history. Archived tasks remain traceable.
func ReadKnowledgeTasks(ctx context.Context, link string, offset, limit int) ([]LinkedKnowledgeTask, error) {
	result := []LinkedKnowledgeTask{}
	db, err := OpenReadOnly()
	if errors.Is(err, ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT t.id,t.title,t.status,t.archived_at IS NOT NULL
        FROM todos t JOIN todo_links l ON l.todo_id=t.id WHERE l.url=?
        ORDER BY t.archived_at IS NOT NULL, t.updated_at DESC, CAST(SUBSTR(t.id,2) AS INTEGER) DESC LIMIT ? OFFSET ?`, link, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item LinkedKnowledgeTask
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.Archived); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
