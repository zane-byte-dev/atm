// Package web serves the local browser workspace. It deliberately owns no
// Agent hook socket, model execution, collection timer, or background sync.
package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

const MaxRequestBytes = 2 * 1024 * 1024

type Dispatch func(context.Context, application.Call, string, json.RawMessage, string) (any, error)
type Attachment func(context.Context, application.Call, string) (string, string, error)
type NativeControl func(context.Context, application.Call, string, json.RawMessage) (any, error)

type Options struct {
	DataDir       string
	Version       string
	Port          int
	Assets        fs.FS
	DevUI         string
	Dispatch      Dispatch
	Attachment    Attachment
	Fingerprints  Fingerprints
	Upload        Upload
	Capabilities  func() map[string]bool
	Companion     func(context.Context, json.RawMessage, bool) (any, error)
	NativeControl NativeControl
	// BeforeUnlock stops background writers before the instance lock is released.
	BeforeUnlock func(context.Context) error
	// StartRuntime is called under the instance lock, before accepting HTTP.
	StartRuntime func(Instance, func(...string)) (func(context.Context) error, error)
	// AllowWrites enables only the explicit mutation methods in methods.go.
	AllowWrites         bool
	DataUpgradeRequired bool
}

type browserSession struct {
	csrf    string
	expires time.Time
}

type Server struct {
	info         Instance
	options      Options
	http         *http.Server
	listener     net.Listener
	lock         *os.File
	controlToken string
	cookieName   string
	mu           sync.Mutex
	tickets      map[string]time.Time
	sessions     map[string]browserSession
	done         chan struct{}
	closeOnce    sync.Once
	serveErr     error
	events       *eventBroker
	devUI        http.Handler
}

func Start(options Options) (*Server, error) {
	if options.Port < 0 || options.Port > 65535 {
		return nil, errors.New("port must be between 0 and 65535")
	}
	var devUI http.Handler
	if options.DevUI != "" {
		var err error
		devUI, err = newDevProxy(options.DevUI)
		if err != nil {
			return nil, err
		}
	}
	if options.Assets == nil && devUI == nil {
		return nil, errors.New("this ATM build has no Web assets; install a full release or run make build")
	}
	if devUI == nil {
		if _, err := fs.Stat(options.Assets, "index.html"); err != nil {
			return nil, fmt.Errorf("ATM Web assets are missing index.html: %w", err)
		}
	}
	dataDir, err := canonicalDataDir(options.DataDir, true)
	if err != nil {
		return nil, err
	}
	lock, err := openInstanceLock(dataDir)
	if err != nil {
		return nil, err
	}
	runtimeDir := filepath.Join(dataDir, "runtime")
	if options.DataUpgradeRequired {
		options.AllowWrites = false
	}
	fail := func(err error) (*Server, error) { unlockInstance(lock); lock.Close(); return nil, err }
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", options.Port))
	if err != nil {
		return fail(fmt.Errorf("listen on local ATM port: %w", err))
	}
	instanceID, err := randomToken()
	if err != nil {
		listener.Close()
		return fail(err)
	}
	controlToken, err := randomToken()
	if err != nil {
		listener.Close()
		return fail(err)
	}
	mode := "workspace"
	if options.StartRuntime != nil {
		mode = "go"
	}
	server := &Server{
		devUI:   devUI,
		info:    Instance{SchemaVersion: 1, PID: os.Getpid(), InstanceID: instanceID, Origin: "http://" + listener.Addr().String(), Version: options.Version, DataDir: dataDir, Mode: mode, StartedAt: time.Now().UTC().Format(time.RFC3339)},
		options: options, listener: listener, lock: lock, controlToken: controlToken,
		cookieName: "atm_session_" + instanceID[:12], tickets: map[string]time.Time{}, sessions: map[string]browserSession{}, done: make(chan struct{}),
	}
	server.http = &http.Server{Handler: server, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	if options.Fingerprints != nil {
		server.events = newEventBroker(instanceID)
	}
	if err := writePrivateFile(filepath.Join(runtimeDir, "control.token"), []byte(controlToken+"\n")); err != nil {
		listener.Close()
		return fail(err)
	}
	if err := writePrivateJSON(filepath.Join(runtimeDir, "server.json"), server.info); err != nil {
		listener.Close()
		os.Remove(filepath.Join(runtimeDir, "control.token"))
		return fail(err)
	}
	if options.StartRuntime != nil {
		cleanup, err := options.StartRuntime(server.info, server.Invalidate)
		if err != nil {
			if server.events != nil {
				server.events.close()
			}
			listener.Close()
			os.Remove(filepath.Join(runtimeDir, "control.token"))
			os.Remove(filepath.Join(runtimeDir, "server.json"))
			return fail(err)
		}
		server.options.BeforeUnlock = cleanup
	}
	if server.events != nil {
		go server.watchChanges()
	}
	go func() {
		err := server.http.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			server.mu.Lock()
			server.serveErr = err
			server.mu.Unlock()
			server.Close()
		}
	}()
	return server, nil
}

func (server *Server) Info() Instance { return server.info }

func (server *Server) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		server.Close()
	case <-server.done:
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.serveErr
}

func (server *Server) Close() {
	server.closeOnce.Do(func() {
		if server.events != nil {
			server.events.close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		drainErr := server.http.Shutdown(ctx)
		if drainErr != nil {
			_ = server.http.Close()
		}
		if server.options.BeforeUnlock != nil {
			runtimeCtx, runtimeCancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer runtimeCancel()
			if err := server.options.BeforeUnlock(runtimeCtx); err != nil {
				server.mu.Lock()
				server.serveErr = err
				server.mu.Unlock()
				// The executable exits on this error. Until the OS has terminated
				// every worker, retain the flock and metadata: a timeout is not
				// proof that the old process has stopped writing.
				close(server.done)
				return
			}
		}
		if drainErr != nil {
			// Closing a connection does not wait for an in-flight mutation to
			// finish its filesystem projection. Keep ownership until process
			// exit when the graceful HTTP drain budget was exhausted.
			server.mu.Lock()
			server.serveErr = drainErr
			server.mu.Unlock()
			close(server.done)
			return
		}
		// We hold the instance lock until request handlers finish. Never unlink
		// server.lock: another process may already be waiting on that inode.
		runtimeDir := filepath.Join(server.info.DataDir, "runtime")
		content, err := os.ReadFile(filepath.Join(runtimeDir, "server.json"))
		var current Instance
		if err == nil && json.Unmarshal(content, &current) == nil && current.InstanceID == server.info.InstanceID {
			_ = os.Remove(filepath.Join(runtimeDir, "server.json"))
			_ = os.Remove(filepath.Join(runtimeDir, "control.token"))
		}
		unlockInstance(server.lock)
		_ = server.lock.Close()
		close(server.done)
	})
}

func (server *Server) BrowserURL() (string, error) {
	ticket, err := randomToken()
	if err != nil {
		return "", err
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	server.pruneAuthLocked()
	if len(server.tickets) >= 32 {
		return "", application.NewError(application.CodeBusy, "too many pending browser connections; try again in a minute")
	}
	server.tickets[ticket] = time.Now().Add(time.Minute)
	return server.info.Origin + "/#ticket=" + ticket, nil
}

func (server *Server) pruneAuthLocked() {
	now := time.Now()
	for key, expires := range server.tickets {
		if !now.Before(expires) {
			delete(server.tickets, key)
		}
	}
	for key, session := range server.sessions {
		if !now.Before(session.expires) {
			delete(server.sessions, key)
		}
	}
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	if server.devUI != nil {
		w.Header().Set("Content-Security-Policy", devContentSecurityPolicy(server.info.Origin))
	}
	if r.Host != strings.TrimPrefix(server.info.Origin, "http://") {
		server.fail(w, 403, "forbidden", "unrecognized ATM host")
		return
	}
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			server.fail(w, 405, "invalid_argument", "method not allowed")
			return
		}
		server.respond(w, 200, map[string]any{"status": "ok", "mode": server.info.Mode, "version": server.info.Version})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
		server.serveAPI(w, r)
		return
	}
	server.serveAsset(w, r)
}

func (server *Server) browserOriginAllowed(r *http.Request, require bool) bool {
	origin := r.Header.Get("Origin")
	if origin != "" && origin != server.info.Origin {
		return false
	}
	if require && origin == "" {
		return false
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	return true
}

func (server *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/v1/control/") {
		server.serveControl(w, r)
		return
	}
	if r.URL.Path == "/api/v1/auth/exchange" {
		server.exchange(w, r)
		return
	}
	if !server.browserOriginAllowed(r, false) {
		server.fail(w, 403, "forbidden", "cross-origin access is not allowed")
		return
	}
	cookie, err := r.Cookie(server.cookieName)
	if err != nil {
		server.fail(w, 401, "unauthorized", "connect with atm serve --open")
		return
	}
	server.mu.Lock()
	server.pruneAuthLocked()
	session, ok := server.sessions[cookie.Value]
	server.mu.Unlock()
	if !ok {
		server.fail(w, 401, "unauthorized", "browser connection expired; run atm serve --open")
		return
	}
	if r.URL.Path == "/api/v1/bootstrap" && r.Method == http.MethodGet {
		capabilities := map[string]any{
			"todo_write": server.options.AllowWrites, "workspace_write": server.options.AllowWrites,
			"data_upgrade_required": server.options.DataUpgradeRequired,
			"workspaces":            []string{"tasks", "collection", "agents", "knowledge", "usage", "ai-day", "settings"},
			"sse":                   server.events != nil, "poll_interval_ms": 5000, "background_sync": false, "agent_hooks": false,
			"collection_run": false, "models": false, "runtime_jobs": false, "attachments_upload": server.options.AllowWrites && server.options.Upload != nil,
		}
		if server.options.Capabilities != nil {
			runtimeCapabilities := server.options.Capabilities()
			for _, name := range []string{"background_sync", "agent_hooks", "collection_run", "models", "runtime_jobs"} {
				capabilities[name] = server.options.AllowWrites && runtimeCapabilities[name]
			}
		}
		server.respond(w, 200, map[string]any{"version": server.info.Version, "instance_id": server.info.InstanceID, "mode": server.info.Mode, "csrf_token": session.csrf, "capabilities": capabilities})
		return
	}
	call := application.Call{RequestID: ipc.NewRequestID(), Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginWeb}}
	if r.URL.Path == "/api/v1/events" {
		server.serveEvents(w, r, session)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/attachments/") && r.Method == http.MethodGet {
		server.serveAttachment(w, r, call)
		return
	}
	if r.Method != http.MethodPost {
		server.fail(w, 405, "invalid_argument", "method not allowed")
		return
	}
	if !server.browserOriginAllowed(r, true) || !sameSecret(r.Header.Get("X-ATM-CSRF"), session.csrf) {
		server.fail(w, 403, "forbidden", "invalid browser request origin or CSRF token")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") && strings.HasSuffix(r.URL.Path, "/images") {
		server.serveImageUpload(w, r, call)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	known, write := workspaceMethodAccess(method)
	if !known {
		server.fail(w, 404, "not_found", "unknown workspace method")
		return
	}
	if write && !server.options.AllowWrites {
		message := "this workspace is read-only"
		if server.options.DataUpgradeRequired {
			message += "; stop the workspace and run atm serve migrate to back up and upgrade its database"
		}
		server.fail(w, 403, "forbidden", message)
		return
	}
	body, ok := readJSONBody(w, r)
	if !ok {
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if len(idempotencyKey) > 128 {
		server.fail(w, 400, "invalid_argument", "Idempotency-Key is too long")
		return
	}
	if server.options.Dispatch == nil {
		server.fail(w, 503, "unavailable", "workspace services are unavailable")
		return
	}
	data, err := server.options.Dispatch(r.Context(), call, method, body, idempotencyKey)
	if err != nil {
		server.applicationError(w, call.RequestID, err)
		return
	}
	if write {
		server.Invalidate(workspaceWriteDomains(method)...)
	}
	server.respondID(w, 200, call.RequestID, data, nil)
}

func (server *Server) exchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		server.fail(w, 405, "invalid_argument", "method not allowed")
		return
	}
	if !server.browserOriginAllowed(r, true) {
		server.fail(w, 403, "forbidden", "browser origin is required")
		return
	}
	body, ok := readJSONBody(w, r)
	if !ok {
		return
	}
	var input struct {
		Ticket string `json:"ticket"`
	}
	if err := decodeStrict(body, &input); err != nil {
		server.fail(w, 400, "invalid_argument", "invalid connection ticket request")
		return
	}
	token, err := randomToken()
	if err != nil {
		server.fail(w, 500, "internal", "cannot create browser connection")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		server.fail(w, 500, "internal", "cannot create browser connection")
		return
	}
	server.mu.Lock()
	server.pruneAuthLocked()
	expires, ok := server.tickets[input.Ticket]
	if !ok || !time.Now().Before(expires) {
		server.mu.Unlock()
		server.fail(w, 401, "unauthorized", "connection ticket is expired or already used")
		return
	}
	delete(server.tickets, input.Ticket)
	// Opening another tab must not replace the browser-wide cookie and break
	// the CSRF token held by tabs which are already editing a task.
	if existing, cookieErr := r.Cookie(server.cookieName); cookieErr == nil {
		if previous, exists := server.sessions[existing.Value]; exists {
			server.mu.Unlock()
			server.respond(w, 200, map[string]string{"csrf_token": previous.csrf})
			return
		}
	}
	if len(server.sessions) >= 32 {
		server.mu.Unlock()
		server.fail(w, 429, "busy", "too many browser sessions; restart the workspace to reconnect")
		return
	}
	server.sessions[token] = browserSession{csrf: csrf, expires: time.Now().Add(12 * time.Hour)}
	server.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: server.cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 60 * 60})
	server.respond(w, 200, map[string]string{"csrf_token": csrf})
}

func (server *Server) serveControl(w http.ResponseWriter, r *http.Request) {
	if !sameSecret(r.Header.Get("Authorization"), "Bearer "+server.controlToken) || r.Header.Get(instanceHeader) != server.info.InstanceID || r.Header.Get("Origin") != "" {
		server.fail(w, 403, "forbidden", "invalid local control capability")
		return
	}
	nativeMethod, nativeRoute := nativeControlMethod(r.URL.Path)
	if nativeRoute && r.Method != http.MethodPost {
		server.fail(w, 405, "invalid_argument", "method not allowed")
		return
	}
	if r.Method == http.MethodPost {
		body, ok := readJSONBody(w, r)
		if !ok {
			return
		}
		if r.URL.Path == "/api/v1/control/companion" || r.URL.Path == "/api/v1/control/companion/ack" {
			if server.options.Companion == nil {
				server.fail(w, 503, "unavailable", "native companion runtime is unavailable")
				return
			}
			data, err := server.options.Companion(r.Context(), body, strings.HasSuffix(r.URL.Path, "/ack"))
			if err != nil {
				server.applicationError(w, ipc.NewRequestID(), err)
				return
			}
			server.respond(w, 200, data)
			return
		}
		if nativeRoute {
			if server.options.NativeControl == nil {
				server.fail(w, 503, "unavailable", "native control runtime is unavailable")
				return
			}
			if nativeControlWrites(nativeMethod) && !server.options.AllowWrites {
				message := "this workspace is read-only"
				if server.options.DataUpgradeRequired {
					message += "; stop the workspace and run atm serve migrate to back up and upgrade its database"
				}
				server.fail(w, 403, "forbidden", message)
				return
			}
			requestID := ipc.NewRequestID()
			call := application.Call{RequestID: requestID, Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginNativeControl}}
			data, err := server.options.NativeControl(r.Context(), call, nativeMethod, body)
			if err != nil {
				server.applicationError(w, requestID, err)
				return
			}
			server.respondID(w, 200, requestID, data, nil)
			return
		}
		if err := decodeStrict(body, &struct{}{}); err != nil {
			server.fail(w, 400, "invalid_argument", "control request must be an empty JSON object")
			return
		}
	}
	switch r.URL.Path {
	case "/api/v1/control/status":
		if r.Method != http.MethodGet {
			server.fail(w, 405, "invalid_argument", "method not allowed")
			return
		}
		server.respond(w, 200, server.info)
	case "/api/v1/control/open":
		if r.Method != http.MethodPost {
			server.fail(w, 405, "invalid_argument", "method not allowed")
			return
		}
		url, err := server.BrowserURL()
		if err != nil {
			server.applicationError(w, ipc.NewRequestID(), err)
			return
		}
		server.respond(w, 200, map[string]string{"url": url})
	case "/api/v1/control/stop":
		if r.Method != http.MethodPost {
			server.fail(w, 405, "invalid_argument", "method not allowed")
			return
		}
		server.respond(w, 200, map[string]bool{"stopping": true})
		go server.Close()
	default:
		server.fail(w, 404, "not_found", "unknown control method")
	}
}

func nativeControlMethod(path string) (string, bool) {
	switch path {
	case "/api/v1/control/session/sync":
		return "session.sync", true
	default:
		return "", false
	}
}

func nativeControlWrites(method string) bool {
	return method == "session.sync"
}

func readJSONBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	typeName, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typeName != "application/json" {
		writeFailure(w, 415, "invalid_argument", "Content-Type must be application/json")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxRequestBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeFailure(w, 413, "invalid_argument", "request body is too large")
		} else {
			writeFailure(w, 400, "invalid_argument", "cannot read request body")
		}
		return nil, false
	}
	if !json.Valid(body) || len(body) == 0 || strings.TrimSpace(string(body))[0] != '{' {
		writeFailure(w, 400, "invalid_argument", "request body must be a JSON object")
		return nil, false
	}
	return body, true
}

func decodeStrict(body []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func sameSecret(a, b string) bool {
	return a != "" && len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (server *Server) serveAttachment(w http.ResponseWriter, r *http.Request, call application.Call) {
	if server.options.Attachment == nil {
		server.fail(w, 404, "not_found", "attachment not found")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/attachments/")
	if id == "" || strings.Contains(id, "/") {
		server.fail(w, 404, "not_found", "attachment not found")
		return
	}
	filePath, mediaType, err := server.options.Attachment(r.Context(), call, id)
	if err != nil {
		server.applicationError(w, call.RequestID, err)
		return
	}
	allowedMedia := map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true, "image/heic": true}
	if !allowedMedia[mediaType] {
		server.fail(w, 415, "invalid_argument", "unsupported attachment type")
		return
	}
	// Open relative to an anchored directory descriptor. Unlike checking a
	// realpath and later ServeFile(path), Root.Open also rejects a symlink
	// replacement that tries to escape between the check and the actual open.
	relative, err := filepath.Rel(server.info.DataDir, filePath)
	if err != nil || !strings.HasPrefix(filepath.ToSlash(relative), "todos/assets/") {
		server.fail(w, 403, "forbidden", "attachment is outside managed storage")
		return
	}
	root, err := os.OpenRoot(server.info.DataDir)
	if err != nil {
		server.fail(w, 404, "not_found", "attachment not found")
		return
	}
	defer root.Close()
	file, err := root.Open(relative)
	if err != nil {
		server.fail(w, 404, "not_found", "attachment not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		server.fail(w, 404, "not_found", "attachment not found")
		return
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (server *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	if server.devUI != nil {
		server.devUI.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		server.fail(w, 405, "invalid_argument", "method not allowed")
		return
	}
	asset := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if asset == "" {
		asset = "index.html"
	}
	content, err := fs.ReadFile(server.options.Assets, asset)
	if err != nil {
		if strings.HasPrefix(asset, "assets/") || path.Ext(asset) != "" {
			http.NotFound(w, r)
			return
		}
		asset = "index.html"
		content, err = fs.ReadFile(server.options.Assets, asset)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if strings.HasPrefix(asset, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if contentType := mime.TypeByExtension(path.Ext(asset)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(200)
	if r.Method != http.MethodHead {
		_, _ = w.Write(content)
	}
}

func (server *Server) applicationError(w http.ResponseWriter, id string, err error) {
	var appErr *application.Error
	if !errors.As(err, &appErr) {
		server.respondID(w, 500, id, nil, application.NewError(application.CodeInternal, "workspace operation failed"))
		return
	}
	status := map[application.ErrorCode]int{application.CodeInvalidArgument: 400, application.CodeNotFound: 404, application.CodeConflict: 409, application.CodeForbidden: 403, application.CodeBusy: 503, application.CodeUnavailable: 503, application.CodeInternal: 500}[appErr.Code]
	if status == 0 {
		status = 500
	}
	server.respondID(w, status, id, nil, appErr)
}

func (server *Server) respond(w http.ResponseWriter, status int, data any) {
	server.respondID(w, status, ipc.NewRequestID(), data, nil)
}
func (server *Server) fail(w http.ResponseWriter, status int, code, message string) {
	writeFailure(w, status, code, message)
}
func writeFailure(w http.ResponseWriter, status int, code, message string) {
	writeEnvelope(w, status, ipc.NewRequestID(), nil, map[string]any{"code": code, "message": message, "retryable": false})
}
func (server *Server) respondID(w http.ResponseWriter, status int, id string, data any, err any) {
	writeEnvelope(w, status, id, data, err)
}
func writeEnvelope(w http.ResponseWriter, status int, id string, data any, err any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	envelope := map[string]any{"api_version": 1, "request_id": id}
	if err != nil {
		envelope["error"] = err
	} else {
		envelope["data"] = data
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope)
}
