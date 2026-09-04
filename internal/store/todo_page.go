package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// TodoPageQuery is the bounded scalar read used by interactive list views.
// Full-text document search remains a separate, deliberately slower path until
// ATM has a durable search index.
type TodoPageQuery struct {
	Status  string
	Project string
	Limit   int
	Offset  int
}

type TodoPage struct {
	Todos    []Todo
	Total    int
	Counts   map[string]int
	Projects []string
}

// ReadTodoPage keeps counts, the selected page and its dependencies on one
// SQLite snapshot. It never loads descriptions, links, images, tags or the
// complete Todo set merely to render a bounded navigator page.
func ReadTodoPage(ctx context.Context, input TodoPageQuery) (TodoPage, error) {
	result := TodoPage{
		Todos:    []Todo{},
		Counts:   map[string]int{"all": 0, "open": 0, "in_progress": 0, "review": 0, "done": 0, "archived": 0},
		Projects: []string{},
	}
	db, err := OpenReadOnly()
	if err != nil {
		return result, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT project FROM todos WHERE project<>'' ORDER BY project`)
	if err != nil {
		return result, err
	}
	var matchingProjects []string
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			rows.Close()
			return result, err
		}
		result.Projects = append(result.Projects, project)
		if config.ProjectMatches(project, input.Project) {
			matchingProjects = append(matchingProjects, project)
		}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	scope, scopeArgs, possible := todoPageProjectScope(input.Project, matchingProjects)
	if !possible {
		return result, tx.Commit()
	}
	rows, err = tx.QueryContext(ctx, `SELECT CASE WHEN archived_at IS NULL THEN status ELSE 'archived' END,COUNT(*) FROM todos`+scope+` GROUP BY 1`, scopeArgs...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return result, err
		}
		result.Counts[status] += count
		if status != "archived" {
			result.Counts["all"] += count
		}
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	where, args := todoPageSelection(input.Status, scope, scopeArgs)
	if input.Status == "archived" {
		result.Total = result.Counts["archived"]
	} else if input.Status == "" || input.Status == "all" {
		result.Total = result.Counts["all"]
	} else {
		result.Total = result.Counts[input.Status]
	}
	args = append(args, input.Limit, input.Offset)
	rows, err = tx.QueryContext(ctx, `SELECT id,title,priority,status,project,wake_condition,review_at,created
		FROM todos`+where+` ORDER BY created DESC,CAST(SUBSTR(id,2) AS INTEGER) DESC,id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(&todo.ID, &todo.Title, &todo.Priority, &todo.Status, &todo.Project, &todo.WakeCondition, &todo.ReviewAt, &todo.Created); err != nil {
			rows.Close()
			return result, err
		}
		result.Todos = append(result.Todos, todo)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := rows.Err(); err != nil {
		return result, err
	}

	if len(result.Todos) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(result.Todos)), ",")
		ids := make([]any, 0, len(result.Todos))
		byID := make(map[string]*Todo, len(result.Todos))
		for index := range result.Todos {
			ids = append(ids, result.Todos[index].ID)
			byID[result.Todos[index].ID] = &result.Todos[index]
		}
		rows, err = tx.QueryContext(ctx, `SELECT todo_id,dependency_id FROM todo_dependencies WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,dependency_id`, ids...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var todoID, dependencyID string
			if err := rows.Scan(&todoID, &dependencyID); err != nil {
				rows.Close()
				return result, err
			}
			if todo := byID[todoID]; todo != nil {
				todo.DependsOn = append(todo.DependsOn, dependencyID)
			}
		}
		if err := rows.Close(); err != nil {
			return result, err
		}
		if err := rows.Err(); err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}

func todoPageProjectScope(filter string, matching []string) (string, []any, bool) {
	if strings.TrimSpace(filter) == "" {
		return "", nil, true
	}
	if len(matching) == 0 {
		return "", nil, false
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(matching)), ",")
	args := make([]any, len(matching))
	for index := range matching {
		args[index] = matching[index]
	}
	return " WHERE project IN (" + placeholders + ")", args, true
}

func todoPageSelection(status, scope string, scopeArgs []any) (string, []any) {
	where := scope
	join := " WHERE "
	if where != "" {
		join = " AND "
	}
	args := append([]any{}, scopeArgs...)
	if status == "archived" {
		return where + join + "archived_at IS NOT NULL", args
	}
	where += join + "archived_at IS NULL"
	if status != "" && status != "all" {
		where += " AND status=?"
		args = append(args, status)
	}
	return where, args
}
