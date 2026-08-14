package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/config"
)

func TestParseBoolValue(t *testing.T) {
	trues := []string{"true", "TRUE", "1", "on", "yes"}
	falses := []string{"false", "FALSE", "0", "off", "no"}
	for _, s := range trues {
		v, err := parseBoolValue(s)
		if err != nil || v != true {
			t.Fatalf("parseBoolValue(%q) = %v, %v", s, v, err)
		}
	}
	for _, s := range falses {
		v, err := parseBoolValue(s)
		if err != nil || v != false {
			t.Fatalf("parseBoolValue(%q) = %v, %v", s, v, err)
		}
	}
	for _, s := range []string{"", "maybe", "2", "enable"} {
		if _, err := parseBoolValue(s); err == nil {
			t.Fatalf("parseBoolValue(%q) should fail", s)
		}
	}
}

func TestParseCollectionConfigValues(t *testing.T) {
	for _, value := range []string{"1", "5", "60"} {
		parsed, err := parsePositiveIntValue(value)
		if err != nil || parsed == nil {
			t.Fatalf("parsePositiveIntValue(%q) = %v, %v", value, parsed, err)
		}
	}
	for _, value := range []string{"", "0", "-1", "five"} {
		if _, err := parsePositiveIntValue(value); err == nil {
			t.Fatalf("parsePositiveIntValue(%q) should fail", value)
		}
	}
	if _, err := parseNonEmptyStringValue("  "); err == nil {
		t.Fatal("blank collection command/path should fail")
	}
	if value, err := parseNonEmptyStringValue("codex"); err != nil || value != "codex" {
		t.Fatalf("parseNonEmptyStringValue = %v, %v", value, err)
	}
}

func TestParseTextModelBaseURL(t *testing.T) {
	for _, value := range []string{"https://api.deepseek.com", "http://localhost:8080/v1/"} {
		parsed, err := parseHTTPURLValue(value)
		if err != nil || parsed == nil {
			t.Fatalf("parseHTTPURLValue(%q) = %v, %v", value, parsed, err)
		}
	}
	for _, value := range []string{"", "api.deepseek.com", "file:///tmp/model", "ftp://example.com"} {
		if _, err := parseHTTPURLValue(value); err == nil {
			t.Fatalf("parseHTTPURLValue(%q) should fail", value)
		}
	}
}

func TestParseTodoRefineDisplaySettings(t *testing.T) {
	if value, err := parseTextModelSourceValue("  company   gateway  "); err != nil || value != "company gateway" {
		t.Fatalf("source = %v, %v", value, err)
	}
	for _, value := range []string{"", "\n\t", strings.Repeat("x", 81)} {
		if _, err := parseTextModelSourceValue(value); err == nil {
			t.Fatalf("source %q should fail", value)
		}
	}
	if value, err := parseTodoRefinePromptValue("\nPrefer observable acceptance criteria.\n"); err != nil ||
		value != "Prefer observable acceptance criteria." {
		t.Fatalf("prompt = %v, %v", value, err)
	}
	if value, err := parseTodoRefinePromptValue(""); err != nil || value != "" {
		t.Fatalf("blank prompt should restore defaults: %v, %v", value, err)
	}
	if _, err := parseTodoRefinePromptValue(strings.Repeat("x", 4001)); err == nil {
		t.Fatal("oversized prompt should fail")
	}
}

func TestConfigTestTextModelReturnsAppContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	t.Setenv("ATM_TEXT_MODEL_API_KEY", "connection-key")
	t.Setenv("ATM_TEXT_MODEL_BASE_URL", server.URL)
	oldJSON := jsonOutput
	jsonOutput = true
	configTestTextModelCmd.SetContext(context.Background())
	t.Cleanup(func() {
		jsonOutput = oldJSON
		configTestTextModelCmd.SetContext(context.Background())
	})

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = configTestTextModelCmd.RunE(configTestTextModelCmd, nil)
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var result struct {
		OK        bool  `json:"ok"`
		LatencyMS int64 `json:"latency_ms"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode output %q: %v", stdout, err)
	}
	if !result.OK || result.LatencyMS < 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestConfigCredentialCommandsNeverPrintTheSecret(t *testing.T) {
	withTempAtmDir(t)
	oldJSON := jsonOutput
	jsonOutput = true
	configCredentialSetCmd.SetIn(strings.NewReader("  command-secret\n"))
	t.Cleanup(func() {
		jsonOutput = oldJSON
		configCredentialSetCmd.SetIn(nil)
	})

	stdout := captureStdout(t, func() {
		if err := configCredentialSetCmd.RunE(configCredentialSetCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(stdout, "command-secret") {
		t.Fatalf("set output leaked credential: %q", stdout)
	}
	value, err := config.ReadTextModelAPIKey()
	if err != nil || value != "command-secret" {
		t.Fatalf("saved credential = %q, %v", value, err)
	}

	stdout = captureStdout(t, func() {
		if err := configCredentialStatusCmd.RunE(configCredentialStatusCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, `"configured": true`) || strings.Contains(stdout, "command-secret") {
		t.Fatalf("status output = %q", stdout)
	}

	captureStdout(t, func() {
		if err := configCredentialDeleteCmd.RunE(configCredentialDeleteCmd, nil); err != nil {
			t.Fatal(err)
		}
	})
	configured, err := config.TextModelAPIKeyConfigured()
	if err != nil || configured {
		t.Fatalf("configured after delete = %v, %v", configured, err)
	}
}
