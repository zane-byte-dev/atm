package web

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitStoppedHonorsContextWhileInstanceOwnsLock(t *testing.T) {
	server := startTestServer(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := WaitStopped(ctx, server.Info().DataDir); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait on running instance = %v", err)
	}
	server.Close()
	if err := WaitStopped(context.Background(), server.Info().DataDir); err != nil {
		t.Fatalf("wait on stopped instance = %v", err)
	}
}

func TestWaitStoppedDoesNotCreateMissingRuntime(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "missing")
	if err := WaitStopped(context.Background(), dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wait created data directory: %v", err)
	}
}
