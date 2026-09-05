package sync

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

const syncTestNow = int64(1_700_000_000)

func syncTestCall() application.Call {
	return application.Call{
		RequestID: "sync-service-test",
		Actor: application.Actor{
			Kind:   application.ActorHuman,
			Origin: application.OriginCLI,
		},
	}
}

func withTempAtmDir(t *testing.T) string {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
	return dir
}

// scanCounts records which scan the service chose, which is the whole point of
// the --agent flag: a per-agent sync must not walk every other source.
type scanCounts struct {
	all    int
	agents []string
}

type recordedSampler struct {
	calls int
	err   error
}

func (sampler *recordedSampler) RecordSamples(*sql.DB, time.Time) error {
	sampler.calls++
	return sampler.err
}

// fakeService stubs the scans and the side job so these tests are about which
// scan runs and how a failing side job is reported. The connection is real
// because the service closes it, but nothing here reads from it.
func fakeService(t *testing.T, counts *scanCounts, sampler QuotaSampler) Service {
	t.Helper()
	withTempAtmDir(t)
	return NewService(ServiceOptions{
		Now:       func() time.Time { return time.Unix(syncTestNow, 0).UTC() },
		OpenWrite: store.Open,
		OpenRead:  store.Open,
		SyncAllContext: func(context.Context, *sql.DB) (int, error) {
			counts.all++
			return 7, nil
		},
		SyncAgentContext: func(_ context.Context, _ *sql.DB, agent string) (int, error) {
			counts.agents = append(counts.agents, agent)
			return 2, nil
		},
		Sampler:     sampler,
		IndexExists: func() (bool, error) { return true, nil },
	})
}

func TestRunWithAnAgentScansOnlyThatAgent(t *testing.T) {
	var counts scanCounts
	service := fakeService(t, &counts, &recordedSampler{})

	result, err := service.Run(context.Background(), syncTestCall(), RunInput{Agent: "codex"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if counts.all != 0 || len(counts.agents) != 1 || counts.agents[0] != "codex" {
		t.Fatalf("scans = %+v, want one codex scan and no full scan", counts)
	}
	if result.SyncedFiles != 2 {
		t.Errorf("synced = %d, want 2", result.SyncedFiles)
	}
}

func TestRunWithoutAnAgentScansEverySource(t *testing.T) {
	var counts scanCounts
	service := fakeService(t, &counts, &recordedSampler{})

	result, err := service.Run(context.Background(), syncTestCall(), RunInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if counts.all != 1 || len(counts.agents) != 0 {
		t.Fatalf("scans = %+v, want one full scan", counts)
	}
	if result.SyncedFiles != 7 {
		t.Errorf("synced = %d, want 7", result.SyncedFiles)
	}
}

func TestRunNormalizesAnAgentAliasAndRejectsAnUnknownOne(t *testing.T) {
	var counts scanCounts
	service := fakeService(t, &counts, &recordedSampler{})

	if _, err := service.Run(context.Background(), syncTestCall(), RunInput{Agent: "CODEX"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(counts.agents) != 1 || counts.agents[0] != "codex" {
		t.Fatalf("alias was not normalized: %+v", counts)
	}

	_, err := service.Run(context.Background(), syncTestCall(), RunInput{Agent: "nope"})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
		t.Fatalf("err = %v, want invalid_argument", err)
	}
}

// Quota history is a convenience; the session index is the point. A sampling
// failure must not fail the refresh — but it must not vanish either, because
// nobody reads stderr when the server runs this on a timer.
func TestRunReportsASamplingFailureWithoutFailingTheRefresh(t *testing.T) {
	var counts scanCounts
	sampler := &recordedSampler{err: errors.New("quota_history table is locked")}
	service := fakeService(t, &counts, sampler)

	result, err := service.Run(context.Background(), syncTestCall(), RunInput{})
	if err != nil {
		t.Fatalf("a sampling failure failed the whole refresh: %v", err)
	}
	if result.SyncedFiles != 7 {
		t.Errorf("synced = %d; the refresh itself should still be reported", result.SyncedFiles)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "quota_history table is locked") {
		t.Fatalf("sampling failure did not surface: %+v", result.Warnings)
	}
	if sampler.calls != 1 {
		t.Errorf("sampler called %d times, want 1", sampler.calls)
	}
}

func TestRunSaysNothingWhenSamplingSucceeds(t *testing.T) {
	var counts scanCounts
	service := fakeService(t, &counts, &recordedSampler{})

	result, err := service.Run(context.Background(), syncTestCall(), RunInput{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warned on a clean refresh: %+v", result.Warnings)
	}
}

// Asking how fresh the index is must not build it. A status read that created an
// empty database would change the thing it was asked to describe — and would then
// report a brand-new index as the answer.
func TestStatusReportsAMissingIndexWithoutCreatingIt(t *testing.T) {
	withTempAtmDir(t)
	opened := 0
	service := NewService(ServiceOptions{
		Now:       func() time.Time { return time.Unix(syncTestNow, 0).UTC() },
		OpenRead:  func() (*sql.DB, error) { opened++; return nil, errors.New("unreachable") },
		OpenWrite: func() (*sql.DB, error) { opened++; return nil, errors.New("unreachable") },
	})

	report, err := service.Status(context.Background(), syncTestCall(), StatusInput{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if report.Index.Exists || report.Sync.Status != "missing" || report.Sync.RunStatus != "never" {
		t.Fatalf("report = %#v", report)
	}
	if report.Sync.StaleAfterSeconds != int64(store.DefaultSyncStaleAfter.Seconds()) {
		t.Errorf("stale_after = %d, want the default", report.Sync.StaleAfterSeconds)
	}
	if opened != 0 {
		t.Errorf("opened the index %d times when it does not exist", opened)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Errorf("status created the database: %v", err)
	}
}

// "Not there yet" is a fresh install; "there but unreadable" is a fault. Reporting
// the second as "missing" would send the user to `atm sync` for a permissions
// problem that syncing cannot fix.
func TestStatusFailsOnAnUnreadableIndexRatherThanCallingItMissing(t *testing.T) {
	withTempAtmDir(t)
	service := NewService(ServiceOptions{
		Now:         func() time.Time { return time.Unix(syncTestNow, 0).UTC() },
		IndexExists: func() (bool, error) { return false, errors.New("permission denied") },
	})

	_, err := service.Status(context.Background(), syncTestCall(), StatusInput{})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeUnavailable {
		t.Fatalf("err = %v, want unavailable", err)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error does not carry the cause: %v", err)
	}
}

// An explicit sync is allowed to build the index, which is the one way this read
// may write.
func TestStatusWithSyncBuildsTheIndexAndReportsIt(t *testing.T) {
	withTempAtmDir(t)
	service := NewService(ServiceOptions{
		Now:            func() time.Time { return time.Unix(syncTestNow, 0).UTC() },
		SyncAllContext: func(context.Context, *sql.DB) (int, error) { return 0, nil },
		Sampler:        &recordedSampler{},
	})

	report, err := service.Status(context.Background(), syncTestCall(), StatusInput{Sync: true})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !report.Index.Exists || report.Index.SchemaVersion != store.SchemaVersion {
		t.Fatalf("report = %#v", report)
	}
	if report.Sync.Scope != store.SyncScopeAll {
		t.Errorf("scope = %q, want %q", report.Sync.Scope, store.SyncScopeAll)
	}
	if report.GeneratedAt != time.Unix(syncTestNow, 0).UTC().Format(time.RFC3339) {
		t.Errorf("generated_at = %q, not the injected clock", report.GeneratedAt)
	}
}

// The scope an already-recorded run was filed under is "all", so status has to
// accept it as well as an agent name.
func TestStatusAcceptsTheAllScopeAndAnAgentName(t *testing.T) {
	withTempAtmDir(t)
	service := NewService(ServiceOptions{
		Now:            func() time.Time { return time.Unix(syncTestNow, 0).UTC() },
		SyncAllContext: func(context.Context, *sql.DB) (int, error) { return 0, nil },
		Sampler:        &recordedSampler{},
	})
	if _, err := service.Status(context.Background(), syncTestCall(),
		StatusInput{Scope: store.SyncScopeAll, Sync: true}); err != nil {
		t.Fatalf("all scope: %v", err)
	}
	report, err := service.Status(context.Background(), syncTestCall(), StatusInput{Scope: "codex"})
	if err != nil {
		t.Fatalf("agent scope: %v", err)
	}
	if report.Sync.Scope != "codex" {
		t.Errorf("scope = %q, want codex", report.Sync.Scope)
	}
	_, err = service.Status(context.Background(), syncTestCall(), StatusInput{Scope: "nope"})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeInvalidArgument {
		t.Fatalf("err = %v, want invalid_argument", err)
	}
}
