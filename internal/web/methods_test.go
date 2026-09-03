package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
