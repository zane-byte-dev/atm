package apphost

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/background"
	quotaapp "github.com/zane-byte-dev/atm/internal/quota"
)

// projectCompanionQuotaCache adds explicitly refreshed provider and product
// data. Reading runtime/quota.json never launches a provider or refresh job.
func projectCompanionQuotaCache(result *CompanionQuota, cache background.QuotaCache, now time.Time) {
	result.Products = []CompanionQuotaProduct{}
	result.ProviderCards = []CompanionProviderQuotaCard{}
	agents := make([]string, 0, len(cache.Snapshot.Agents))
	for agent := range cache.Snapshot.Agents {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	products := make([]CompanionQuotaProduct, 0)
	cards := make([]CompanionProviderQuotaCard, 0)
	for _, agent := range agents {
		value := cache.Snapshot.Agents[agent]
		if value == nil {
			continue
		}
		observed, err := time.Parse(time.RFC3339, cache.UpdatedAtFor(agent))
		if err != nil || observed.After(now.Add(time.Minute)) {
			continue
		}
		for index := range result.Windows {
			window := &result.Windows[index]
			if window.Agent != agent {
				continue
			}
			selectedAt, selectedErr := time.Parse(time.RFC3339, window.ObservedAt)
			if selectedErr == nil && observed.Before(selectedAt) {
				continue
			}
			window.Source = boundedCompanionText(value.Source, 64)
			window.Plan = boundedCompanionText(value.Plan, 80)
			for _, cachedWindow := range value.Windows() {
				if cachedWindow.WindowMinutes == window.WindowMinutes {
					window.Trend = projectCompanionQuotaTrend(quotaTrendDTO(cachedWindow.Trend))
					break
				}
			}
		}
		for _, product := range value.Products {
			if strings.TrimSpace(product.Name) == "" || !finiteCompanionNumber(product.UsedPercent) {
				continue
			}
			products = append(products, CompanionQuotaProduct{Agent: boundedCompanionText(agent, 100), Product: boundedCompanionText(product.Name, 100), UsedPercent: clampCompanionPercent(product.UsedPercent)})
		}
		for _, card := range value.ProviderCards {
			projected := projectCompanionProviderCard(agent, card)
			if projected.ID != "" {
				cards = append(cards, projected)
			}
		}
	}
	sort.SliceStable(products, func(i, j int) bool {
		if products[i].Agent != products[j].Agent {
			return products[i].Agent < products[j].Agent
		}
		return products[i].Product < products[j].Product
	})
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].Agent != cards[j].Agent {
			return cards[i].Agent < cards[j].Agent
		}
		if cards[i].Provider != cards[j].Provider {
			return cards[i].Provider < cards[j].Provider
		}
		return cards[i].ID < cards[j].ID
	})
	result.ProductsTotal, result.ProviderCardsTotal = len(products), len(cards)
	result.Truncated = result.Truncated || len(products) > companionProductLimit || len(cards) > companionProviderCardLimit
	result.Products = append(result.Products, products[:min(len(products), companionProductLimit)]...)
	result.ProviderCards = append(result.ProviderCards, cards[:min(len(cards), companionProviderCardLimit)]...)
}

func projectCompanionProviderCard(fallbackAgent string, card quotaapp.ProviderCard) CompanionProviderQuotaCard {
	agent := strings.TrimSpace(card.Agent)
	if agent == "" {
		agent = fallbackAgent
	}
	if strings.TrimSpace(card.ID) == "" || strings.TrimSpace(agent) == "" || strings.TrimSpace(card.Title) == "" {
		return CompanionProviderQuotaCard{}
	}
	result := CompanionProviderQuotaCard{ID: boundedCompanionText(card.ID, 80), Agent: boundedCompanionText(agent, 100), Provider: boundedCompanionText(card.Provider, 80), Title: boundedCompanionText(card.Title, 160), Period: boundedCompanionText(card.Period, 100), ObservedAt: boundedCompanionText(card.ObservedAt, 64), Source: boundedCompanionText(card.Source, 80), URL: safeCompanionURL(card.URL), Unavailable: card.Unavailable, Metrics: []CompanionQuotaMetric{}}
	if card.Unavailable {
		result.UnavailableReason = "额度来源暂不可用"
	}
	for _, metric := range card.Metrics {
		if len(result.Metrics) == companionProviderMetricLimit {
			break
		}
		if strings.TrimSpace(metric.ID) == "" || strings.TrimSpace(metric.Label) == "" || !finiteCompanionNumber(metric.Used) || !finiteCompanionNumber(metric.Limit) || !finiteCompanionNumber(metric.UsedPercent) || metric.Used < 0 || metric.Limit <= 0 {
			continue
		}
		result.Metrics = append(result.Metrics, CompanionQuotaMetric{ID: boundedCompanionText(metric.ID, 80), Label: boundedCompanionText(metric.Label, 120), Used: metric.Used, Limit: metric.Limit, UsedPercent: clampCompanionPercent(metric.UsedPercent), Unit: boundedCompanionText(metric.Unit, 40), Currency: boundedCompanionText(metric.Currency, 16), Precision: max(min(metric.Precision, 8), 0)})
	}
	if !result.Unavailable && len(result.Metrics) == 0 {
		return CompanionProviderQuotaCard{}
	}
	return result
}

func safeCompanionURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || len(value) > 2048 {
		return ""
	}
	return value
}
