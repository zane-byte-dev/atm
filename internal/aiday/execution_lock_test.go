package aiday_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/executionlock"
)

func TestProjectionExecutorsShareDatabaseLockAndCancelWaits(t *testing.T) {
	// This opener deliberately restores the global config after initializing its
	// isolated database. The supplied connection must determine the lock scope.
	opener := newApplicationServiceDatabase(t)
	db, err := opener()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := executionlock.AcquireDatabase(context.Background(), db, "aiday")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	service := aiday.NewService(aiday.ServiceOptions{OpenRead: opener, OpenWrite: opener})
	for _, test := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"today", func(ctx context.Context) error {
			_, _, err := service.Today(ctx, aiday.TodayInput{})
			return err
		}},
		{"rebuild", func(ctx context.Context) error {
			_, _, err := service.Rebuild(ctx, aiday.RebuildInput{})
			return err
		}},
		{"dashboard", func(ctx context.Context) error {
			_, _, err := service.Dashboard(ctx, aiday.DashboardInput{Days: 7})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := test.run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("projection escaped executor lock: %v", err)
			}
		})
	}
}
