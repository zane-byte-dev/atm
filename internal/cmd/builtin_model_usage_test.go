package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

func TestBuiltinModelFailureLogDoesNotPersistProviderPayload(t *testing.T) {
	withTempAtmDir(t)
	const secret = "sk-provider-echo-must-not-enter-diagnostics"
	logBuiltinModelCall(textmodel.Call{
		Task:  textmodel.TaskCheck,
		Model: "draft-model",
		Err:   "provider rejected Authorization: Bearer " + secret,
	})

	raw, err := os.ReadFile(logging.Path())
	if err != nil {
		t.Fatalf("read CLI log: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, secret) || strings.Contains(text, "Authorization") {
		t.Fatalf("CLI log leaked provider payload: %s", text)
	}
	if !strings.Contains(text, "built-in text model call failed") {
		t.Fatalf("CLI log lost safe failure class: %s", text)
	}
}
