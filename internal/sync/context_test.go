package sync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/executionlock"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestDefaultSyncPortsCancelWhileWaitingForAnotherExecutor(t *testing.T) {
	dir := withTempAtmDir(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	lock, err := executionlock.Acquire(context.Background(), dir, "sync")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	service := NewService(ServiceOptions{})
	for _, test := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"run-all", func(ctx context.Context) error {
			_, err := service.Run(ctx, syncTestCall(), RunInput{})
			return err
		}},
		{"run-agent", func(ctx context.Context) error {
			_, err := service.Run(ctx, syncTestCall(), RunInput{Agent: "claude"})
			return err
		}},
		{"status-sync", func(ctx context.Context) error {
			_, err := service.Status(ctx, syncTestCall(), StatusInput{Sync: true})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := test.run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("context was not propagated to sync: %v", err)
			}
		})
	}
}
