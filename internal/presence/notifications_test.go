package presence

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func todoNote(key string) Notification {
	return Notification{ID: "todo-t1", Kind: "todo_review", Action: "post", Title: "ATM", Subtitle: "t1 待验收", Body: "fixture", ObjectID: "t1", DedupKey: key}
}

func receiveNotification(t *testing.T, delivered <-chan Notification) Notification {
	t.Helper()
	select {
	case n := <-delivered:
		return n
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not delivered")
		return Notification{}
	}
}

func TestCompanionLeaseHasOneDisplayOwner(t *testing.T) {
	opts, clock := fixture(t)
	delivered := make(chan Notification, 16)
	opts.Notify = func(n Notification) { delivered <- n }
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if ok, err := r.Publish(todoNote("before-companion")); !ok || err != nil {
		t.Fatalf("publish: %v %v", ok, err)
	}
	if n := receiveNotification(t, delivered); n.Sequence != 1 {
		t.Fatalf("wrong fallback: %+v", n)
	}
	feed, err := r.ClaimCompanion("native-1", 0)
	if err != nil || len(feed.Notifications) != 0 || feed.Cursor != 1 || feed.LeaseUntil == nil {
		t.Fatalf("claim replayed fallback: %+v %v", feed, err)
	}
	if _, err := r.ClaimCompanion("native-2", 0); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("second display owner: %v", err)
	}
	if _, err := r.Publish(todoNote("native-event")); err != nil {
		t.Fatal(err)
	}
	feed, err = r.ClaimCompanion("native-1", 1)
	if err != nil || len(feed.Notifications) != 1 || feed.Notifications[0].Sequence != 2 {
		t.Fatalf("native feed: %+v %v", feed, err)
	}
	if err := r.AckCompanion("native-2", 2); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("foreign acknowledgement: %v", err)
	}
	if err := r.AckCompanion("native-1", 3); err == nil {
		t.Fatal("future acknowledgement accepted")
	}
	if err := r.AckCompanion("native-1", 2); err != nil {
		t.Fatal(err)
	}
	feed, _ = r.ClaimCompanion("native-1", 0)
	if len(feed.Notifications) != 0 {
		t.Fatal("acknowledged banner replayed")
	}
	select {
	case n := <-delivered:
		t.Fatalf("native notification also used fallback: %+v", n)
	default:
	}
	if _, err := r.Publish(todoNote("unacknowledged")); err != nil {
		t.Fatal(err)
	}
	clock.advance(CompanionLease + time.Second)
	r.expire()
	if n := receiveNotification(t, delivered); n.Sequence != 3 {
		t.Fatalf("wrong expired-lease fallback: %+v", n)
	}
	r.expire()
	feed, err = r.ClaimCompanion("native-2", 0)
	if err != nil || len(feed.Notifications) != 0 {
		t.Fatalf("takeover replayed expired lease: %+v %v", feed, err)
	}
	if _, err := r.Publish(todoNote("release")); err != nil {
		t.Fatal(err)
	}
	if err := r.ReleaseCompanion("native-2"); err != nil {
		t.Fatal(err)
	}
	if n := receiveNotification(t, delivered); n.Sequence != 4 {
		t.Fatalf("wrong release fallback: %+v", n)
	}
	if len(r.Notifications(0).Notifications) != 4 {
		t.Fatal("native display receipts hid Web feed")
	}
}

func TestLostNativeAcknowledgementDoesNotSwitchDisplayChannels(t *testing.T) {
	opts, clock := fixture(t)
	delivered := make(chan Notification, 8)
	opts.Notify = func(n Notification) { delivered <- n }
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.ClaimCompanion("native", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Publish(todoNote("offered-but-unacknowledged")); err != nil {
		t.Fatal(err)
	}
	feed, err := r.ClaimCompanion("native", 0)
	if err != nil || len(feed.Notifications) != 1 {
		t.Fatalf("native did not get notification: %+v %v", feed, err)
	}
	clock.advance(CompanionLease + time.Second)
	r.expire()
	feed, err = r.ClaimCompanion("replacement", 0)
	if err != nil || len(feed.Notifications) != 0 {
		t.Fatalf("replacement replayed ambiguous display: %+v %v", feed, err)
	}
	select {
	case n := <-delivered:
		t.Fatalf("ambiguous native delivery also used fallback: %+v", n)
	default:
	}
	if len(r.Notifications(0).Notifications) != 1 {
		t.Fatal("ambiguous delivery removed Web state")
	}
}

func TestDisabledSystemNotificationsSuppressFallbackAndSurviveRestart(t *testing.T) {
	opts, _ := fixture(t)
	delivered := make(chan Notification, 8)
	opts.Notify = func(notification Notification) { delivered <- notification }
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ClaimCompanion("native", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Publish(todoNote("pending-at-disable")); err != nil {
		t.Fatal(err)
	}
	feed, err := r.DisableSystemNotifications("native", 0)
	if err != nil || feed.Cursor != 1 || len(feed.Notifications) != 1 || feed.LeaseUntil != nil {
		t.Fatalf("disabled feed: %+v %v", feed, err)
	}
	if r.SystemNotificationsEnabled() {
		t.Fatal("disabled preference did not reach notification boundary")
	}
	if _, err := r.Publish(todoNote("created-while-disabled")); err != nil {
		t.Fatal(err)
	}
	if web := r.Notifications(0); web.Cursor != 2 || len(web.Notifications) != 2 {
		t.Fatalf("disabled notifications disappeared from Web feed: %+v", web)
	}
	select {
	case notification := <-delivered:
		t.Fatalf("disabled notification used OS fallback: %+v", notification)
	default:
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	opts.InstanceID = "restarted-fixture"
	next, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if next.SystemNotificationsEnabled() {
		t.Fatal("service restart lost disabled notification preference")
	}
	if _, err := next.Publish(todoNote("after-restart")); err != nil {
		t.Fatal(err)
	}
	select {
	case notification := <-delivered:
		t.Fatalf("restart resumed OS fallback: %+v", notification)
	default:
	}

	// Menu has already advanced through the disabled feed. Enabling at that
	// cursor must not replay the suppressed entry, while the next event is live.
	feed, err = next.ClaimCompanion("native-restarted", 3)
	if err != nil || len(feed.Notifications) != 0 || feed.LeaseUntil == nil {
		t.Fatalf("enable replayed disabled history: %+v %v", feed, err)
	}
	if !next.SystemNotificationsEnabled() {
		t.Fatal("enabled preference did not reach notification boundary")
	}
	if _, err := next.Publish(todoNote("new-after-enable")); err != nil {
		t.Fatal(err)
	}
	feed, err = next.ClaimCompanion("native-restarted", 3)
	if err != nil || len(feed.Notifications) != 1 || feed.Notifications[0].Sequence != 4 {
		t.Fatalf("new enabled notification missing: %+v %v", feed, err)
	}
	select {
	case notification := <-delivered:
		t.Fatalf("active native owner also used OS fallback: %+v", notification)
	default:
	}
}

func TestRapidReenableDoesNotReleaseAlreadyQueuedFallback(t *testing.T) {
	opts, _ := fixture(t)
	queued := make(chan Notification, 1)
	opts.Notify = func(notification Notification) { queued <- notification }
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Publish(todoNote("queued-before-disable")); err != nil {
		t.Fatal(err)
	}
	queuedNotification := receiveNotification(t, queued)
	if _, err := r.DisableSystemNotifications("native", queuedNotification.Sequence); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ClaimCompanion("native", queuedNotification.Sequence); err != nil {
		t.Fatal(err)
	}
	if r.ShouldDisplayFallback(queuedNotification) {
		t.Fatal("rapid re-enable released a fallback queued before disable")
	}
}

func TestNotificationDedupSurvivesRestartWithoutHistoryReplay(t *testing.T) {
	opts, _ := fixture(t)
	delivered := make(chan Notification, 8)
	opts.Notify = func(n Notification) { delivered <- n }
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Publish(todoNote("transition")); !ok || err != nil {
		t.Fatal(ok, err)
	}
	_ = receiveNotification(t, delivered)
	if ok, err := r.Publish(todoNote("transition")); ok || err != nil {
		t.Fatal("duplicate accepted", ok, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if ok, err := next.Publish(todoNote("transition")); ok || err != nil {
		t.Fatal("restart replayed receipt", ok, err)
	}
	feed, err := next.ClaimCompanion("new-native", 0)
	if err != nil || feed.Cursor != 1 || !feed.Truncated || len(feed.Notifications) != 0 {
		t.Fatalf("restart baseline: %+v %v", feed, err)
	}
	select {
	case n := <-delivered:
		t.Fatalf("replayed old notification: %+v", n)
	default:
	}
	if ok, err := next.Publish(todoNote("next-transition")); !ok || err != nil {
		t.Fatal(ok, err)
	}
	feed, _ = next.ClaimCompanion("new-native", 1)
	if len(feed.Notifications) != 1 || feed.Notifications[0].Sequence != 2 {
		t.Fatal("new transition not delivered after restart")
	}
}

func TestForwardAcknowledgesTypedTransitionsAndPreservesOwnershipOnFailure(t *testing.T) {
	opts, _ := fixture(t)
	if owned, err := Forward(opts.DataDir, opts.SocketPath, todoNote("missing")); owned || err != nil {
		t.Fatalf("standalone CLI lost fallback: %v %v", owned, err)
	}
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < 2; i++ {
		if owned, err := Forward(opts.DataDir, opts.SocketPath, todoNote("retry")); !owned || err != nil {
			t.Fatalf("forward: %v %v", owned, err)
		}
	}
	if r.Notifications(0).Cursor != 1 {
		t.Fatal("CLI retry produced multiple notifications")
	}
	unsafe := todoNote("unsafe")
	unsafe.Kind = "shell"
	if owned, err := Forward(opts.DataDir, opts.SocketPath, unsafe); !owned || err == nil {
		t.Fatalf("unsafe notification accepted: %v %v", owned, err)
	}
	if r.Notifications(0).Cursor != 1 {
		t.Fatal("invalid envelope changed cursor")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if owned, err := Forward(opts.DataDir, opts.SocketPath, todoNote("stopped")); !owned || err == nil {
		t.Fatalf("stopped Go owner reopened CLI fallback: %v %v", owned, err)
	}
}

func TestCursorFailureNeverDeliversUnrecordedNotification(t *testing.T) {
	r, _ := startFixture(t)
	path := filepath.Join(r.opts.DataDir, "runtime", NotificationFile)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Publish(todoNote("blocked-cursor")); ok || err == nil {
		t.Fatalf("accepted without receipt: %v %v", ok, err)
	}
	if r.Notifications(0).Cursor != 0 || len(r.seen) != 0 {
		t.Fatal("failed publication left a receipt")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.Publish(todoNote("blocked-cursor")); !ok || err != nil {
		t.Fatalf("retry failed: %v %v", ok, err)
	}
}

func TestConcurrentPublicationAndSnapshotsDeduplicate(t *testing.T) {
	r, _ := startFixture(t)
	var count atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := r.Publish(todoNote("same-transition")); err != nil {
				t.Error(err)
			} else if ok {
				count.Add(1)
			}
			_ = r.Snapshot()
			_ = r.Notifications(0)
		}()
	}
	wg.Wait()
	if count.Load() != 1 || r.Notifications(0).Cursor != 1 {
		t.Fatal("concurrent publishers duplicated a transition")
	}
}
