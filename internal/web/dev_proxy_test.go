package web

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDevUIOnlyAcceptsExplicitLoopbackHTTPOrigin(t *testing.T) {
	for _, valid := range []string{"http://127.0.0.1:5173", "http://[::1]:5173"} {
		if err := validateDevUI(valid); err != nil {
			t.Fatalf("valid origin %s: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"", "http://localhost:5173", "https://127.0.0.1:5173", "http://127.1:5173", "http://127.0.0.2:5173", "http://0.0.0.0:5173",
		"http://169.254.169.254:80", "http://example.com:5173", "http://user:password@127.0.0.1:5173", "http://127.0.0.1:5173/",
		"http://127.0.0.1:5173/path", "http://127.0.0.1:5173?x=1", "http://127.0.0.1:5173?", "http://127.0.0.1:5173#x", "http://127.0.0.1:5173#",
		"http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:99999", "http://[::ffff:127.0.0.1]:5173",
	} {
		if err := validateDevUI(invalid); err == nil {
			t.Fatalf("accepted unsafe origin %q", invalid)
		}
	}
}

func TestDevProxyFixesTargetAndStripsBrowserCapabilities(t *testing.T) {
	seen := make(chan *http.Request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Clone(r.Context())
		w.Header().Set("Set-Cookie", "poison=1")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Security-Policy", "default-src *")
		w.Write([]byte("export default 1"))
	}))
	defer upstream.Close()
	proxy, err := newDevProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/src/main.tsx?t=123", nil)
	request.Host = "127.0.0.1:47321"
	request.Header.Set("Cookie", "atm_session=private")
	request.Header.Set("Authorization", "Bearer private")
	request.Header.Set("X-ATM-CSRF", "private")
	request.Header.Set("X-ATM-Instance", "private")
	request.Header.Set("X-Forwarded-Host", "other.example")
	request.Header.Set("Forwarded", "for=other.example")
	request.Header.Set("Referer", "http://127.0.0.1:47321/private")
	request.Header.Set("Origin", "http://"+request.Host)
	response := httptest.NewRecorder()
	response.Header().Set("Content-Security-Policy", "default-src 'self'")
	proxy.ServeHTTP(response, request)
	if response.Code != 200 || response.Body.String() != "export default 1" {
		t.Fatalf("response=%d %s", response.Code, response.Body.String())
	}
	forwarded := <-seen
	if forwarded.Host != strings.TrimPrefix(upstream.URL, "http://") || forwarded.URL.RequestURI() != "/src/main.tsx?t=123" {
		t.Fatalf("changed target/path: %s %s", forwarded.Host, forwarded.URL)
	}
	for _, key := range []string{"Cookie", "Authorization", "X-ATM-CSRF", "X-ATM-Instance", "X-Forwarded-Host", "Forwarded", "Referer"} {
		if forwarded.Header.Get(key) != "" {
			t.Fatalf("forwarded capability %s", key)
		}
	}
	if forwarded.Header.Get("Origin") != upstream.URL {
		t.Fatalf("upstream origin=%s", forwarded.Header.Get("Origin"))
	}
	if response.Header().Get("Set-Cookie") != "" || response.Header().Get("Access-Control-Allow-Origin") != "" || response.Header().Get("Content-Security-Policy") != "default-src 'self'" {
		t.Fatalf("upstream changed security headers: %+v", response.Header())
	}
}

func TestDevProxyRejectsAPIsOriginsAndOtherUpgrades(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(200) }))
	defer upstream.Close()
	proxy, err := newDevProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, host, origin, upgrade string }{
		{"/api/v1/todo.show", "127.0.0.1:47321", "", ""},
		{"/x/../api/v1/control/open", "127.0.0.1:47321", "", ""},
		{"/healthz", "127.0.0.1:47321", "", ""},
		{"/src/main.tsx", "evil.example:47321", "", ""},
		{"/src/main.tsx", "127.0.0.1:47321", "http://127.0.0.1:5173", ""},
		{"/__atm_hmr", "127.0.0.1:47321", "", "websocket"},
		{"/wrong-websocket", "127.0.0.1:47321", "http://127.0.0.1:47321", "websocket"},
		{"/__atm_hmr", "127.0.0.1:47321", "http://evil.example", "websocket"},
		{"/__atm_hmr", "127.0.0.1:47321", "http://127.0.0.1:47321", "h2c"},
	} {
		request := httptest.NewRequest("GET", test.path, nil)
		request.Host = test.host
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Upgrade", test.upgrade)
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		if response.Code < 400 {
			t.Fatalf("accepted request %+v", test)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("forbidden requests reached upstream %d times", hits.Load())
	}
}

func TestDevProxyTunnelsSameOriginViteWebSocket(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-ATM-CSRF") != "" {
			t.Error("WebSocket leaked browser capabilities")
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		accept := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nSec-WebSocket-Protocol: vite-hmr\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:]))
		rw.Flush()
		// A short server text frame proves data crosses the upgraded tunnel.
		conn.Write([]byte{0x81, 2, 'o', 'k'})
	}))
	defer upstream.Close()
	proxy, err := newDevProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(conn, "GET /__atm_hmr?token=development HTTP/1.1\r\nHost: %s\r\nOrigin: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: vite-hmr\r\nCookie: private=value\r\nX-ATM-CSRF: secret\r\n\r\n", strings.TrimPrefix(server.URL, "http://"), server.URL)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade=%+v err=%v", response, err)
	}
	frame := make([]byte, 4)
	if _, err := io.ReadFull(reader, frame); err != nil || string(frame) != string([]byte{0x81, 2, 'o', 'k'}) {
		t.Fatalf("frame=%v err=%v", frame, err)
	}
}

func TestDevProxyErrorsAreGenericAndCSPIsOriginBound(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	proxy, err := newDevProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/src/main.tsx", nil)
	request.Host = "127.0.0.1:47321"
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	if response.Code != 502 || strings.Contains(response.Body.String(), upstream.URL) || strings.Contains(response.Body.String(), "dial tcp") {
		t.Fatalf("leaky error: %d %s", response.Code, response.Body.String())
	}
	csp := devContentSecurityPolicy("http://127.0.0.1:47321")
	if !strings.Contains(csp, "connect-src 'self' ws://127.0.0.1:47321;") || strings.Contains(csp, "ws://127.0.0.1:5173") {
		t.Fatalf("websocket origin policy=%s", csp)
	}
}
