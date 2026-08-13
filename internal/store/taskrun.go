package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	TaskRunStarting    = "starting"
	TaskRunRunning     = "running"
	TaskRunCompleted   = "completed"
	TaskRunFailed      = "failed"
	TaskRunInterrupted = "interrupted"
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
	// ResumeSessionID is intent, SessionID is evidence. Set only when the run was
	// dispatched with `--continue`, it tells the controller to resume that agent
	// thread instead of starting a fresh one, and it never changes as the run's
	// own session identity is discovered or its outcome message is written.
	ResumeSessionID *string `json:"resume_session_id,omitempty"`
}

const taskRunColumns = `id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,end_ts,exit_code,message,session_id,resume_session_id`

// CreateTaskRun also claims the Todo: the partial unique index refuses a second
// starting/running row until the first controller records an outcome.
func CreateTaskRun(db *sql.DB, run TaskRun) error {
	_, err := db.Exec(`INSERT INTO task_runs
		(id,todo_id,agent,project,work_dir,prompt,policy,log_path,status,pid,start_ts,message,session_id,resume_session_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.TodoID, run.Agent, run.Project, run.WorkDir, run.Prompt, run.Policy,
		run.LogPath, run.Status, run.PID, run.StartTS, run.Message, run.SessionID, run.ResumeSessionID)
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

// RecordTaskRunControllerPID closes the small dispatch window between starting
// the detached controller and that controller marking itself running. Keeping a
// PID on either active state lets an immediate user interrupt reach the process.
func RecordTaskRunControllerPID(db *sql.DB, id string, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("task run controller pid must be positive")
	}
	result, err := db.Exec(`UPDATE task_runs SET pid=? WHERE id=? AND status IN (?,?)`,
		pid, id, TaskRunStarting, TaskRunRunning)
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

// InterruptTaskRun records a user-requested stop without conflating it with an
// Agent failure. The conditional update makes a finish/interrupt race honest:
// whichever outcome reaches the active row first wins.
func InterruptTaskRun(db *sql.DB, id string, endTS int64, message string) error {
	result, err := db.Exec(`UPDATE task_runs
		SET status=?,exit_code=NULL,end_ts=?,message=?
		WHERE id=? AND status IN (?,?)`, TaskRunInterrupted, endTS, message, id,
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

// LatestResumableTaskRun returns the most recent finished run whose Codex
// thread can be resumed. Active runs are deliberately excluded: a follow-up is
// a new turn after an outcome, never a second controller racing the first.
func LatestResumableTaskRun(db *sql.DB, todoID string) (*TaskRun, error) {
	return LatestResumableTaskRunForAgent(db, todoID, "codex")
}

// LatestResumableTaskRunForAgent keeps continuation inside the selected Agent's
// own session history when a Todo has runs from more than one provider.
//
// The `agent` filter outlived multi-agent dispatch and has to stay. Dispatch is
// Codex only now, but `task_runs` still holds the Claude, Grok and Pi rows from
// before — they are execution history and are not rewritten. Their session ids
// are ATM-generated UUIDs that Codex would read as a *thread name* and silently
// start a fresh session under, so dropping the filter would turn `--continue`
// into "quietly begin again" on any Todo whose last run predates the collapse.
func LatestResumableTaskRunForAgent(db *sql.DB, todoID, agent string) (*TaskRun, error) {
	var run TaskRun
	err := scanTaskRun(db.QueryRow(`SELECT `+taskRunColumns+` FROM task_runs
		WHERE todo_id=? AND agent=? AND status IN (?,?,?)
			AND session_id IS NOT NULL AND TRIM(session_id)<>''
		ORDER BY start_ts DESC,id DESC LIMIT 1`, todoID, strings.TrimSpace(agent), TaskRunCompleted, TaskRunFailed, TaskRunInterrupted), &run)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// codexThreadIDPattern matches the UUID Codex uses as a thread id.
var codexThreadIDPattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// ResumableThreadID extracts the thread id `codex exec resume` will accept from
// a stored run session id.
//
// This matters because the column holds two shapes: the bare UUID an Agent
// reports through `atm session bind`, and the `rollout-<timestamp>-<uuid>` form
// transcript sync derives from the rollout filename. Codex treats anything that
// does not parse as a UUID as a *thread name*, and an unknown name does not
// fail — it silently starts a brand-new session. Passing the rollout form
// through would therefore report "continued" while the Agent begins again with
// no context at all, so callers must normalize first and refuse when they
// cannot.
func ResumableThreadID(sessionID *string) string {
	if sessionID == nil {
		return ""
	}
	value := strings.TrimSpace(*sessionID)
	if value == "" {
		return ""
	}
	matches := codexThreadIDPattern.FindAllString(value, -1)
	if len(matches) == 0 {
		return ""
	}
	// The rollout form carries its timestamp first, so the id is the last match.
	return strings.ToLower(matches[len(matches)-1])
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

// LinkTaskRunSession records the session identity reported by the dispatched
// Agent itself. Unlike the sync-time proximity heuristic below, ATM_RUN_ID is
// carried into exactly one child process, so this link is authoritative and may
// replace an earlier guessed association from a concurrent run.
func LinkTaskRunSession(db *sql.DB, runID, todoID, sessionID string) error {
	runID = strings.TrimSpace(runID)
	todoID = strings.TrimSpace(todoID)
	sessionID = strings.TrimSpace(sessionID)
	if runID == "" || todoID == "" || sessionID == "" {
		return fmt.Errorf("task run id, todo id, and session id are required")
	}
	result, err := db.Exec(`UPDATE task_runs SET session_id=? WHERE id=? AND todo_id=?`,
		sessionID, runID, todoID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("task run %s does not belong to todo %s", runID, todoID)
	}
	return nil
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
		&run.EndTS, &run.ExitCode, &run.Message, &run.SessionID, &run.ResumeSessionID)
}
