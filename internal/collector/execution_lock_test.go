package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/executionlock"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectionEntrypointsShareExecutionLock(t *testing.T) {
	withCollectorStore(t)
	lock, err := executionlock.Acquire(context.Background(), config.AtmDir, "collection")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	service := Service{Extractor: &fakeExtractor{}, Connectors: testRegistry(&fakeFetcher{})}
	for _, test := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"run", func(ctx context.Context) error { _, err := service.Run(ctx, ""); return err }},
		{"due", func(ctx context.Context) error { _, err := service.RunDue(ctx, ""); return err }},
		{"typed-run", func(ctx context.Context) error {
			_, err := service.RunCollection(ctx, itemTestCall(), RunInput{})
			return err
		}},
		{"reprocess", func(ctx context.Context) error {
			_, err := service.Reprocess(ctx, itemTestCall(), ReprocessInput{ItemID: "ci_1"})
			return err
		}},
		{"analyze", func(ctx context.Context) error {
			_, err := service.Analyze(ctx, "source", AnalyzeOptions{Local: true})
			return err
		}},
		{"promote", func(ctx context.Context) error {
			_, err := service.Promote(ctx, itemTestCall(), PromoteInput{ItemID: "ci_1"})
			return err
		}},
		{"delete-items", func(ctx context.Context) error {
			_, err := service.DeleteItems(ctx, itemTestCall(), DeleteItemsInput{ItemIDs: []string{"ci_1"}, Confirmed: true})
			return err
		}},
		{"delete-source", func(ctx context.Context) error {
			_, err := service.DeleteSource(ctx, itemTestCall(), DeleteSourceInput{SourceID: "source", Confirmed: true})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := test.run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("collection escaped shared lock: %v", err)
			}
		})
	}
}

type heldCollectionFetcher struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (*heldCollectionFetcher) ID() string { return "test" }

func (fetcher *heldCollectionFetcher) Fetch(ctx context.Context, _ store.CollectionSource, _ int64) ([]Message, int64, error) {
	if fetcher.calls.Add(1) == 1 {
		close(fetcher.entered)
	}
	select {
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-fetcher.release:
		return nil, time.Now().Unix(), nil
	}
}

func TestOverlappingDueRunsRecheckCadenceAfterLockAcquisition(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &heldCollectionFetcher{entered: make(chan struct{}), release: make(chan struct{})}
	service := Service{Extractor: &fakeExtractor{}, Connectors: testRegistry(fetcher)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		report RunReport
		err    error
	}
	results := make(chan result, 2)
	run := func() {
		report, err := service.RunDue(ctx, source.ID)
		results <- result{report, err}
	}
	go run()
	select {
	case <-fetcher.entered:
	case <-ctx.Done():
		t.Fatal("first collection run did not start")
	}
	go run()
	select {
	case got := <-results:
		t.Fatalf("executor finished before fetch release: %+v", got)
	case <-time.After(75 * time.Millisecond):
	}
	close(fetcher.release)
	totalRuns := 0
	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatal(got.err)
		}
		totalRuns += len(got.report.Runs)
	}
	if totalRuns != 1 || fetcher.calls.Load() != 1 {
		t.Fatalf("overlapping schedulers repeated collection: runs=%d fetches=%d", totalRuns, fetcher.calls.Load())
	}
}
