package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

func startTestServer(t *testing.T, dispatch Dispatch) *Server {
	t.Helper()
	server, err := Start(Options{DataDir: t.TempDir(), Version: "test", Port: 0, Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ATM</title>")}, "assets/app-abcd.js": {Data: []byte("export const ok = true;")}}, Dispatch: dispatch, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server
}

func browserRequest(server *Server, method, path, body string, cookie *http.Cookie, csrf string) *http.Request {
	r := httptest.NewRequest(method, server.info.Origin+path, strings.NewReader(body))
	r.Host = strings.TrimPrefix(server.info.Origin, "http://")
	r.Header.Set("Origin", server.info.Origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-ATM-CSRF", csrf)
	}
	return r
}

func connectBrowser(t *testing.T, server *Server) (*http.Cookie, string) {
	t.Helper()
	link, err := server.BrowserURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	values, _ := url.ParseQuery(parsed.Fragment)
	request := browserRequest(server, http.MethodPost, "/api/v1/auth/exchange", `{"ticket":"`+values.Get("ticket")+`"}`, nil, "")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("connect = %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe cookie: %+v", cookies)
	}
	var payload struct {
		Data struct {
			CSRF string `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.CSRF) < 32 {
		t.Fatal("missing CSRF token")
	}
	return cookies[0], payload.Data.CSRF
}

func TestBrowserAuthenticationAndTypedBoundary(t *testing.T) {
	var calls atomic.Int32
	server := startTestServer(t, func(ctx context.Context, call application.Call, method string, input json.RawMessage, key string) (any, error) {
		calls.Add(1)
		if call.Actor.Kind != application.ActorHuman || string(call.Actor.Origin) != "web" || call.Actor.SessionID != "" {
			t.Errorf("actor = %+v", call.Actor)
		}
		if method != "todo.list" || string(input) != "{}" {
			t.Errorf("dispatch %q %s", method, input)
		}
		return map[string]any{"items": []any{}}, nil
	})
	cookie, csrf := connectBrowser(t, server)
	for _, test := range []struct {
		name, path string
		mutate     func(*http.Request)
		code       int
	}{
		{"valid", "/api/v1/todo.list", nil, 200},
		{"no cookie", "/api/v1/todo.list", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"no csrf", "/api/v1/todo.list", func(r *http.Request) { r.Header.Del("X-ATM-CSRF") }, 403},
		{"wrong origin", "/api/v1/todo.list", func(r *http.Request) { r.Header.Set("Origin", "https://example.com") }, 403},
		{"same-site other port", "/api/v1/todo.list", func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:123") }, 403},
		{"missing origin", "/api/v1/todo.list", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"fetch cross-site", "/api/v1/todo.list", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"host rebinding", "/api/v1/todo.list", func(r *http.Request) { r.Host = "example.com" }, 403},
		{"form body", "/api/v1/todo.list", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"guard not exposed", "/api/v1/guard.approve", nil, 404},
		{"config not exposed", "/api/v1/config.save", nil, 404},
		{"arbitrary ipc not exposed", "/api/v1/_ipc", nil, 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := browserRequest(server, http.MethodPost, test.path, "{}", cookie, csrf)
			if test.mutate != nil {
				test.mutate(r)
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != test.code {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	if calls.Load() != 1 {
		t.Fatalf("rejected requests reached services: calls=%d", calls.Load())
	}
	r := browserRequest(server, http.MethodGet, "/api/v1/bootstrap", "", cookie, "")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"background_sync":false`) || !strings.Contains(w.Body.String(), `"sse":false`) {
		t.Fatalf("bootstrap overstates capabilities: %s", w.Body.String())
	}
}

func TestTicketSingleUseExpiryAndCrossOriginExchange(t *testing.T) {
	server := startTestServer(t, nil)
	link, err := server.BrowserURL()
	if err != nil {
		t.Fatal(err)
	}
	ticket := strings.Split(link, "#ticket=")[1]
	body := `{"ticket":"` + ticket + `"}`
	r := browserRequest(server, http.MethodPost, "/api/v1/auth/exchange", body, nil, "")
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-origin exchange = %d", w.Code)
	}
	for _, code := range []int{200, 401} {
		w = httptest.NewRecorder()
		server.ServeHTTP(w, browserRequest(server, http.MethodPost, "/api/v1/auth/exchange", body, nil, ""))
		if w.Code != code {
			t.Fatalf("exchange = %d, want %d", w.Code, code)
		}
	}
	server.mu.Lock()
	server.tickets["expired"] = time.Now().Add(-time.Second)
	server.mu.Unlock()
	w = httptest.NewRecorder()
	server.ServeHTTP(w, browserRequest(server, http.MethodPost, "/api/v1/auth/exchange", `{"ticket":"expired"}`, nil, ""))
	if w.Code != 401 {
		t.Fatalf("expired exchange = %d", w.Code)
	}
}

func TestOpenAnotherTabPreservesExistingBrowserSession(t *testing.T) {
	server := startTestServer(t, nil)
	cookie, csrf := connectBrowser(t, server)
	link, err := server.BrowserURL()
	if err != nil {
		t.Fatal(err)
	}
	ticket := strings.Split(link, "#ticket=")[1]
	w := httptest.NewRecorder()
	server.ServeHTTP(w, browserRequest(server, http.MethodPost, "/api/v1/auth/exchange", `{"ticket":"`+ticket+`"}`, cookie, ""))
	if w.Code != 200 || !strings.Contains(w.Body.String(), csrf) || len(w.Result().Cookies()) != 0 {
		t.Fatalf("opening another tab replaced the current session: %d %s", w.Code, w.Body.String())
	}
	server.mu.Lock()
	count := len(server.sessions)
	server.mu.Unlock()
	if count != 1 {
		t.Fatalf("sessions = %d, want 1", count)
	}
}

func TestAttachmentReadIsAuthenticatedAndAnchoredToDataDirectory(t *testing.T) {
	server := startTestServer(t, nil)
	assets := filepath.Join(server.info.DataDir, "todos", "assets", "t1")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(assets, "image.png")
	if err := os.WriteFile(image, []byte("image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := os.WriteFile(outside, []byte("private secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(assets, "link.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	server.options.Attachment = func(_ context.Context, _ application.Call, id string) (string, string, error) {
		switch id {
		case "normal":
			return image, "image/png", nil
		case "symlink":
			return link, "image/png", nil
		case "outside":
			return outside, "image/png", nil
		case "svg":
			return image, "image/svg+xml", nil
		}
		return "", "", application.NewError(application.CodeNotFound, "missing")
	}
	cookie, _ := connectBrowser(t, server)
	for _, test := range []struct {
		id     string
		cookie *http.Cookie
		status int
	}{{"normal", nil, 401}, {"normal", cookie, 200}, {"symlink", cookie, 404}, {"outside", cookie, 403}, {"svg", cookie, 415}} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, browserRequest(server, http.MethodGet, "/api/v1/attachments/"+test.id, "", test.cookie, ""))
		if w.Code != test.status {
			t.Errorf("%s = %d, want %d: %s", test.id, w.Code, test.status, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private secret") {
			t.Fatal("escaped attachment storage")
		}
	}
}

func TestInstanceControlAndExclusiveOwnership(t *testing.T) {
	server := startTestServer(t, nil)
	status, err := ReadStatus(context.Background(), server.info.DataDir)
	if err != nil || !status.Running || status.Instance.InstanceID != server.info.InstanceID {
		t.Fatalf("status %+v: %v", status, err)
	}
	if _, err := Start(server.options); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second instance: %v", err)
	}
	link, err := OpenExisting(context.Background(), server.info.DataDir)
	if err != nil || !strings.HasPrefix(link, server.info.Origin+"/#ticket=") {
		t.Fatalf("open existing %q: %v", link, err)
	}
	for name, wants := range map[string]os.FileMode{"runtime": 0o700, "runtime/server.json": 0o600, "runtime/control.token": 0o600, "runtime/server.lock": 0o600} {
		info, err := os.Stat(filepath.Join(server.info.DataDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != wants {
			t.Errorf("%s permissions %o", name, info.Mode().Perm())
		}
	}
	for _, name := range []string{"atm.db", "notch.sock"} {
		if _, err := os.Stat(filepath.Join(server.info.DataDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("workspace unexpectedly initialized %s: %v", name, err)
		}
	}
	if err := Stop(context.Background(), server.info.DataDir); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish")
	}
	status, err = ReadStatus(context.Background(), server.info.DataDir)
	if err != nil || status.Running {
		t.Fatalf("stopped status %+v: %v", status, err)
	}
	restarted, err := Start(server.options)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.info.InstanceID == server.info.InstanceID || restarted.controlToken == server.controlToken {
		t.Fatal("restart reused capabilities")
	}
}

func TestInstanceReportsRuntimeMode(t *testing.T) {
	started := false
	server, err := Start(Options{
		DataDir: t.TempDir(), Version: "test", Port: 0,
		Assets: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ATM</title>")}},
		StartRuntime: func(info Instance, _ func(...string)) (func(context.Context) error, error) {
			started = true
			if info.Mode != "go" {
				t.Fatalf("runtime received mode %q", info.Mode)
			}
			return func(context.Context) error { return nil }, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	if !started || server.Info().Mode != "go" {
		t.Fatalf("runtime mode = %q, started=%v", server.Info().Mode, started)
	}
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, server.info.Origin+"/healthz", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mode":"go"`) {
		t.Fatalf("health mode = %d %s", w.Code, w.Body.String())
	}
	cookie, _ := connectBrowser(t, server)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, browserRequest(server, http.MethodGet, "/api/v1/bootstrap", "", cookie, ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"mode":"go"`) {
		t.Fatalf("bootstrap mode = %d %s", w.Code, w.Body.String())
	}
}

func TestControlRequiresCapabilityAndInstanceIdentity(t *testing.T) {
	server := startTestServer(t, nil)
	for _, test := range []struct{ token, instance, origin string }{{"", server.info.InstanceID, ""}, {server.controlToken, "wrong", ""}, {server.controlToken, server.info.InstanceID, server.info.Origin}} {
		r := httptest.NewRequest(http.MethodPost, server.info.Origin+"/api/v1/control/stop", strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer "+test.token)
		r.Header.Set(instanceHeader, test.instance)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("invalid control request = %d", w.Code)
		}
	}
	select {
	case <-server.done:
		t.Fatal("unauthorized request stopped server")
	default:
	}
}

func TestMaintenanceAndWorkspaceShareExclusiveLock(t *testing.T) {
	server := startTestServer(t, nil)
	called := false
	err := WithStoppedInstance(server.Info().DataDir, func() error { called = true; return nil })
	if !errors.Is(err, ErrAlreadyRunning) || called {
		t.Fatalf("maintenance overlapped workspace: called=%v err=%v", called, err)
	}
	server.Close()
	if err := WithStoppedInstance(server.Info().DataDir, func() error {
		started, err := Start(server.options)
		if started != nil {
			started.Close()
		}
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("workspace started during maintenance: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	restarted, err := Start(server.options)
	if err != nil {
		t.Fatalf("maintenance failed to release lock: %v", err)
	}
	restarted.Close()
}

func TestJSONLimitsErrorsAndStaticFallback(t *testing.T) {
	server := startTestServer(t, func(context.Context, application.Call, string, json.RawMessage, string) (any, error) {
		return nil, application.NewError(application.CodeConflict, "edited elsewhere")
	})
	cookie, csrf := connectBrowser(t, server)
	for _, test := range []struct {
		body string
		code int
	}{{"{", 400}, {"null", 400}, {"[]", 400}, {"{} {}", 400}, {`{"value":"` + strings.Repeat("x", MaxRequestBytes) + `"}`, 413}, {"{}", 409}} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, browserRequest(server, http.MethodPost, "/api/v1/todo.update", test.body, cookie, csrf))
		if w.Code != test.code {
			t.Errorf("body length %d: got %d want %d", len(test.body), w.Code, test.code)
		}
	}
	for _, test := range []struct {
		path string
		code int
		html bool
	}{{"/tasks/t123", 200, true}, {"/assets/missing.js", 404, false}, {"/api/v1/nope", 401, false}, {"/assets/app-abcd.js", 200, false}} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, server.info.Origin+test.path, nil))
		if w.Code != test.code {
			t.Errorf("%s: %d", test.path, w.Code)
		}
		if bytes.Contains(w.Body.Bytes(), []byte("<!doctype html>")) != test.html {
			t.Errorf("wrong fallback for %s: %s", test.path, w.Body.String())
		}
	}
	response, err := http.Get(server.info.Origin + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || strings.Contains(string(body), server.info.DataDir) {
		t.Fatalf("health leaks data: %s", body)
	}
}

func TestInstanceRecordCannotRedirectControlCredential(t *testing.T) {
	dataDir := t.TempDir()
	runtimeDir := filepath.Join(dataDir, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://example.com", "http://127.0.0.1:80/path", "http://localhost:80", "http://user:secret@127.0.0.1:80", "http://127.0.0.1:80?x=1"} {
		if err := writePrivateJSON(filepath.Join(runtimeDir, "server.json"), Instance{SchemaVersion: 1, InstanceID: "test", Origin: origin, DataDir: dataDir}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readInstance(dataDir); err == nil {
			t.Errorf("accepted origin %s", origin)
		}
	}
}
