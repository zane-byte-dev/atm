package store

import (
	"database/sql"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// TodoCompletion is one durable completion event for delivery statistics.
// Archived Todos remain in SQLite, so this history does not shrink when the
// working set is cleaned up.
type TodoCompletion struct {
	TodoID        string `json:"todo_id"`
	Title         string `json:"title"`
	Project       string `json:"project"`
	Priority      string `json:"priority"`
	Creator       string `json:"creator"`
	CreatedDate   string `json:"created_date"`
	CompletedDate string `json:"completed_date"`
	CompletedTS   int64  `json:"completed_ts"`
}

// GetTodoCompletions returns every Todo that has a trustworthy completion date.
// Closed is the authoritative local calendar day; DoneTS preserves ordering and
// supplies the day for older rows that only recorded an instant.
func GetTodoCompletions(db *sql.DB) ([]TodoCompletion, error) {
	rows, err := db.Query(`SELECT id,title,project,priority,creator,created,
		COALESCE(closed,''),COALESCE(done_ts,0)
		FROM todos
		WHERE status='done'
			AND ((closed IS NOT NULL AND closed<>'') OR COALESCE(done_ts,0)>0)
		ORDER BY COALESCE(done_ts,0) DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []TodoCompletion{}
	for rows.Next() {
		var result TodoCompletion
		if err := rows.Scan(
			&result.TodoID, &result.Title, &result.Project, &result.Priority,
			&result.Creator, &result.CreatedDate, &result.CompletedDate,
			&result.CompletedTS,
		); err != nil {
			return nil, err
		}
		result.Project = config.CanonicalProject(result.Project)
		if result.CompletedDate == "" && result.CompletedTS > 0 {
			result.CompletedDate = time.Unix(result.CompletedTS, 0).In(config.Loc).Format("2006-01-02")
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
