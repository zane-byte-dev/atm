package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestWorkspaceMethodsKeepAuthenticationAndCapabilityBoundary(t *testing.T) {
	dispatched := 0
	server := startTestServer(t, func(_ context.Context, call application.Call, _ string, _ json.RawMessage, _ string) (any, error) {
		dispatched++
		if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginWeb || call.Actor.SessionID != "" {
			t.Fatalf("unexpected browser identity: %+v", call.Actor)
		}
		return map[string]any{}, nil
	})
	cookie, csrf := connectBrowser(t, server)
	reads := []string{"session.list", "usage.snapshot", "knowledge.catalog", "memory.recall", "collect.overview", "day.snapshot", "settings.get"}
	for _, method := range reads {
		t.Run(method, func(t *testing.T) {
			for _, authenticated := range []bool{false, true} {
				req := browserRequest(server, http.MethodPost, "/api/v1/"+method, "{}", cookie, csrf)
				want := http.StatusOK
				if !authenticated {
					req.Header.Del("X-ATM-CSRF")
					want = http.StatusForbidden
				}
				response := httptest.NewRecorder()
				server.ServeHTTP(response, req)
				if response.Code != want {
					t.Fatalf("status=%d, want=%d: %s", response.Code, want, response.Body.String())
				}
			}
		})
	}
	if dispatched != len(reads) {
		t.Fatalf("unauthenticated reads reached services: calls=%d", dispatched)
	}
	writes := []string{"knowledge.document.create", "memory.create", "collect.item.read", "settings.preferences.save"}
	for _, method := range writes {
		for _, authenticated := range []bool{false, true} {
			request := browserRequest(server, http.MethodPost, "/api/v1/"+method, "{}", cookie, csrf)
			want := http.StatusOK
			if !authenticated {
				request.Header.Del("X-ATM-CSRF")
				want = http.StatusForbidden
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != want {
				t.Fatalf("%s status=%d, want=%d: %s", method, response.Code, want, response.Body.String())
			}
		}
	}
	for _, method := range []string{"session.sync", "collect.run", "day.refresh", "settings.config.save", "knowledge.document.import", "guard.approve"} {
		request := browserRequest(server, http.MethodPost, "/api/v1/"+method, "{}", cookie, csrf)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s exposed undeclared capability: %d", method, response.Code)
		}
	}
	if dispatched != len(reads)+len(writes) {
		t.Fatalf("unexpected dispatch count=%d", dispatched)
	}
}

func TestWorkspaceMethodAccess(t *testing.T) {
	tests := []struct {
		method string
		write  bool
	}{
		{"todo.list", false}, {"jobs.show", false}, {"presence.snapshot", false},
		{"session.search", false}, {"usage.snapshot", false}, {"quota.cached", false},
		{"knowledge.query", false}, {"memory.get", false}, {"collect.overview", false},
		{"day.snapshot", false}, {"settings.get", false},
		{"todo.create", true}, {"jobs.run", true}, {"knowledge.document.create", true},
		{"memory.create", true}, {"collect.source.save", true}, {"settings.business.save", true},
	}

	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			known, write := workspaceMethodAccess(test.method)
			if !known || write != test.write {
				t.Fatalf("access = known %v, write %v; want true, %v", known, write, test.write)
			}
		})
	}

	if known, write := workspaceMethodAccess("undeclared.method"); known || write {
		t.Fatalf("undeclared method access = known %v, write %v", known, write)
	}
}

func TestWorkspaceMutationPublishesOnlyAfterSuccessfulWrites(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		dispatchErr error
		wantStatus  int
		wantReset   bool
	}{
		{name: "successful write", method: "todo.update", wantStatus: http.StatusOK, wantReset: true},
		{name: "failed write", method: "todo.update", dispatchErr: application.NewError(application.CodeConflict, "stale edit"), wantStatus: http.StatusConflict},
		{name: "successful read", method: "todo.list", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startTestServer(t, func(context.Context, application.Call, string, json.RawMessage, string) (any, error) {
				return map[string]bool{"ok": true}, test.dispatchErr
			})
			server.events = newEventBroker(server.info.InstanceID)
			subscriber, initial := server.events.subscribe([]string{"todos"}, server.info.InstanceID+":0")
			if subscriber == nil || len(initial) != 0 {
				t.Fatalf("subscribe initial=%+v", initial)
			}
			defer server.events.unsubscribe(subscriber)
			cookie, csrf := connectBrowser(t, server)
			request := browserRequest(server, http.MethodPost, "/api/v1/"+test.method, "{}", cookie, csrf)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}

			if !test.wantReset {
				if len(subscriber.queue) != 0 {
					t.Fatalf("published after %s", test.name)
				}
				return
			}
			if len(subscriber.queue) != 1 || (<-subscriber.queue).Kind != "reset" {
				t.Fatal("successful mutation did not publish a workspace reset")
			}
		})
	}
}
