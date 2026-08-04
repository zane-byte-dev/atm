package store

import (
	"database/sql"
	"fmt"
	"time"
)

const (
	SyncScopeAll           = "all"
	DefaultSyncStaleAfter  = 10 * time.Minute
	interruptedSyncAfter   = 10 * time.Minute
	syncRunStatusRunning   = "running"
	syncRunStatusSucceeded = "succeeded"
	syncRunStatusFailed    = "failed"
)

type SyncHealth struct {
	Scope             string  `json:"scope"`
	Status            string  `json:"status"`
	RunStatus         string  `json:"run_status"`
	SchemaVersion     int     `json:"schema_version"`
	IndexedSessions   int     `json:"indexed_sessions"`
	RetainedSessions  int     `json:"retained_sessions"`
	LastAttemptAt     *string `json:"last_attempt_at"`
	LastSuccessAt     *string `json:"last_success_at"`
	AgeSeconds        *int64  `json:"age_seconds"`
	StaleAfterSeconds int64   `json:"stale_after_seconds"`
	LastError         string  `json:"last_error"`
	LastSyncedFiles   int     `json:"last_synced_files"`
}

func beginSyncRun(db *sql.DB, scope string, startedAt time.Time) error {
	_, err := db.Exec(`INSERT INTO sync_health (
			scope, last_attempt_ts, last_success_ts, last_status, last_error, last_synced_files
		) VALUES (?, ?, 0, ?, '', 0)
		ON CONFLICT(scope) DO UPDATE SET
			last_attempt_ts = excluded.last_attempt_ts,
			last_status = excluded.last_status,
			last_error = ''`,
		scope, startedAt.Unix(), syncRunStatusRunning)
	return err
}

func finishSyncRun(db *sql.DB, scope string, finishedAt time.Time, files int, syncErr error) error {
	status := syncRunStatusSucceeded
	message := ""
	successTS := finishedAt.Unix()
	if syncErr != nil {
		status = syncRunStatusFailed
		message = syncErr.Error()
		successTS = 0
	}
	_, err := db.Exec(`UPDATE sync_health SET
			last_success_ts = CASE WHEN ? > 0 THEN ? ELSE last_success_ts END,
			last_status = ?,
			last_error = ?,
			last_synced_files = ?
		WHERE scope = ?`,
		successTS, successTS, status, message, files, scope)
	return err
}

func runTrackedSync(db *sql.DB, scope string, syncFn func() (int, error)) (int, error) {
	startedAt := time.Now().UTC()
	if err := beginSyncRun(db, scope, startedAt); err != nil {
		return 0, fmt.Errorf("record sync attempt: %w", err)
	}
	files, syncErr := syncFn()
	recordErr := finishSyncRun(db, scope, time.Now().UTC(), files, syncErr)
	if syncErr != nil {
		if recordErr != nil {
			return files, fmt.Errorf("%w (also failed to record sync result: %v)", syncErr, recordErr)
		}
		return files, syncErr
	}
	if recordErr != nil {
		return files, fmt.Errorf("record sync result: %w", recordErr)
	}
	return files, nil
}

func ReadSyncHealth(db *sql.DB, scope string, now time.Time, staleAfter time.Duration) (SyncHealth, error) {
	if scope == "" {
		scope = SyncScopeAll
	}
	if staleAfter <= 0 {
		staleAfter = DefaultSyncStaleAfter
	}
	health := SyncHealth{
		Scope:             scope,
		Status:            "never",
		RunStatus:         "never",
		StaleAfterSeconds: int64(staleAfter.Seconds()),
	}
	if err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&health.SchemaVersion); err != nil {
		return health, fmt.Errorf("read schema version: %w", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&health.IndexedSessions); err != nil {
		return health, fmt.Errorf("count indexed sessions: %w", err)
	}
	// Part of IndexedSessions, reported separately because that total counts
	// every session ever seen rather than the transcripts on disk now: ATM keeps
	// a session after its source is rotated away. See forgetRemovedSources.
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions s
		WHERE NOT EXISTS (SELECT 1 FROM sync_state ss WHERE ss.file_path = s.file_path)`).
		Scan(&health.RetainedSessions); err != nil {
		return health, fmt.Errorf("count retained sessions: %w", err)
	}

	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sync_health'`).Scan(&tableCount); err != nil {
		return health, fmt.Errorf("inspect sync health schema: %w", err)
	}
	if tableCount == 0 {
		return health, nil
	}

	var attemptTS, successTS int64
	err := db.QueryRow(`SELECT last_attempt_ts, last_success_ts, last_status, last_error, last_synced_files
		FROM sync_health WHERE scope = ?`, scope).
		Scan(&attemptTS, &successTS, &health.RunStatus, &health.LastError, &health.LastSyncedFiles)
	if err == sql.ErrNoRows {
		return health, nil
	}
	if err != nil {
		return health, fmt.Errorf("read sync health: %w", err)
	}

	now = now.UTC()
	if attemptTS > 0 {
		value := time.Unix(attemptTS, 0).UTC().Format(time.RFC3339)
		health.LastAttemptAt = &value
	}
	if successTS > 0 {
		value := time.Unix(successTS, 0).UTC().Format(time.RFC3339)
		health.LastSuccessAt = &value
		age := max(now.Unix()-successTS, 0)
		health.AgeSeconds = &age
	}

	switch health.RunStatus {
	case syncRunStatusRunning:
		if attemptTS > 0 && now.Sub(time.Unix(attemptTS, 0)) > interruptedSyncAfter {
			health.Status = "failed"
			health.LastError = "last sync did not complete"
		} else {
			health.Status = "syncing"
		}
	case syncRunStatusFailed:
		health.Status = "failed"
	case syncRunStatusSucceeded:
		if health.AgeSeconds != nil && *health.AgeSeconds > health.StaleAfterSeconds {
			health.Status = "stale"
		} else {
			health.Status = "fresh"
		}
	default:
		health.Status = "never"
	}
	return health, nil
}
