package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func startBlockedChangeWatcher(t *testing.T, fingerprint Fingerprints) *Server {
	t.Helper()
	server, err := Start(Options{
		DataDir:      t.TempDir(),
		Assets:       fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
		Fingerprints: fingerprint,
		StartRuntime: func(Instance, func(...string)) (func(context.Context) error, error) {
			return func(context.Context) error { return nil }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if subscriber, _ := server.events.subscribe([]string{"todos"}, ""); subscriber == nil {
		server.Close()
		t.Fatal("failed to subscribe")
	}
	server.Invalidate("todos")
	return server
}

func waitForWatcherSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(message)
	}
}

func assertWatcherOwnsInstance(t *testing.T, server *Server) {
	t.Helper()
	if err := WithStoppedInstance(server.Info().DataDir, func() error { return nil }); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("instance lock released while watcher still active: %v", err)
	}
	for _, name := range []string{"server.json", "control.token"} {
		if _, err := os.Stat(filepath.Join(server.Info().DataDir, "runtime", name)); err != nil {
			t.Fatalf("active watcher lost %s: %v", name, err)
		}
	}
}

func TestCloseCancelsAndDrainsChangeWatcherBeforeUnlock(t *testing.T) {
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	server := startBlockedChangeWatcher(t, func(ctx context.Context, _ []string) (map[string]string, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		// Cancellation starts cleanup; ownership must cover that cleanup too.
		<-release
		return nil, ctx.Err()
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); server.Close() })
	waitForWatcherSignal(t, entered, "fingerprint callback did not start")
	closed := make(chan struct{})
	go func() { server.Close(); close(closed) }()
	waitForWatcherSignal(t, canceled, "shutdown did not cancel the fingerprint context")
	select {
	case <-closed:
		t.Fatal("Close returned before the canceled projection finished cleanup")
	default:
	}
	assertWatcherOwnsInstance(t, server)
	releaseOnce.Do(func() { close(release) })
	waitForWatcherSignal(t, closed, "Close did not finish after projection cleanup")
	if err := server.Wait(context.Background()); err != nil {
		t.Fatalf("cooperative watcher shutdown failed: %v", err)
	}
	if err := WithStoppedInstance(server.Info().DataDir, func() error { return nil }); err != nil {
		t.Fatalf("drained watcher retained instance ownership: %v", err)
	}
}

func TestChangeWatcherTimeoutRetainsOwnershipUntilProjectionExits(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	server := startBlockedChangeWatcher(t, func(context.Context, []string) (map[string]string, error) {
		close(entered)
		// Simulate a callback that cannot yet cancel its filesystem write.
		<-release
		return map[string]string{"todos": "late projection committed"}, nil
	})
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }); server.Close() })
	waitForWatcherSignal(t, entered, "fingerprint callback did not start")
	closed := make(chan struct{})
	go func() { server.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(15 * time.Second):
		t.Fatal("uncooperative watcher made Close wait beyond its drain budget")
	}
	if err := server.Wait(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("watcher timeout was not reported: %v", err)
	}
	assertWatcherOwnsInstance(t, server)
	releaseOnce.Do(func() { close(release) })
	waitForWatcherSignal(t, server.watcherDone, "released projection did not exit")
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := WithStoppedInstance(server.Info().DataDir, func() error { return nil })
		if err == nil {
			break
		}
		if !errors.Is(err, ErrAlreadyRunning) || time.Now().After(deadline) {
			t.Fatalf("finished watcher did not release instance ownership: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, name := range []string{"server.json", "control.token"} {
		if _, err := os.Stat(filepath.Join(server.Info().DataDir, "runtime", name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("finished watcher retained %s: %v", name, err)
		}
	}
	server.Close() // Late ownership cleanup remains compatible with idempotent Close.
}
