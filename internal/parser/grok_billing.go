package parser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Live Grok quota. Off by default (config.GrokLiveQuota): `atm quota` stays a
// local log read unless the user opts in, at which point we call the same
// billing endpoint the Grok shell itself uses and fall back to a short cache
// and then the log when the network or credential is unavailable.

// grokBillingURL is the credits endpoint of the Grok CLI chat proxy
// (`/billing?format=credits`, same call the shell logs as "billing: fetched
// credits config"). Var so tests can point it at a local server.
var grokBillingURL = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"

// grokBillingHTTPClient never follows redirects: the billing endpoint has no
// legitimate redirect, and treating 30x as an error keeps the bearer token
// pinned to the configured host by construction (the Go client would already
// strip Authorization cross-host, but not following at all is simpler to
// reason about — the response is surfaced as a non-200 failure instead).
var grokBillingHTTPClient = &http.Client{
	Timeout: 8 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const (
	// A cache younger than this is served without touching the network, so a
	// GUI polling every minute does not turn into a request per poll.
	grokQuotaCacheFresh = 2 * time.Minute
	// After a live failure a cache this old is still better than the log,
	// because the log only updates when an interactive Grok shell starts.
	grokQuotaCacheMaxStale = time.Hour
)

// GrokAuthPath is the Grok CLI credential store, sibling of the sessions dir
// (~/.grok/auth.json by default) so a custom sessions root still works.
func GrokAuthPath() string {
	return filepath.Join(filepath.Dir(config.GrokSessions), "auth.json")
}

// grokAuthToken returns a bearer token from the Grok credential store, or an
// error when there is none. Callers must treat an error as "do not touch the
// network": no token means the user never logged in on this machine.
//
// auth.json can hold several credentials (issuer × client). Map iteration is
// random in Go, so pick deterministically: the unexpired entry with the
// latest expires_at, which is also the one the Grok shell refreshed last.
func grokAuthToken() (string, error) {
	data, err := os.ReadFile(GrokAuthPath())
	if err != nil {
		return "", fmt.Errorf("grok credentials unavailable: %w", err)
	}
	// Top level is keyed by "<issuer>::<client-id>"; each value carries the
	// access token in "key" plus an "expires_at" timestamp.
	var entries map[string]map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		return "", fmt.Errorf("grok credentials unreadable: %w", err)
	}
	var best string
	var bestExpiry time.Time
	for _, entry := range entries {
		token := config.GetStr(entry, "key")
		if token == "" {
			continue
		}
		// Entries without expires_at stay usable but rank below any entry
		// with a known future expiry (zero time sorts first).
		var expiry time.Time
		if exp, ok := parseTimestampString(config.GetStr(entry, "expires_at")); ok {
			if exp.Before(time.Now()) {
				continue
			}
			expiry = exp
		}
		if best == "" || expiry.After(bestExpiry) {
			best = token
			bestExpiry = expiry
		}
	}
	if best == "" {
		return "", errors.New("no unexpired grok credential in auth.json")
	}
	return best, nil
}

// GrokLiveQuota fetches the current credits snapshot from the billing API.
// It never touches the network without a locally stored, unexpired token.
func GrokLiveQuota() (*QuotaInfo, error) {
	token, err := grokAuthToken()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, grokBillingURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("x-grok-client-mode", "cli")
	resp, err := grokBillingHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grok billing request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grok billing API returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var payload struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Config) == 0 {
		return nil, errors.New("grok billing response has no config")
	}
	info := grokQuotaInfoFromRawConfig(payload.Config, "")
	if info == nil {
		return nil, errors.New("grok billing config missing usage fields")
	}
	info.Timestamp = time.Now()
	info.Source = "live"
	writeGrokQuotaCache(grokQuotaCacheEntry{FetchedAt: info.Timestamp, Config: payload.Config})
	return info, nil
}

// GrokQuotaAuto is the merge strategy behind `atm quota`:
// live (opt-in) → fresh cache → live fetch → stale cache → local log.
func GrokQuotaAuto(live bool) *QuotaInfo {
	if !live {
		return GrokQuota()
	}
	now := time.Now()
	if cached := readGrokQuotaCache(now, grokQuotaCacheFresh); cached != nil {
		return grokFillPlanFromLog(cached)
	}
	if info, err := GrokLiveQuota(); err == nil {
		return grokFillPlanFromLog(info)
	}
	if cached := readGrokQuotaCache(now, grokQuotaCacheMaxStale); cached != nil {
		return grokFillPlanFromLog(cached)
	}
	return GrokQuota()
}

// grokFillPlanFromLog backfills Plan on live/cache snapshots: the billing API
// does not return the subscription tier, but the shell log does.
func grokFillPlanFromLog(info *QuotaInfo) *QuotaInfo {
	if info != nil && info.Plan == "" {
		if fromLog := GrokQuota(); fromLog != nil {
			info.Plan = fromLog.Plan
		}
	}
	return info
}

type grokQuotaCacheEntry struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Config    json.RawMessage `json:"config"`
}

func grokQuotaCachePath() string {
	return filepath.Join(config.AtmDir, "grok_quota_cache.json")
}

func writeGrokQuotaCache(entry grokQuotaCacheEntry) {
	// Best-effort: the cache only saves a network round-trip, so a failed
	// write must never fail the quota read itself.
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(config.AtmDir, 0755); err != nil {
		return
	}
	_ = os.WriteFile(grokQuotaCachePath(), data, 0600)
}

// readGrokQuotaCache returns the cached live snapshot when it is younger than
// maxAge, or nil.
func readGrokQuotaCache(now time.Time, maxAge time.Duration) *QuotaInfo {
	data, err := os.ReadFile(grokQuotaCachePath())
	if err != nil {
		return nil
	}
	var entry grokQuotaCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return nil
	}
	if entry.FetchedAt.IsZero() || now.Sub(entry.FetchedAt) > maxAge {
		return nil
	}
	info := grokQuotaInfoFromRawConfig(entry.Config, "")
	if info == nil {
		return nil
	}
	info.Timestamp = entry.FetchedAt
	info.Source = "cache"
	return info
}

func grokQuotaInfoFromRawConfig(raw json.RawMessage, plan string) *QuotaInfo {
	var cfg map[string]any
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	return grokQuotaFromConfig(cfg, plan)
}
