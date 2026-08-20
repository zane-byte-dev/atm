package config

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestParseSettingValues(t *testing.T) {
	for _, testCase := range []struct {
		key  string
		raw  string
		want any
	}{
		{"grok_live_quota", "TRUE", true},
		{"collection_enabled", "off", false},
		{"collection_interval_minutes", "5", 5},
		{"collection_message_retention_days", "0", 0},
		{"text_model_base_url", " http://localhost:8080/v1/ ", "http://localhost:8080/v1"},
		{"text_model_source", "  company   gateway  ", "company gateway"},
		{"todo_refine_prompt", "\nPrefer observable acceptance criteria.\n", "Prefer observable acceptance criteria."},
	} {
		t.Run(testCase.key+"/"+testCase.raw, func(t *testing.T) {
			got, err := parseSetting(testCase.key, testCase.raw)
			if err != nil || !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("parseSetting(%q, %q) = %#v, %v; want %#v", testCase.key, testCase.raw, got, err, testCase.want)
			}
		})
	}

	for _, testCase := range []struct {
		key string
		raw string
	}{
		{"grok_live_quota", "maybe"},
		{"collection_interval_minutes", "0"},
		{"collection_message_retention_days", "-1"},
		{"text_model_base_url", "ftp://example.com"},
		{"text_model_name", "  "},
		{"text_model_source", strings.Repeat("x", 81)},
		{"todo_refine_prompt", strings.Repeat("x", 4001)},
		{"unknown", "value"},
	} {
		t.Run("reject/"+testCase.key, func(t *testing.T) {
			if _, err := parseSetting(testCase.key, testCase.raw); err == nil {
				t.Fatalf("parseSetting(%q, %q) should fail", testCase.key, testCase.raw)
			} else if !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("parseSetting error = %v, want invalid_argument", err)
			}
		})
	}
}

func TestSettableKeysMatchReadableSettings(t *testing.T) {
	keys := SettableKeys()
	if !slices.IsSorted(keys) {
		t.Fatalf("SettableKeys is not sorted: %v", keys)
	}
	readable := SettingValues(Settings{})
	if len(keys) != len(readable) {
		t.Fatalf("settable=%d readable=%d", len(keys), len(readable))
	}
	for _, key := range keys {
		if _, ok := readable[key]; !ok {
			t.Errorf("settable key %q is not readable", key)
		}
	}
}

func TestSettingsPatchJSONUsesProtocolFieldNames(t *testing.T) {
	var patch SettingsPatch
	if err := json.Unmarshal([]byte(`{"owner_name":"MJ","todo_refine_on_add":true}`), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.OwnerName == nil || *patch.OwnerName != "MJ" || patch.TodoRefineOnAdd == nil || !*patch.TodoRefineOnAdd {
		t.Fatalf("decoded patch = %+v", patch)
	}
}

func TestServiceSetAndGetUseCanonicalValidation(t *testing.T) {
	withTempConfigHome(t)
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"future_setting":{"kept":true}}`), 0600); err != nil {
		t.Fatal(err)
	}

	value, err := Default.Set("text_model_base_url", "https://models.example/v1/")
	if err != nil || value != "https://models.example/v1" {
		t.Fatalf("Set = %#v, %v", value, err)
	}
	got, err := Default.Get("text_model_base_url")
	if err != nil || got != "https://models.example/v1" {
		t.Fatalf("Get = %#v, %v", got, err)
	}
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"future_setting"`) {
		t.Fatalf("unknown field was dropped:\n%s", data)
	}
	if _, err := Default.Set("text_model_name", "  "); err == nil || !strings.Contains(err.Error(), "text_model_name") {
		t.Fatalf("blank model error = %v", err)
	}
	if _, err := Default.Get("unknown"); err == nil || !strings.Contains(err.Error(), "readable:") {
		t.Fatalf("unknown get error = %v", err)
	}
}

func TestServiceApplyIsAtomicAndReturnsEffectiveSnapshot(t *testing.T) {
	withTempConfigHome(t)
	if err := os.MkdirAll(AtmDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath, []byte(`{"text_model_source":"original","text_model_name":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	LoadConfig()

	changed := "changed"
	badURL := "not-a-url"
	if _, err := Default.Apply(SettingsPatch{TextModelSource: &changed, TextModelBaseURL: &badURL}); err == nil {
		t.Fatal("invalid patch was accepted")
	} else if !strings.Contains(err.Error(), "text_model_base_url") {
		t.Fatalf("error does not identify field: %v", err)
	}
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "changed") {
		t.Fatalf("rejected patch partially wrote:\n%s", data)
	}

	model := "new-model"
	source := "  company   gateway  "
	refine := true
	snapshot, err := Default.Apply(SettingsPatch{
		TextModelName:   &model,
		TextModelSource: &source,
		TodoRefineOnAdd: &refine,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TextModelName != "new-model" || snapshot.TextModelSource != "company gateway" || !snapshot.TodoRefineOnAdd {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if _, err := Default.Apply(SettingsPatch{}); err == nil || !strings.Contains(err.Error(), "no settings given") ||
		!errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty patch error = %v", err)
	}
}

func TestSetConfigValueRejectsWrongTypedValuesBeforeWrite(t *testing.T) {
	withTempConfigHome(t)
	if err := SetConfigValue("collection_interval_minutes", "five"); err == nil {
		t.Fatal("wrong typed value was accepted")
	}
	if _, err := os.Stat(ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("invalid batch created config: %v", err)
	}
}
