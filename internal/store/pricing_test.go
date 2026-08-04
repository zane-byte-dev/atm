package store

import "testing"

// withPricingTable replaces the rate table for one test. The live table is
// seeded from ~/.atm/pricing.json at init, so a test asserting on real model
// names would otherwise pass or fail depending on the developer's machine.
func withPricingTable(t *testing.T, table map[string][4]float64) {
	t.Helper()
	old := modelPricing
	modelPricing = table
	t.Cleanup(func() { modelPricing = old })
}

func TestPricingForResolvesExactNormalizedFamilyAndDefault(t *testing.T) {
	withPricingTable(t, map[string][4]float64{
		"codex-auto-review": {2.0, 10.0, 0, 0},
		"claude-opus-4.6":   {5.0, 25.0, 6.25, 0.50},
	})

	cases := []struct {
		name       string
		model      string
		wantSource PricingSource
		wantPrice  [4]float64
	}{{
		name:       "exact table hit",
		model:      "codex-auto-review",
		wantSource: PricingExact,
		wantPrice:  [4]float64{2.0, 10.0, 0, 0},
	}, {
		// The agent logs write claude-opus-4-6; the table keys it with a dot.
		name:       "version normalization hit",
		model:      "claude-opus-4-6",
		wantSource: PricingExact,
		wantPrice:  [4]float64{5.0, 25.0, 6.25, 0.50},
	}, {
		name:       "family keyword fallback",
		model:      "claude-opus-9-unreleased",
		wantSource: PricingFamily,
		wantPrice:  [4]float64{5.0, 25.0, 6.25, 0.50},
	}, {
		name:       "no table and no family",
		model:      "totally-unknown-vendor-x1",
		wantSource: PricingDefault,
		wantPrice:  defaultPricing,
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			price, source := PricingFor(tc.model)
			if source != tc.wantSource {
				t.Fatalf("source = %q, want %q", source, tc.wantSource)
			}
			if price != tc.wantPrice {
				t.Fatalf("price = %v, want %v", price, tc.wantPrice)
			}
			if got, want := source.Estimated(), tc.wantSource != PricingExact; got != want {
				t.Fatalf("Estimated() = %v, want %v", got, want)
			}
		})
	}
}

// TestPricingForKeepsRealMissingModelsOutOfTheOpusDefault pins the models that
// motivated the family layer. Every one of them is a version number that arrived
// after the table was written, and each was being charged at the Opus rate: 43%
// of all recorded spend at the time, with gpt-5.6-sol alone reading $3524
// instead of a few hundred. A static table will keep falling behind, so the
// guarantee worth testing is that a new version number lands on its own family.
func TestPricingForKeepsRealMissingModelsOutOfTheOpusDefault(t *testing.T) {
	withPricingTable(t, map[string][4]float64{
		"gpt-5.5":         {5.0, 30.0, 0, 0.50},
		"claude-opus-4.8": {5.0, 25.0, 6.25, 0.50},
	})
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "claude-opus-5"} {
		price, source := PricingFor(model)
		if source != PricingFamily {
			t.Fatalf("%s: source = %q, want %q", model, source, PricingFamily)
		}
		if price == defaultPricing {
			t.Fatalf("%s: still priced at the Opus default %v", model, price)
		}
	}
	// The families are distinct: a GPT model must not inherit the Claude rate
	// just because both are estimates.
	gpt, _ := PricingFor("gpt-5.6-sol")
	opus, _ := PricingFor("claude-opus-5")
	if gpt == opus {
		t.Fatalf("gpt and opus families resolved to the same rate %v", gpt)
	}
}

// The most specific keyword has to win: gpt-5.1-codex contains both "codex" and
// "gpt", and codex rates are not the flagship GPT rates.
func TestPricingForPrefersTheMoreSpecificFamilyKeyword(t *testing.T) {
	withPricingTable(t, map[string][4]float64{})
	codex, source := PricingFor("gpt-5.9-codex-unreleased")
	if source != PricingFamily {
		t.Fatalf("source = %q, want %q", source, PricingFamily)
	}
	gpt, _ := PricingFor("gpt-5.9-unreleased")
	if codex == gpt {
		t.Fatalf("codex model resolved to the plain gpt rate %v", gpt)
	}
}

// A user rate in ~/.atm/pricing.json (or config, which shares SetPricing) is the
// top of the layering: it makes the model exact, so its cost stops being marked
// as an estimate.
func TestSetPricingMakesAnEstimatedModelExact(t *testing.T) {
	withPricingTable(t, map[string][4]float64{})
	if _, source := PricingFor("gpt-5.6-sol"); source != PricingFamily {
		t.Fatalf("precondition: source = %q, want %q", source, PricingFamily)
	}
	SetPricing(map[string][4]float64{"gpt-5.6-sol": {1.0, 2.0, 0, 0.1}})
	price, source := PricingFor("gpt-5.6-sol")
	if source != PricingExact {
		t.Fatalf("source = %q, want %q", source, PricingExact)
	}
	if source.Estimated() {
		t.Fatal("an overridden rate is still reported as estimated")
	}
	if want := [4]float64{1.0, 2.0, 0, 0.1}; price != want {
		t.Fatalf("price = %v, want %v", price, want)
	}
}

func TestNormalizeModelNameJoinsOnlyVersionRuns(t *testing.T) {
	cases := map[string]string{
		// The case the rate table depends on, and the one that silently did
		// nothing before: every claude model in the transcripts arrives hyphenated.
		"claude-opus-4-6": "claude-opus-4.6",
		"claude-opus-4-8": "claude-opus-4.8",
		// Already dotted, or no version to join: unchanged.
		"gpt-5.6-sol":       "gpt-5.6-sol",
		"claude-opus-5":     "claude-opus-5",
		"codex-auto-review": "codex-auto-review",
		// Numbers that do not touch are not one version number.
		"gpt-51-codex-1113-global": "gpt-51-codex-1113-global",
		// A longer run collapses into a single dotted version.
		"claude-haiku-4-5-20251001": "claude-haiku-4.5.20251001",
	}
	for input, want := range cases {
		if got := normalizeModelName(input); got != want {
			t.Errorf("normalizeModelName(%q) = %q, want %q", input, got, want)
		}
	}
}

// A user entry keyed the way rates are published has to reach a model the
// transcripts name with hyphens, which is only true while the built-in table
// keys agree with the published form.
func TestPricingForAppliesADottedOverrideToAHyphenatedModelName(t *testing.T) {
	withPricingTable(t, map[string][4]float64{"claude-opus-4.6": {15.0, 75.0, 18.75, 1.50}})
	SetPricing(map[string][4]float64{"claude-opus-4.6": {5.0, 25.0, 6.25, 0.50}})
	price, source := PricingFor("claude-opus-4-6")
	if source != PricingExact {
		t.Fatalf("source = %q, want %q", source, PricingExact)
	}
	if want := [4]float64{5.0, 25.0, 6.25, 0.50}; price != want {
		t.Fatalf("price = %v, want %v (built-in rate shadowed the override)", price, want)
	}
}

func TestCalcCostUsesTheResolvedRate(t *testing.T) {
	withPricingTable(t, map[string][4]float64{"known": {1.0, 2.0, 4.0, 8.0}})
	// 1M of each bucket makes the expected total the sum of the rates.
	got := CalcCost("known", 1e6, 1e6, 1e6, 1e6)
	if want := 15.0; got != want {
		t.Fatalf("CalcCost = %v, want %v", got, want)
	}
}
