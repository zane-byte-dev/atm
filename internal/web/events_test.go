package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func startEventTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := Start(Options{DataDir: t.TempDir(), Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, Fingerprints: func(context.Context, []string) (map[string]string, error) { return map[string]string{}, nil }, StartRuntime: testRuntime})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server
}

func TestEventsRequireBrowserAuthenticationAndValidDomains(t *testing.T) {
	server := startEventTestServer(t)
	cookie, _ := connectBrowser(t, server)
	for _, tc := range []struct {
		name, method, path string
		cookie             *http.Cookie
		origin             string
		status             int
	}{
		{"no session", "GET", "/api/v1/events?domains=todos", nil, server.info.Origin, 401},
		{"foreign origin", "GET", "/api/v1/events?domains=todos", cookie, "https://example.com", 403},
		{"unknown domain", "GET", "/api/v1/events?domains=todos,arbitrary", cookie, server.info.Origin, 400},
		{"empty domain", "GET", "/api/v1/events?domains=", cookie, server.info.Origin, 400},
		{"unknown option", "GET", "/api/v1/events?domains=todos&path=anything", cookie, server.info.Origin, 400},
		{"too large", "GET", "/api/v1/events?domains=" + strings.Repeat("todos,", 100), cookie, server.info.Origin, 400},
		{"wrong method", "POST", "/api/v1/events?domains=todos", cookie, server.info.Origin, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := browserRequest(server, tc.method, tc.path, "", tc.cookie, "")
			request.Header.Set("Origin", tc.origin)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", response.Code, tc.status, response.Body.String())
			}
		})
	}
	expired := browserSession{csrf: strings.Repeat("A", 43), issuedAt: time.Now().Add(-2 * time.Hour), expiresAt: time.Now().Add(-time.Hour)}
	value, err := encodeBrowserSession(server.browserKey, expired)
	if err != nil {
		t.Fatal(err)
	}
	cookie.Value = value
	response := httptest.NewRecorder()
	server.ServeHTTP(response, browserRequest(server, "GET", "/api/v1/events?domains=todos", "", cookie, ""))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", response.Code)
	}
}

func TestEventBrokerCoalescesChangesAndResetsAfterDisconnect(t *testing.T) {
	broker := newEventBroker("instance")
	defer broker.close()
	broker.publish()
	broker.publish()
	broker.publish()
	for _, last := range []string{"", "foreign:2", "instance:1", "instance:999", "instance:invalid"} {
		sub, initial := broker.subscribe([]string{"todos"}, last)
		if sub == nil || len(initial) != 1 || initial[0].Kind != "reset" || initial[0].Sequence != 3 {
			t.Fatalf("last=%q initial=%+v", last, initial)
		}
		broker.unsubscribe(sub)
	}
	sub, initial := broker.subscribe([]string{"todos"}, "instance:3")
	if sub == nil || len(initial) != 0 {
		t.Fatalf("current subscriber initial=%+v", initial)
	}
	broker.publish()
	broker.publish()
	if len(sub.queue) != 1 {
		t.Fatalf("coalesced queue length=%d, want 1", len(sub.queue))
	}
	event := <-sub.queue
	if event.Kind != "reset" || event.Sequence != 4 {
		t.Fatalf("queued event=%+v", event)
	}
	broker.unsubscribe(sub)
}

func TestEventBrokerBoundsSubscribersAndClosesPromptly(t *testing.T) {
	broker := newEventBroker("bounded")
	slow, _ := broker.subscribe([]string{"todos"}, "bounded:0")
	other, _ := broker.subscribe([]string{"sessions"}, "bounded:0")
	for i := 0; i < 1000; i++ {
		broker.publish()
	}
	if len(slow.queue) != 1 {
		t.Fatalf("slow subscriber buffered %d events", len(slow.queue))
	}
	if len(other.queue) != 1 || len(broker.subs) != 2 {
		t.Fatalf("workspace reset was not broadcast: other=%d subscribers=%d", len(other.queue), len(broker.subs))
	}
	for i := 0; i < 30; i++ {
		if sub, _ := broker.subscribe([]string{"todos"}, ""); sub == nil {
			t.Fatalf("subscriber %d rejected early", i)
		}
	}
	if sub, _ := broker.subscribe([]string{"todos"}, ""); sub != nil {
		t.Fatal("unbounded subscriber count")
	}
	broker.close()
	broker.close()
	select {
	case <-broker.done:
	default:
		t.Fatal("close did not signal watcher")
	}
	for range other.queue {
		// A coalesced reset may already be buffered; closure must follow it.
	}
	if sub, _ := broker.subscribe([]string{"todos"}, ""); sub != nil {
		t.Fatal("closed broker accepted subscriber")
	}
	broker.publish()
}

func TestLocalInvalidationRebaselinesBeforePolling(t *testing.T) {
	value := "before"
	server := &Server{
		events: newEventBroker("baseline"),
		options: Options{Fingerprints: func(context.Context, []string) (map[string]string, error) {
			return map[string]string{"todos": value}, nil
		}},
	}
	defer server.events.close()
	server.Invalidate("todos")
	changed, err := server.refreshFingerprintChanges(context.Background(), []string{"todos"})
	if err != nil || len(changed) != 0 {
		t.Fatalf("unchanged poll after local invalidation = %v, %v", changed, err)
	}
	value = "external"
	changed, err = server.refreshFingerprintChanges(context.Background(), []string{"todos"})
	if err != nil || !reflect.DeepEqual(changed, []string{"todos"}) {
		t.Fatalf("external change after baseline = %v, %v", changed, err)
	}
}

func TestFingerprintPollingCoversAllDomainsForACollectionSubscriber(t *testing.T) {
	var requested []string
	server := &Server{
		events:       newEventBroker("all-domains"),
		fingerprints: map[string]string{"settings": "before"},
		options: Options{Fingerprints: func(_ context.Context, domains []string) (map[string]string, error) {
			requested = append([]string(nil), domains...)
			return map[string]string{"settings": "after"}, nil
		}},
	}
	defer server.events.close()
	sub, initial := server.events.subscribe([]string{"collection"}, "all-domains:0")
	if sub == nil || len(initial) != 0 {
		t.Fatalf("collection subscriber = %v, initial=%+v", sub, initial)
	}
	defer server.events.unsubscribe(sub)

	if err := server.pollFingerprintChanges(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requested, allEventDomains()) {
		t.Fatalf("fingerprint domains = %v, want every event domain %v", requested, allEventDomains())
	}
	select {
	case event := <-sub.queue:
		if event.Kind != "reset" || event.Sequence != 1 {
			t.Fatalf("settings change event = %+v", event)
		}
	default:
		t.Fatal("settings change did not reset a collection-only subscriber")
	}
}

func eventStream(t *testing.T, server *Server, cookie *http.Cookie, last string) (*http.Response, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	request, err := http.NewRequestWithContext(ctx, "GET", server.info.Origin+"/api/v1/events?domains=todos", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", server.info.Origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if last != "" {
		request.Header.Set("Last-Event-ID", last)
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); response.Body.Close() })
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" {
		cancel()
		t.Fatalf("stream status=%d headers=%v", response.StatusCode, response.Header)
	}
	return response, cancel
}

func readEventFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var frame strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE: %v (%s)", err, frame.String())
		}
		if line == "\n" {
			if strings.Contains(frame.String(), "event:") {
				return frame.String()
			}
			frame.Reset()
			continue
		}
		frame.WriteString(line)
	}
}

func TestEventsHTTPReplayAndServerCloseReleaseOpenStream(t *testing.T) {
	server := startEventTestServer(t)
	cookie, _ := connectBrowser(t, server)
	server.Invalidate("todos")
	server.Invalidate("sessions")
	server.Invalidate("todos")
	response, cancel := eventStream(t, server, cookie, server.info.InstanceID+":1")
	defer cancel()
	reader := bufio.NewReader(response.Body)
	frame := readEventFrame(t, reader)
	if !strings.Contains(frame, "id: "+server.info.InstanceID+":3\n") || !strings.Contains(frame, "event: reset\n") || !strings.Contains(frame, `data: {}`) {
		t.Fatalf("replayed frame=%s", frame)
	}
	started := time.Now()
	server.Close()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("close waited %s for live stream", elapsed)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("closed stream did not end cleanly: %v", err)
	}
}

func TestEventsHTTPUnknownInstanceSendsReset(t *testing.T) {
	server := startEventTestServer(t)
	cookie, _ := connectBrowser(t, server)
	server.Invalidate("todos")
	response, cancel := eventStream(t, server, cookie, "old-instance:1")
	defer cancel()
	frame := readEventFrame(t, bufio.NewReader(response.Body))
	if !strings.Contains(frame, "event: reset\n") || !strings.Contains(frame, "id: "+server.info.InstanceID+":1\n") {
		t.Fatalf("reset frame=%s", frame)
	}
}
