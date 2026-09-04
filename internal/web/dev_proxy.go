package web

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const devHMRPath = "/__atm_hmr"

// newDevProxy is opt-in development plumbing. The fixed upstream is an IP
// literal: redirects, DNS rebinding and proxy environment variables cannot
// turn browser asset requests into requests to another machine.
func newDevProxy(raw string) (http.Handler, error) {
	target, err := parseDevUI(raw)
	if err != nil {
		return nil, err
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.Host = target.Host
			for key := range request.Out.Header {
				lower := strings.ToLower(key)
				if lower == "cookie" || lower == "authorization" || lower == "proxy-authorization" || lower == "referer" || lower == "forwarded" ||
					strings.HasPrefix(lower, "x-atm-") || strings.HasPrefix(lower, "x-forwarded-") {
					request.Out.Header.Del(key)
				}
			}
			// Incoming origin was checked against the Go listener. Vite receives
			// only its own local origin and none of ATM's browser capabilities.
			if request.Out.Header.Get("Origin") != "" {
				request.Out.Header.Set("Origin", target.Scheme+"://"+target.Host)
			}
		},
		Transport: &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: 5 * time.Second, IdleConnTimeout: 30 * time.Second,
		},
		ModifyResponse: func(response *http.Response) error {
			if location := response.Header.Get("Location"); location != "" {
				u, err := url.Parse(location)
				if err != nil || u.IsAbs() || u.Host != "" {
					return fmt.Errorf("development redirects must stay on the Go origin")
				}
			}
			// A frontend dev server must not replace ATM's authenticated cookie,
			// change its security policy or open CORS on the Go API origin.
			for key := range response.Header {
				lower := strings.ToLower(key)
				if lower == "set-cookie" || lower == "content-security-policy" || lower == "content-security-policy-report-only" ||
					strings.HasPrefix(lower, "access-control-") || strings.HasPrefix(lower, "x-atm-") {
					response.Header.Del(key)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeFailure(w, http.StatusBadGateway, "unavailable", "frontend development server is unavailable")
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		clean := path.Clean(request.URL.Path)
		if clean == "/api" || strings.HasPrefix(clean, "/api/") || clean == "/healthz" {
			writeFailure(w, http.StatusNotFound, "not_found", "this route belongs to the ATM server")
			return
		}
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writeFailure(w, http.StatusMethodNotAllowed, "invalid_argument", "development assets require GET or HEAD")
			return
		}
		if !validDevHost(request.Host) || request.URL.IsAbs() || request.URL.Host != "" {
			writeFailure(w, http.StatusForbidden, "forbidden", "invalid development request host")
			return
		}
		origin := request.Header.Get("Origin")
		if origin != "" && origin != "http://"+request.Host {
			writeFailure(w, http.StatusForbidden, "forbidden", "cross-origin development access is not allowed")
			return
		}
		if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeFailure(w, http.StatusForbidden, "forbidden", "cross-origin development access is not allowed")
			return
		}
		upgrade := request.Header.Get("Upgrade")
		if upgrade != "" {
			if !strings.EqualFold(upgrade, "websocket") || clean != devHMRPath || origin == "" || request.Method != http.MethodGet ||
				request.Header.Get("Sec-WebSocket-Version") != "13" || request.Header.Get("Sec-WebSocket-Key") == "" ||
				!headerToken(request.Header.Get("Sec-WebSocket-Protocol"), "vite-hmr") {
				writeFailure(w, http.StatusForbidden, "forbidden", "invalid development websocket request")
				return
			}
			// ReverseProxy hijacks the connection for HMR; the ordinary asset
			// response deadline must not close that live socket after 30 seconds.
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Time{})
			_ = controller.SetWriteDeadline(time.Time{})
		}
		w.Header().Set("Cache-Control", "no-store")
		proxy.ServeHTTP(w, request)
	}), nil
}

func validateDevUI(raw string) error {
	_, err := parseDevUI(raw)
	return err
}

func parseDevUI(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Opaque != "" || !validDevHost(u.Host) {
		return nil, fmt.Errorf("--dev-ui requires an HTTP loopback IP and port, e.g. http://127.0.0.1:5173; credentials, paths and queries are not allowed")
	}
	return u, nil
}

func validDevHost(host string) bool {
	ip, port, err := net.SplitHostPort(host)
	if err != nil || (ip != "127.0.0.1" && ip != "::1") {
		return false
	}
	number, err := strconv.Atoi(port)
	return err == nil && number > 0 && number <= 65535 && strconv.Itoa(number) == port
}

func headerToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

// Development alone allows React Refresh's inline module preamble. Production
// keeps the existing CSP. Websockets are restricted to the Go listener origin,
// so Vite's direct-port fallback cannot bypass the same-origin tunnel.
func devContentSecurityPolicy(origin string) string {
	connect := "'self'"
	if strings.HasPrefix(origin, "http://") && validDevHost(strings.TrimPrefix(origin, "http://")) {
		connect += " ws://" + strings.TrimPrefix(origin, "http://")
	}
	return "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src " + connect + "; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
}
