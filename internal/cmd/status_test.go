package cmd

import (
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/parser"
)

func TestTerminalBundleIdentifier(t *testing.T) {
	tests := map[string]string{
		"/System/Applications/Utilities/Terminal.app/Contents/MacOS/Terminal": "com.apple.Terminal",
		"/Applications/iTerm.app/Contents/MacOS/iTerm2":                       "com.googlecode.iterm2",
		"/Applications/Ghostty.app/Contents/MacOS/ghostty":                    "com.mitchellh.ghostty",
		"/Applications/Visual Studio Code.app/Contents/MacOS/Electron":        "com.microsoft.VSCode",
	}
	for command, expected := range tests {
		if actual := terminalBundleIdentifier(command); actual != expected {
			t.Fatalf("terminalBundleIdentifier(%q) = %q, want %q", command, actual, expected)
		}
	}
}

func TestNormalizeProcessTTY(t *testing.T) {
	if actual := normalizeProcessTTY("/dev/ttys009"); actual != "ttys009" {
		t.Fatalf("normalizeProcessTTY returned %q", actual)
	}
	if actual := normalizeProcessTTY("??"); actual != "" {
		t.Fatalf("normalizeProcessTTY should ignore unknown TTY, got %q", actual)
	}
}

func TestStatusSessionRetentionMatchesPrimaryAgentHistoryWindow(t *testing.T) {
	if statusSessionRetention != 30*time.Minute {
		t.Fatalf("statusSessionRetention = %s, want 30m", statusSessionRetention)
	}
}

func TestMatchingAIProcessSkipsDesktopAndUsesClosestTerminal(t *testing.T) {
	startedAt := time.Date(2026, 8, 2, 15, 8, 19, 0, time.UTC)
	processes := []aiProcess{
		{PID: "1", Name: "codex", StartTime: startedAt.Add(-10 * time.Minute)},
		{
			PID:              "2",
			Name:             "codex",
			StartTime:        startedAt.Add(2 * time.Second),
			TerminalBundleID: "com.googlecode.iterm2",
		},
	}
	used := []bool{false, false}

	desktop := parser.Session{Tool: "Codex", Client: "Codex Desktop", StartedAt: startedAt}
	if actual := matchingAIProcessIndex(desktop, "codex", processes, used); actual != -1 {
		t.Fatalf("desktop session matched process %d", actual)
	}

	cli := parser.Session{Tool: "Codex", Client: "Codex CLI", StartedAt: startedAt}
	if actual := matchingAIProcessIndex(cli, "codex", processes, used); actual != 1 {
		t.Fatalf("CLI session matched process %d, want 1", actual)
	}
}

func TestIsGrokProcessCommand(t *testing.T) {
	tests := map[string]bool{
		"grok": true,
		"/Users/tester/.grok/bin/grok": true,
		"/Users/tester/.grok/downloads/grok-0.2.118-macos-aarch64": true,
		"rg -i grok": false,
		"echo grok is cool": false,
		"": false,
	}
	for command, want := range tests {
		if got := isGrokProcessCommand(command); got != want {
			t.Fatalf("isGrokProcessCommand(%q) = %v, want %v", command, got, want)
		}
	}
}

func TestSessionAgentKeyKnowsGrokBuild(t *testing.T) {
	if got := sessionAgentKey("Grok Build"); got != "grokbuild" {
		t.Fatalf("sessionAgentKey(Grok Build) = %q, want grokbuild", got)
	}
}
