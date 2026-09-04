package config

import "time"

// ResetBusinessDefaults clears one account's mutable business configuration
// before loading a complete replacement snapshot. It deliberately preserves
// HOME, Agent transcript source paths and the explicitly selected ATM data path.
// Resident callers hold their configuration gate exclusively; CLI callers use
// it during startup only.
func ResetBusinessDefaults() {
	Loc = time.FixedZone("CST", 8*3600)
	Pricing, Subscriptions, ProjectAliases = nil, nil, nil
	OwnerName = ""
	GrokLiveQuota, CollectionEnabled, TodoRefineOnAdd = false, false, false
	CollectionIntervalMinutes, CollectionLookbackMinutes, CollectionMessageRetentionDays = 5, 60, 90
	CollectionDigestCollection, CollectionDigestIntervalMinutes = "inbox", 60
	CollectionConnectors, QuotaProviders = nil, nil
	Guard = GuardConfig{}
	TextModelBaseURL, TextModelName, TextModelSource = "https://api.deepseek.com", "deepseek-v4-flash", "deepseek"
	TodoRefinePrompt = DefaultTodoRefinePrompt
}
