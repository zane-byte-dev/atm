package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	TaskRunStarting  = "starting"
	TaskRunRunning   = "running"
	TaskRunCompleted = "completed"
	TaskRunFailed    = "failed"
)

// TaskRun is one explicitly dispatched Agent process. It is execution evidence,
// not Todo lifecycle: completion can move work to review, never to done.
type TaskRun struct {
	ID        string  `json:"id"`
	TodoID    string  `json:"todo_id"`
	Agent     string  `json:"agent"`
	Project   string  `json:"project,omitempty"`
	WorkDir   string  `json:"work_dir"`
	Prompt    string  `json:"-"`
	Policy    string  `json:"policy"`
	LogPath   string  `json:"log_path"`
	Status    string  `json:"status"`
	PID       int     `json:"pid,omitempty"`
	StartTS   int64   `json:"start_ts"`
	EndTS     *int64  `json:"end_ts,omitempty"`
	ExitCode  *int    `json:"exit_code,omitempty"`
	Message   string  `json:"message,omitempty"`
	SessionID *string `json:"session_id,omitempty"`
}

const taskRunColumns = `id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,end_ts,exit_code,message,session_id`

// CreateTaskRun also claims the Todo: the partial unique index refuses a second
// starting/running row until the first controller records an outcome.
func CreateTaskRun(db *sql.DB, run TaskRun) error {
	_, err := db.Exec(`INSERT INTO task_runs
		(id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,message)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.TodoID, run.Agent, run.Project, run.WorkDir, run.Prompt, run.Policy,
		run.LogPath, run.Status, run.PID, run.StartTS, run.Message)
	if err != nil && (strings.Contains(err.Error(), "idx_task_runs_active_todo") ||
		strings.Contains(err.Error(), "task_runs.todo_id")) {
		return fmt.Errorf("todo %s already has an active agent run", run.TodoID)
	}
	return err
}

func GetTaskRun(db *sql.DB, id string) (*TaskRun, error) {
	var run TaskRun
	err := scanTaskRun(db.QueryRow(`SELECT `+taskRunColumns+` FROM task_runs WHERE id=?`, id), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// MarkTaskRunRunning is deliberately conditional: a late controller must not
// revive a run that dispatch already closed as failed.
func MarkTaskRunRunning(db *sql.DB, id string, pid int) error {
	result, err := db.Exec(`UPDATE task_runs SET status=?,pid=? WHERE id=? AND status=?`,
		TaskRunRunning, pid, id, TaskRunStarting)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("task run %s is not starting", id)
	}
	return nil
}

func FinishTaskRun(db *sql.DB, id string, exitCode int, endTS int64, message string) error {
	status := TaskRunCompleted
	if exitCode != 0 {
		status = TaskRunFailed
	}
	result, err := db.Exec(`UPDATE task_runs
		SET status=?,exit_code=?,end_ts=?,message=?
		WHERE id=? AND status IN (?,?)`, status, exitCode, endTS, message, id,
		TaskRunStarting, TaskRunRunning)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("task run %s is not active", id)
	}
	return nil
}

func ListTaskRuns(db *sql.DB, todoID string) ([]TaskRun, error) {
	rows, err := db.Query(`SELECT `+taskRunColumns+` FROM task_runs
		WHERE todo_id=? ORDER BY start_ts DESC,id DESC`, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []TaskRun
	for rows.Next() {
		var run TaskRun
		if err := scanTaskRun(rows, &run); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func LatestTaskRun(db *sql.DB, todoID string) (*TaskRun, error) {
	var run TaskRun
	err := scanTaskRun(db.QueryRow(`SELECT `+taskRunColumns+` FROM task_runs
		WHERE todo_id=? ORDER BY start_ts DESC,id DESC LIMIT 1`, todoID), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func ActiveTaskRun(db *sql.DB, todoID string) (*TaskRun, error) {
	var run TaskRun
	err := scanTaskRun(db.QueryRow(`SELECT `+taskRunColumns+` FROM task_runs
		WHERE todo_id=? AND status IN (?,?) ORDER BY start_ts DESC,id DESC LIMIT 1`,
		todoID, TaskRunStarting, TaskRunRunning), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// linkTaskRun attaches a newly indexed Agent session to the closest unlinked
// explicit run. Session binding remains the Todo relationship; this field is
// execution evidence used to navigate from a run into the existing monitor.
func linkTaskRun(tx *sql.Tx, sessionID, agent, project string, createdTS int64) error {
	if sessionID == "" || agent == "" || createdTS <= 0 {
		return nil
	}
	_, err := tx.Exec(`UPDATE task_runs SET session_id=? WHERE id=(
		SELECT id FROM task_runs
		WHERE session_id IS NULL AND agent=?
			AND start_ts<=? AND ?<=COALESCE(end_ts,start_ts+86400)+300
			AND (?='' OR project=? OR work_dir LIKE ?)
		ORDER BY start_ts DESC LIMIT 1)`,
		sessionID, agent, createdTS+300, createdTS, project, project,
		"%/"+filepath.Base(project))
	return err
}

type taskRunScanner interface {
	Scan(dest ...any) error
}

func scanTaskRun(scanner taskRunScanner, run *TaskRun) error {
	return scanner.Scan(&run.ID, &run.TodoID, &run.Agent, &run.Project, &run.WorkDir,
		&run.Prompt, &run.Policy, &run.LogPath, &run.Status, &run.PID, &run.StartTS,
		&run.EndTS, &run.ExitCode, &run.Message, &run.SessionID)
}
