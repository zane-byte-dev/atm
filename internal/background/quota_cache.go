package background

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/zane-byte-dev/atm/internal/executionlock"
	"github.com/zane-byte-dev/atm/internal/quota"
)

// QuotaCache is the explicit refresh result consumed by read-only views. Its
// source call happens only through quota.refresh; merely reading this cannot
// invoke configured provider executables or a billing endpoint.
type QuotaCache struct {
	UpdatedAt      string            `json:"updated_at"`
	AgentUpdatedAt map[string]string `json:"agent_updated_at,omitempty"`
	Snapshot       quota.Snapshot    `json:"snapshot"`
}

func (cache QuotaCache) UpdatedAtFor(agent string) string {
	if ts := cache.AgentUpdatedAt[agent]; ts != "" {
		return ts
	}
	return cache.UpdatedAt
}

const maxQuotaCacheBytes = 2 * 1024 * 1024

func saveQuotaSnapshot(ctx context.Context, dataDir string, snapshot quota.Snapshot, agent string) error {
	lock, err := executionlock.Acquire(ctx, dataDir, "quota-cache")
	if err != nil {
		return err
	}
	defer lock.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	updated := map[string]string{}
	for name := range snapshot.Agents {
		updated[name] = now
	}
	if agent != "" {
		if old, err := ReadQuotaCache(dataDir); err == nil {
			if old.Snapshot.Agents == nil {
				old.Snapshot.Agents = map[string]*quota.AgentQuota{}
			}
			for name := range old.Snapshot.Agents {
				if updated[name] == "" {
					updated[name] = old.UpdatedAtFor(name)
				}
			}
			for name, value := range snapshot.Agents {
				old.Snapshot.Agents[name] = value
			}
			seen := map[string]bool{}
			order := []string{}
			for _, name := range append(old.Snapshot.Order, snapshot.Order...) {
				if !seen[name] {
					seen[name] = true
					order = append(order, name)
				}
			}
			snapshot.Agents = old.Snapshot.Agents
			snapshot.Order = order
		}
	}
	// Provider warnings may carry credentials or raw response text.
	snapshot.Warnings = nil
	data, err := json.Marshal(QuotaCache{UpdatedAt: now, AgentUpdatedAt: updated, Snapshot: snapshot})
	if err != nil {
		return err
	}
	if len(data) > maxQuotaCacheBytes {
		return fmt.Errorf("quota cache exceeds the 2 MiB limit")
	}
	dir := filepath.Join(dataDir, "runtime")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "quota-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "quota.json"))
}

func ReadQuotaCache(dataDir string) (QuotaCache, error) {
	var cache QuotaCache
	f, err := os.Open(filepath.Join(dataDir, "runtime", "quota.json"))
	if err != nil {
		return cache, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxQuotaCacheBytes+1))
	if err != nil {
		return cache, err
	}
	if len(data) > maxQuotaCacheBytes {
		return cache, fmt.Errorf("quota cache exceeds the 2 MiB limit")
	}
	err = json.Unmarshal(data, &cache)
	return cache, err
}
