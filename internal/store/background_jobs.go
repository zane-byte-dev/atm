package store

import (
	"context"
	"database/sql"
	"errors"
)

// BackgroundJobRecord is a durable acceptance receipt for an explicit local
// operation. The background application owns the versioned JSON payload. A
// restart never automatically replays an accepted external/model operation.
type BackgroundJobRecord struct {
	ID, Key, Digest, Kind, Status, RequestJSON, ResultJSON string
	CreatedAt                                              int64
	Automatic                                              bool
}

const backgroundJobsSchema = `CREATE TABLE IF NOT EXISTS background_jobs (
	id TEXT PRIMARY KEY,
	idempotency_key TEXT NOT NULL UNIQUE,
	payload_hash TEXT NOT NULL,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	request_json TEXT NOT NULL,
	result_json TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	automatic INTEGER NOT NULL DEFAULT 0
)`

func migrateV55ToV56(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{backgroundJobsSchema,
		`CREATE INDEX IF NOT EXISTS idx_background_jobs_created ON background_jobs(created_at DESC)`,
		`UPDATE schema_version SET version = 56`} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if err := InstallWorkspaceChangeTracking(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func InsertBackgroundJob(ctx context.Context, db *sql.DB, r BackgroundJobRecord) error {
	_, err := db.ExecContext(ctx, `INSERT INTO background_jobs
		(id,idempotency_key,payload_hash,kind,status,request_json,result_json,created_at,automatic)
		VALUES(?,?,?,?,?,?,?,?,?)`, r.ID, r.Key, r.Digest, r.Kind, r.Status, r.RequestJSON, r.ResultJSON, r.CreatedAt, r.Automatic)
	return err
}

func UpdateBackgroundJob(ctx context.Context, db *sql.DB, id, status, result string) error {
	_, err := db.ExecContext(ctx, `UPDATE background_jobs SET status=?,result_json=? WHERE id=?`, status, result, id)
	return err
}

func GetBackgroundJob(ctx context.Context, db *sql.DB, id, key string) (*BackgroundJobRecord, error) {
	var r BackgroundJobRecord
	err := db.QueryRowContext(ctx, `SELECT id,idempotency_key,payload_hash,kind,status,
		request_json,result_json,created_at,automatic FROM background_jobs WHERE id=? OR idempotency_key=?`, id, key).
		Scan(&r.ID, &r.Key, &r.Digest, &r.Kind, &r.Status, &r.RequestJSON, &r.ResultJSON, &r.CreatedAt, &r.Automatic)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

func ListBackgroundJobs(ctx context.Context, db *sql.DB, limit int, unfinished bool) ([]BackgroundJobRecord, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	query := `SELECT id,idempotency_key,payload_hash,kind,status,request_json,result_json,created_at,automatic FROM background_jobs`
	if unfinished {
		query += ` WHERE status IN ('queued','running')`
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []BackgroundJobRecord{}
	for rows.Next() {
		var r BackgroundJobRecord
		if err := rows.Scan(&r.ID, &r.Key, &r.Digest, &r.Kind, &r.Status, &r.RequestJSON, &r.ResultJSON, &r.CreatedAt, &r.Automatic); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// Scheduled jobs have no client retry key. Keep only their latest 200 terminal
// receipts. Manual receipts remain available for stable retries; their compact
// payload contains only IDs, counts and phases, never transcript/model bodies.
func PruneScheduledBackgroundJobs(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DELETE FROM background_jobs WHERE automatic=1
		AND status NOT IN ('queued','running') AND id NOT IN
		(SELECT id FROM background_jobs WHERE automatic=1 ORDER BY created_at DESC,id DESC LIMIT 200)`)
	return err
}
