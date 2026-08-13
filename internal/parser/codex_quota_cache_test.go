package parser

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func codexQuotaTestEnv(t *testing.T) (sessionsDir string) {
	t.Helper()
	oldSessions, oldAtmDir := config.CodexSessions, config.AtmDir
	t.Cleanup(func() {
		config.CodexSessions = oldSessions
		config.AtmDir = oldAtmDir
	})
	root := t.TempDir()
	config.CodexSessions = filepath.Join(root, "sessions")
	config.AtmDir = filepath.Join(root, "atm")
	return config.CodexSessions
}

func writeCodexQuotaRollout(t *testing.T, sessionsDir string, usedPercent float64) {
	t.Helper()
	writeJSONL(t, filepath.Join(sessionsDir, "2026", "08", "08", "rollout-2026-08-08T10-00-00-quota.jsonl"),
		fmt.Sprintf(`{"timestamp":"2026-08-08T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"plan_type":"plus","primary":{"used_percent":%g,"window_minutes":300,"resets_at":1786500000}}}}`, usedPercent),
	)
}

// TestCodexQuotaCachesScan verifies a fresh cache short-circuits the sessions
// scan, and that a different sessions dir invalidates the cached entry.
func TestCodexQuotaCachesScan(t *testing.T) {
	sessions := codexQuotaTestEnv(t)
	writeCodexQuotaRollout(t, sessions, 42)

	first := CodexQuota()
	if first == nil || first.Primary == nil || first.Primary.UsedPercent != 42 {
		t.Fatalf("first read = %+v", first)
	}

	// The rollout moves, but a fresh cache answers without rescanning.
	writeCodexQuotaRollout(t, sessions, 77)
	second := CodexQuota()
	if second == nil || second.Primary == nil || second.Primary.UsedPercent != 42 {
		t.Fatalf("cached read = %+v, want cached 42", second)
	}

	// A different sessions dir (another test, another HOME) must not reuse it.
	config.CodexSessions = filepath.Join(t.TempDir(), "other-sessions")
	if got := CodexQuota(); got != nil {
		t.Fatalf("cache leaked across sessions dirs: %+v", got)
	}
}

// TestCodexQuotaStaleCacheRescans verifies an expired cache entry falls back
// to a real scan of the sessions tree.
func TestCodexQuotaStaleCacheRescans(t *testing.T) {
	sessions := codexQuotaTestEnv(t)
	writeCodexQuotaRollout(t, sessions, 42)

	if first := CodexQuota(); first == nil {
		t.Fatal("first read = nil")
	}
	writeCodexQuotaRollout(t, sessions, 77)
	writeCodexQuotaCache(codexQuotaCacheEntry{
		ScannedAt:   time.Now().Add(-codexQuotaCacheFresh - time.Minute),
		SessionsDir: config.CodexSessions,
		Quota:       &QuotaInfo{Primary: &QuotaLimit{UsedPercent: 42}},
	})

	got := CodexQuota()
	if got == nil || got.Primary == nil || got.Primary.UsedPercent != 77 {
		t.Fatalf("stale cache read = %+v, want rescan 77", got)
	}
}
