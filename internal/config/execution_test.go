package config

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecutionPolicyCopiesNestedProviderSettings(t *testing.T) {
	previousProviders, previousConnectors := QuotaProviders, CollectionConnectors
	t.Cleanup(func() { QuotaProviders, CollectionConnectors = previousProviders, previousConnectors })
	QuotaProviders = map[string]QuotaProviderConfig{"fixture": {Args: []string{"original"}, VisibleMetrics: []string{"metric"}}}
	CollectionConnectors = map[string]CollectionConnectorConfig{"fixture": {Args: []string{"original"}}}
	policy := CaptureExecutionPolicy()
	QuotaProviders["fixture"].Args[0] = "changed"
	QuotaProviders["fixture"].VisibleMetrics[0] = "changed"
	CollectionConnectors["fixture"].Args[0] = "changed"
	if policy.Providers["fixture"].Args[0] != "original" || policy.Providers["fixture"].VisibleMetrics[0] != "metric" || policy.Connectors["fixture"].Args[0] != "original" {
		t.Fatal("policy retained mutable registry slices")
	}
}

func TestExternalSerializesSiblingBoundariesAndAllowsNesting(t *testing.T) {
	var active, calls atomic.Int32
	ctx := WithExecutionPolicy(context.Background(), &ExecutionPolicy{}, func(ctx context.Context, run func(context.Context) error) error {
		if active.Add(1) != 1 {
			t.Error("sibling boundaries released the resident gate twice")
		}
		defer active.Add(-1)
		calls.Add(1)
		return run(ctx)
	})
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := External(ctx, func(nested context.Context) error {
				return External(nested, func(context.Context) error { time.Sleep(time.Millisecond); return nil })
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 8 || active.Load() != 0 {
		t.Fatalf("unbalanced boundaries: calls=%d active=%d", calls.Load(), active.Load())
	}
}

func TestWaitingExternalBoundaryHonorsCancellation(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx := WithExecutionPolicy(context.Background(), &ExecutionPolicy{}, func(ctx context.Context, run func(context.Context) error) error { return run(ctx) })
	go func() {
		defer close(finished)
		_ = External(ctx, func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	defer func() { close(release); <-finished }()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := External(canceled, func(context.Context) error { t.Error("canceled sibling ran"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled boundary: %v", err)
	}
}

func TestExecutionBoundariesAcceptLegacyNilContext(t *testing.T) {
	if PolicyForExecution(nil) == nil {
		t.Fatal("missing policy")
	}
	if err := External(nil, func(ctx context.Context) error {
		if ctx == nil {
			t.Fatal("external work received nil context")
		}
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	policy := &ExecutionPolicy{}
	if PolicyForExecution(WithExecutionPolicy(nil, policy, nil)) != policy {
		t.Fatal("lost policy")
	}
}
