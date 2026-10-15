package background

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestNightlyWindow(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	for _, tc := range []struct{ at, slot string }{
		{"2026-10-10T22:59:59+08:00", ""},
		{"2026-10-10T23:00:00+08:00", "2026-10-10"},
		{"2026-10-11T00:00:00+08:00", "2026-10-10"},
		{"2026-10-11T05:59:59+08:00", "2026-10-10"},
		{"2026-10-11T06:00:00+08:00", ""},
		{"2027-01-01T01:00:00+08:00", "2026-12-31"},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		if got := nightlySlot(at.UTC().In(loc)); got != tc.slot {
			t.Errorf("%s: slot %q, want %q", tc.at, got, tc.slot)
		}
	}
}

func TestNightlySyncPrecedesDayAndSurvivesRestart(t *testing.T) {
	dir := testData(t)
	oldLoc := config.Loc
	config.Loc = time.FixedZone("UTC+8", 8*60*60)
	t.Cleanup(func() { config.Loc = oldLoc })
	var clock atomic.Int64
	setTime := func(day, hour int) {
		clock.Store(time.Date(2026, 10, day, hour, 0, 0, 0, config.Loc).Unix())
	}
	var syncs, days atomic.Int32
	options := Options{DataDir: dir,
		Now:           func() time.Time { return time.Unix(clock.Load(), 0) },
		CollectionDue: func(context.Context, time.Time) (bool, error) { return false, nil },
		Execute: func(_ context.Context, call application.Call, request Request, _ func(string)) (any, error) {
			if call.Actor.Kind != application.ActorController {
				t.Error("nightly job must use controller authority")
			}
			switch request.Kind {
			case SessionSync:
				if request.Agent != "codex" {
					t.Errorf("default automatic source=%q", request.Agent)
				}
				syncs.Add(1)
			case DayRebuild:
				if syncs.Load() <= days.Load() {
					t.Error("day ran before sync")
				}
				days.Add(1)
			}
			return nil, nil
		},
	}
	setTime(10, 12)
	m := startManager(t, options)
	checkJobs := func(manager *Manager, count int) {
		t.Helper()
		jobs, err := manager.List(context.Background(), 100)
		if err != nil || len(jobs) != count {
			t.Fatalf("jobs=%d want=%d err=%v", len(jobs), count, err)
		}
		for _, job := range jobs {
			if result := waitJob(t, manager, job.ID); result.Status != "succeeded" {
				t.Fatalf("job failed: %+v", result)
			}
		}
	}
	m.scheduleTick()
	checkJobs(m, 0)
	setTime(10, 23)
	m.scheduleTick()
	checkJobs(m, 1)
	m.scheduleTick()
	checkJobs(m, 2)
	setTime(11, 1)
	m.scheduleTick()
	checkJobs(m, 2)
	if err := m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	m = startManager(t, options)
	m.scheduleTick()
	checkJobs(m, 2)
	setTime(11, 12)
	m.scheduleTick()
	checkJobs(m, 2)
	setTime(11, 23)
	m.scheduleTick()
	checkJobs(m, 3)
	m.scheduleTick()
	checkJobs(m, 4)
	if syncs.Load() != 2 || days.Load() != 2 {
		t.Fatalf("syncs=%d days=%d", syncs.Load(), days.Load())
	}

	// Busy connector schedules must not prune the nightly idempotency receipt.
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("other-%d", i)
		if err := store.InsertBackgroundJob(context.Background(), m.db, store.BackgroundJobRecord{
			ID: id, Key: id, Kind: string(CollectionRun), Status: "succeeded",
			RequestJSON: "{}", ResultJSON: "{}", CreatedAt: clock.Load()*int64(time.Second) + int64(i+1), Automatic: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PruneScheduledBackgroundJobs(context.Background(), m.db); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"nightly-sync-2026-10-11-codex", "nightly-day-2026-10-11"} {
		record, err := store.GetBackgroundJob(context.Background(), m.db, "", key)
		if err != nil || record == nil {
			t.Fatalf("nightly receipt pruned: %s %v", key, err)
		}
	}
}

func TestNightlySourceOptOutAndAuxiliaryOptIn(t *testing.T) {
	dir := testData(t)
	old := config.SessionSyncAgents
	t.Cleanup(func() { config.SessionSyncAgents = old })
	config.SessionSyncAgents = "none"
	m := startManager(t, Options{DataDir: dir,
		Now:           func() time.Time { return time.Date(2026, 10, 10, 23, 0, 0, 0, config.Loc) },
		CollectionDue: func(context.Context, time.Time) (bool, error) { return false, nil },
		Execute:       func(context.Context, application.Call, Request, func(string)) (any, error) { return nil, nil },
	})
	m.scheduleTick()
	jobs, err := m.List(context.Background(), 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("disabled sources scheduled: %v %v", jobs, err)
	}
	config.SessionSyncAgents = "codex,qoderwork"
	for i := 0; i < 3; i++ {
		m.scheduleTick()
		jobs, err = m.List(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, job := range jobs {
			waitJob(t, m, job.ID)
		}
	}
	if len(jobs) != 3 {
		t.Fatalf("want two sources and day: %v", jobs)
	}
	for _, agent := range []string{"codex", "qoderwork"} {
		r, err := store.GetBackgroundJob(context.Background(), m.db, "", "nightly-sync-2026-10-10-"+agent)
		if err != nil || r == nil {
			t.Fatalf("missing %s job: %v", agent, err)
		}
	}
	// A source disabled after acceptance is rechecked before reading it.
	config.SessionSyncAgents = "codex"
	result, err := DefaultExecutor(dir)(context.Background(), application.Call{RequestID: "disabled-source", Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController}}, Request{Kind: SessionSync, Agent: "qoderwork"}, func(string) {})
	if err != nil || result.(map[string]any)["skipped"] != true {
		t.Fatalf("disabled source ran: %v %v", result, err)
	}
}

func TestNightlyFailureDoesNotLoopOrRebuild(t *testing.T) {
	dir := testData(t)
	m := startManager(t, Options{DataDir: dir,
		Now:           func() time.Time { return time.Date(2026, 10, 10, 23, 0, 0, 0, config.Loc) },
		CollectionDue: func(context.Context, time.Time) (bool, error) { return false, nil },
		Execute: func(context.Context, application.Call, Request, func(string)) (any, error) {
			return nil, errors.New("source unavailable")
		},
	})
	m.scheduleTick()
	jobs, err := m.List(context.Background(), 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
	if job := waitJob(t, m, jobs[0].ID); job.Status != "failed" {
		t.Fatalf("expected failed job: %+v", job)
	}
	m.scheduleTick()
	jobs, err = m.List(context.Background(), 10)
	if err != nil || len(jobs) != 1 || jobs[0].Kind != SessionSync {
		t.Fatalf("failure retried or rebuilt: %v %v", jobs, err)
	}
}
