package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestQuotaProviderHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_QUOTA_PROVIDER_HELPER") != "1" {
		return
	}
	mode := "success"
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
			break
		}
	}
	switch mode {
	case "empty":
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[]}`)
	case "invalid":
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"claude","title":"Plan","observed_at":"2026-08-04T03:28:37Z","metrics":[{"id":"count","label":"Count","used":1,"limit":0}]}]}`)
	case "error":
		fmt.Fprintln(os.Stderr, "provider unavailable")
		os.Exit(2)
	case "badurl":
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"claude","title":"Plan","observed_at":"2026-08-04T03:28:37Z","url":"file:///etc/passwd","metrics":[{"id":"count","label":"Count","used":1,"limit":10}]}]}`)
	default:
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"Claude","title":"Plan","period":"today","observed_at":"2026-08-04T03:28:37Z","source":"browser","url":"https://example.com/account","metrics":[{"id":"count","label":"Count","used":25,"limit":100,"unit":"requests"},{"id":"amount","label":"Amount","used":12.5,"limit":50,"currency":"CNY","precision":2}]}]}`)
	}
	os.Exit(0)
}

func quotaProviderHelperConfig(mode string) config.QuotaProviderConfig {
	return config.QuotaProviderConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestQuotaProviderHelperProcess", "--", mode},
	}
}

func TestCallQuotaProviderNormalizesCardsAndComputesPercent(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	cards, err := callQuotaProvider(context.Background(), "example", quotaProviderHelperConfig("success"))
	if err != nil {
		t.Fatalf("callQuotaProvider: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %#v", cards)
	}
	card := cards[0]
	if card.Agent != "claude" || card.Provider != "example" || card.ID != "daily" {
		t.Fatalf("normalized card = %#v", card)
	}
	if got := card.Metrics[0].UsedPercent; got != 25 {
		t.Fatalf("used percent = %v", got)
	}
	if card.URL != "https://example.com/account" {
		t.Fatalf("card url = %q", card.URL)
	}
}

// The App opens this URL in the browser, so a scheme it should never launch has
// to be refused before it reaches either the card or the on-disk cache.
func TestCallQuotaProviderRejectsANonHTTPCardURL(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	_, err := callQuotaProvider(context.Background(), "example", quotaProviderHelperConfig("badurl"))
	if err == nil || !strings.Contains(err.Error(), "must be an absolute http(s) URL") {
		t.Fatalf("error = %v", err)
	}
}

func TestCallQuotaProviderFiltersVisibleMetrics(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	providerConfig := quotaProviderHelperConfig("success")
	providerConfig.VisibleMetrics = []string{" Amount "}
	cards, err := callQuotaProvider(context.Background(), "example", providerConfig)
	if err != nil {
		t.Fatalf("callQuotaProvider: %v", err)
	}
	if len(cards) != 1 || len(cards[0].Metrics) != 1 || cards[0].Metrics[0].ID != "amount" {
		t.Fatalf("filtered cards = %#v", cards)
	}
}

// A provider with nothing to report is not a misconfigured one: this printed a
// visible_metrics warning on every run until the day's first observation landed.
func TestCallQuotaProviderAcceptsAnEmptyResponseWithVisibleMetrics(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	providerConfig := quotaProviderHelperConfig("empty")
	providerConfig.VisibleMetrics = []string{"amount"}
	cards, err := callQuotaProvider(context.Background(), "example", providerConfig)
	if err != nil || len(cards) != 0 {
		t.Fatalf("cards = %#v, err = %v", cards, err)
	}
}

func TestCallQuotaProviderRejectsUnknownVisibleMetrics(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	providerConfig := quotaProviderHelperConfig("success")
	providerConfig.VisibleMetrics = []string{"missing"}
	_, err := callQuotaProvider(context.Background(), "example", providerConfig)
	if err == nil || err.Error() != "quota provider example visible_metrics matched no returned metrics" {
		t.Fatalf("error = %v", err)
	}
}

func TestCallQuotaProviderRejectsInvalidMetricBounds(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	_, err := callQuotaProvider(context.Background(), "example", quotaProviderHelperConfig("invalid"))
	if err == nil {
		t.Fatal("invalid metric limit should fail")
	}
}

func TestCallQuotaProviderReportsProviderStderr(t *testing.T) {
	t.Setenv("GO_WANT_QUOTA_PROVIDER_HELPER", "1")
	_, err := callQuotaProvider(context.Background(), "example", quotaProviderHelperConfig("error"))
	if err == nil || err.Error() != "quota provider example failed: provider unavailable" {
		t.Fatalf("error = %v", err)
	}
}
