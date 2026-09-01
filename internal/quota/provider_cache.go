package quota

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
)

// A provider that reports nothing — a daily quota before today's first
// observation, a browser bridge that is not running, a command that timed out —
// used to drop its card out of the grid entirely, which reads as "this quota is
// gone" when the truth is "this reading is missing". ATM remembers the last
// cards each provider returned and holds them in place as metric-less
// placeholders until the provider reports again.
const (
	providerCacheVersion = 1
	// Long enough to cover a gap in any provider's data, short enough that a
	// provider left in config but abandoned stops haunting the grid.
	providerCacheTTL = 7 * 24 * time.Hour
	// `atm quota` runs on the App's one-minute refresh timer, so rewriting an
	// identical file on every tick is pure churn. The freshness stamp therefore
	// advances at most this often — coarse against the TTL, which is all that
	// reads it.
	providerCacheStampInterval = time.Hour
)

// Why a card has no reading. The App turns these into its own wording.
const (
	ProviderReasonEmpty = "empty"
	ProviderReasonError = "error"
)

type providerCacheEntry struct {
	FetchedAt string         `json:"fetched_at"`
	Cards     []ProviderCard `json:"cards"`
}

type providerCache struct {
	Version   int                           `json:"version"`
	Providers map[string]providerCacheEntry `json:"providers"`
}

func normalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func providerCachePath() string {
	return filepath.Join(config.AtmDir, "quota_provider_cards.json")
}

// loadProviderCache never fails. The cache only decides whether a card may
// stay on screen, so a missing, unreadable, or hand-edited file degrades to the
// old behaviour — no placeholder — instead of breaking a quota reading.
func loadProviderCache() providerCache {
	empty := providerCache{
		Version:   providerCacheVersion,
		Providers: map[string]providerCacheEntry{},
	}
	data, err := os.ReadFile(providerCachePath())
	if err != nil {
		return empty
	}
	var cache providerCache
	if err := json.Unmarshal(data, &cache); err != nil || cache.Version != providerCacheVersion {
		return empty
	}
	if cache.Providers == nil {
		cache.Providers = map[string]providerCacheEntry{}
	}
	for providerID, entry := range cache.Providers {
		// A card read back from disk reaches the App on the same path as one a
		// provider just sent, so it passes the same validation.
		if !providerToken.MatchString(providerID) ||
			normalizeProviderCards(providerID, entry.Cards) != nil ||
			len(entry.Cards) == 0 {
			delete(cache.Providers, providerID)
		}
	}
	return cache
}

// saveProviderCache never fails its caller: a cache ATM could not update is
// one placeholder the next run cannot draw, which is not worth an error on a
// command whose job is to print a quota. The failure goes to the log instead of
// nowhere, so `atm diagnose` can explain a card that stopped ageing.
func saveProviderCache(cache providerCache) {
	if err := writeProviderCache(cache); err != nil {
		logging.Failure("quota_provider_cache_not_saved", "atm quota", err, nil)
	}
}

func writeProviderCache(cache providerCache) error {
	cache.Version = providerCacheVersion
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	path := providerCachePath()
	temporary, err := os.CreateTemp(filepath.Dir(path), ".atm-quota-provider-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// pruneProviderCache forgets providers that are no longer configured, so
// removing one from config.json removes its card instead of leaving a
// placeholder nothing can ever refresh.
func pruneProviderCache(cache *providerCache, configured []string) bool {
	known := make(map[string]bool, len(configured))
	for _, providerID := range configured {
		known[normalizeProviderID(providerID)] = true
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

// rememberProviderCards records what a provider just returned and reports
// whether the file needs rewriting.
func rememberProviderCards(cache *providerCache, providerID string,
	cards []ProviderCard, now time.Time) bool {
	if previous, ok := cache.Providers[providerID]; ok &&
		reflect.DeepEqual(previous.Cards, cards) {
		stamped, err := time.Parse(time.RFC3339, previous.FetchedAt)
		if err == nil && now.Sub(stamped) < providerCacheStampInterval {
			return false
		}
	}
	if cache.Providers == nil {
		cache.Providers = map[string]providerCacheEntry{}
	}
	cache.Providers[providerID] = providerCacheEntry{
		FetchedAt: now.Format(time.RFC3339),
		Cards:     cards,
	}
	return true
}

// providerPlaceholders turns the last known cards into metric-less ones
// that hold their place in the grid. The reading is what went missing, so the
// numbers and the source badge go with it and the observation time stays: the
// card can then say when it was last true.
func providerPlaceholders(cache providerCache, providerID, reason string,
	now time.Time) []ProviderCard {
	entry, ok := cache.Providers[providerID]
	if !ok {
		return nil
	}
	stamped, err := time.Parse(time.RFC3339, entry.FetchedAt)
	if err != nil || now.Sub(stamped) > providerCacheTTL {
		return nil
	}
	placeholders := make([]ProviderCard, 0, len(entry.Cards))
	for _, card := range entry.Cards {
		// An empty slice, not nil: consumers decode `metrics` as a list and a
		// null would fail where "no metrics" has to be readable.
		card.Metrics = []ProviderMetric{}
		card.Source = ""
		card.Unavailable = true
		card.UnavailableReason = reason
		placeholders = append(placeholders, card)
	}
	return placeholders
}
