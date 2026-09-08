package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestControlWritesDoNotBlockCancellationOrShutdown(t *testing.T) {
	dir := testData(t)
	entered, release := make(chan struct{}), make(chan struct{})
	canceled := make(chan struct{})
	var blocked atomic.Bool
	m := startManager(t, Options{DataDir: dir,
		Execute: func(ctx context.Context, _ application.Call, _ Request, progress func(string)) (any, error) {
			progress("blocked-progress")
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		},
		updateJob: func(ctx context.Context, db *sql.DB, id, status, result string) error {
			var job Job
			if err := json.Unmarshal([]byte(result), &job); err != nil {
				return err
			}
			if job.Phase == "blocked-progress" && blocked.CompareAndSwap(false, true) {
				close(entered)
				// A deliberately uncooperative storage seam proves the manager's
				// memory lock and Close deadline do not depend on writer completion.
				<-release
			}
			return store.UpdateBackgroundJob(ctx, db, id, status, result)
		},
	})
	defer close(release)
	job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "blocked-progress")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("progress write did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = m.Cancel(ctx, job.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel persistence error = %v", err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("cancel waited for the blocked writer")
	}
	select {
	case <-canceled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("executor was not canceled promptly")
	}
	closeCtx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	started = time.Now()
	if err := m.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close = %v", err)
	}
	if time.Since(started) > 200*time.Millisecond {
		t.Fatal("Close did not honor its deadline")
	}
}

func TestProgressCoalescesAndCannotOverwriteTerminalState(t *testing.T) {
	dir := testData(t)
	var phaseWrites atomic.Int32
	var lastEvent atomic.Value
	started, release := make(chan func(string), 1), make(chan struct{})
	m := startManager(t, Options{DataDir: dir,
		OnChange: func(job Job) { lastEvent.Store(job.Status) },
		Execute: func(_ context.Context, _ application.Call, _ Request, progress func(string)) (any, error) {
			for i := 0; i < 1000; i++ {
				progress(fmt.Sprintf("progress-%d", i))
			}
			started <- progress
			<-release
			return nil, nil
		},
		updateJob: func(ctx context.Context, db *sql.DB, id, status, result string) error {
			var job Job
			if err := json.Unmarshal([]byte(result), &job); err != nil {
				return err
			}
			if len(job.Phase) >= 9 && job.Phase[:9] == "progress-" {
				phaseWrites.Add(1)
			}
			return store.UpdateBackgroundJob(ctx, db, id, status, result)
		},
	})
	job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "coalesced")
	if err != nil {
		t.Fatal(err)
	}
	late := <-started
	m.mu.Lock()
	stale := m.active[job.ID].job
	m.mu.Unlock()
	m.flushProgress()
	if got := phaseWrites.Load(); got > 2 {
		t.Fatalf("1000 updates caused %d phase writes", got)
	}
	close(release)
	if got := waitJob(t, m, job.ID); got.Status != "succeeded" {
		t.Fatalf("result = %+v", got)
	}
	late("late progress after executor returned")
	if err := m.persist(stale); err != nil {
		t.Fatal(err)
	}
	// Delayed callers can arrive after the final notification as well as after
	// its database write. Neither channel may regress to a running snapshot.
	m.changed(stale)
	m.flushProgress()
	record, err := store.GetBackgroundJob(context.Background(), m.db, job.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "succeeded" {
		t.Fatalf("late progress replaced terminal: %+v", record)
	}
	deadline := time.Now().Add(time.Second)
	for lastEvent.Load() != "succeeded" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	m.changed(stale)
	if lastEvent.Load() != "succeeded" {
		t.Fatalf("late progress replaced terminal notification: %v", lastEvent.Load())
	}
}

func TestSQLiteContentionDoesNotHoldManagerLock(t *testing.T) {
	dir := testData(t)
	started, canceled := make(chan func(string), 1), make(chan struct{})
	m := startManager(t, Options{DataDir: dir, Execute: func(ctx context.Context, _ application.Call, _ Request, progress func(string)) (any, error) {
		started <- progress
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}})
	job, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "sqlite-contention")
	if err != nil {
		t.Fatal(err)
	}
	progress := <-started
	other, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	tx, err := other.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE background_jobs SET status=status WHERE id=?`, job.ID); err != nil {
		t.Fatal(err)
	}
	progress("busy database")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _ = m.Cancel(ctx, job.ID)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("cancel took %s", elapsed)
	}
	select {
	case <-canceled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SQLite lock delayed executor cancellation")
	}
	if !m.mu.TryLock() {
		t.Fatal("SQLite write held Manager.mu")
	}
	m.mu.Unlock()
}

func TestTerminalSnapshotPublishesAfterReceiptOrRecoveryOwnership(t *testing.T) {
	for _, mode := range []string{"success", "fallback", "pending"} {
		t.Run(mode, func(t *testing.T) {
			dir := testData(t)
			firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
			firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
			var firstOnce, secondOnce sync.Once
			releaseFirst := func() { firstOnce.Do(func() { close(firstRelease) }) }
			releaseSecond := func() { secondOnce.Do(func() { close(secondRelease) }) }
			defer releaseFirst()
			defer releaseSecond()
			var writes, executions atomic.Int32
			lateProgress := make(chan func(string), 1)
			m := startManager(t, Options{DataDir: dir,
				Execute: func(_ context.Context, _ application.Call, _ Request, progress func(string)) (any, error) {
					executions.Add(1)
					lateProgress <- progress
					return map[string]string{"effect": "executed-once"}, nil
				},
				updateJob: func(ctx context.Context, db *sql.DB, id, status, result string) error {
					if status == "queued" || status == "running" {
						return store.UpdateBackgroundJob(ctx, db, id, status, result)
					}
					switch writes.Add(1) {
					case 1:
						close(firstEntered)
						<-firstRelease
						if mode != "success" {
							return errors.New("first terminal write failed")
						}
					case 2:
						close(secondEntered)
						<-secondRelease
					}
					if mode == "pending" {
						return errors.New("terminal storage unavailable")
					}
					// The seam controls completion, independently of scheduling delay
					// in the test. Production always uses its bounded write context.
					return store.UpdateBackgroundJob(context.Background(), db, id, status, result)
				},
			})
			accepted, err := m.Run(context.Background(), humanCall(), Request{Kind: SessionSync}, "finalizing-"+mode)
			if err != nil {
				t.Fatal(err)
			}
			awaitWrite := func(entered <-chan struct{}) {
				t.Helper()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("terminal writer did not reach barrier")
				}
			}
			readAll := func() Job {
				t.Helper()
				shown, err := m.Get(context.Background(), accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				canceled, err := m.Cancel(context.Background(), accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				listed, err := m.List(context.Background(), 10)
				if err != nil || len(listed) != 1 {
					t.Fatalf("list: %+v %v", listed, err)
				}
				wire := func(job Job) string { data, _ := json.Marshal(job); return string(data) }
				if wire(shown) != wire(canceled) || wire(shown) != wire(listed[0]) {
					t.Fatalf("inconsistent published snapshots: Get=%s Cancel=%s List=%s", wire(shown), wire(canceled), wire(listed[0]))
				}
				return shown
			}
			awaitWrite(firstEntered)
			published := readAll()
			if published.Status != "running" || published.CancelRequested || len(published.Result) != 0 {
				t.Fatalf("uncommitted terminal escaped: %+v", published)
			}
			(<-lateProgress)("late progress while finalizing")
			if got := readAll(); !reflect.DeepEqual(got, published) {
				t.Fatalf("finalizing snapshot changed: %+v", got)
			}
			releaseFirst()
			if mode != "success" {
				awaitWrite(secondEntered)
				if got := readAll(); !reflect.DeepEqual(got, published) {
					t.Fatalf("fallback exposed an unpublished terminal: %+v", got)
				}
				releaseSecond()
			}
			terminal := waitJob(t, m, accepted.ID)
			want := "succeeded"
			if mode != "success" {
				want = "interrupted"
			}
			if terminal.Status != want {
				t.Fatalf("terminal = %+v", terminal)
			}
			if got := readAll(); got.Status != want {
				t.Fatalf("published status = %s", got.Status)
			}
			if executions.Load() != 1 {
				t.Fatalf("external operation replayed %d times", executions.Load())
			}
		})
	}
}
