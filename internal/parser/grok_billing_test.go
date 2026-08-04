package parser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// grokBillingTestEnv points GrokSessions (and thus auth.json / unified.jsonl)
// and the live cache at a temp dir, and grokBillingURL at handler. It returns
// the temp root plus a counter of live requests actually made.
func grokBillingTestEnv(t *testing.T, handler http.HandlerFunc) (root string, requests *int) {
	t.Helper()
	oldSessions, oldAtmDir, oldURL := config.GrokSessions, config.AtmDir, grokBillingURL
	t.Cleanup(func() {
		config.GrokSessions = oldSessions
		config.AtmDir = oldAtmDir
		grokBillingURL = oldURL
	})
	root = t.TempDir()
	config.GrokSessions = filepath.Join(root, "sessions")
	config.AtmDir = filepath.Join(root, "atm")

	count := 0
	requests = &count
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if handler != nil {
			handler(w, r)
			return
		}
		http.Error(w, "no handler", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	grokBillingURL = srv.URL
	return root, requests
}

func writeGrokAuth(t *testing.T, root string, expiresAt time.Time) {
	t.Helper()
	auth := fmt.Sprintf(`{"https://auth.x.ai::client-1":{"key":"test-token","auth_mode":"oidc","expires_at":%q}}`,
		expiresAt.UTC().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(root, "auth.json"), []byte(auth), 0600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
}

func writeGrokBillingLog(t *testing.T, root string, usedPercent float64) {
	t.Helper()
	writeJSONL(t, filepath.Join(root, "logs", "unified.jsonl"),
		fmt.Sprintf(`{"ts":"2026-07-29T01:00:00.000Z","src":"shell","msg":"billing: fetched credits config","ctx":{"subscriptionTier":"SuperGrok","config":{"creditUsagePercent":%g,"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-07-28T07:28:53+00:00","end":"2026-08-04T07:28:53+00:00"},"billingPeriodStart":"2026-07-28T07:28:53+00:00","billingPeriodEnd":"2026-08-04T07:28:53+00:00","onDemandCap":{"val":0},"onDemandUsed":{"val":0}}}}`, usedPercent),
	)
}

const grokLiveBillingBody = `{"config":{
  "currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-07-28T07:28:53+00:00","end":"2026-08-04T07:28:53+00:00"},
  "creditUsagePercent":19.0,
  "onDemandCap":{"val":0},"onDemandUsed":{"val":0},
  "productUsage":[
    {"product":"GrokBuild","usagePercent":13.0},
    {"product":"GrokImagine","usagePercent":4.0},
    {"product":"GrokChat","usagePercent":2.0}
  ],
  "billingPeriodStart":"2026-07-28T07:28:53+00:00","billingPeriodEnd":"2026-08-04T07:28:53+00:00"
}}`

func TestGrokQuotaAutoDisabledNeverTouchesNetwork(t *testing.T) {
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, grokLiveBillingBody)
	})
	writeGrokAuth(t, root, time.Now().Add(time.Hour))
	writeGrokBillingLog(t, root, 42.5)

	got := GrokQuotaAuto(false)
	if got == nil || got.Source != "log" {
		t.Fatalf("expected log-sourced quota, got %#v", got)
	}
	if got.Primary.UsedPercent != 42.5 {
		t.Fatalf("primary used = %v", got.Primary.UsedPercent)
	}
	if *requests != 0 {
		t.Fatalf("live disabled but %d billing requests were made", *requests)
	}
}

func TestGrokQuotaAutoWithoutTokenStaysOffline(t *testing.T) {
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, grokLiveBillingBody)
	})
	// No auth.json at all: live enabled must not open a connection.
	writeGrokBillingLog(t, root, 10)

	got := GrokQuotaAuto(true)
	if got == nil || got.Source != "log" {
		t.Fatalf("expected log fallback, got %#v", got)
	}
	if *requests != 0 {
		t.Fatalf("no token but %d billing requests were made", *requests)
	}
}

func TestGrokQuotaAutoExpiredTokenStaysOffline(t *testing.T) {
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, grokLiveBillingBody)
	})
	writeGrokAuth(t, root, time.Now().Add(-time.Hour))
	writeGrokBillingLog(t, root, 10)

	if got := GrokQuotaAuto(true); got == nil || got.Source != "log" {
		t.Fatalf("expected log fallback, got %#v", got)
	}
	if *requests != 0 {
		t.Fatalf("expired token but %d billing requests were made", *requests)
	}
}

func TestGrokQuotaAutoLiveParsesProductsAndCaches(t *testing.T) {
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, grokLiveBillingBody)
	})
	writeGrokAuth(t, root, time.Now().Add(time.Hour))
	writeGrokBillingLog(t, root, 42.5)

	got := GrokQuotaAuto(true)
	if got == nil {
		t.Fatal("GrokQuotaAuto returned nil")
	}
	if got.Source != "live" {
		t.Fatalf("source = %q, want live", got.Source)
	}
	if got.Primary == nil || got.Primary.UsedPercent != 19.0 {
		t.Fatalf("primary = %#v", got.Primary)
	}
	if got.Primary.WindowMinutes != 7*24*60 {
		t.Fatalf("window minutes = %d", got.Primary.WindowMinutes)
	}
	// Plan comes from the local log because the billing API omits the tier.
	if got.Plan != "SuperGrok" {
		t.Fatalf("plan = %q", got.Plan)
	}
	want := []QuotaProduct{
		{Name: "GrokBuild", UsedPercent: 13},
		{Name: "GrokImagine", UsedPercent: 4},
		{Name: "GrokChat", UsedPercent: 2},
	}
	if len(got.Products) != len(want) {
		t.Fatalf("products = %#v", got.Products)
	}
	for i, p := range want {
		if got.Products[i] != p {
			t.Fatalf("products[%d] = %#v, want %#v", i, got.Products[i], p)
		}
	}

	// Second call inside the fresh window is served from cache: no new request.
	again := GrokQuotaAuto(true)
	if again == nil || again.Source != "cache" {
		t.Fatalf("expected cache-sourced quota, got %#v", again)
	}
	if len(again.Products) != len(want) {
		t.Fatalf("cached products = %#v", again.Products)
	}
	if *requests != 1 {
		t.Fatalf("expected exactly 1 billing request, got %d", *requests)
	}
}

func TestGrokQuotaAutoLiveFailureFallsBackToLog(t *testing.T) {
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	writeGrokAuth(t, root, time.Now().Add(time.Hour))
	writeGrokBillingLog(t, root, 42.5)

	got := GrokQuotaAuto(true)
	if got == nil || got.Source != "log" {
		t.Fatalf("expected log fallback after live failure, got %#v", got)
	}
	if got.Primary.UsedPercent != 42.5 {
		t.Fatalf("primary used = %v", got.Primary.UsedPercent)
	}
	if *requests != 1 {
		t.Fatalf("expected 1 failed billing request, got %d", *requests)
	}
}

func TestGrokQuotaAutoLiveFailureUsesStaleCache(t *testing.T) {
	root, _ := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	writeGrokAuth(t, root, time.Now().Add(time.Hour))
	writeGrokBillingLog(t, root, 42.5)
	// A cache older than the fresh TTL but within the stale window beats the log.
	writeGrokQuotaCache(grokQuotaCacheEntry{
		FetchedAt: time.Now().Add(-10 * time.Minute),
		Config:    []byte(`{"creditUsagePercent":33.0,"billingPeriodEnd":"2026-08-04T07:28:53+00:00","productUsage":[{"product":"GrokBuild","usagePercent":30.0}]}`),
	})

	got := GrokQuotaAuto(true)
	if got == nil || got.Source != "cache" {
		t.Fatalf("expected stale cache fallback, got %#v", got)
	}
	if got.Primary.UsedPercent != 33.0 {
		t.Fatalf("primary used = %v", got.Primary.UsedPercent)
	}
	if len(got.Products) != 1 || got.Products[0].Name != "GrokBuild" {
		t.Fatalf("products = %#v", got.Products)
	}
}

func TestGrokLiveQuotaRefusesRedirects(t *testing.T) {
	// The billing endpoint has no legitimate redirect; a 30x must be treated
	// as a failure (and fall back to the log) instead of being followed with
	// the bearer token attached.
	followed := false
	root, requests := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
			fmt.Fprint(w, grokLiveBillingBody)
			return
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	writeGrokAuth(t, root, time.Now().Add(time.Hour))
	writeGrokBillingLog(t, root, 42.5)

	got := GrokQuotaAuto(true)
	if got == nil || got.Source != "log" {
		t.Fatalf("expected log fallback on redirect, got %#v", got)
	}
	if followed {
		t.Fatal("client followed the redirect")
	}
	if *requests != 1 {
		t.Fatalf("expected 1 request, got %d", *requests)
	}
}

func TestGrokAuthTokenPicksLatestExpiry(t *testing.T) {
	// Several credentials in auth.json: selection must be deterministic — the
	// unexpired entry with the latest expires_at, not map iteration order.
	root, _ := grokBillingTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer newest" {
			http.Error(w, "wrong token", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, grokLiveBillingBody)
	})
	now := time.Now().UTC()
	auth := fmt.Sprintf(`{
		"https://auth.x.ai::old":     {"key":"expired", "expires_at":%q},
		"https://auth.x.ai::no-exp":  {"key":"undated"},
		"https://auth.x.ai::soon":    {"key":"older",  "expires_at":%q},
		"https://auth.x.ai::current": {"key":"newest", "expires_at":%q}
	}`,
		now.Add(-time.Hour).Format(time.RFC3339),
		now.Add(30*time.Minute).Format(time.RFC3339),
		now.Add(6*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(root, "auth.json"), []byte(auth), 0600); err != nil {
		t.Fatalf("write auth: %v", err)
	}

	token, err := grokAuthToken()
	if err != nil {
		t.Fatalf("grokAuthToken: %v", err)
	}
	if token != "newest" {
		t.Fatalf("token = %q, want newest", token)
	}
	// And the live path succeeds with that token.
	if got, err := GrokLiveQuota(); err != nil || got == nil {
		t.Fatalf("GrokLiveQuota with newest token: %v, %#v", err, got)
	}
}
