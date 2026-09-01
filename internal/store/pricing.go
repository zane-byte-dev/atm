package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/zane-byte-dev/atm/internal/config"
)

// modelPricing is the built-in rate table, merged with the user's
// ~/.atm/pricing.json at init. Keys are written in the dotted form rates are
// published in, never the hyphenated form transcripts use, so that a user entry
// and a built-in entry for the same model collide on one key and the user's wins.
// A hyphenated built-in key would instead be matched before normalization ever
// ran and would silently shadow the override; see normalizeModelName.
var modelPricing = map[string][4]float64{
	// [input, output, cache_create, cache_read] per million tokens
	"claude-opus-4.6":           {15.0, 75.0, 18.75, 1.50},
	"claude-sonnet-4.6":         {3.0, 15.0, 3.75, 0.30},
	"claude-haiku-4-5-20251001": {0.80, 4.0, 1.0, 0.08},
	"gpt-5.5":                   {2.0, 10.0, 0, 0},
	"gpt-51-codex-1113-global":  {2.0, 10.0, 0, 0},
	"codex-auto-review":         {2.0, 10.0, 0, 0},
	"deepseek-v4-pro":           {2.19, 8.87, 0, 0},
	// Grok Build models; override via ~/.atm/pricing.json when public rates change.
	"grok-4.5":       {3.0, 15.0, 0, 0.75},
	"grok-4.5-build": {3.0, 15.0, 0, 0.75},
}

var defaultPricing = [4]float64{15.0, 75.0, 18.75, 1.50}

// familyPricing anchors a whole model family to one representative rate, for the
// models the table has never heard of. New version numbers arrive faster than any
// static table is updated — gpt-5.6-*, claude-opus-5 and friends all showed up
// this way — and pricing every one of them at the Opus rate in defaultPricing
// puts a cheap model's spend off by an order of magnitude, not a few percent.
//
// These are deliberately coarse: an anchor is the family's current flagship tier,
// close enough to keep a total in the right decade, never a quote. Anything
// resolved here is reported as PricingFamily so the number can be shown as the
// estimate it is, and ~/.atm/pricing.json still overrides it exactly.
//
// Order matters, first substring wins: "codex" sits before "gpt" because
// gpt-5.1-codex contains both, and the specific claude tiers sit before nothing
// at all — no other family's names contain them.
var familyPricing = []struct {
	keyword string
	price   [4]float64
}{
	{"opus", [4]float64{5.0, 25.0, 6.25, 0.50}},
	{"sonnet", [4]float64{3.0, 15.0, 3.75, 0.30}},
	{"haiku", [4]float64{1.0, 5.0, 1.25, 0.10}},
	{"fable", [4]float64{10.0, 50.0, 12.5, 1.0}},
	{"codex", [4]float64{1.75, 14.0, 0, 0.175}},
	{"gpt", [4]float64{2.50, 15.0, 0, 0.25}},
	{"gemini", [4]float64{2.0, 12.0, 0.375, 0.20}},
	{"grok", [4]float64{1.25, 2.50, 0, 0.20}},
	{"qwen", [4]float64{0.78, 3.90, 0.975, 0.156}},
	{"deepseek", [4]float64{0.435, 0.87, 0, 0.0036}},
	{"glm", [4]float64{0.60, 1.92, 0, 0.12}},
	{"kimi", [4]float64{0.55, 3.20, 0, 0.11}},
	{"minimax", [4]float64{0.30, 1.20, 0, 0.06}},
	{"mistral", [4]float64{0.50, 1.50, 0, 0.05}},
	{"llama", [4]float64{0.40, 0.40, 0, 0}},
}

// PricingSource records how a model's rate was resolved. Cost built on anything
// but PricingExact is a guess ATM made, and every surface that shows money is
// expected to say so rather than let it read as a quote.
type PricingSource string

const (
	// PricingExact means the model itself is in the table, directly or after
	// version normalization.
	PricingExact PricingSource = "exact"
	// PricingFamily means only the model's family was recognized.
	PricingFamily PricingSource = "family"
	// PricingDefault means nothing matched and the rate is the conservative
	// Opus-tier upper bound in defaultPricing.
	PricingDefault PricingSource = "default"
	// PricingMixed means an aggregate row combines requests resolved through
	// more than one pricing source. CostEstimated and EstimatedCostUSD carry the
	// actionable confidence signal for that row.
	PricingMixed PricingSource = "mixed"
)

// Estimated reports whether a cost computed from this source is ATM's guess
// rather than a rate it knows.
func (s PricingSource) Estimated() bool { return s != PricingExact }

func init() {
	loadPricingFile()
}

func loadPricingFile() {
	path := filepath.Join(config.AtmDir, "pricing.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var prices map[string][4]float64
	if json.Unmarshal(data, &prices) == nil {
		for model, p := range prices {
			modelPricing[model] = p
		}
	}
}

// SetPricing merges user-supplied per-model pricing overrides from config.
// Each entry is [input, output, cache_create, cache_read] USD per million tokens.
func SetPricing(overrides map[string][4]float64) {
	for model, p := range overrides {
		modelPricing[model] = p
	}
}

// PricingFor reports the rate ATM will charge a model at and how it got there.
// Callers that display the resulting money use the source to mark an estimate;
// see PricingSource.
func PricingFor(model string) ([4]float64, PricingSource) {
	return lookupPricing(model)
}

func lookupPricing(model string) ([4]float64, PricingSource) {
	if p, ok := modelPricing[model]; ok {
		return p, PricingExact
	}
	// Try normalizing: claude-opus-4-6 -> claude-opus-4.6
	normalized := normalizeModelName(model)
	if p, ok := modelPricing[normalized]; ok {
		return p, PricingExact
	}
	// Both the raw and normalized names are matched against family keywords: the
	// name that misses the table is the one the user will see, and normalization
	// only ever rewrites a trailing version number, so neither form is a more
	// reliable carrier of the family than the other.
	lower := strings.ToLower(model)
	lowerNormalized := strings.ToLower(normalized)
	for _, f := range familyPricing {
		if strings.Contains(lower, f.keyword) || strings.Contains(lowerNormalized, f.keyword) {
			return f.price, PricingFamily
		}
	}
	return defaultPricing, PricingDefault
}

// normalizeModelName rewrites a hyphen-separated version number into the dotted
// form rates are published in: claude-opus-4-6 -> claude-opus-4.6. Transcripts
// disagree with rate tables on this, and only on this.
//
// A run of adjacent numeric segments is one version number, so only runs are
// joined: gpt-51-codex-1113-global keeps its hyphens because no two of its
// numbers touch, and gpt-5.6-sol is already dotted and comes back unchanged.
func normalizeModelName(name string) string {
	parts := strings.Split(name, "-")
	var out []string
	for i, part := range parts {
		if i > 0 && startsWithDigit(part) && startsWithDigit(parts[i-1]) {
			// Extend the version number already being built.
			out[len(out)-1] += "." + part
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, "-")
}

func startsWithDigit(s string) bool { return s != "" && s[0] >= '0' && s[0] <= '9' }

func CalcCost(model string, input, output, cacheCreate, cacheRead int64) float64 {
	p, _ := lookupPricing(model)
	return float64(input)*p[0]/1e6 +
		float64(output)*p[1]/1e6 +
		float64(cacheCreate)*p[2]/1e6 +
		float64(cacheRead)*p[3]/1e6
}
