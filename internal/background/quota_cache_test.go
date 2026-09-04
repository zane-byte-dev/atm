package background

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/quota"
)

func TestQuotaCachePreservesOtherAgentsFreshnessAndOmitsWarnings(t *testing.T) {
	dir := t.TempDir()
	old := QuotaCache{UpdatedAt: "2020-01-01T00:00:00Z", Snapshot: quota.Snapshot{Agents: map[string]*quota.AgentQuota{"codex": {Plan: "old"}, "claude": {Plan: "unchanged"}}}}
	if err := os.MkdirAll(filepath.Join(dir, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(old)
	if err := os.WriteFile(filepath.Join(dir, "runtime", "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveQuotaSnapshot(context.Background(), dir, quota.Snapshot{Agents: map[string]*quota.AgentQuota{"codex": {Plan: "fresh"}}, Warnings: []string{"secret-provider-response"}}, "codex"); err != nil {
		t.Fatal(err)
	}
	cache, err := ReadQuotaCache(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cache.Snapshot.Agents["codex"].Plan != "fresh" || cache.Snapshot.Agents["claude"].Plan != "unchanged" || cache.UpdatedAtFor("claude") != old.UpdatedAt || cache.UpdatedAtFor("codex") == old.UpdatedAt {
		t.Fatalf("cache=%+v", cache)
	}
	data, err = os.ReadFile(filepath.Join(dir, "runtime", "quota.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-provider-response") {
		t.Fatal("provider diagnostic leaked to cache")
	}
}

func TestQuotaCacheRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime", "quota.json"), []byte(strings.Repeat(" ", maxQuotaCacheBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadQuotaCache(dir); err == nil {
		t.Fatal("oversized cache was accepted")
	}
}
