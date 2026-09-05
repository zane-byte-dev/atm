package config

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
)

// Service is the application boundary for effective settings. Transports pass
// intent here instead of sharing parser tables or coordinating config file
// writes themselves.
type Service struct{}

var Default Service

// Settings is every effective setting the browser workspace reads, after config file
// and environment overrides have been applied.
//
// TextModelAPIKeyConfigured is a fact about the credential rather than a
// setting: the key itself lives in credentials.json and is deliberately never
// serialised, so the only thing anyone outside can learn is whether one is
// present.
type Settings struct {
	OwnerName                      string `json:"owner_name"`
	GrokLiveQuota                  bool   `json:"grok_live_quota"`
	CollectionEnabled              bool   `json:"collection_enabled"`
	CollectionIntervalMinutes      int    `json:"collection_interval_minutes"`
	CollectionLookbackMinutes      int    `json:"collection_lookback_minutes"`
	CollectionMessageRetentionDays int    `json:"collection_message_retention_days"`
	TextModelBaseURL               string `json:"text_model_base_url"`
	TextModelName                  string `json:"text_model_name"`
	TextModelSource                string `json:"text_model_source"`
	TodoRefinePrompt               string `json:"todo_refine_prompt"`
	TodoRefineOnAdd                bool   `json:"todo_refine_on_add"`
	TextModelAPIKeyConfigured      bool   `json:"text_model_api_key_configured"`
}

// SettingsPatch is one atomic settings change. Pointer fields preserve the
// difference between an omitted value and an explicit zero value; in particular
// an empty TodoRefinePrompt restores the default policy.
type SettingsPatch struct {
	OwnerName                      *string `json:"owner_name,omitempty"`
	GrokLiveQuota                  *bool   `json:"grok_live_quota,omitempty"`
	CollectionEnabled              *bool   `json:"collection_enabled,omitempty"`
	CollectionIntervalMinutes      *int    `json:"collection_interval_minutes,omitempty"`
	CollectionLookbackMinutes      *int    `json:"collection_lookback_minutes,omitempty"`
	CollectionMessageRetentionDays *int    `json:"collection_message_retention_days,omitempty"`
	TextModelBaseURL               *string `json:"text_model_base_url,omitempty"`
	TextModelName                  *string `json:"text_model_name,omitempty"`
	TextModelSource                *string `json:"text_model_source,omitempty"`
	TodoRefinePrompt               *string `json:"todo_refine_prompt,omitempty"`
	TodoRefineOnAdd                *bool   `json:"todo_refine_on_add,omitempty"`
}

type settingDefinition struct {
	parse     func(string) (any, error)
	normalize func(any) (any, error)
}

// settingDefinitions is the single registry for readable and writable settings.
// It belongs to the application service rather than either transport so CLI and
// adapters cannot disagree about accepted values.
var settingDefinitions = map[string]settingDefinition{
	"owner_name":                        stringSetting(parseNonEmptyStringValue),
	"grok_live_quota":                   boolSetting(),
	"collection_enabled":                boolSetting(),
	"collection_interval_minutes":       intSetting(parsePositiveIntValue),
	"collection_lookback_minutes":       intSetting(parsePositiveIntValue),
	"collection_message_retention_days": intSetting(parseNonNegativeIntValue),
	"text_model_base_url":               stringSetting(parseHTTPURLValue),
	"text_model_name":                   stringSetting(parseNonEmptyStringValue),
	"text_model_source":                 stringSetting(parseTextModelSourceValue),
	"todo_refine_prompt":                stringSetting(parseTodoRefinePromptValue),
	"todo_refine_on_add":                boolSetting(),
}

func stringSetting(parse func(string) (any, error)) settingDefinition {
	return settingDefinition{
		parse: parse,
		normalize: func(value any) (any, error) {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("expected a string, got %T", value)
			}
			return parse(text)
		},
	}
}

func boolSetting() settingDefinition {
	return settingDefinition{
		parse: parseBoolValue,
		normalize: func(value any) (any, error) {
			boolean, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("expected a boolean, got %T", value)
			}
			return boolean, nil
		},
	}
}

func intSetting(parse func(string) (any, error)) settingDefinition {
	return settingDefinition{
		parse: parse,
		normalize: func(value any) (any, error) {
			integer, ok := value.(int)
			if !ok {
				return nil, fmt.Errorf("expected an integer, got %T", value)
			}
			return parse(strconv.Itoa(integer))
		},
	}
}

// Snapshot reads every effective setting plus whether a text model credential
// is present. An unreadable credentials.json is an error rather than a false:
// reporting "no key configured" could make the settings screen overwrite one.
func (service Service) Snapshot() (Settings, error) {
	credential, err := service.CredentialStatus()
	if err != nil {
		return Settings{}, err
	}
	return Settings{
		OwnerName:                      OwnerName,
		GrokLiveQuota:                  GrokLiveQuota,
		CollectionEnabled:              CollectionEnabled,
		CollectionIntervalMinutes:      CollectionIntervalMinutes,
		CollectionLookbackMinutes:      CollectionLookbackMinutes,
		CollectionMessageRetentionDays: CollectionMessageRetentionDays,
		TextModelBaseURL:               TextModelBaseURL,
		TextModelName:                  TextModelName,
		TextModelSource:                TextModelSource,
		TodoRefinePrompt:               TodoRefinePrompt,
		TodoRefineOnAdd:                TodoRefineOnAdd,
		TextModelAPIKeyConfigured:      credential.Configured,
	}, nil
}

// Apply validates an entire typed patch before writing any field, persists it in
// one load-modify-save cycle, and returns the effective state after env overrides.
func (service Service) Apply(patch SettingsPatch) (Settings, error) {
	values := patch.values()
	if len(values) == 0 {
		return Settings{}, application.NewError(application.CodeInvalidArgument, "no settings given")
	}
	normalized, err := normalizeSettings(values)
	if err != nil {
		return Settings{}, err
	}
	if err := writeSettings(normalized); err != nil {
		return Settings{}, err
	}
	return service.Snapshot()
}

// Set parses and writes one CLI string value using the same registry Apply uses.
// The returned value is the normalized value that was persisted, for rendering.
func (Service) Set(key, raw string) (any, error) {
	value, err := parseSetting(key, raw)
	if err != nil {
		return nil, err
	}
	if err := writeSettings(map[string]any{key: value}); err != nil {
		return nil, err
	}
	return value, nil
}

// Get returns one effective value, including environment overrides.
func (service Service) Get(key string) (any, error) {
	settings, err := service.Snapshot()
	if err != nil {
		return nil, err
	}
	value, ok := SettingValues(settings)[key]
	if !ok {
		return nil, unknownSettingError(key, "unknown key", "readable")
	}
	return value, nil
}

func parseSetting(key, raw string) (any, error) {
	definition, ok := settingDefinitions[key]
	if !ok {
		return nil, unknownSettingError(key, "unknown or non-settable key", "settable")
	}
	value, err := definition.parse(raw)
	if err != nil {
		return nil, invalidSettingValueError(key, err)
	}
	return value, nil
}

func SettableKeys() []string {
	names := make([]string, 0, len(settingDefinitions))
	for name := range settingDefinitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (patch SettingsPatch) values() map[string]any {
	values := map[string]any{}
	add := func(key string, value any, present bool) {
		if present {
			values[key] = value
		}
	}
	add("owner_name", dereference(patch.OwnerName), patch.OwnerName != nil)
	add("grok_live_quota", dereference(patch.GrokLiveQuota), patch.GrokLiveQuota != nil)
	add("collection_enabled", dereference(patch.CollectionEnabled), patch.CollectionEnabled != nil)
	add("collection_interval_minutes", dereference(patch.CollectionIntervalMinutes), patch.CollectionIntervalMinutes != nil)
	add("collection_lookback_minutes", dereference(patch.CollectionLookbackMinutes), patch.CollectionLookbackMinutes != nil)
	add("collection_message_retention_days", dereference(patch.CollectionMessageRetentionDays), patch.CollectionMessageRetentionDays != nil)
	add("text_model_base_url", dereference(patch.TextModelBaseURL), patch.TextModelBaseURL != nil)
	add("text_model_name", dereference(patch.TextModelName), patch.TextModelName != nil)
	add("text_model_source", dereference(patch.TextModelSource), patch.TextModelSource != nil)
	add("todo_refine_prompt", dereference(patch.TodoRefinePrompt), patch.TodoRefinePrompt != nil)
	add("todo_refine_on_add", dereference(patch.TodoRefineOnAdd), patch.TodoRefineOnAdd != nil)
	return values
}

func dereference[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizeSettings(values map[string]any) (map[string]any, error) {
	normalized := make(map[string]any, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		definition, ok := settingDefinitions[key]
		if !ok {
			return nil, unknownSettingError(key, "unknown or non-settable key", "settable")
		}
		parsed, err := definition.normalize(value)
		if err != nil {
			return nil, invalidSettingValueError(key, err)
		}
		normalized[key] = parsed
	}
	return normalized, nil
}

func unknownSettingError(key, message, listLabel string) *application.Error {
	appErr := application.NewError(application.CodeInvalidArgument,
		fmt.Sprintf("%s: %s (%s: %s)", message, key, listLabel, strings.Join(SettableKeys(), ", ")))
	appErr.Details = map[string]any{"field": key, "settable": SettableKeys()}
	return appErr
}

func invalidSettingValueError(key string, cause error) *application.Error {
	appErr := application.WrapError(application.CodeInvalidArgument,
		fmt.Sprintf("invalid value for %s: %v", key, cause), cause)
	appErr.Details = map[string]any{"field": key}
	return appErr
}

func writeSettings(values map[string]any) error {
	if len(values) == 0 {
		return nil
	}
	if err := mutateRawConfig(func(raw map[string]any) error {
		for key, value := range values {
			raw[key] = value
		}
		return nil
	}); err != nil {
		return err
	}
	// A save-and-snapshot request must observe the new file in this process. This
	// also reapplies environment overrides, which correctly remain authoritative.
	LoadConfig()
	return nil
}

// SettingValues projects effective settings by key. Credential presence is left
// out because it is readable but deliberately not settable.
func SettingValues(settings Settings) map[string]any {
	return map[string]any{
		"owner_name":                        settings.OwnerName,
		"grok_live_quota":                   settings.GrokLiveQuota,
		"collection_enabled":                settings.CollectionEnabled,
		"collection_interval_minutes":       settings.CollectionIntervalMinutes,
		"collection_lookback_minutes":       settings.CollectionLookbackMinutes,
		"collection_message_retention_days": settings.CollectionMessageRetentionDays,
		"text_model_base_url":               settings.TextModelBaseURL,
		"text_model_name":                   settings.TextModelName,
		"text_model_source":                 settings.TextModelSource,
		"todo_refine_prompt":                settings.TodoRefinePrompt,
		"todo_refine_on_add":                settings.TodoRefineOnAdd,
	}
}

func parseTextModelSourceValue(value string) (any, error) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return nil, fmt.Errorf("source label must not be empty")
	}
	if utf8.RuneCountInString(value) > 80 {
		return nil, fmt.Errorf("source label must be at most 80 characters")
	}
	return value, nil
}

func parseTodoRefinePromptValue(value string) (any, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 4000 {
		return nil, fmt.Errorf("todo refine prompt must be at most 4000 characters")
	}
	return value, nil
}

func parseHTTPURLValue(value string) (any, error) {
	normalized := strings.TrimRight(strings.TrimSpace(value), "/")
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("expected an http or https URL, got %q", value)
	}
	return normalized, nil
}

func parseBoolValue(value string) (any, error) {
	switch strings.ToLower(value) {
	case "true", "1", "on", "yes":
		return true, nil
	case "false", "0", "off", "no":
		return false, nil
	default:
		return nil, fmt.Errorf("expected true or false, got %q", value)
	}
}

func parsePositiveIntValue(value string) (any, error) {
	integer, err := strconv.Atoi(value)
	if err != nil || integer < 1 {
		return nil, fmt.Errorf("expected a positive integer, got %q", value)
	}
	return integer, nil
}

func parseNonNegativeIntValue(value string) (any, error) {
	integer, err := strconv.Atoi(value)
	if err != nil || integer < 0 {
		return nil, fmt.Errorf("expected zero or a positive integer, got %q", value)
	}
	return integer, nil
}

func parseNonEmptyStringValue(value string) (any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("value must not be empty")
	}
	return value, nil
}
