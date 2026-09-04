package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestOtherWorkspacesKeepAuthenticationAndUpgradeBoundary(t *testing.T) {
	dispatched := 0
	server := startTestServer(t, func(_ context.Context, call application.Call, _ string, _ json.RawMessage, _ string) (any, error) {
		dispatched++
		if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginWeb || call.Actor.SessionID != "" {
			t.Fatalf("unexpected browser identity: %+v", call.Actor)
		}
		return map[string]any{}, nil
	})
	server.options.AllowWrites = false
	server.options.DataUpgradeRequired = true
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
	for _, method := range []string{"knowledge.document.create", "knowledge.collection.create", "memory.create", "memory.supersede", "collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted", "settings.preferences.save"} {
		request := browserRequest(server, http.MethodPost, "/api/v1/"+method, "{}", cookie, csrf)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s passed upgrade boundary: %d %s", method, response.Code, response.Body.String())
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
	if dispatched != len(reads) {
		t.Fatalf("blocked operations reached services: calls=%d", dispatched)
	}
}

func TestWorkspaceMethodWriteDomains(t *testing.T) {
	tests := []struct {
		method  string
		write   bool
		domains []string
	}{
		{"todo.list", false, nil},
		{"todo.show", false, nil},
		{"todo.doc", false, nil},
		{"todo.dependency.list", false, nil},
		{"jobs.list", false, nil},
		{"jobs.show", false, nil},
		{"presence.snapshot", false, nil},
		{"session.list", false, nil},
		{"session.search", false, nil},
		{"session.show", false, nil},
		{"session.status", false, nil},
		{"usage.snapshot", false, nil},
		{"quota.cached", false, nil},
		{"knowledge.catalog", false, nil},
		{"knowledge.query", false, nil},
		{"knowledge.document.get", false, nil},
		{"memory.recall", false, nil},
		{"memory.get", false, nil},
		{"collect.overview", false, nil},
		{"collect.items", false, nil},
		{"collect.item.show", false, nil},
		{"collect.history", false, nil},
		{"day.snapshot", false, nil},
		{"day.show", false, nil},
		{"day.ledger", false, nil},
		{"settings.get", false, nil},

		{"todo.create", true, []string{"todos"}},
		{"todo.update", true, []string{"todos"}},
		{"todo.start", true, []string{"todos"}},
		{"todo.done", true, []string{"todos"}},
		{"todo.archive", true, []string{"todos"}},
		{"todo.restore", true, []string{"todos"}},
		{"todo.plan.set", true, []string{"todos"}},
		{"todo.progress.append", true, []string{"todos"}},
		{"todo.dependency.add", true, []string{"todos"}},
		{"todo.dependency.remove", true, []string{"todos"}},
		{"todo.wait.update", true, []string{"todos"}},
		{"todo.wake", true, []string{"todos"}},
		{"jobs.run", true, []string{"jobs"}},
		{"jobs.cancel", true, []string{"jobs"}},
		{"knowledge.document.create", true, []string{"knowledge"}},
		{"knowledge.document.update", true, []string{"knowledge"}},
		{"knowledge.collection.create", true, []string{"knowledge"}},
		{"memory.create", true, []string{"memory"}},
		{"memory.supersede", true, []string{"memory"}},
		{"collect.item.read", true, []string{"collection"}},
		{"collect.item.archive", true, []string{"collection"}},
		{"collect.source.enabled", true, []string{"collection"}},
		{"collect.source.muted", true, []string{"collection"}},
		{"collect.source.save", true, []string{"collection"}},
		{"collect.source.delete", true, []string{"collection"}},
		{"settings.preferences.save", true, []string{"settings"}},
		{"settings.business.save", true, []string{"settings"}},
		{"settings.credential.save", true, []string{"settings"}},
		{"settings.credential.delete", true, []string{"settings"}},
	}

	for _, test := range tests {
		t.Run(test.method, func(t *testing.T) {
			known, write := workspaceMethodAccess(test.method)
			if !known || write != test.write {
				t.Fatalf("access = known %v, write %v; want true, %v", known, write, test.write)
			}
			if got := workspaceWriteDomains(test.method); !reflect.DeepEqual(got, test.domains) {
				t.Fatalf("domains = %v, want %v", got, test.domains)
			}
		})
	}

	if known, write := workspaceMethodAccess("undeclared.method"); known || write {
		t.Fatalf("undeclared method access = known %v, write %v", known, write)
	}
	if domains := workspaceWriteDomains("undeclared.method"); domains != nil {
		t.Fatalf("undeclared method domains = %v, want nil", domains)
	}
}

func TestWorkspaceMutationPublishesOnlyAfterSuccessfulWrites(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		dispatchErr error
		wantStatus  int
		wantDomains []string
	}{
		{name: "successful write", method: "todo.update", wantStatus: http.StatusOK, wantDomains: []string{"todos"}},
		{name: "failed write", method: "todo.update", dispatchErr: application.NewError(application.CodeConflict, "stale edit"), wantStatus: http.StatusConflict},
		{name: "successful read", method: "todo.list", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startTestServer(t, func(context.Context, application.Call, string, json.RawMessage, string) (any, error) {
				return map[string]bool{"ok": true}, test.dispatchErr
			})
			server.events = newEventBroker(server.info.InstanceID)
			cookie, csrf := connectBrowser(t, server)
			request := browserRequest(server, http.MethodPost, "/api/v1/"+test.method, "{}", cookie, csrf)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}

			server.events.mu.Lock()
			history := append([]changeEvent(nil), server.events.history...)
			server.events.mu.Unlock()
			if len(test.wantDomains) == 0 {
				if len(history) != 0 {
					t.Fatalf("published after %s: %+v", test.name, history)
				}
				return
			}
			if len(history) != 1 || history[0].Kind != "resource.changed" || !reflect.DeepEqual(history[0].Domains, test.wantDomains) {
				t.Fatalf("published = %+v, want one event for %v", history, test.wantDomains)
			}
		})
	}
}
