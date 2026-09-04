package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

func testData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	config.AtmDir, config.AtmDB = dir, filepath.Join(dir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
	return dir
}

func startManager(t *testing.T, options Options) *Manager {
	t.Helper()
	m, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}

func humanCall() application.Call {
	return application.Call{RequestID: "test-request", Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginWeb}}
}

func waitJob(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		job, err := m.Get(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Terminal() {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s never completed", id)
	return Job{}
}

func TestDurableAcceptanceRetryCancelAndRestart(t *testing.T) {
	dir := testData(t)
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	m := startManager(t, Options{DataDir: dir, Execute: func(ctx context.Context, _ application.Call, _ Request, progress func(string)) (any, error) {
		calls.Add(1)
		progress("testing")
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "same-key")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	retry, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "same-key")
	if err != nil || retry.ID != job.ID {
		t.Fatalf("retry=%+v %v", retry, err)
	}
	if _, err := m.Run(context.Background(), humanCall(), Request{Kind: QuotaRefresh}, "same-key"); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("changed key payload=%v", err)
	}
	if _, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "new-key"); !errors.Is(err, application.ErrBusy) {
		t.Fatalf("duplicate active operation=%v", err)
	}
	if _, err := m.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitJob(t, m, job.ID); got.Status != "canceled" {
		t.Fatalf("cancel=%+v", got)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	m2 := startManager(t, Options{DataDir: dir, Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
		calls.Add(1)
		return nil, nil
	}})
	retry, err = m2.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "same-key")
	if err != nil || retry.ID != job.ID || retry.Status != "canceled" {
		t.Fatalf("restart replay=%+v %v", retry, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("operation replayed %d times", calls.Load())
	}
}

func TestStartupMarksAcceptedWorkInterruptedWithoutReplay(t *testing.T) {
	dir := testData(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	job := Job{ID: "job-before-crash", Kind: CollectionRun, Status: "running", CreatedAt: time.Now().Format(time.RFC3339)}
	data, _ := json.Marshal(job)
	if err := store.InsertBackgroundJob(context.Background(), db, store.BackgroundJobRecord{ID: job.ID, Key: "before-crash", Digest: "hash", Kind: string(job.Kind), Status: job.Status, RequestJSON: `{"kind":"collect.run"}`, ResultJSON: string(data), CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	var calls atomic.Int32
	m := startManager(t, Options{DataDir: dir, Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
		calls.Add(1)
		return nil, nil
	}})
	got, err := m.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "interrupted" || got.Error.Code != "interrupted" || calls.Load() != 0 {
		t.Fatalf("recovery=%+v calls=%d", got, calls.Load())
	}
}

func TestFinalJobPersistenceRetriesWithoutReplayingTheOperation(t *testing.T) {
	dir := testData(t)
	var executions atomic.Int32
	var terminalWrites atomic.Int32
	m := startManager(t, Options{
		DataDir: dir,
		Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
			executions.Add(1)
			return map[string]int{"changed": 1}, nil
		},
		updateJob: func(ctx context.Context, db *sql.DB, id, status, result string) error {
			if status != "queued" && status != "running" && terminalWrites.Add(1) <= 2 {
				return errors.New("temporary final-state write failure")
			}
			return store.UpdateBackgroundJob(ctx, db, id, status, result)
		},
		durabilityRetry: 5 * time.Millisecond,
	})
	job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "persist-retry")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitJob(t, m, job.ID); got.Status != "interrupted" {
		t.Fatalf("ambiguous final result = %+v", got)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		record, readErr := store.GetBackgroundJob(context.Background(), m.db, job.ID, "")
		if readErr != nil {
			t.Fatal(readErr)
		}
		if record != nil && record.Status == "interrupted" {
			var persisted Job
			if err := json.Unmarshal([]byte(record.ResultJSON), &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.Error == nil || persisted.Error.Code != "unavailable" {
				t.Fatalf("persisted fallback = %+v", persisted)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("database remained non-terminal: %+v", record)
		}
		time.Sleep(time.Millisecond)
	}
	if executions.Load() != 1 {
		t.Fatalf("persistence retry replayed operation %d times", executions.Load())
	}
}

func TestPendingUsageJournalRecoversWhileServiceKeepsRunning(t *testing.T) {
	dir := testData(t)
	var executions atomic.Int32
	m := startManager(t, Options{
		DataDir: dir,
		Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
			executions.Add(1)
			return nil, nil
		},
		durabilityRetry: 5 * time.Millisecond,
	})
	recorder := &usageRecorder{dataDir: dir, id: "live-recovery"}
	recorder.record(textmodel.Call{
		Task: "collection", Model: "fixture", Usage: textmodel.Usage{InputTokens: 31, OutputTokens: 7}, StartedAt: time.Now(),
	})
	if recorder.err != nil {
		t.Fatal(recorder.err)
	}
	if err := recorder.file.Close(); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "model-usage-pending", "live-recovery.jsonl")
	m.retryUsageLater("live-recovery")
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, statErr := os.Stat(journal)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			t.Fatal(statErr)
		}
		if time.Now().After(deadline) {
			t.Fatal("usage journal was not recovered without a service restart")
		}
		time.Sleep(time.Millisecond)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, input int
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(input_tokens),0) FROM usage_events WHERE session_id='atm-live-recovery'`).Scan(&count, &input); err != nil {
		t.Fatal(err)
	}
	if count != 1 || input != 31 || executions.Load() != 0 {
		t.Fatalf("recovery count=%d input=%d executions=%d", count, input, executions.Load())
	}
}

func TestQueueBoundAndShutdownDoNotExecuteQueuedWork(t *testing.T) {
	dir := testData(t)
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	m := startManager(t, Options{DataDir: dir, Execute: func(ctx context.Context, _ application.Call, _ Request, _ func(string)) (any, error) {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	for i := 0; i < 2; i++ {
		_, err := m.Run(context.Background(), humanCall(), Request{Kind: CollectionReprocess, ItemID: fmt.Sprintf("i%d", i)}, fmt.Sprintf("k%d", i))
		if err != nil {
			t.Fatal(err)
		}
		<-started
	}
	for i := 2; i < 18; i++ {
		_, err := m.Run(context.Background(), humanCall(), Request{Kind: CollectionReprocess, ItemID: fmt.Sprintf("i%d", i)}, fmt.Sprintf("k%d", i))
		if err != nil {
			t.Fatalf("queue %d: %v", i, err)
		}
	}
	if _, err := m.Run(context.Background(), humanCall(), Request{Kind: QuotaRefresh}, "overflow"); !errors.Is(err, application.ErrBusy) {
		t.Fatalf("overflow=%v", err)
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("shutdown executed %d queued/active operations", calls.Load())
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	records, err := store.ListBackgroundJobs(context.Background(), db, 100, true)
	if err != nil || len(records) != 0 {
		t.Fatalf("unfinished after shutdown=%d %v", len(records), err)
	}
	if _, err := m.Run(context.Background(), humanCall(), Request{Kind: QuotaRefresh}, "after-close"); !errors.Is(err, application.ErrBusy) {
		t.Fatalf("post-close=%v", err)
	}
}

func TestJobFailureTimeoutAndPanicStaySafe(t *testing.T) {
	dir := testData(t)
	m := startManager(t, Options{DataDir: dir, JobTimeout: 30 * time.Millisecond, Execute: func(ctx context.Context, _ application.Call, r Request, _ func(string)) (any, error) {
		switch r.Agent {
		case "codex":
			return nil, errors.New("secret-provider-response")
		case "claude":
			panic("secret panic")
		default:
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}})
	for _, agent := range []string{"codex", "claude", "pi"} {
		job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync, Agent: agent}, agent)
		if err != nil {
			t.Fatal(err)
		}
		got := waitJob(t, m, job.ID)
		if got.Status != "failed" || got.Error == nil {
			t.Fatalf("failure=%+v", got)
		}
		if agent == "pi" && got.Error.Code != "timeout" {
			t.Fatalf("timeout=%+v", got)
		}
		if got.Error.Message == "secret-provider-response" || got.Error.Message == "secret panic" {
			t.Fatal("provider error leaked")
		}
	}
}

func TestCollectionCompletionReachesOnlyTheLiveTerminalCallback(t *testing.T) {
	dir := testData(t)
	changed := make(chan Job, 8)
	completion := &CollectionCompletion{Runs: []CollectionNotificationRun{{ID: "cr-one"}}}
	m := startManager(t, Options{DataDir: dir,
		Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
			return collectionJobResult{Runs: 1, Succeeded: 1, completion: completion}, nil
		},
		OnChange: func(job Job) { changed <- job },
	})
	accepted, err := m.Run(context.Background(), humanCall(), Request{Kind: CollectionRun}, "collection-callback")
	if err != nil {
		t.Fatal(err)
	}
	var terminal Job
	for !terminal.Terminal() || terminal.ID == "" {
		select {
		case terminal = <-changed:
		case <-time.After(2 * time.Second):
			t.Fatal("terminal callback did not arrive")
		}
	}
	if terminal.Collection == nil || len(terminal.Collection.Runs) != 1 || terminal.Collection.Runs[0].ID != "cr-one" {
		t.Fatalf("callback lost collection completion: %+v", terminal)
	}
	persisted, err := m.Get(context.Background(), accepted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Collection != nil {
		t.Fatalf("private collection content entered job history: %+v", persisted.Collection)
	}
}

func TestValidationRunsBeforeAcceptance(t *testing.T) {
	dir := testData(t)
	m := startManager(t, Options{DataDir: dir, Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
		t.Error("invalid job executed")
		return nil, nil
	}})
	for i, request := range []Request{{Kind: "shell.run"}, {Kind: CollectionRun, DueOnly: true}, {Kind: CollectionReprocess}, {Kind: SessionSync, Agent: "no-agent"}, {Kind: DayRebuild, From: "2026-01-01", To: "2028-01-01"}, {Kind: QuotaRefresh, SourceID: "source"}} {
		if _, err := m.Run(context.Background(), humanCall(), request, fmt.Sprint(i)); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("input=%+v err=%v", request, err)
		}
	}
	jobs, err := m.List(context.Background(), 100)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("invalid requests persisted=%+v %v", jobs, err)
	}
}

func TestSchedulerUsesControllerAndDueSourcePolicy(t *testing.T) {
	dir := testData(t)
	runs := make(chan Request, 40)
	m := startManager(t, Options{DataDir: dir, Schedule: true, TickInterval: 5 * time.Millisecond, SyncInterval: 20 * time.Millisecond, DayInterval: 35 * time.Millisecond,
		CollectionDue: func(_ context.Context, last time.Time) (bool, error) { return last.IsZero(), nil },
		Execute: func(_ context.Context, call application.Call, request Request, _ func(string)) (any, error) {
			if call.Actor.Kind != application.ActorController {
				t.Error("timer used human authority")
			}
			runs <- request
			return nil, nil
		}})
	seen := map[Kind]int{}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for seen[SessionSync] < 2 || seen[DayRebuild] < 1 || seen[CollectionRun] < 1 {
		select {
		case request := <-runs:
			seen[request.Kind]++
			if request.Kind == CollectionRun && !request.DueOnly {
				t.Error("scheduled collection bypassed cadence")
			}
		case <-timer.C:
			t.Fatalf("scheduler runs=%v", seen)
		}
	}
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledCollectionRechecksOptInBeforeExecution(t *testing.T) {
	dir := testData(t)
	old := config.CollectionEnabled
	config.CollectionEnabled = false
	t.Cleanup(func() { config.CollectionEnabled = old })
	call := application.Call{RequestID: "scheduled-after-disable", Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController}}
	result, err := DefaultExecutor(dir)(context.Background(), call, Request{Kind: CollectionRun, DueOnly: true}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := result.(map[string]any)
	if !ok || value["skipped"] != true {
		t.Fatalf("disabled scheduled collection executed: %+v", result)
	}
}

func TestDayRetryRetainsOriginallyAcceptedDateAcrossMidnight(t *testing.T) {
	dir := testData(t)
	var clock, calls atomic.Int64
	firstDay := time.Date(2026, 9, 3, 23, 50, 0, 0, time.UTC)
	clock.Store(firstDay.UnixNano())
	m := startManager(t, Options{DataDir: dir, Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() }, Execute: func(_ context.Context, _ application.Call, r Request, _ func(string)) (any, error) {
		calls.Add(1)
		return map[string]string{"from": r.From}, nil
	}})
	first, err := m.Run(context.Background(), humanCall(), Request{Kind: DayRebuild}, "overnight")
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, m, first.ID)
	clock.Store(firstDay.Add(24 * time.Hour).UnixNano())
	retry, err := m.Run(context.Background(), humanCall(), Request{Kind: DayRebuild}, "overnight")
	if err != nil || retry.ID != first.ID || calls.Load() != 1 {
		t.Fatalf("overnight replay=%+v calls=%d err=%v", retry, calls.Load(), err)
	}
}
