package cmd

import (
	"context"
	"fmt"
	"os"
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
	case "invalid":
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"claude","title":"Plan","observed_at":"2026-08-04T03:28:37Z","metrics":[{"id":"count","label":"Count","used":1,"limit":0}]}]}`)
	case "error":
		fmt.Fprintln(os.Stderr, "provider unavailable")
		os.Exit(2)
	default:
		fmt.Fprint(os.Stdout, `{"version":1,"cards":[{"id":"daily","agent":"Claude","title":"Plan","period":"today","observed_at":"2026-08-04T03:28:37Z","source":"browser","metrics":[{"id":"count","label":"Count","used":25,"limit":100,"unit":"requests"}]}]}`)
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
