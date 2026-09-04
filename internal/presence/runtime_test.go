package presence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
)

type fixtureClock struct{ nano atomic.Int64 }

func (c *fixtureClock) now() time.Time                 { return time.Unix(0, c.nano.Load()).UTC() }
func (c *fixtureClock) advance(duration time.Duration) { c.nano.Add(int64(duration)) }

func fixture(t *testing.T) (Options, *fixtureClock) {
	t.Helper()
	dir, err := os.MkdirTemp("", "atm-presence-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	clock := &fixtureClock{}
	clock.nano.Store(time.Now().UTC().Truncate(time.Second).UnixNano())
	return Options{DataDir: dir, SocketPath: filepath.Join(dir, "notch.sock"), InstanceID: "fixture", Now: clock.now}, clock
}

func startFixture(t *testing.T) (*Runtime, *fixtureClock) {
	t.Helper()
	opts, clock := fixture(t)
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r, clock
}

func eventAt(clock *fixtureClock, source, id string, kind agentevent.Kind) agentevent.Envelope {
	return agentevent.Envelope{Version: 1, Source: source, SessionID: id, CWD: "/workspace/shared", Event: kind, At: clock.now().Format(time.RFC3339Nano)}
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

func TestRuntimeOwnershipConflictAndRestart(t *testing.T) {
	opts, _ := fixture(t)
	r, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := Start(opts); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second owner: %v", err)
	}
	owner, err := ReadOwner(opts.DataDir)
	if err != nil || !owner.Running || owner.Owner != "go" {
		t.Fatalf("owner: %+v %v", owner, err)
	}
	for _, path := range []string{opts.SocketPath, filepath.Join(opts.DataDir, "runtime", OwnerFile)} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private mode %s: %v %v", path, info, err)
		}
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	owner, err = ReadOwner(opts.DataDir)
	if err != nil || owner.Running || owner.Owner != "go" {
		t.Fatalf("ownership choice did not survive stop: %+v %v", owner, err)
	}
	if _, err = os.Lstat(opts.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned socket remains: %v", err)
	}
	opts.InstanceID = "second-fixture"
	next, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	_ = r.Close()
	if _, err := os.Stat(opts.SocketPath); err != nil {
		t.Fatalf("previous owner's repeated Close removed new socket: %v", err)
	}
}

func TestForeignLiveSocketIsNeverRemoved(t *testing.T) {
	opts, _ := fixture(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: opts.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	before, _ := os.Lstat(opts.SocketPath)
	if _, err = Start(opts); !errors.Is(err, ErrSocketOwned) {
		t.Fatalf("foreign owner: %v", err)
	}
	after, err := os.Lstat(opts.SocketPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("foreign socket was changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.DataDir, "runtime", OwnerFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed takeover published owner: %v", err)
	}
}

func TestCloseDoesNotUnlinkReplacementSocket(t *testing.T) {
	r, _ := startFixture(t)
	if err := os.Remove(r.opts.SocketPath); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: r.opts.SocketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	before, _ := os.Lstat(r.opts.SocketPath)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(r.opts.SocketPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("Close removed replacement socket: %v", err)
	}
}

func TestStaleSocketRecoveryAndUnsafeFiles(t *testing.T) {
	t.Run("stale socket", func(t *testing.T) {
		opts, _ := fixture(t)
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: opts.SocketPath, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		listener.SetUnlinkOnClose(false)
		_ = listener.Close()
		r, err := Start(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
	})
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			opts, _ := fixture(t)
			if kind == "regular" {
				if err := os.WriteFile(opts.SocketPath, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink("untouched", opts.SocketPath); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Lstat(opts.SocketPath)
			if _, err := Start(opts); !errors.Is(err, ErrSocketOwned) {
				t.Fatalf("unsafe path: %v", err)
			}
			after, err := os.Lstat(opts.SocketPath)
			if err != nil || !os.SameFile(before, after) {
				t.Fatal("unsafe path was modified")
			}
		})
	}
}

func TestSocketProtocolBoundedFramingAndGuard(t *testing.T) {
	r, clock := startFixture(t)
	conn, err := net.Dial("unix", r.opts.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	line, _ := eventAt(clock, "codex", "thread", agentevent.KindAttention).Line()
	if _, err = conn.Write(append([]byte("bad json\n{\"v\":99}\n"), line[:15]...)); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Write(line[15:]); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	eventually(t, func() bool { return r.Snapshot().AttentionCount == 1 })
	guard := agentevent.GuardEnvelope{Version: 1, Type: agentevent.TypeGuardRequest, ID: "g1", Label: "发送消息", Body: "fixture approval", ExpiresAt: clock.now().Add(time.Minute).Unix()}
	line, _ = guard.Line()
	conn, err = net.Dial("unix", r.opts.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = conn.Write(line)
	_, _ = conn.Write(line)
	_ = conn.Close()
	eventually(t, func() bool { return len(r.Notifications(0).Notifications) == 2 })
	if len(r.Snapshot().Sessions) != 1 {
		t.Fatal("guard request invented an Agent session")
	}
	conn, err = net.Dial("unix", r.opts.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = conn.Write([]byte(strings.Repeat("x", 65<<10) + "\n"))
	_, err = conn.Read(make([]byte, 1))
	_ = conn.Close()
	if err == nil {
		t.Fatal("oversize connection remained open")
	}
}

func TestAttentionJoinOrderingClearAndExpiry(t *testing.T) {
	r, clock := startFixture(t)
	r.Merge([]Session{{ID: "short", ResumeID: "full-codex", Source: "codex", CWD: "/workspace/shared", State: "active"}, {ID: "claude-session", Source: "claude", CWD: "/workspace/shared", State: "active"}})
	event := eventAt(clock, "codex", "full-codex", agentevent.KindAttention)
	event.Reason, event.Text = "permission_prompt", "test tool"
	if err := r.Apply(event); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if len(snapshot.Sessions) != 2 || snapshot.AttentionCount != 1 || snapshot.Sessions[0].Attention == nil || snapshot.Sessions[1].Attention != nil {
		t.Fatalf("joined wrong session: %+v", snapshot)
	}
	old := event
	old.At = clock.now().Add(-time.Second).Format(time.RFC3339Nano)
	old.Event = agentevent.KindCompleted
	_ = r.Apply(old)
	if r.Snapshot().AttentionCount != 1 {
		t.Fatal("out-of-order completion cleared current attention")
	}
	clock.advance(time.Second)
	_ = r.Apply(eventAt(clock, "codex", "full-codex", agentevent.KindSessionStart))
	if got := r.Snapshot().Sessions[0].Attention; got == nil || got.Reason != "permission_prompt" || got.Text != "test tool" {
		t.Fatalf("session discovery rewrote attention: %+v", got)
	}
	clock.advance(time.Second)
	_ = r.Apply(eventAt(clock, "codex", "full-codex", agentevent.KindResumed))
	if r.Snapshot().AttentionCount != 0 {
		t.Fatal("resumed did not clear attention")
	}
	feed := r.Notifications(0)
	if len(feed.Notifications) != 2 || feed.Notifications[1].Action != "withdraw" || feed.Notifications[0].ID != feed.Notifications[1].ID {
		t.Fatalf("missing stable withdrawal: %+v", feed)
	}
	clock.advance(time.Second)
	event = eventAt(clock, "claude", "", agentevent.KindAttention)
	_ = r.Apply(event)
	if r.Snapshot().AttentionCount != 1 || r.Snapshot().Sessions[0].Attention != nil {
		t.Fatal("directory signal contaminated another Agent")
	}
	clock.advance(AttentionTTL)
	r.expire()
	if r.Snapshot().AttentionCount != 0 {
		t.Fatal("attention did not expire")
	}
	feed = r.Notifications(0)
	if feed.Notifications[len(feed.Notifications)-1].Action != "withdraw" {
		t.Fatal("expiry did not withdraw banner")
	}
}

func TestSameSecondHooksKeepSourceOrderAndStartANewTurn(t *testing.T) {
	r, clock := startFixture(t)
	base := clock.now()
	raw := map[agentevent.Kind]string{
		agentevent.KindStarted:   `{"hook_event_name":"UserPromptSubmit","session_id":"same-second","cwd":"/workspace/shared"}`,
		agentevent.KindAttention: `{"hook_event_name":"Notification","session_id":"same-second","cwd":"/workspace/shared","notification_type":"permission_prompt"}`,
		agentevent.KindResumed:   `{"hook_event_name":"PostToolUse","session_id":"same-second","cwd":"/workspace/shared"}`,
		agentevent.KindCompleted: `{"hook_event_name":"Stop","session_id":"same-second","cwd":"/workspace/shared"}`,
	}
	makeEvent := func(kind agentevent.Kind, offset time.Duration) agentevent.Envelope {
		t.Helper()
		event, ok, err := agentevent.FromHook(agentevent.Input{Source: agentevent.SourceClaude, Raw: []byte(raw[kind]), Now: base.Add(offset)})
		if err != nil || !ok {
			t.Fatalf("FromHook(%s) = %+v, %v, %v", kind, event, ok, err)
		}
		return event
	}
	started := makeEvent(agentevent.KindStarted, 100*time.Millisecond)
	attention := makeEvent(agentevent.KindAttention, 200*time.Millisecond)
	resumed := makeEvent(agentevent.KindResumed, 300*time.Millisecond)
	completed := makeEvent(agentevent.KindCompleted, 400*time.Millisecond)
	if started.At == attention.At || attention.At == resumed.At || resumed.At == completed.At {
		t.Fatalf("same-second hooks lost subsecond order: %q %q %q %q", started.At, attention.At, resumed.At, completed.At)
	}

	// Separate socket connections are read by separate goroutines. Model a
	// completion winning the scheduler race, followed by older queued events.
	for _, event := range []agentevent.Envelope{started, completed, attention, resumed} {
		if err := r.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := r.Snapshot()
	if len(snapshot.Sessions) != 1 || snapshot.Sessions[0].State != "completed" || snapshot.AttentionCount != 0 {
		t.Fatalf("out-of-order delivery rewound state: %+v", snapshot)
	}
	if feed := r.Notifications(0); len(feed.Notifications) != 1 || feed.Notifications[0].Kind != "completed" {
		t.Fatalf("out-of-order delivery emitted stale transitions: %+v", feed)
	}

	// Legacy v1 senders used whole-second timestamps. A Started at the same
	// instant after completion is treated as the next explicit turn boundary.
	next := started
	next.At = completed.At
	next.Text = "next turn"
	if err := r.Apply(next); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().Sessions[0].State; got != "busy" {
		t.Fatalf("same-time next turn was suppressed: %q", got)
	}

	legacyID := "legacy-same-second"
	legacyAt := base.Format(time.RFC3339)
	legacy := func(event agentevent.Envelope, kind agentevent.Kind) agentevent.Envelope {
		event.Event = kind
		event.SessionID = legacyID
		event.At = legacyAt
		return event
	}
	legacyStarted := legacy(started, agentevent.KindStarted)
	legacyAttention := legacy(attention, agentevent.KindAttention)
	legacyResumed := legacy(resumed, agentevent.KindResumed)
	legacyCompleted := legacy(completed, agentevent.KindCompleted)
	for _, event := range []agentevent.Envelope{legacyStarted, legacyCompleted, legacyAttention, legacyResumed} {
		if err := r.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	byID := func(id string) Session {
		t.Helper()
		for _, session := range r.Snapshot().Sessions {
			if session.ID == id {
				return session
			}
		}
		t.Fatalf("session %q is missing", id)
		return Session{}
	}
	if got := byID(legacyID).State; got != "completed" {
		t.Fatalf("legacy same-second events rewound completion: %q", got)
	}
	if err := r.Apply(legacyStarted); err != nil {
		t.Fatal(err)
	}
	if got := byID(legacyID).State; got != "busy" {
		t.Fatalf("legacy same-second next turn was suppressed: %q", got)
	}
}

func TestConcurrentSameSecondApplyConvergesOnLatestEvent(t *testing.T) {
	r, clock := startFixture(t)
	base := clock.now()
	id := "concurrent-same-second"
	start := agentevent.Envelope{
		Version: agentevent.Version, Source: "claude", SessionID: id,
		CWD: "/workspace/shared", Event: agentevent.KindSessionStart,
		At: base.Add(50 * time.Millisecond).Format(time.RFC3339Nano),
	}
	if err := r.Apply(start); err != nil {
		t.Fatal(err)
	}
	events := []agentevent.Envelope{
		{Version: agentevent.Version, Source: "claude", SessionID: id, CWD: start.CWD, Event: agentevent.KindStarted, At: base.Add(100 * time.Millisecond).Format(time.RFC3339Nano)},
		{Version: agentevent.Version, Source: "claude", SessionID: id, CWD: start.CWD, Event: agentevent.KindAttention, At: base.Add(200 * time.Millisecond).Format(time.RFC3339Nano)},
		{Version: agentevent.Version, Source: "claude", SessionID: id, CWD: start.CWD, Event: agentevent.KindResumed, At: base.Add(300 * time.Millisecond).Format(time.RFC3339Nano)},
		{Version: agentevent.Version, Source: "claude", SessionID: id, CWD: start.CWD, Event: agentevent.KindCompleted, At: base.Add(400 * time.Millisecond).Format(time.RFC3339Nano)},
	}
	release := make(chan struct{})
	errs := make(chan error, len(events))
	var wg sync.WaitGroup
	for _, event := range events {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-release
			errs <- r.Apply(event)
		}()
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range r.Snapshot().Sessions {
		if session.ID == id {
			if session.State != "completed" || session.Attention != nil {
				t.Fatalf("concurrent apply did not converge: %+v", session)
			}
			return
		}
	}
	t.Fatalf("session %q is missing", id)
}

func TestAmbiguousCWDHookDoesNotPolluteSiblingSessions(t *testing.T) {
	r, clock := startFixture(t)
	sessions := []Session{
		{ID: "one", SessionID: "one", Source: "codex", CWD: "/workspace/shared", State: "active", ResultKey: "old-one"},
		{ID: "two", SessionID: "two", Source: "codex", CWD: "/workspace/shared", State: "active", ResultKey: "old-two"},
	}
	r.Merge(sessions)

	attention := eventAt(clock, "codex", "", agentevent.KindAttention)
	attention.Reason = "permission_prompt"
	if err := r.Apply(attention); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	if len(snapshot.Sessions) != 3 || snapshot.AttentionCount != 1 {
		t.Fatalf("ambiguous cwd signal should remain one generic row: %+v", snapshot)
	}
	for _, session := range snapshot.Sessions[:2] {
		if session.Attention != nil || session.HookBacked {
			t.Fatalf("cwd signal polluted %s: %+v", session.ID, session)
		}
	}
	if generic := snapshot.Sessions[2]; generic.Attention == nil || generic.SessionID != "" {
		t.Fatalf("missing generic cwd signal: %+v", generic)
	}

	// An ambiguous hook must not take completion authority from either parser
	// row. Only the changed row should produce the normal inferred completion.
	sessions[0].ResultKey = "new-one"
	r.Merge(sessions)
	if feed := r.Notifications(0); len(feed.Notifications) != 2 || feed.Notifications[1].ObjectID != "one" {
		t.Fatalf("ambiguous cwd hook suppressed parser completion: %+v", feed)
	}

	clock.advance(time.Second)
	if err := r.Apply(eventAt(clock, "codex", "one", agentevent.KindStarted)); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().AttentionCount; got != 1 {
		t.Fatalf("one sibling cleared an unattributed cwd signal: %d", got)
	}
	clock.advance(time.Second)
	if err := r.Apply(eventAt(clock, "codex", "", agentevent.KindResumed)); err != nil {
		t.Fatal(err)
	}
	if got := r.Snapshot().AttentionCount; got != 0 {
		t.Fatalf("cwd clearing event did not clear its own signal: %d", got)
	}

	clock.advance(time.Second)
	explicit := eventAt(clock, "codex", "two", agentevent.KindAttention)
	explicit.Reason = "permission_prompt"
	if err := r.Apply(explicit); err != nil {
		t.Fatal(err)
	}
	snapshot = r.Snapshot()
	if snapshot.Sessions[0].Attention != nil || snapshot.Sessions[1].Attention == nil || snapshot.AttentionCount != 1 {
		t.Fatalf("session id did not isolate sibling sessions: %+v", snapshot)
	}
}

func TestStartupBaselineCompletionAndResumedAuthority(t *testing.T) {
	r, clock := startFixture(t)
	_ = r.Apply(eventAt(clock, "codex", "existing", agentevent.KindCompleted))
	if r.Notifications(0).Cursor != 0 {
		t.Fatal("startup replayed hook completion")
	}
	r.Merge([]Session{{ID: "unhooked", Source: "grokbuild", ResultKey: "old"}})
	if r.Notifications(0).Cursor != 0 {
		t.Fatal("first projection replayed old completion")
	}
	clock.advance(time.Second)
	_ = r.Apply(eventAt(clock, "grokbuild", "unhooked", agentevent.KindResumed))
	r.Merge([]Session{{ID: "unhooked", Source: "grokbuild", ResultKey: "new"}})
	if r.Notifications(0).Cursor != 1 {
		t.Fatal("tool-only resumed hook silenced unhooked completion")
	}
	clock.advance(time.Second)
	_ = r.Apply(eventAt(clock, "codex", "existing", agentevent.KindStarted))
	clock.advance(time.Second)
	done := eventAt(clock, "codex", "existing", agentevent.KindCompleted)
	_ = r.Apply(done)
	_ = r.Apply(done)
	if r.Notifications(0).Cursor != 2 {
		t.Fatal("live completion was missed or duplicated")
	}
	r.Merge(nil)
	r.Merge([]Session{{ID: "unhooked", Source: "grokbuild", ResultKey: "old"}})
	if r.Notifications(0).Cursor != 2 {
		t.Fatal("reopened historical session replayed completion")
	}
}

func TestPresenceBoundAndValidation(t *testing.T) {
	r, clock := startFixture(t)
	for i := 0; i < MaxSessions+25; i++ {
		if err := r.Apply(eventAt(clock, "pi", fmt.Sprintf("s-%d", i), agentevent.KindSessionStart)); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.Snapshot().Sessions) != MaxSessions || len(r.hooks) != MaxSessions {
		t.Fatal("presence state exceeded budget")
	}
	for _, change := range []func(*agentevent.Envelope){func(e *agentevent.Envelope) { e.Version = 2 }, func(e *agentevent.Envelope) { e.SessionID = ""; e.CWD = "" }, func(e *agentevent.Envelope) { e.At = "bad" }, func(e *agentevent.Envelope) { e.At = clock.now().Add(time.Hour).Format(time.RFC3339Nano) }} {
		event := eventAt(clock, "pi", "invalid", agentevent.KindAttention)
		change(&event)
		if err := r.Apply(event); err == nil {
			t.Fatal("invalid event accepted")
		}
	}
	if r.Snapshot().AttentionCount != 0 {
		t.Fatal("invalid event changed presence")
	}
}

func TestOwnerMarkerOnlyContainsRuntimeMetadata(t *testing.T) {
	r, clock := startFixture(t)
	event := eventAt(clock, "pi", "test-session", agentevent.KindAttention)
	event.Text = "private fixture conversation"
	_ = r.Apply(event)
	for _, filename := range []string{OwnerFile, NotificationFile} {
		content, err := os.ReadFile(filepath.Join(r.opts.DataDir, "runtime", filename))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), event.Text) || strings.Contains(string(content), event.SessionID) {
			t.Fatalf("%s persisted conversation text", filename)
		}
		if !json.Valid(content) {
			t.Fatal("invalid JSON state")
		}
	}
}
