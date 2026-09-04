package web

import (
	"bufio"
	"context"
	"fmt"
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
	server, err := Start(Options{DataDir: t.TempDir(), Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, Fingerprints: func(context.Context, []string) (map[string]string, error) { return map[string]string{}, nil }})
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
	server.mu.Lock()
	server.sessions[cookie.Value] = browserSession{expires: time.Now().Add(-time.Second)}
	server.mu.Unlock()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, browserRequest(server, "GET", "/api/v1/events?domains=todos", "", cookie, ""))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status=%d", response.Code)
	}
}

func TestEventReplayIsScopedAndResetsForLostHistory(t *testing.T) {
	broker := newEventBroker("instance")
	defer broker.close()
	broker.publish([]string{"todos", "todos", "not-a-domain"})
	broker.publish([]string{"sessions"})
	broker.publish([]string{"todos", "knowledge"})
	sub, replay := broker.subscribe([]string{"todos"}, "instance:1")
	if sub == nil || len(replay) != 1 || replay[0].Sequence != 3 || replay[0].Kind != "resource.changed" {
		t.Fatalf("replay=%+v", replay)
	}
	if !reflect.DeepEqual(replay[0].Domains, []string{"knowledge", "todos"}) {
		t.Fatalf("domains=%v", replay[0].Domains)
	}
	broker.unsubscribe(sub)
	for _, last := range []string{"", "foreign:2", "instance:999", "instance:invalid"} {
		sub, initial := broker.subscribe([]string{"todos"}, last)
		if sub == nil || len(initial) != 1 || initial[0].Kind != "reset" || initial[0].Sequence != 3 {
			t.Fatalf("last=%q initial=%+v", last, initial)
		}
		broker.unsubscribe(sub)
	}
	for i := 0; i < 140; i++ {
		broker.publish([]string{"todos"})
	}
	if len(broker.history) != 128 {
		t.Fatalf("history length=%d", len(broker.history))
	}
	sub, replay = broker.subscribe([]string{"todos"}, "instance:1")
	if len(replay) != 1 || replay[0].Kind != "reset" {
		t.Fatalf("lost history replay=%+v", replay)
	}
	broker.unsubscribe(sub)
	lastRetained := broker.history[0].Sequence
	sub, replay = broker.subscribe([]string{"todos"}, fmt.Sprintf("instance:%d", lastRetained-1))
	if len(replay) != 128 || replay[0].Kind != "resource.changed" {
		t.Fatalf("complete retained replay len=%d first=%+v", len(replay), replay)
	}
	broker.unsubscribe(sub)
}

func TestEventBrokerBoundsSlowSubscribersAndClosesPromptly(t *testing.T) {
	broker := newEventBroker("bounded")
	slow, _ := broker.subscribe([]string{"todos"}, "bounded:0")
	other, _ := broker.subscribe([]string{"sessions"}, "bounded:0")
	for i := 0; i < 1000; i++ {
		broker.publish([]string{"todos"})
	}
	if len(slow.queue) != 16 {
		t.Fatalf("slow subscriber buffered %d events", len(slow.queue))
	}
	count := 0
	drained := false
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !drained {
		select {
		case _, ok := <-slow.queue:
			if !ok {
				drained = true
			} else {
				count++
			}
		case <-deadline.C:
			t.Fatal("slow subscriber was not closed after overflowing its queue")
		}
	}
	if count != 16 || len(broker.subs) != 1 {
		t.Fatalf("slow subscriber eviction: count=%d subscribers=%d", count, len(broker.subs))
	}
	if len(other.queue) != 0 {
		t.Fatalf("unrelated subscriber received %d events", len(other.queue))
	}
	for i := 0; i < 31; i++ {
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
	select {
	case _, ok := <-other.queue:
		if ok {
			t.Fatal("close did not release active subscriber")
		}
	default:
		t.Fatal("close left an idle subscriber waiting")
	}
	if sub, _ := broker.subscribe([]string{"todos"}, ""); sub != nil {
		t.Fatal("closed broker accepted subscriber")
	}
	broker.publish([]string{"todos"})
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
	if !strings.Contains(frame, "id: "+server.info.InstanceID+":3\n") || !strings.Contains(frame, "event: resource.changed\n") || !strings.Contains(frame, `"domains":["todos"]`) {
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
