package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestHTTPDrainTimeoutRetainsOwnershipAfterRuntimeCleanup(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	var runtimeStopped atomic.Bool
	server, err := Start(Options{
		DataDir: t.TempDir(),
		Assets:  fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}},
		Dispatch: func(context.Context, application.Call, string, json.RawMessage, string) (any, error) {
			close(entered)
			defer close(finished)
			// A filesystem projection can still be waiting for its flock after
			// its HTTP request was canceled. Keep it alive across Close.
			<-release
			return map[string]bool{"committed": true}, nil
		},
		StartRuntime: func(Instance, func(...string)) (func(context.Context) error, error) {
			return func(context.Context) error { runtimeStopped.Store(true); return nil }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
		// Production retains the lock until process exit after a failed drain.
		// This test owns a temporary directory and must release that descriptor.
		unlockInstance(server.lock)
		_ = server.lock.Close()
	})
	cookie, csrf := connectBrowser(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.Info().Origin+"/api/v1/todo.update", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", server.Info().Origin)
	request.Header.Set("X-ATM-CSRF", csrf)
	request.Header.Set("Content-Type", "application/json")
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	requestDone := make(chan error, 1)
	go func() {
		response, err := (&http.Client{Transport: transport}).Do(request)
		if response != nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case err := <-requestDone:
		t.Fatalf("mutation did not reach the handler: %v", err)
	case <-ctx.Done():
		t.Fatal("mutation did not enter before its deadline")
	}
	server.Close()
	if err := server.Wait(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain failure was not returned: %v", err)
	}
	if !runtimeStopped.Load() {
		t.Fatal("HTTP drain failure skipped runtime cleanup")
	}
	select {
	case <-finished:
		t.Fatal("test mutation ended before ownership was checked")
	default:
	}
	for _, name := range []string{"server.json", "control.token"} {
		if _, err := os.Stat(filepath.Join(server.Info().DataDir, "runtime", name)); err != nil {
			t.Fatalf("drain failure removed %s: %v", name, err)
		}
	}
	if replacement, err := Start(server.options); !errors.Is(err, ErrAlreadyRunning) {
		if replacement != nil {
			replacement.Close()
		}
		t.Fatalf("second instance started while the old mutation was active: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("released mutation did not finish")
	}
}

func TestFirstFingerprintInvalidatesChangesAfterInitialReset(t *testing.T) {
	file := filepath.Join(t.TempDir(), "knowledge.md")
	if err := os.WriteFile(file, []byte("before reset"), 0600); err != nil {
		t.Fatal(err)
	}
	broker := newEventBroker("first-scan")
	defer broker.close()
	server := &Server{events: broker, options: Options{Fingerprints: func(context.Context, []string) (map[string]string, error) {
		data, err := os.ReadFile(file)
		return map[string]string{"knowledge": string(data)}, err
	}}}
	sub, initial := broker.subscribe([]string{"knowledge"}, "")
	if sub == nil || len(initial) != 1 || initial[0].Kind != "reset" {
		t.Fatalf("initial event = %+v", initial)
	}
	// The browser has refreshed for reset, then a CLI edits knowledge before
	// the watcher's first scan establishes its baseline.
	if data, err := os.ReadFile(file); err != nil || string(data) != "before reset" {
		t.Fatalf("initial document = %q: %v", data, err)
	}
	if err := os.WriteFile(file, []byte("external CLI update"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); server.watchChanges() }()
	defer func() { broker.close(); <-done }()
	select {
	case event := <-sub.queue:
		if event.Kind != "reset" {
			t.Fatalf("first scan event = %+v", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first scan swallowed the external change as its baseline")
	}
}
