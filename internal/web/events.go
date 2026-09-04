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
	Domains  []string
}
type eventSubscriber struct {
	domains map[string]bool
	queue   chan changeEvent
}
type eventBroker struct {
	mu       sync.Mutex
	instance string
	sequence uint64
	history  []changeEvent
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
func (s *eventSubscriber) matches(e changeEvent) bool {
	for _, d := range e.Domains {
		if s.domains[d] {
			return true
		}
	}
	return e.Kind == "reset"
}
func (b *eventBroker) publish(domains []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	set := map[string]bool{}
	for _, d := range domains {
		if eventDomains[d] {
			set[d] = true
		}
	}
	domains = nil
	for d := range set {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	if len(domains) == 0 {
		return
	}
	b.sequence++
	e := changeEvent{Sequence: b.sequence, Kind: "resource.changed", Domains: domains}
	b.history = append(b.history, e)
	if len(b.history) > 128 {
		b.history = append([]changeEvent(nil), b.history[len(b.history)-128:]...)
	}
	for s := range b.subs {
		if !s.matches(e) {
			continue
		}
		select {
		case s.queue <- e:
		default:
			close(s.queue)
			delete(b.subs, s)
		}
	}
}
func (b *eventBroker) subscribe(domains []string, last string) (*eventSubscriber, []changeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.subs) >= 32 {
		return nil, nil
	}
	s := &eventSubscriber{domains: map[string]bool{}, queue: make(chan changeEvent, 16)}
	for _, d := range domains {
		s.domains[d] = true
	}
	b.subs[s] = true
	prefix := b.instance + ":"
	seq, err := strconv.ParseUint(strings.TrimPrefix(last, prefix), 10, 64)
	reset := !strings.HasPrefix(last, prefix) || err != nil || seq > b.sequence
	if len(b.history) > 0 && seq+1 < b.history[0].Sequence {
		reset = true
	}
	if reset {
		return s, []changeEvent{{Sequence: b.sequence, Kind: "reset", Domains: domains}}
	}
	var initial []changeEvent
	for _, e := range b.history {
		if e.Sequence > seq && s.matches(e) {
			initial = append(initial, e)
		}
	}
	return s, initial
}
func (b *eventBroker) unsubscribe(s *eventSubscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[s] {
		delete(b.subs, s)
		close(s.queue)
	}
}
func (b *eventBroker) domains() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	set := map[string]bool{}
	for s := range b.subs {
		for d := range s.domains {
			set[d] = true
		}
	}
	var result []string
	for d := range set {
		result = append(result, d)
	}
	sort.Strings(result)
	return result
}
func (server *Server) Invalidate(domains ...string) {
	if server.events != nil {
		server.events.publish(domains)
	}
}
func (server *Server) watchChanges() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	previous := map[string]string{}
	for {
		select {
		case <-server.events.done:
			return
		case <-ticker.C:
		}
		domains := server.events.domains()
		if len(domains) == 0 {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		values, err := server.options.Fingerprints(ctx, domains)
		cancel()
		if err != nil {
			continue
		}
		var changed []string
		for _, d := range domains {
			if value, ok := values[d]; ok {
				if old, exists := previous[d]; !exists || old != value {
					changed = append(changed, d)
				}
				previous[d] = value
			}
		}
		server.Invalidate(changed...)
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
		data, _ := json.Marshal(map[string]any{"domains": e.Domains})
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
	expire := time.NewTimer(time.Until(session.expires))
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
