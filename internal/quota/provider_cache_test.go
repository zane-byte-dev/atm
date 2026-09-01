package quota

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/logging"
)

func withQuotaProviders(t *testing.T, providers map[string]config.QuotaProviderConfig) {
	t.Helper()
	previous := config.QuotaProviders
	config.QuotaProviders = providers
	t.Cleanup(func() { config.QuotaProviders = previous })
}

func TestLoadQuotaProviderCardsHoldsTheCardWhenAProviderReportsNothing(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")

	withQuotaProviders(t, map[string]config.QuotaProviderConfig{
		"example": quotaProviderHelperConfig("success"),
	})
	if cards, errs := loadProviderCards(context.Background()); len(errs) != 0 ||
		len(cards["claude"]) != 1 {
		t.Fatalf("first run: cards = %#v, errs = %v", cards, errs)
	}

	withQuotaProviders(t, map[string]config.QuotaProviderConfig{
		"example": quotaProviderHelperConfig("empty"),
	})
	cards, errs := loadProviderCards(context.Background())
	if len(errs) != 0 {
		t.Fatalf("nothing to report is not a failure: %v", errs)
	}
	if len(cards["claude"]) != 1 {
		t.Fatalf("the card left the grid: %#v", cards)
	}
	card := cards["claude"][0]
	if !card.Unavailable || card.UnavailableReason != ProviderReasonEmpty {
		t.Fatalf("placeholder = %#v", card)
	}
	// Identity stays so the card is recognisable; the reading and the source
	// badge go, because neither is true any more.
	if card.ID != "daily" || card.Title != "Plan" || card.ObservedAt != "2026-08-04T03:28:37Z" {
		t.Fatalf("placeholder lost its identity: %#v", card)
	}
	if len(card.Metrics) != 0 || card.Source != "" {
		t.Fatalf("placeholder kept a reading: %#v", card)
	}
	// The link outlives the reading: a card with no numbers is exactly when
	// "where do I go to refresh this" is worth a click.
	if card.URL != "https://example.com/account" {
		t.Fatalf("placeholder dropped its page: %#v", card)
	}
}

func TestLoadQuotaProviderCardsHoldsTheCardWhenAProviderFails(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")

	withQuotaProviders(t, map[string]config.QuotaProviderConfig{
		"example": quotaProviderHelperConfig("success"),
	})
	if _, errs := loadProviderCards(context.Background()); len(errs) != 0 {
		t.Fatalf("first run: %v", errs)
	}

	withQuotaProviders(t, map[string]config.QuotaProviderConfig{
		"example": quotaProviderHelperConfig("error"),
	})
	cards, errs := loadProviderCards(context.Background())
	// The warning still reaches stderr — the placeholder replaces the missing
	// card, not the report of why it is missing.
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if len(cards["claude"]) != 1 ||
		cards["claude"][0].UnavailableReason != ProviderReasonError {
		t.Fatalf("placeholder = %#v", cards)
	}
}

func TestLoadQuotaProviderCardsWithoutAnythingRememberedShowsNoCard(t *testing.T) {
	withTempAtmDir(t)
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	withQuotaProviders(t, map[string]config.QuotaProviderConfig{
		"example": quotaProviderHelperConfig("empty"),
	})
	cards, errs := loadProviderCards(context.Background())
	if len(cards) != 0 || len(errs) != 0 {
		t.Fatalf("cards = %#v, errs = %v", cards, errs)
	}
}

func quotaProviderCacheFixture(fetchedAt time.Time) providerCache {
	return providerCache{
		Version: providerCacheVersion,
		Providers: map[string]providerCacheEntry{
			"example": {
				FetchedAt: fetchedAt.Format(time.RFC3339),
				Cards: []ProviderCard{{
					ID: "daily", Agent: "claude", Provider: "example", Title: "Plan",
					ObservedAt: "2026-08-04T03:28:37Z", Source: "browser",
					Metrics: []ProviderMetric{{
						ID: "count", Label: "Count", Used: 25, Limit: 100, UsedPercent: 25,
					}},
				}},
			},
		},
	}
}

func TestQuotaProviderPlaceholdersStopAfterTheProviderGoesQuietForTooLong(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	cache := quotaProviderCacheFixture(now.Add(-providerCacheTTL + time.Hour))
	if got := providerPlaceholders(cache, "example", ProviderReasonEmpty, now); len(got) != 1 {
		t.Fatalf("within the window = %#v", got)
	}
	cache = quotaProviderCacheFixture(now.Add(-providerCacheTTL - time.Hour))
	if got := providerPlaceholders(cache, "example", ProviderReasonEmpty, now); len(got) != 0 {
		t.Fatalf("past the window = %#v", got)
	}
}

func TestRememberQuotaProviderCardsSkipsUnchangedRewrites(t *testing.T) {
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	cache := quotaProviderCacheFixture(now.Add(-time.Minute))
	cards := cache.Providers["example"].Cards
	if rememberProviderCards(&cache, "example", cards, now) {
		t.Fatal("an unchanged reading must not rewrite the cache on every refresh")
	}
	// Past the stamp interval the freshness marker is what changed, and the TTL
	// reads it, so the file has to be written.
	stale := quotaProviderCacheFixture(now.Add(-providerCacheStampInterval - time.Minute))
	if !rememberProviderCards(&stale, "example", cards, now) {
		t.Fatal("a stale stamp must be refreshed")
	}
	changed := append([]ProviderCard{}, cards...)
	changed[0].Title = "Other plan"
	if !rememberProviderCards(&cache, "example", changed, now) {
		t.Fatal("a changed card must be written")
	}
}

func TestQuotaProviderCacheRoundTripsAndPrunes(t *testing.T) {
	withTempAtmDir(t)
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	saveProviderCache(quotaProviderCacheFixture(now))

	cache := loadProviderCache()
	if len(cache.Providers["example"].Cards) != 1 {
		t.Fatalf("round trip = %#v", cache)
	}
	// A provider dropped from config.json must not leave a placeholder behind
	// that nothing can ever refresh.
	if !pruneProviderCache(&cache, []string{"other"}) || len(cache.Providers) != 0 {
		t.Fatalf("pruned cache = %#v", cache)
	}
}

// TestSaveQuotaProviderCacheLogsAWriteFailure covers the failure the atomic write
// deliberately hides from its caller: `atm quota` still has a reading to print,
// so a cache that could not be replaced has to reach the log instead of nowhere.
func TestSaveQuotaProviderCacheLogsAWriteFailure(t *testing.T) {
	withTempAtmDir(t)
	// A rename cannot replace a directory, so the write fails at the last step,
	// with the temporary file already written.
	if err := os.Mkdir(providerCachePath(), 0o755); err != nil {
		t.Fatalf("seed blocker: %v", err)
	}

	saveProviderCache(quotaProviderCacheFixture(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)))

	lines, err := logging.Tail(logging.Path(), 0)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logged := false
	for _, line := range lines {
		if strings.Contains(line, "quota_provider_cache_not_saved") {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("write failure never reached the log: %v", lines)
	}
}

func TestLoadQuotaProviderCacheDiscardsUnusableEntries(t *testing.T) {
	withTempAtmDir(t)
	// Hand-edited or half-written files must degrade to "no placeholder", never
	// to a card the App cannot place.
	data := []byte(`{"version":1,"providers":{"example":{"fetched_at":"2026-08-05T09:00:00Z",` +
		`"cards":[{"id":"daily","agent":"","provider":"example","title":"Plan","metrics":[]}]}}}`)
	if err := os.WriteFile(providerCachePath(), data, 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	if cache := loadProviderCache(); len(cache.Providers) != 0 {
		t.Fatalf("cache = %#v", cache)
	}
}
