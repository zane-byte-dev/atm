package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

func nativeControlRequest(server *Server, method, path, body string) *http.Request {
	request := httptest.NewRequest(method, server.info.Origin+path, strings.NewReader(body))
	request.Host = strings.TrimPrefix(server.info.Origin, "http://")
	request.Header.Set("Authorization", "Bearer "+server.controlToken)
	request.Header.Set(instanceHeader, server.info.InstanceID)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestNativeControlRoutesDeriveCapabilityActor(t *testing.T) {
	server := startTestServer(t, nil)
	if server.http.WriteTimeout != 30*time.Second {
		t.Fatalf("HTTP write timeout = %s, want bounded non-Guard response budget", server.http.WriteTimeout)
	}
	methods := make(chan string, 1)
	server.options.NativeControl = func(_ context.Context, call application.Call, method string, raw json.RawMessage) (any, error) {
		if call.RequestID == "" || call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginNativeControl ||
			call.Actor.SessionID != "" || call.Actor.BindingID != 0 || call.Actor.Agent != "" {
			t.Errorf("untrusted native actor: %+v", call)
		}
		if string(raw) != `{"fixture":true}` {
			t.Errorf("body = %s", raw)
		}
		methods <- method
		return map[string]bool{"ok": true}, nil
	}

	routes := []struct{ path, method string }{{"/api/v1/control/session/sync", "session.sync"}}
	for _, route := range routes {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, nativeControlRequest(server, http.MethodPost, route.path, `{"fixture":true}`))
		if response.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", route.path, response.Code, response.Body.String())
		}
		if got := <-methods; got != route.method {
			t.Fatalf("%s dispatched %q, want %q", route.path, got, route.method)
		}
	}
}

func TestNativeControlRejectsEveryAmbientBrowserCredential(t *testing.T) {
	var calls atomic.Int32
	server := startTestServer(t, nil)
	server.options.NativeControl = func(context.Context, application.Call, string, json.RawMessage) (any, error) {
		calls.Add(1)
		return nil, nil
	}
	cookie, csrf := connectBrowser(t, server)

	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"missing bearer", func(request *http.Request) { request.Header.Del("Authorization") }},
		{"wrong bearer", func(request *http.Request) { request.Header.Set("Authorization", "Bearer wrong") }},
		{"missing instance", func(request *http.Request) { request.Header.Del(instanceHeader) }},
		{"wrong instance", func(request *http.Request) { request.Header.Set(instanceHeader, "other") }},
		{"same origin", func(request *http.Request) { request.Header.Set("Origin", server.info.Origin) }},
		{"foreign origin", func(request *http.Request) { request.Header.Set("Origin", "https://example.com") }},
		{"browser cookie and csrf", func(request *http.Request) {
			request.Header.Del("Authorization")
			request.Header.Del(instanceHeader)
			request.Header.Set("Origin", server.info.Origin)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			request.Header.Set("X-ATM-CSRF", csrf)
			request.AddCookie(cookie)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := nativeControlRequest(server, http.MethodPost, "/api/v1/control/session/sync", `{}`)
			test.mutate(request)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected control calls reached callback: %d", calls.Load())
	}
}

func TestNativeControlPathAndMethodWhitelist(t *testing.T) {
	var calls atomic.Int32
	server := startTestServer(t, nil)
	server.options.NativeControl = func(context.Context, application.Call, string, json.RawMessage) (any, error) {
		calls.Add(1)
		return struct{}{}, nil
	}

	for _, test := range []struct {
		method, path, body string
		code               int
	}{
		{http.MethodGet, "/api/v1/control/session/sync", "", http.StatusMethodNotAllowed},
		{http.MethodPut, "/api/v1/control/session/sync", `{}`, http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/v1/control/guard/pending", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/control/guard/detail", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/control/guard/decision", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/control/guard/approve", `{}`, http.StatusNotFound},
		{http.MethodPost, "/api/v1/control/guard/decision/extra", `{}`, http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, nativeControlRequest(server, test.method, test.path, test.body))
		if response.Code != test.code {
			t.Fatalf("%s %s = %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("non-whitelisted control paths reached callback: %d", calls.Load())
	}

	// A normal browser request still has no Guard method, even when the native
	// callback is configured in the same process.
	cookie, csrf := connectBrowser(t, server)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, browserRequest(server, http.MethodPost, "/api/v1/guard.decision", `{}`, cookie, csrf))
	if response.Code != http.StatusNotFound || calls.Load() != 0 {
		t.Fatalf("browser Guard surface = %d calls=%d", response.Code, calls.Load())
	}
}

func TestNativeControlRespectsReadOnlyWorkspaceGate(t *testing.T) {
	var calls atomic.Int32
	server := startTestServer(t, nil)
	server.options.AllowWrites = false
	server.options.DataUpgradeRequired = true
	server.options.NativeControl = func(context.Context, application.Call, string, json.RawMessage) (any, error) {
		calls.Add(1)
		return struct{}{}, nil
	}
	for _, path := range []string{"/api/v1/control/session/sync"} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, nativeControlRequest(server, http.MethodPost, path, `{}`))
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "serve migrate") {
			t.Fatalf("read-only %s = %d %s", path, response.Code, response.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("read-only native writes reached callback: %d", calls.Load())
	}
}
