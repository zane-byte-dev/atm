package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"modernc.org/sqlite"
)

// EmptyTodoSelectionRevision is also used when no database exists yet.
const EmptyTodoSelectionRevision = "todo-members-v1-e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func init() {
	// Match the workbench's zh-CN numeric ordering inside SQLite, so sorting a
	// page never requires loading and sorting every Todo in Go first. Collators
	// hold scratch buffers and cannot be shared between concurrent connections.
	pool := sync.Pool{New: func() any { return collate.New(language.SimplifiedChinese, collate.Numeric) }}
	sqlite.MustRegisterCollationUtf8("atm_task_order", func(left, right string) int {
		c := pool.Get().(*collate.Collator)
		result := c.CompareString(left, right)
		pool.Put(c)
		return result
	})
	sqlite.MustRegisterDeterministicScalarFunction("atm_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, _ := args[0].(string)
		return strings.ToLower(value), nil
	})
}

// ValidateTodoWorkbenchQuery rejects unknown selectors before constructing SQL.
// SQL fragments below are chosen from constants; all user values are bound.
func ValidateTodoWorkbenchQuery(input TodoPageQuery) error {
	if input.Limit < 1 || input.Limit > 200 || input.Offset < 0 {
		return fmt.Errorf("limit must be 1–200 and offset non-negative")
	}
	for name, field := range map[string]struct{ value, allowed string }{
		"focus":     {input.Focus, ",all,unfinished,review,waiting"},
		"sort":      {input.Sort, ",auto,updated,title,created,priority"},
		"direction": {input.Direction, ",asc,desc"},
		"group_by":  {input.GroupBy, ",none,status,project,project_status"},
		"partition": {input.Partition, ",active,done"},
	} {
		if !containsTodoSelector(strings.Split(field.allowed, ","), field.value) {
			return fmt.Errorf("invalid todo %s", name)
		}
	}
	if input.Status != "" && input.Status != "all" && input.Status != "archived" {
		for _, status := range strings.Split(input.Status, ",") {
			if !containsTodoSelector([]string{"open", "in_progress", "review", "done"}, status) {
				return fmt.Errorf("invalid todo status")
			}
		}
	}
	if input.Priority != "" {
		for _, priority := range strings.Split(input.Priority, ",") {
			if !containsTodoSelector([]string{"P0", "P1", "P2", "P3"}, priority) {
				return fmt.Errorf("invalid todo priority")
			}
		}
	}
	for _, day := range []string{input.From, input.To} {
		if day != "" {
			if _, err := time.Parse("2006-01-02", day); err != nil {
				return fmt.Errorf("invalid todo created date")
			}
		}
	}
	if input.GroupKey != "" {
		var key []string
		if err := json.Unmarshal([]byte(input.GroupKey), &key); err != nil || len(key) != 2 {
			return fmt.Errorf("invalid todo group_key")
		}
		if (input.GroupBy == "" || input.GroupBy == "none") && (key[0] != "" || key[1] != "") ||
			input.GroupBy == "status" && key[0] != "" || input.GroupBy == "project" && key[1] != "" {
			return fmt.Errorf("todo group_key does not match group_by")
		}
	}
	return nil
}

func containsTodoSelector(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func readTodoWorkbenchPage(ctx context.Context, input TodoPageQuery) (TodoPage, error) {
	result := TodoPage{Todos: []Todo{}}
	if err := ValidateTodoWorkbenchQuery(input); err != nil {
		return result, err
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

	if input.MetadataOnly {
		if err := readTodoWorkbenchMetadata(ctx, tx, input, &result); err != nil {
			return result, err
		}
		result.Total = result.MatchedTotal
		return result, tx.Commit()
	}
	where, args := todoWorkbenchSelection(input)
	if input.GroupKey != "" {
		var key []string
		_ = json.Unmarshal([]byte(input.GroupKey), &key) // validated above
		if input.GroupBy == "project" || input.GroupBy == "project_status" {
			where += " AND t.project=?"
			args = append(args, key[0])
		}
		if input.GroupBy == "status" || input.GroupBy == "project_status" {
			where += " AND t.status=?"
			args = append(args, key[1])
		}
	}
	if input.Partition == "active" {
		where += " AND t.status<>'done'"
	} else if input.Partition == "done" {
		where += " AND t.status='done'"
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM todos t`+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, input.Limit, input.Offset)
	rows, err := tx.QueryContext(ctx, `SELECT t.id,t.title,t.priority,t.status,t.project,t.wake_condition,t.review_at,t.created,t.done_ts,t.creator,t.updated_at
		FROM todos t`+where+` ORDER BY `+todoWorkbenchOrder(input)+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(&todo.ID, &todo.Title, &todo.Priority, &todo.Status, &todo.Project, &todo.WakeCondition, &todo.ReviewAt, &todo.Created, &todo.DoneTS, &todo.Creator, &todo.UpdatedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Todos = append(result.Todos, todo)
	}
	if err := closeTodoPageRows(rows); err != nil {
		return result, err
	}
	if err := readTodoPageRelations(ctx, tx, result.Todos); err != nil {
		return result, err
	}
	return result, tx.Commit()
}

func todoWorkbenchSelection(input TodoPageQuery) (string, []any) {
	where := " WHERE t.archived_at IS NULL"
	if input.Status == "archived" {
		where = " WHERE t.archived_at IS NOT NULL"
	}
	var args []any
	switch input.Focus {
	case "unfinished":
		where += " AND t.status<>'done'"
	case "review":
		where += " AND t.status='review'"
	case "waiting":
		where += " AND " + todoWaitingSQL
	}
	for _, field := range []struct{ column, value string }{{"status", input.Status}, {"priority", input.Priority}} {
		if field.value == "" || field.value == "all" || field.value == "archived" {
			continue
		}
		values := strings.Split(field.value, ",")
		where += " AND t." + field.column + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(values)), ",") + ")"
		for _, value := range values {
			args = append(args, value)
		}
	}
	for _, field := range []struct{ column, value string }{{"project", input.Project}, {"creator", input.Creator}} {
		if field.value != "" {
			where += " AND t." + field.column + "=?"
			args = append(args, field.value)
		}
	}
	if input.Tag != "" {
		where += " AND EXISTS (SELECT 1 FROM todo_tags tags WHERE tags.todo_id=t.id AND tags.tag=?)"
		args = append(args, input.Tag)
	}
	if input.From != "" {
		where += " AND t.created<>'' AND SUBSTR(t.created,1,10)>=?"
		args = append(args, input.From)
	}
	if input.To != "" {
		where += " AND t.created<>'' AND SUBSTR(t.created,1,10)<=?"
		args = append(args, input.To)
	}
	if query := strings.TrimSpace(input.Query); query != "" {
		// The browser searches one concatenated metadata string, including phrases
		// crossing field boundaries. Keep the tag order used by the public DTO.
		where += ` AND INSTR(atm_lower(t.title||' '||t.id||' '||t.project||' '||t.creator||' '||
			COALESCE((SELECT GROUP_CONCAT(tag,' ') FROM (SELECT tag FROM todo_tags WHERE todo_id=t.id ORDER BY position,tag)),'')),?)>0`
		args = append(args, strings.ToLower(query))
	}
	return where, args
}

const todoWaitingSQL = `(t.status='in_progress' AND (t.wake_condition<>'' OR t.review_at<>''))`

func todoWorkbenchOrder(input TodoPageQuery) string {
	direction := " ASC"
	if input.Direction == "desc" {
		direction = " DESC"
	}
	id := "CAST(SUBSTR(t.id,2) AS INTEGER) ASC,t.id ASC"
	switch input.Sort {
	case "updated":
		return "t.updated_at" + direction + "," + id
	case "title", "created", "priority":
		return "t." + input.Sort + " COLLATE atm_task_order" + direction + "," + id
	default:
		// Auto keeps active tasks in priority order and completed tasks in their
		// own most-recent-first tail, independently of the priority direction.
		return "(t.status='done') ASC,CASE WHEN t.status='done' THEN t.updated_at END DESC," +
			"CASE WHEN t.status='done' THEN CAST(SUBSTR(t.id,2) AS INTEGER) END DESC," +
			"CASE WHEN t.status<>'done' THEN t.priority END COLLATE atm_task_order" + direction + "," + id
	}
}

func readTodoWorkbenchMetadata(ctx context.Context, tx *sql.Tx, input TodoPageQuery, result *TodoPage) error {
	result.Groups = []TodoPageGroup{}
	result.Projects = []string{}
	result.Facets = map[string][]TodoPageFacet{}
	result.Counts = map[string]int{"all": 0, "open": 0, "in_progress": 0, "review": 0, "done": 0, "archived": 0}
	result.ViewCounts = map[string]int{"all": 0, "unfinished": 0, "review": 0, "waiting": 0}
	where, args := todoWorkbenchSelection(input)
	project, status := "''", "''"
	if input.GroupBy == "project" || input.GroupBy == "project_status" {
		project = "t.project"
	}
	if input.GroupBy == "status" || input.GroupBy == "project_status" {
		status = "t.status"
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+project+`,`+status+`,COUNT(*),SUM(t.status<>'done'),SUM(t.status='done') FROM todos t`+where+
		` GROUP BY 1,2 ORDER BY 1 COLLATE atm_task_order,CASE `+status+` WHEN 'open' THEN 0 WHEN 'in_progress' THEN 1 WHEN 'review' THEN 2 ELSE 3 END`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var group TodoPageGroup
		if err := rows.Scan(&group.Project, &group.Status, &group.Total, &group.ActiveTotal, &group.DoneTotal); err != nil {
			rows.Close()
			return err
		}
		key, _ := json.Marshal([]string{group.Project, group.Status})
		group.Key = string(key)
		result.MatchedTotal += group.Total
		result.Groups = append(result.Groups, group)
	}
	if err := closeTodoPageRows(rows); err != nil {
		return err
	}
	// Selection describes matching members, not how they are sorted/grouped or
	// how many have been loaded. Stream scalar IDs into a digest so equal-sized
	// membership replacements are detectable without hydrating every Todo.
	rows, err = tx.QueryContext(ctx, `SELECT t.id FROM todos t`+where+` ORDER BY t.id`, args...)
	if err != nil {
		return err
	}
	digest := sha256.New()
	var size [8]byte
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(id)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(id))
	}
	if err := closeTodoPageRows(rows); err != nil {
		return err
	}
	result.SelectionRevision = "todo-members-v1-" + hex.EncodeToString(digest.Sum(nil))

	// Built-in view badges replace focus and clear status, like selecting that
	// view in the URL. Other facets and metadata search stay in force.
	viewInput := input
	viewInput.Focus, viewInput.Status = "all", ""
	where, args = todoWorkbenchSelection(viewInput)
	var all, unfinished, review, waiting int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(t.status<>'done'),0),COALESCE(SUM(t.status='review'),0),COALESCE(SUM(`+todoWaitingSQL+`),0) FROM todos t`+where, args...).Scan(&all, &unfinished, &review, &waiting); err != nil {
		return err
	}
	result.ViewCounts = map[string]int{"all": all, "unfinished": unfinished, "review": review, "waiting": waiting}

	// Each facet is a grouped SQL aggregate after replacing just that filter.
	// Distinct option values come from the entire active/archive scope, so zero
	// counts remain selectable even when the current page contains none of them.
	for _, field := range []string{"status", "priority", "project", "creator", "tag"} {
		facetInput := input
		switch field {
		case "status":
			facetInput.Status, facetInput.Focus = "", "all"
		case "priority":
			facetInput.Priority = ""
		case "project":
			facetInput.Project = ""
		case "creator":
			facetInput.Creator = ""
		case "tag":
			facetInput.Tag = ""
		}
		where, args = todoWorkbenchSelection(facetInput)
		column, from := "t."+field, "todos t"
		if field == "tag" {
			column, from = "tags.tag", "todos t JOIN todo_tags tags ON tags.todo_id=t.id"
		}
		if field == "status" {
			column = "CASE WHEN t.archived_at IS NULL THEN t.status ELSE 'archived' END"
			where = strings.Replace(where, " WHERE t.archived_at IS NULL", " WHERE 1=1", 1)
		}
		counts := map[string]int{}
		rows, err = tx.QueryContext(ctx, `SELECT `+column+`,COUNT(*) FROM `+from+where+` GROUP BY 1`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var value string
			var count int
			if err := rows.Scan(&value, &count); err != nil {
				rows.Close()
				return err
			}
			counts[value] = count
		}
		if err := closeTodoPageRows(rows); err != nil {
			return err
		}
		options := []string{}
		if field == "status" {
			options = []string{"open", "in_progress", "review", "done", "archived"}
			for _, value := range options {
				result.Counts[value] = counts[value]
				if value != "archived" {
					result.Counts["all"] += counts[value]
				}
			}
		} else if field == "priority" {
			options = []string{"P0", "P1", "P2", "P3"}
		} else {
			archive := "t.archived_at IS NULL"
			if input.Status == "archived" {
				archive = "t.archived_at IS NOT NULL"
			}
			rows, err = tx.QueryContext(ctx, `SELECT DISTINCT `+column+` FROM `+from+` WHERE `+archive+` AND `+column+`<>'' ORDER BY 1 COLLATE atm_task_order`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var value string
				if err := rows.Scan(&value); err != nil {
					rows.Close()
					return err
				}
				options = append(options, value)
			}
			if err := closeTodoPageRows(rows); err != nil {
				return err
			}
		}
		result.Facets[field] = []TodoPageFacet{}
		for _, value := range options {
			result.Facets[field] = append(result.Facets[field], TodoPageFacet{Value: value, Count: counts[value]})
		}
		if field == "project" {
			result.Projects = options
		}
	}
	return nil
}

func closeTodoPageRows(rows *sql.Rows) error {
	if err := rows.Close(); err != nil {
		return err
	}
	return rows.Err()
}

func readTodoPageRelations(ctx context.Context, tx *sql.Tx, todos []Todo) error {
	if len(todos) == 0 {
		return nil
	}
	ids := make([]any, len(todos))
	byID := make(map[string]*Todo, len(todos))
	for index := range todos {
		ids[index] = todos[index].ID
		byID[todos[index].ID] = &todos[index]
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(todos)), ",")
	for _, relation := range []struct{ table, column string }{{"todo_tags", "tag"}, {"todo_dependencies", "dependency_id"}} {
		rows, err := tx.QueryContext(ctx, `SELECT todo_id,`+relation.column+` FROM `+relation.table+` WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,`+relation.column, ids...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return err
			}
			if relation.column == "tag" {
				byID[id].Tags = append(byID[id].Tags, value)
			} else {
				byID[id].DependsOn = append(byID[id].DependsOn, value)
			}
		}
		if err := closeTodoPageRows(rows); err != nil {
			return err
		}
	}
	return nil
}
