package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/guard"
)

func TestGuardInstallAdapterCannotGiveAgentManagementAuthority(t *testing.T) {
	guardTestEnv(t)
	t.Setenv("CODEX_THREAD_ID", "agent-thread")
	directory := t.TempDir()
	binPath := filepath.Join(directory, "dws")
	original := []byte("#!/bin/sh\necho untouched\n")
	if err := os.WriteFile(binPath, original, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, _, err := runGuard(t, "install", "dws", "--bin", binPath)
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent install error = %v, want forbidden", err)
	}
	got, readErr := os.ReadFile(binPath)
	if readErr != nil || string(got) != string(original) {
		t.Fatalf("Agent install changed binary: body=%q error=%v", got, readErr)
	}
	if _, statErr := os.Stat(guard.RealBinPath(binPath)); !os.IsNotExist(statErr) {
		t.Fatalf("Agent install displaced binary: %v", statErr)
	}
	if _, statErr := os.Stat(config.ConfigPath); !os.IsNotExist(statErr) {
		t.Fatalf("Agent install wrote config: %v", statErr)
	}
}

func TestGuardRuleAdaptersOnlyParseAndRenderAroundService(t *testing.T) {
	guardTestEnv(t)
	oldJSON := jsonOutput
	jsonOutput = false
	rootCmd.SetIn(strings.NewReader(`{"id":"send","path":["chat","send"]}`))
	t.Cleanup(func() {
		jsonOutput = oldJSON
		rootCmd.SetIn(nil)
	})

	var runErr error
	stdout := captureStdout(t, func() {
		_, _, _, runErr = runGuard(t, "rule", "set", "custom")
	})
	if runErr != nil {
		t.Fatalf("rule set: %v", runErr)
	}
	if !strings.Contains(stdout, "custom：1 条规则，1 条启用") {
		t.Fatalf("rule set output changed: %q", stdout)
	}
	views := guard.RuleViews("custom")
	if len(views) != 1 || views[0].ID != "send" || strings.Join(views[0].Path, " ") != "chat send" {
		t.Fatalf("persisted rule = %#v", views)
	}

	stdout = captureStdout(t, func() {
		_, _, _, runErr = runGuard(t, "rule", "remove", "custom", "send")
	})
	if runErr != nil {
		t.Fatalf("rule remove: %v", runErr)
	}
	if !strings.Contains(stdout, "custom：0 条规则，0 条启用") {
		t.Fatalf("rule remove output changed: %q", stdout)
	}
	if views := guard.RuleViews("custom"); len(views) != 0 {
		t.Fatalf("removed rule remains: %#v", views)
	}
}
