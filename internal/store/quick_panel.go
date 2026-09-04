package store

import (
	"context"
	"database/sql"
	"strings"
)

const maxQuickTodoRowsPerGroup = 5

// QuickTodoRow keeps the complete persisted Todo needed to calculate its
// optimistic-concurrency ETag, alongside the menu-only lifecycle projection.
// ReadQuickTodos hydrates at most fifteen of these rows.
type QuickTodoRow struct {
	Todo      Todo   `json:"todo"`
	MenuState string `json:"menu_state"`
}

// QuickTodoCounts are exact whole-working-set counts. Working is the legacy
// work.working count: every in_progress row, including due and externally
// waiting work. PureWorking is the classified working-only subset.
type QuickTodoCounts struct {
	Review      int `json:"review"`
	Blocked     int `json:"blocked"`
	Due         int `json:"due"`
	Working     int `json:"working"`
	PureWorking int `json:"pure_working"`
	Waiting     int `json:"waiting"`
}

// QuickTodoSnapshot is a bounded menu projection. Working intentionally
// overlaps NeedsAction for due in-progress work and Waiting for externally
// waiting in-progress work, preserving the original quick-panel semantics.
// Summary remains exact even when the row pages are truncated.
type QuickTodoSnapshot struct {
	NeedsAction []QuickTodoRow  `json:"needs_action"`
	Working     []QuickTodoRow  `json:"working"`
	Waiting     []QuickTodoRow  `json:"waiting"`
	Summary     QuickTodoCounts `json:"summary"`
	Truncated   bool            `json:"truncated"`
}

const quickTodoClassifiedCTE = `WITH classified AS (
	SELECT id,priority,created,status,
		CASE
			WHEN status='review' THEN 'review'
			WHEN status='blocked' THEN 'blocked'
			WHEN status IN ('in_progress','waiting') AND review_at<>'' AND review_at<=? THEN 'due'
			WHEN status IN ('in_progress','waiting') AND (trim(wake_condition)<>'' OR review_at<>'') THEN 'waiting'
			WHEN status IN ('in_progress','waiting') THEN 'working'
			ELSE ''
		END AS menu_state
	FROM todos WHERE archived_at IS NULL
) `

const quickTodoWithinGroupOrderSQL = ` ORDER BY
	CASE priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 WHEN 'P3' THEN 3 ELSE 99 END,
	created,
	id`

const quickTodoNeedsActionOrderSQL = ` ORDER BY
	CASE menu_state WHEN 'review' THEN 0 WHEN 'blocked' THEN 1 WHEN 'due' THEN 2 ELSE 3 END,
	CASE priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 WHEN 'P3' THEN 3 ELSE 99 END,
	created,
	id`

// ReadQuickTodos returns exact counters and at most limit rows from each menu
// group. limit is capped so a caller cannot accidentally turn the quick panel
// into a full Todo export. Candidate selection reads IDs only; the complete
// scalar and child state needed by work.TodoETag is fetched solely for those
// selected IDs.
func ReadQuickTodos(ctx context.Context, db *sql.DB, today string, limit int) (QuickTodoSnapshot, error) {
	result := QuickTodoSnapshot{
		NeedsAction: []QuickTodoRow{},
		Working:     []QuickTodoRow{},
		Waiting:     []QuickTodoRow{},
	}
	if limit <= 0 || limit > maxQuickTodoRowsPerGroup {
		limit = maxQuickTodoRowsPerGroup
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx, quickTodoClassifiedCTE+`SELECT
		COALESCE(SUM(menu_state='review'),0),
		COALESCE(SUM(menu_state='blocked'),0),
		COALESCE(SUM(menu_state='due'),0),
		COALESCE(SUM(status='in_progress'),0),
		COALESCE(SUM(menu_state='working'),0),
		COALESCE(SUM(menu_state='waiting'),0)
		FROM classified`, today).Scan(
		&result.Summary.Review, &result.Summary.Blocked, &result.Summary.Due,
		&result.Summary.Working, &result.Summary.PureWorking, &result.Summary.Waiting,
	); err != nil {
		return result, err
	}

	needsAction, err := readQuickTodoCandidates(ctx, tx, today,
		`menu_state IN ('review','blocked','due')`, quickTodoNeedsActionOrderSQL, limit)
	if err != nil {
		return result, err
	}
	working, err := readQuickTodoCandidates(ctx, tx, today, `status='in_progress'`, quickTodoWithinGroupOrderSQL, limit)
	if err != nil {
		return result, err
	}
	waiting, err := readQuickTodoCandidates(ctx, tx, today, `menu_state='waiting'`, quickTodoWithinGroupOrderSQL, limit)
	if err != nil {
		return result, err
	}

	all := make([]quickTodoCandidate, 0, len(needsAction)+len(working)+len(waiting))
	all = append(all, needsAction...)
	all = append(all, working...)
	all = append(all, waiting...)
	todos, err := readQuickTodoDetails(ctx, tx, all)
	if err != nil {
		return result, err
	}
	result.NeedsAction = assembleQuickTodoRows(needsAction, todos)
	result.Working = assembleQuickTodoRows(working, todos)
	result.Waiting = assembleQuickTodoRows(waiting, todos)
	needsActionCount := result.Summary.Review + result.Summary.Blocked + result.Summary.Due
	result.Truncated = needsActionCount > len(result.NeedsAction) ||
		result.Summary.Working > len(result.Working) ||
		result.Summary.Waiting > len(result.Waiting)
	if err := tx.Commit(); err != nil {
		return QuickTodoSnapshot{
			NeedsAction: []QuickTodoRow{}, Working: []QuickTodoRow{}, Waiting: []QuickTodoRow{},
		}, err
	}
	return result, nil
}

type quickTodoCandidate struct {
	id        string
	menuState string
}

func readQuickTodoCandidates(ctx context.Context, tx *sql.Tx, today, filter, order string, limit int) ([]quickTodoCandidate, error) {
	rows, err := tx.QueryContext(ctx, quickTodoClassifiedCTE+
		`SELECT id,menu_state FROM classified WHERE `+filter+order+` LIMIT ?`,
		today, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]quickTodoCandidate, 0, limit)
	for rows.Next() {
		var item quickTodoCandidate
		if err := rows.Scan(&item.id, &item.menuState); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readQuickTodoDetails(ctx context.Context, tx *sql.Tx, candidates []quickTodoCandidate) (map[string]Todo, error) {
	result := make(map[string]Todo, len(candidates))
	if len(candidates) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, item := range candidates {
		if !seen[item.id] {
			seen[item.id] = true
			ids = append(ids, item.id)
		}
	}
	placeholders, args := quickTodoQueryArgs(ids)
	rows, err := tx.QueryContext(ctx, `SELECT id,title,description,priority,status,project,wake_condition,
		review_at,maintenance_limit,created,source,creator,closed,closed_reason,on_done,start_ts,done_ts
		FROM todos WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var todo Todo
		if err := rows.Scan(&todo.ID, &todo.Title, &todo.Description, &todo.Priority, &todo.Status,
			&todo.Project, &todo.WakeCondition, &todo.ReviewAt, &todo.MaintenanceLimit,
			&todo.Created, &todo.Source, &todo.Creator, &todo.Closed, &todo.ClosedReason,
			&todo.OnDone, &todo.StartTS, &todo.DoneTS); err != nil {
			rows.Close()
			return nil, err
		}
		result[todo.ID] = todo
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	if err := readQuickTodoStrings(ctx, tx, result, ids, `SELECT todo_id,tag FROM todo_tags
		WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,tag`, func(todo *Todo, value string) {
		todo.Tags = append(todo.Tags, value)
	}); err != nil {
		return nil, err
	}
	if err := readQuickTodoStrings(ctx, tx, result, ids, `SELECT todo_id,dependency_id FROM todo_dependencies
		WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,dependency_id`, func(todo *Todo, value string) {
		todo.DependsOn = append(todo.DependsOn, value)
	}); err != nil {
		return nil, err
	}
	if err := readQuickTodoLinks(ctx, tx, result, args, placeholders); err != nil {
		return nil, err
	}
	if err := readQuickTodoImages(ctx, tx, result, args, placeholders); err != nil {
		return nil, err
	}
	return result, nil
}

func quickTodoQueryArgs(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

func readQuickTodoStrings(ctx context.Context, tx *sql.Tx, todos map[string]Todo, ids []string,
	query string, appendValue func(*Todo, string)) error {
	_, args := quickTodoQueryArgs(ids)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			return err
		}
		todo, ok := todos[id]
		if !ok {
			continue
		}
		appendValue(&todo, value)
		todos[id] = todo
	}
	return rows.Err()
}

func readQuickTodoLinks(ctx context.Context, tx *sql.Tx, todos map[string]Todo, args []any, placeholders string) error {
	rows, err := tx.QueryContext(ctx, `SELECT todo_id,url,kind,title,relation FROM todo_links
		WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,url`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var link TodoLink
		if err := rows.Scan(&id, &link.URL, &link.Kind, &link.Title, &link.Relation); err != nil {
			return err
		}
		todo, ok := todos[id]
		if !ok {
			continue
		}
		todo.Links = append(todo.Links, link)
		todos[id] = todo
	}
	return rows.Err()
}

func readQuickTodoImages(ctx context.Context, tx *sql.Tx, todos map[string]Todo, args []any, placeholders string) error {
	rows, err := tx.QueryContext(ctx, `SELECT todo_id,stored_name,original_name,media_type,size_bytes
		FROM todo_images WHERE todo_id IN (`+placeholders+`) ORDER BY todo_id,position,stored_name`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var image TodoImage
		if err := rows.Scan(&id, &image.StoredName, &image.Name, &image.MediaType, &image.SizeBytes); err != nil {
			return err
		}
		todo, ok := todos[id]
		if !ok {
			continue
		}
		image.Path = TodoImagePath(id, image.StoredName)
		todo.Images = append(todo.Images, image)
		todos[id] = todo
	}
	return rows.Err()
}

func assembleQuickTodoRows(candidates []quickTodoCandidate, todos map[string]Todo) []QuickTodoRow {
	result := make([]QuickTodoRow, 0, len(candidates))
	for _, candidate := range candidates {
		if todo, ok := todos[candidate.id]; ok {
			result = append(result, QuickTodoRow{Todo: todo, MenuState: candidate.menuState})
		}
	}
	return result
}
