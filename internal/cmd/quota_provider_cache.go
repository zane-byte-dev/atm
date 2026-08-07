package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// A provider that reports nothing — a daily quota before today's first
// observation, a browser bridge that is not running, a command that timed out —
// used to drop its card out of the grid entirely, which reads as "this quota is
// gone" when the truth is "this reading is missing". ATM remembers the last
// cards each provider returned and holds them in place as metric-less
// placeholders until the provider reports again.
const (
	quotaProviderCacheVersion = 1
	// Long enough to cover a gap in any provider's data, short enough that a
	// provider left in config but abandoned stops haunting the grid.
	quotaProviderCacheTTL = 7 * 24 * time.Hour
	// `atm quota` runs on the App's one-minute refresh timer, so rewriting an
	// identical file on every tick is pure churn. The freshness stamp therefore
	// advances at most this often — coarse against the TTL, which is all that
	// reads it.
	quotaProviderCacheStampInterval = time.Hour
)

// Why a card has no reading. The App turns these into its own wording.
const (
	quotaProviderReasonEmpty = "empty"
	quotaProviderReasonError = "error"
)

type quotaProviderCacheEntry struct {
	FetchedAt string              `json:"fetched_at"`
	Cards     []quotaProviderCard `json:"cards"`
}

type quotaProviderCache struct {
	Version   int                                `json:"version"`
	Providers map[string]quotaProviderCacheEntry `json:"providers"`
}

func normalizeQuotaProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func quotaProviderCachePath() string {
	return filepath.Join(config.AtmDir, "quota_provider_cards.json")
}

// loadQuotaProviderCache never fails. The cache only decides whether a card may
// stay on screen, so a missing, unreadable, or hand-edited file degrades to the
// old behaviour — no placeholder — instead of breaking a quota reading.
func loadQuotaProviderCache() quotaProviderCache {
	empty := quotaProviderCache{
		Version:   quotaProviderCacheVersion,
		Providers: map[string]quotaProviderCacheEntry{},
	}
	data, err := os.ReadFile(quotaProviderCachePath())
	if err != nil {
		return empty
	}
	var cache quotaProviderCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != quotaProviderCacheVersion {
		return empty
	}
	if cache.Providers == nil {
		cache.Providers = map[string]quotaProviderCacheEntry{}
	}
	for providerID, entry := range cache.Providers {
		// A card read back from disk reaches the App on the same path as one a
		// provider just sent, so it passes the same validation.
		if !quotaProviderToken.MatchString(providerID) ||
			normalizeQuotaProviderCards(providerID, entry.Cards) != nil ||
			len(entry.Cards) == 0 {
			delete(cache.Providers, providerID)
		}
	}
	return cache
}

// saveQuotaProviderCache writes atomically and swallows every failure: a cache
// ATM could not update is one placeholder the next run cannot draw, which is not
// worth a warning on a command whose job is to print a quota.
func saveQuotaProviderCache(cache quotaProviderCache) {
	cache.Version = quotaProviderCacheVersion
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	path := quotaProviderCachePath()
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atm-quota-provider-*.tmp")
	if err != nil {
		return
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return
	}
	if err := temporary.Close(); err != nil {
		return
	}
	_ = os.Rename(temporaryPath, path)
}

// pruneQuotaProviderCache forgets providers that are no longer configured, so
// removing one from config.json removes its card instead of leaving a
// placeholder nothing can ever refresh.
func pruneQuotaProviderCache(cache *quotaProviderCache, configured []string) bool {
	known := make(map[string]bool, len(configured))
	for _, providerID := range configured {
		known[normalizeQuotaProviderID(providerID)] = true
	}
	changed := false
	for providerID := range cache.Providers {
		if !known[providerID] {
			delete(cache.Providers, providerID)
			changed = true
		}
	}
	return changed
}

// rememberQuotaProviderCards records what a provider just returned and reports
// whether the file needs rewriting.
func rememberQuotaProviderCards(cache *quotaProviderCache, providerID string,
	cards []quotaProviderCard, now time.Time) bool {
	if previous, ok := cache.Providers[providerID]; ok &&
		reflect.DeepEqual(previous.Cards, cards) {
		stamped, err := time.Parse(time.RFC3339, previous.FetchedAt)
		if err == nil && now.Sub(stamped) < quotaProviderCacheStampInterval {
			return false
		}
	}
	if cache.Providers == nil {
		cache.Providers = map[string]quotaProviderCacheEntry{}
	}
	cache.Providers[providerID] = quotaProviderCacheEntry{
		FetchedAt: now.Format(time.RFC3339),
		Cards:     cards,
	}
	return true
}

// quotaProviderPlaceholders turns the last known cards into metric-less ones
// that hold their place in the grid. The reading is what went missing, so the
// numbers and the source badge go with it and the observation time stays: the
// card can then say when it was last true.
func quotaProviderPlaceholders(cache quotaProviderCache, providerID, reason string,
	now time.Time) []quotaProviderCard {
	entry, ok := cache.Providers[providerID]
	if !ok {
		return nil
	}
	stamped, err := time.Parse(time.RFC3339, entry.FetchedAt)
	if err != nil || now.Sub(stamped) > quotaProviderCacheTTL {
		return nil
	}
	placeholders := make([]quotaProviderCard, 0, len(entry.Cards))
	for _, card := range entry.Cards {
		// An empty slice, not nil: consumers decode `metrics` as a list and a
		// null would fail where "no metrics" has to be readable.
		card.Metrics = []quotaProviderMetric{}
		card.Source = ""
		card.Unavailable = true
		card.UnavailableReason = reason
		placeholders = append(placeholders, card)
	}
	return placeholders
}
