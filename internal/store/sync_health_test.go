package store

import (
	"errors"
	"testing"
	"time"
)

func TestSyncHealthTracksFreshStaleAndFailedRuns(t *testing.T) {
	db := openTempDB(t)
	started := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)

	if err := beginSyncRun(db, SyncScopeAll, started); err != nil {
		t.Fatalf("begin sync: %v", err)
	}
	running, err := ReadSyncHealth(db, SyncScopeAll, started.Add(time.Minute), DefaultSyncStaleAfter)
	if err != nil {
		t.Fatalf("read running health: %v", err)
	}
	if running.Status != "syncing" || running.RunStatus != syncRunStatusRunning {
		t.Fatalf("running health = %#v", running)
	}

	if err := finishSyncRun(db, SyncScopeAll, started, 4, nil); err != nil {
		t.Fatalf("finish successful sync: %v", err)
	}
	fresh, err := ReadSyncHealth(db, SyncScopeAll, started.Add(2*time.Minute), DefaultSyncStaleAfter)
	if err != nil {
		t.Fatalf("read fresh health: %v", err)
	}
	if fresh.Status != "fresh" || fresh.LastSyncedFiles != 4 || fresh.AgeSeconds == nil || *fresh.AgeSeconds != 120 {
		t.Fatalf("fresh health = %#v", fresh)
	}

	stale, err := ReadSyncHealth(db, SyncScopeAll, started.Add(11*time.Minute), DefaultSyncStaleAfter)
	if err != nil {
		t.Fatalf("read stale health: %v", err)
	}
	if stale.Status != "stale" {
		t.Fatalf("stale health = %#v", stale)
	}

	failedAt := started.Add(12 * time.Minute)
	if err := beginSyncRun(db, SyncScopeAll, failedAt); err != nil {
		t.Fatalf("begin failed sync: %v", err)
	}
	if err := finishSyncRun(db, SyncScopeAll, failedAt, 2, errors.New("source unavailable")); err != nil {
		t.Fatalf("finish failed sync: %v", err)
	}
	failed, err := ReadSyncHealth(db, SyncScopeAll, failedAt.Add(time.Minute), DefaultSyncStaleAfter)
	if err != nil {
		t.Fatalf("read failed health: %v", err)
	}
	if failed.Status != "failed" || failed.LastError != "source unavailable" || failed.LastSyncedFiles != 2 {
		t.Fatalf("failed health = %#v", failed)
	}
	if failed.LastSuccessAt == nil || *failed.LastSuccessAt != started.Format(time.RFC3339) {
		t.Fatalf("last successful sync was not preserved: %#v", failed)
	}
}

func TestSyncHealthTreatsAbandonedRunningRunAsFailed(t *testing.T) {
	db := openTempDB(t)
	started := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	if err := beginSyncRun(db, SyncScopeAll, started); err != nil {
		t.Fatalf("begin sync: %v", err)
	}

	health, err := ReadSyncHealth(db, SyncScopeAll, started.Add(11*time.Minute), DefaultSyncStaleAfter)
	if err != nil {
		t.Fatalf("read health: %v", err)
	}
	if health.Status != "failed" || health.LastError != "last sync did not complete" {
		t.Fatalf("health = %#v", health)
	}
}
