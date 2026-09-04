package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
)

func nativeControlCall() application.Call {
	return application.Call{RequestID: "native-control-test", Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginNativeControl}}
}

func TestNativeSessionSyncIsBoundedIdempotentSingleFlight(t *testing.T) {
	h := testHost(t)
	started := make(chan background.Request, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	attachFixtureJobs(t, h, func(ctx context.Context, _ application.Call, request background.Request, _ func(string)) (any, error) {
		select {
		case started <- request:
		default:
		}
		select {
		case <-release:
			return map[string]bool{"ok": true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	value, err := h.NativeControl(context.Background(), nativeControlCall(), NativeControlSessionSync, json.RawMessage(`{"idempotency_key":"panel:open:1"}`))
	if err != nil {
		t.Fatal(err)
	}
	first := value.(background.Job)
	if first.ID == "" || first.Kind != background.SessionSync {
		t.Fatalf("job = %+v", first)
	}
	select {
	case request := <-started:
		if request.Kind != background.SessionSync || request.DueOnly || request.SourceID != "" {
			t.Fatalf("unbounded native request: %+v", request)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session sync was not started")
	}

	retry, err := h.NativeControl(context.Background(), nativeControlCall(), NativeControlSessionSync, json.RawMessage(`{"idempotency_key":"panel:open:1"}`))
	if err != nil || retry.(background.Job).ID != first.ID {
		t.Fatalf("durable retry = %#v, %v", retry, err)
	}
	// A second panel gesture with a new key does not start a duplicate. It is not
	// recorded as accepted, so retrying that key after this job finishes remains
	// a real future refresh rather than an invented idempotency receipt.
	if _, err := h.NativeControl(context.Background(), nativeControlCall(), NativeControlSessionSync, json.RawMessage(`{"idempotency_key":"panel:open:2"}`)); !errors.Is(err, application.ErrBusy) {
		t.Fatalf("single-flight error = %v", err)
	}

	for _, body := range []string{
		`{}`,
		`{"idempotency_key":"spaces are not opaque ids"}`,
		`{"idempotency_key":"panel:open:3","kind":"collect.run"}`,
		`{"idempotency_key":"panel:open:3","due_only":true}`,
		`{"idempotency_key":"panel:open:3","command":"rm"}`,
	} {
		if _, err := h.NativeControl(context.Background(), nativeControlCall(), NativeControlSessionSync, json.RawMessage(body)); !errors.Is(err, application.ErrInvalidArgument) {
			t.Errorf("body %s error = %v", body, err)
		}
	}
	web := nativeControlCall()
	web.Actor.Origin = application.OriginWeb
	if _, err := h.NativeControl(context.Background(), web, NativeControlSessionSync, json.RawMessage(`{"idempotency_key":"panel:open:4"}`)); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Web actor error = %v", err)
	}
	for _, method := range []string{"guard.pending", "guard.detail", "guard.decision"} {
		if _, err := h.NativeControl(context.Background(), nativeControlCall(), method, json.RawMessage(`{}`)); !errors.Is(err, application.ErrNotFound) {
			t.Errorf("removed native method %s error = %v, want not_found", method, err)
		}
	}
	releaseOnce.Do(func() { close(release) })
}
