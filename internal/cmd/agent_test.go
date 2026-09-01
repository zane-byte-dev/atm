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
