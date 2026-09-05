package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Fingerprints func(context.Context, []string) (map[string]string, error)

var eventDomains = map[string]bool{"todos": true, "sessions": true, "usage": true, "knowledge": true, "memory": true, "collection": true, "day": true, "settings": true, "jobs": true, "presence": true}

type changeEvent struct {
	Sequence uint64
	Kind     string
}
type eventSubscriber struct {
	queue chan changeEvent
}
type eventBroker struct {
	mu       sync.Mutex
	instance string
	sequence uint64
	subs     map[*eventSubscriber]bool
	done     chan struct{}
	closed   bool
}

func newEventBroker(instance string) *eventBroker {
	return &eventBroker{instance: instance, subs: map[*eventSubscriber]bool{}, done: make(chan struct{})}
}
func (b *eventBroker) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	close(b.done)
	for s := range b.subs {
		close(s.queue)
		delete(b.subs, s)
	}
}

// publish coalesces every workspace change into a reset. The browser already
// knows which queries are visible on its route, so reproducing that dependency
// graph in Go made missed invalidations much more likely than an extra refetch.
func (b *eventBroker) publish() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.sequence++
	e := changeEvent{Sequence: b.sequence, Kind: "reset"}
	for s := range b.subs {
		select {
		case s.queue <- e:
		default:
			// A reset supersedes every earlier reset, so a pending notification is
			// sufficient even when a browser tab is temporarily slow.
		}
	}
}
func (b *eventBroker) subscribe(domains []string, last string) (*eventSubscriber, []changeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.subs) >= 32 {
		return nil, nil
	}
	s := &eventSubscriber{queue: make(chan changeEvent, 1)}
	b.subs[s] = true
	prefix := b.instance + ":"
	seq, err := strconv.ParseUint(strings.TrimPrefix(last, prefix), 10, 64)
	if !strings.HasPrefix(last, prefix) || err != nil || seq != b.sequence {
		return s, []changeEvent{{Sequence: b.sequence, Kind: "reset"}}
	}
	return s, nil
}
func (b *eventBroker) unsubscribe(s *eventSubscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[s] {
		delete(b.subs, s)
		close(s.queue)
	}
}
func (b *eventBroker) hasSubscribers() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs) > 0
}

func allEventDomains() []string {
	result := make([]string, 0, len(eventDomains))
	for d := range eventDomains {
		result = append(result, d)
	}
	sort.Strings(result)
	return result
}
func (server *Server) Invalidate(domains ...string) {
	if server.events == nil {
		return
	}
	// A successful in-process mutation is already the invalidation clients need.
	// Rebaseline its durable fingerprints before publishing so the poller does
	// not emit the same change again two seconds later. Failure is harmless: the
	// poller will conservatively publish a second event.
	if server.options.Fingerprints != nil {
		if len(domains) == 0 && server.events.hasSubscribers() {
			domains = allEventDomains()
		}
		if len(domains) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			_, _ = server.refreshFingerprintChanges(ctx, domains)
			cancel()
		}
	}
	server.events.publish()
}

func (server *Server) refreshFingerprintChanges(ctx context.Context, domains []string) ([]string, error) {
	server.fingerprintMu.Lock()
	defer server.fingerprintMu.Unlock()
	values, err := server.options.Fingerprints(ctx, domains)
	if err != nil {
		return nil, err
	}
	if server.fingerprints == nil {
		server.fingerprints = map[string]string{}
	}
	var changed []string
	for _, domain := range domains {
		value, ok := values[domain]
		if !ok {
			continue
		}
		if old, exists := server.fingerprints[domain]; !exists || old != value {
			changed = append(changed, domain)
		}
		server.fingerprints[domain] = value
	}
	return changed, nil
}

func (server *Server) pollFingerprintChanges(ctx context.Context) error {
	if server.events == nil || !server.events.hasSubscribers() {
		return nil
	}
	changed, err := server.refreshFingerprintChanges(ctx, allEventDomains())
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		// Publish directly: calling Invalidate would re-read the fingerprints
		// that this scan just established.
		server.events.publish()
	}
	return nil
}

func (server *Server) watchChanges() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-server.events.done:
			return
		case <-ticker.C:
		}
		if !server.events.hasSubscribers() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		err := server.pollFingerprintChanges(ctx)
		cancel()
		if err != nil {
			continue
		}
	}
}
func (server *Server) serveEvents(w http.ResponseWriter, r *http.Request, session browserSession) {
	if r.Method != http.MethodGet {
		server.fail(w, 405, "invalid_argument", "events requires GET")
		return
	}
	if server.events == nil {
		server.fail(w, 503, "unavailable", "live updates are unavailable")
		return
	}
	if len(r.URL.RawQuery) > 512 {
		server.fail(w, 400, "invalid_argument", "event subscription is too large")
		return
	}
	for key := range r.URL.Query() {
		if key != "domains" {
			server.fail(w, 400, "invalid_argument", "unknown event subscription field")
			return
		}
	}
	domains := strings.Split(r.URL.Query().Get("domains"), ",")
	if len(domains) > len(eventDomains) {
		server.fail(w, 400, "invalid_argument", "too many event domains")
		return
	}
	for _, d := range domains {
		if !eventDomains[d] {
			server.fail(w, 400, "invalid_argument", "unknown event domain")
			return
		}
	}
	sub, initial := server.events.subscribe(domains, r.Header.Get("Last-Event-ID"))
	if sub == nil {
		server.fail(w, 429, "busy", "too many live workspace connections")
		return
	}
	defer server.events.unsubscribe(sub)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	control := http.NewResponseController(w)
	write := func(e changeEvent) error {
		_ = control.SetWriteDeadline(time.Now().Add(5 * time.Second))
		defer control.SetWriteDeadline(time.Time{})
		data, _ := json.Marshal(struct{}{})
		if _, err := fmt.Fprintf(w, "id: %s:%d\nevent: %s\ndata: %s\n\n", server.info.InstanceID, e.Sequence, e.Kind, data); err != nil {
			return err
		}
		return control.Flush()
	}
	for _, e := range initial {
		if write(e) != nil {
			return
		}
	}
	_ = control.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprint(w, "retry: 2000\n\n"); err != nil {
		return
	}
	if control.Flush() != nil {
		return
	}
	_ = control.SetWriteDeadline(time.Time{})
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	expire := time.NewTimer(time.Until(session.expiresAt))
	defer expire.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-expire.C:
			_ = write(changeEvent{Kind: "session.expired"})
			return
		case e, ok := <-sub.queue:
			if !ok {
				return
			}
			if write(e) != nil {
				return
			}
		case <-heartbeat.C:
			_ = control.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			if control.Flush() != nil {
				return
			}
			_ = control.SetWriteDeadline(time.Time{})
		}
	}
}
