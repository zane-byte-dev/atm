package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
)

// runHook drives the command the way a hook system does: real argument parsing
// from the root command, payload on stdin, nothing interactive. Returns stdout
// and stderr separately because stdout staying empty is a correctness
// requirement, not a cosmetic one.
func runHook(t *testing.T, payload string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// The flags are package-level, and cobra does not reset them between runs.
	agentHookSource, agentHookReason, agentHookVerbose = "", "", false
	rootCmd.SetIn(strings.NewReader(payload))
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(append([]string{"agent", "hook"}, args...))
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		agentHookSource, agentHookReason, agentHookVerbose = "", "", false
	})
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atmhook")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// TestAgentHookNeverDisturbsTheAgent is the safety net for the whole design.
// Every one of these inputs is something an agent will really hand us, and none
// of them may produce a non-zero exit or a byte on stdout: for PreToolUse and
// PermissionRequest, Claude Code reads exit 2 as "block the tool call" and
// stdout as a decision, so a bug here would silently break the user's session.
func TestAgentHookNeverDisturbsTheAgent(t *testing.T) {
	t.Setenv(agentevent.SocketEnvVar, filepath.Join(shortSocketDir(t), "absent.sock"))

	cases := []struct {
		name    string
		payload string
		args    []string
	}{
		{
			name:    "app not running",
			payload: `{"hook_event_name":"Stop","session_id":"abc","cwd":"/w"}`,
			args:    []string{"--source", "claude"},
		},
		{
			name:    "missing source flag",
			payload: `{"hook_event_name":"Stop","session_id":"abc"}`,
		},
		{
			name:    "unsupported source",
			payload: `{"hook_event_name":"Stop","session_id":"abc"}`,
			args:    []string{"--source", "gemini"},
		},
		{
			name:    "payload is not JSON",
			payload: "not json at all",
			args:    []string{"--source", "claude"},
		},
		{
			name:    "empty payload",
			payload: "",
			args:    []string{"--source", "claude"},
		},
		{
			name:    "event we do not care about",
			payload: `{"hook_event_name":"PreCompact","session_id":"abc","cwd":"/w"}`,
			args:    []string{"--source", "claude"},
		},
		{
			name:    "payload without any session identifier",
			payload: `{"hook_event_name":"Stop"}`,
			args:    []string{"--source", "claude"},
		},
		{
			name:    "dropped notification matcher",
			payload: `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			args:    []string{"--source", "claude", "--reason", "auth_success"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := runHook(t, tc.payload, tc.args...)
			if err != nil {
				t.Errorf("hook returned an error (would exit non-zero): %v", err)
			}
			if stdout != "" {
				t.Errorf("hook wrote to stdout: %q", stdout)
			}
		})
	}
}

func TestAgentHookDeliversToAListeningApp(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "notch.sock")
	t.Setenv(agentevent.SocketEnvVar, path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		received <- line
	}()

	stdout, _, err := runHook(
		t,
		`{"hook_event_name":"Notification","session_id":"abc","cwd":"/w","message":"needs permission"}`,
		"--source", "claude", "--reason", "permission_prompt",
	)
	if err != nil {
		t.Fatalf("hook returned an error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("hook wrote to stdout: %q", stdout)
	}

	select {
	case line := <-received:
		var envelope agentevent.Envelope
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("app got an undecodable line %q: %v", line, err)
		}
		if envelope.Event != agentevent.KindAttention {
			t.Errorf("event = %q, want %q", envelope.Event, agentevent.KindAttention)
		}
		if envelope.Reason != "permission_prompt" {
			t.Errorf("reason = %q", envelope.Reason)
		}
		if envelope.SessionID != "abc" {
			t.Errorf("session id = %q", envelope.SessionID)
		}
		if envelope.Source != agentevent.SourceClaude {
			t.Errorf("source = %q", envelope.Source)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the listening app never received the event")
	}
}

func TestAgentHookVerboseExplainsItselfOnStderrOnly(t *testing.T) {
	t.Setenv(agentevent.SocketEnvVar, filepath.Join(shortSocketDir(t), "absent.sock"))
	stdout, stderr, err := runHook(
		t,
		`{"hook_event_name":"Stop","session_id":"abc","cwd":"/w"}`,
		"--source", "claude", "--verbose",
	)
	if err != nil {
		t.Fatalf("hook returned an error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("verbose output leaked to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "not delivered") {
		t.Errorf("stderr did not explain the failure: %q", stderr)
	}
}

func TestHookConfigForSourcePointsPiAtItsExtensionFile(t *testing.T) {
	// Pi loads a TypeScript extension rather than a hooks config, so reporting a
	// config-file failure for it would be misleading.
	entry := hookConfigForSource(hookConfigStatus, "pi", t.TempDir(), "/usr/local/bin/atm")
	if entry.Manual == "" {
		t.Error("expected pi to report manual extension instructions")
	}
	if entry.Error != "" {
		t.Errorf("pi should not report an error: %q", entry.Error)
	}
	if entry.Path != "" {
		t.Errorf("pi should not claim a config path: %q", entry.Path)
	}
}

func TestHookConfigForSourceRoundTripsGrokInstall(t *testing.T) {
	home := t.TempDir()
	binary := "/usr/local/bin/atm"

	missing := hookConfigForSource(hookConfigStatus, "grokbuild", home, binary)
	if len(missing.Missing) == 0 {
		t.Fatal("expected Grok hooks to be reported missing before install")
	}

	installed := hookConfigForSource(hookConfigInstall, "grokbuild", home, binary)
	if len(installed.Added) == 0 {
		t.Fatal("expected Grok install to add hooks")
	}
	if !strings.HasSuffix(installed.Path, filepath.Join(".grok", "hooks", "atm-notch.json")) {
		t.Fatalf("Grok path = %q, want .../.grok/hooks/atm-notch.json", installed.Path)
	}

	after := hookConfigForSource(hookConfigStatus, "grokbuild", home, binary)
	if len(after.Missing) != 0 {
		t.Fatalf("Grok hooks still missing after install: %v", after.Missing)
	}
	if len(after.Installed) == 0 {
		t.Fatal("expected Grok hooks to be installed")
	}

	removed := hookConfigForSource(hookConfigUninstall, "grokbuild", home, binary)
	if len(removed.Removed) == 0 {
		t.Fatal("expected Grok uninstall to remove hooks")
	}
	if _, err := os.Stat(installed.Path); !os.IsNotExist(err) {
		t.Fatalf("Grok atm-notch.json should be deleted, stat err=%v", err)
	}
}

func TestHookConfigForSourceRoundTripsInstallAndUninstall(t *testing.T) {
	home := t.TempDir()
	binary := "/usr/local/bin/atm"

	missing := hookConfigForSource(hookConfigStatus, "claude", home, binary)
	if len(missing.Missing) == 0 {
		t.Fatal("expected hooks to be reported missing before install")
	}
	if len(missing.Installed) != 0 {
		t.Errorf("nothing should be installed yet: %v", missing.Installed)
	}

	installed := hookConfigForSource(hookConfigInstall, "claude", home, binary)
	if len(installed.Added) != len(missing.Missing) {
		t.Errorf("added %v, expected all of %v", installed.Added, missing.Missing)
	}

	after := hookConfigForSource(hookConfigStatus, "claude", home, binary)
	if len(after.Missing) != 0 {
		t.Errorf("still missing after install: %v", after.Missing)
	}

	removed := hookConfigForSource(hookConfigUninstall, "claude", home, binary)
	if len(removed.Removed) != len(installed.Added) {
		t.Errorf("removed %v, expected all of %v", removed.Removed, installed.Added)
	}
}

func TestHookConfigForSourceReportsMalformedConfigAsAnError(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	entry := hookConfigForSource(hookConfigInstall, "claude", home, "/usr/local/bin/atm")
	if entry.Error == "" {
		t.Error("expected an error rather than a silent overwrite")
	}
}

func TestAgentHookCommandRendering(t *testing.T) {
	if got := agentHookCommand("/usr/local/bin/atm", "claude", ""); got != "/usr/local/bin/atm agent hook --source claude" {
		t.Errorf("unexpected command: %q", got)
	}
	if got := agentHookCommand("/usr/local/bin/atm", "claude", "permission_prompt"); got != "/usr/local/bin/atm agent hook --source claude --reason permission_prompt" {
		t.Errorf("unexpected command: %q", got)
	}
	// A path with a space has to survive being written into a JSON config and
	// re-parsed by the agent's shell.
	if got := agentHookCommand("/Applications/My Tools/atm", "codex", ""); got != `"/Applications/My Tools/atm" agent hook --source codex` {
		t.Errorf("unexpected command: %q", got)
	}
}
