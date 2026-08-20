package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// TodoPlanRevision is one immutable execution-plan snapshot. SnapshotJSON is
// owned by the Work application layer; the store preserves it byte-for-byte
// together with provenance rather than interpreting short-lived plan steps as
// permanent Todo rows.
type TodoPlanRevision struct {
	TodoID       string
	Revision     int64
	BaseRevision int64
	SnapshotJSON string
	SnapshotHash string
	RequestID    string
	ActorKind    string
	Origin       string
	SessionID    string
	BindingID    *int64
	RunID        string
	Agent        string
	CreatedAt    int64
}

func scanTodoPlanRevision(scanner interface{ Scan(...any) error }) (*TodoPlanRevision, error) {
	var revision TodoPlanRevision
	err := scanner.Scan(
		&revision.TodoID,
		&revision.Revision,
		&revision.BaseRevision,
		&revision.SnapshotJSON,
		&revision.SnapshotHash,
		&revision.RequestID,
		&revision.ActorKind,
		&revision.Origin,
		&revision.SessionID,
		&revision.BindingID,
		&revision.RunID,
		&revision.Agent,
		&revision.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func latestTodoPlanRevision(queryer sqlQueryer, todoID string) (*TodoPlanRevision, error) {
	return scanTodoPlanRevision(queryer.QueryRow(`SELECT
		todo_id,revision,base_revision,snapshot_json,snapshot_hash,request_id,
		actor_kind,origin,session_id,binding_id,run_id,agent,created_at
		FROM todo_plan_revisions WHERE todo_id=? ORDER BY revision DESC LIMIT 1`, todoID))
}

// LatestTodoPlanRevision reads the newest snapshot without creating or
// migrating a database. A Todo without a plan returns (nil, nil).
func LatestTodoPlanRevision(todoID string) (*TodoPlanRevision, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return latestTodoPlanRevision(db, NormalizeTodoID(todoID))
}

func (state *WorkStateTx) LatestTodoPlanRevision(todoID string) (*TodoPlanRevision, error) {
	return latestTodoPlanRevision(state.tx, NormalizeTodoID(todoID))
}

// AppendTodoPlanRevision writes a caller-validated immutable revision inside
// the surrounding Work transaction. The database owns monotonic uniqueness;
// the application service owns base-revision conflict semantics.
func (state *WorkStateTx) AppendTodoPlanRevision(revision TodoPlanRevision) error {
	if revision.TodoID == "" || revision.Revision < 1 || revision.BaseRevision < 0 ||
		revision.SnapshotJSON == "" || revision.SnapshotHash == "" ||
		revision.RequestID == "" || revision.ActorKind == "" ||
		revision.Origin == "" || revision.CreatedAt == 0 {
		return fmt.Errorf("todo plan revision is missing required fields")
	}
	_, err := state.tx.Exec(`INSERT INTO todo_plan_revisions
		(todo_id,revision,base_revision,snapshot_json,snapshot_hash,request_id,
		 actor_kind,origin,session_id,binding_id,run_id,agent,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		revision.TodoID, revision.Revision, revision.BaseRevision,
		revision.SnapshotJSON, revision.SnapshotHash, revision.RequestID,
		revision.ActorKind, revision.Origin, revision.SessionID,
		revision.BindingID, revision.RunID, revision.Agent, revision.CreatedAt,
	)
	return err
}
