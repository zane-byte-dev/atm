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
