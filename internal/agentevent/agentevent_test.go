package agentevent

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)

func TestFromHookMapsEveryEventWeAct1On(t *testing.T) {
	cases := []struct {
		name       string
		source     string
		reason     string
		raw        string
		wantKind   Kind
		wantReason string
		wantText   string
		wantTool   string
		wantDrop   bool
	}{
		{
			name:     "claude session start",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"SessionStart","session_id":"abc","cwd":"/w"}`,
			wantKind: KindSessionStart,
		},
		{
			name:     "claude prompt submit starts work",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"UserPromptSubmit","session_id":"abc","cwd":"/w","prompt":"fix the notch"}`,
			wantKind: KindStarted,
			wantText: "fix the notch",
		},
		{
			name:       "claude permission prompt needs the user",
			source:     SourceClaude,
			reason:     "permission_prompt",
			raw:        `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w","message":"Claude needs permission to run npm test"}`,
			wantKind:   KindAttention,
			wantReason: "permission_prompt",
			wantText:   "Claude needs permission to run npm test",
		},
		{
			// Fires a minute *after* the turn ends, so it cannot mean the agent
			// is blocked on the user. Surfacing it turned every finished turn
			// orange and displaced the completion card.
			name:     "idle_prompt is dropped, it means the turn is over",
			source:   SourceClaude,
			reason:   "idle_prompt",
			raw:      `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantDrop: true,
		},
		{
			// Grok's hook file pins no matcher, so the reason has to come out of
			// the payload. Captured verbatim from a real Grok Build 0.2.118 idle
			// notification.
			name:     "grok idle notification is dropped via notificationType",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"notification","sessionId":"abc","cwd":"/w","notificationType":"idle_prompt","message":"Turn complete","level":"info"}`,
			wantDrop: true,
		},
		{
			name:       "grok notificationType classifies a real permission prompt",
			source:     SourceGrokbuild,
			raw:        `{"hookEventName":"notification","sessionId":"abc","cwd":"/w","notificationType":"permission_prompt","message":"Grok needs permission to run rm"}`,
			wantKind:   KindAttention,
			wantReason: "permission_prompt",
			wantText:   "Grok needs permission to run rm",
		},
		{
			name:       "grok notificationType ATM does not know still surfaces",
			source:     SourceGrokbuild,
			raw:        `{"hookEventName":"notification","sessionId":"abc","cwd":"/w","notificationType":"some_future_type"}`,
			wantKind:   KindAttention,
			wantReason: "some_future_type",
		},
		{
			// The matcher baked into the command line wins: Claude Code selects
			// by matcher and does not repeat it, so a payload field would at
			// best agree and at worst be a different notification's.
			name:       "an explicit matcher beats the payload's notificationType",
			source:     SourceClaude,
			reason:     "permission_prompt",
			raw:        `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w","notificationType":"idle_prompt"}`,
			wantKind:   KindAttention,
			wantReason: "permission_prompt",
		},
		{
			name:       "claude agent_completed is a completion not an attention",
			source:     SourceClaude,
			reason:     "agent_completed",
			raw:        `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantKind:   KindCompleted,
			wantReason: "agent_completed",
		},
		{
			// The whole point of PostToolUse: answering a permission prompt is
			// neither a new prompt nor the end of a turn, so this is the only
			// event that says the user is off the hook.
			name:     "claude post tool use means the agent is unblocked",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"PostToolUse","session_id":"abc","cwd":"/w","tool_name":"Bash"}`,
			wantKind: KindResumed,
			wantTool: "Bash",
		},
		{
			// A tool that failed still ran, so the block is just as resolved.
			name:     "claude post tool use failure also unblocks",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"PostToolUseFailure","session_id":"abc","cwd":"/w","tool_name":"Bash"}`,
			wantKind: KindResumed,
			wantTool: "Bash",
		},
		{
			name:     "grok snake_case post_tool_use unblocks",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"post_tool_use","sessionId":"abc","cwd":"/w","toolName":"shell"}`,
			wantKind: KindResumed,
			wantTool: "shell",
		},
		{
			// Crying wolf is the failure mode that makes a notch worth turning
			// off, so matchers that are not the user's problem must drop.
			name:     "claude auth_success is dropped",
			source:   SourceClaude,
			reason:   "auth_success",
			raw:      `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantDrop: true,
		},
		{
			name:     "claude elicitation_complete is dropped",
			source:   SourceClaude,
			reason:   "elicitation_complete",
			raw:      `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantDrop: true,
		},
		{
			name:       "unknown matcher still surfaces rather than vanishing",
			source:     SourceClaude,
			reason:     "some_future_matcher",
			raw:        `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantKind:   KindAttention,
			wantReason: "some_future_matcher",
		},
		{
			name:       "notification without a matcher defaults to attention",
			source:     SourceClaude,
			raw:        `{"hook_event_name":"Notification","session_id":"abc","cwd":"/w"}`,
			wantKind:   KindAttention,
			wantReason: "notification",
		},
		{
			// Stop carries the final text precisely so hooks need not read the
			// transcript, which Claude Code documents as lagging.
			name:     "claude stop prefers last_assistant_message",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"Stop","session_id":"abc","cwd":"/w","message":"ignored","last_assistant_message":"done"}`,
			wantKind: KindCompleted,
			wantText: "done",
		},
		{
			name:     "claude session end",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"SessionEnd","session_id":"abc","cwd":"/w"}`,
			wantKind: KindSessionEnd,
		},
		{
			name:       "permission request reports the blocked tool",
			source:     SourceClaude,
			raw:        `{"hook_event_name":"PermissionRequest","session_id":"abc","cwd":"/w","tool_name":"Bash"}`,
			wantKind:   KindAttention,
			wantReason: "permission_request",
			wantTool:   "Bash",
		},
		{
			// The question dialog is the one blocking prompt Claude Code raises
			// without notifying anyone, so PreToolUse is the only signal there is.
			name:       "ask user question shows what is being asked",
			source:     SourceClaude,
			reason:     "ask_user_question",
			raw:        `{"hook_event_name":"PreToolUse","session_id":"abc","cwd":"/w","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"哪个方案?","header":"Approach"},{"question":"second"}]}}`,
			wantKind:   KindAttention,
			wantReason: "ask_user_question",
			wantText:   "哪个方案?",
			wantTool:   "AskUserQuestion",
		},
		{
			// The tool name decides, not the reason on the command line: a
			// PreToolUse hook installed with a broader matcher by anyone else must
			// not turn every tool call into an attention signal.
			name:     "pre tool use for any other tool drops",
			source:   SourceClaude,
			reason:   "ask_user_question",
			raw:      `{"hook_event_name":"PreToolUse","session_id":"abc","cwd":"/w","tool_name":"Bash"}`,
			wantDrop: true,
		},
		{
			name:     "codex shares the claude event names",
			source:   SourceCodex,
			raw:      `{"hook_event_name":"Stop","session_id":"thread-1","cwd":"/w"}`,
			wantKind: KindCompleted,
		},
		{
			name:     "codex thread_id substitutes for session_id",
			source:   SourceCodex,
			raw:      `{"hook_event_name":"SessionStart","thread_id":"thread-1","cwd":"/w"}`,
			wantKind: KindSessionStart,
		},
		{
			// Installing broad matchers means we see events we have no use for.
			// Those must be cheap no-ops, not errors.
			name:     "uninteresting events drop",
			source:   SourceClaude,
			raw:      `{"hook_event_name":"PreCompact","session_id":"abc","cwd":"/w"}`,
			wantDrop: true,
		},
		{
			// Grok Build's hook stdin is camelCase. Without this branch the
			// notch would silently ignore every Grok event.
			name:     "grok camelCase session start",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"SessionStart","sessionId":"g1","cwd":"/Users/tester/mox/atm"}`,
			wantKind: KindSessionStart,
		},
		{
			name:     "grok camelCase prompt submit",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"UserPromptSubmit","sessionId":"g1","cwd":"/w","prompt":"wire the notch"}`,
			wantKind: KindStarted,
			wantText: "wire the notch",
		},
		{
			name:     "grok camelCase stop prefers lastAssistantMessage",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"Stop","sessionId":"g1","cwd":"/w","message":"ignored","lastAssistantMessage":"shipped"}`,
			wantKind: KindCompleted,
			wantText: "shipped",
		},
		{
			name:     "grok session end",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"SessionEnd","sessionId":"g1","cwd":"/w"}`,
			wantKind: KindSessionEnd,
		},
		{
			name:       "grok notification without matcher is attention",
			source:     SourceGrokbuild,
			raw:        `{"hookEventName":"Notification","sessionId":"g1","cwd":"/w","message":"needs you"}`,
			wantKind:   KindAttention,
			wantReason: "notification",
			wantText:   "needs you",
		},
		{
			// Grok's documented stdin uses snake_case event values
			// (hookEventName: "stop"), not PascalCase. Without this branch every
			// real Grok hook delivery would drop as uninteresting.
			name:     "grok snake_case stop value",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"stop","sessionId":"g1","cwd":"/w","reason":"end_turn","lastAssistantMessage":"shipped"}`,
			wantKind: KindCompleted,
			wantText: "shipped",
		},
		{
			name:     "grok snake_case user_prompt_submit",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"user_prompt_submit","sessionId":"g1","cwd":"/w","prompt":"hello"}`,
			wantKind: KindStarted,
			wantText: "hello",
		},
		{
			name:       "grok session-end stop is session_end not completed",
			source:     SourceGrokbuild,
			raw:        `{"hookEventName":"stop","sessionId":"g1","cwd":"/w","reason":"channel_closed"}`,
			wantKind:   KindSessionEnd,
			wantReason: "channel_closed",
		},
		{
			name:     "grok snake_case pre_tool_use for bash drops",
			source:   SourceGrokbuild,
			raw:      `{"hookEventName":"pre_tool_use","sessionId":"g1","cwd":"/w","toolName":"run_terminal_command"}`,
			wantDrop: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope, ok, err := FromHook(Input{
				Source: tc.source,
				Reason: tc.reason,
				Raw:    []byte(tc.raw),
				Now:    fixedNow,
			})
			if err != nil {
				t.Fatalf("FromHook returned error: %v", err)
			}
			if tc.wantDrop {
				if ok {
					t.Fatalf("expected event to drop, got %+v", envelope)
				}
				return
			}
			if !ok {
				t.Fatal("expected an envelope, got a drop")
			}
			if envelope.Event != tc.wantKind {
				t.Errorf("event = %q, want %q", envelope.Event, tc.wantKind)
			}
			if envelope.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", envelope.Reason, tc.wantReason)
			}
			if envelope.Text != tc.wantText {
				t.Errorf("text = %q, want %q", envelope.Text, tc.wantText)
			}
			if envelope.Tool != tc.wantTool {
				t.Errorf("tool = %q, want %q", envelope.Tool, tc.wantTool)
			}
			if envelope.Version != Version {
				t.Errorf("version = %d, want %d", envelope.Version, Version)
			}
			if envelope.Source != tc.source {
				t.Errorf("source = %q, want %q", envelope.Source, tc.source)
			}
			if envelope.At != "2026-08-03T10:00:00Z" {
				t.Errorf("at = %q", envelope.At)
			}
		})
	}
}

func TestFromHookRejectsUnusablePayloads(t *testing.T) {
	if _, _, err := FromHook(Input{Source: "gemini", Raw: []byte(`{}`), Now: fixedNow}); err == nil {
		t.Error("expected an error for an unsupported source")
	}
	if _, _, err := FromHook(Input{Source: SourceClaude, Raw: []byte(`not json`), Now: fixedNow}); err == nil {
		t.Error("expected an error for a non-JSON payload")
	}
	// Neither identifier means the app could create an attention signal it can
	// never join to a row or clear.
	if _, _, err := FromHook(Input{
		Source: SourceClaude,
		Raw:    []byte(`{"hook_event_name":"Stop"}`),
		Now:    fixedNow,
	}); err == nil {
		t.Error("expected an error when the payload has neither session_id nor cwd")
	}
}

func TestAttentionLifecycleKinds(t *testing.T) {
	if !KindAttention.Valid() || Kind("nope").Valid() {
		t.Error("Valid did not classify kinds correctly")
	}
	for _, kind := range []Kind{KindStarted, KindResumed, KindCompleted, KindSessionEnd} {
		if !kind.ClearsAttention() {
			t.Errorf("%q should clear a pending attention signal", kind)
		}
	}
	for _, kind := range []Kind{KindAttention, KindSessionStart} {
		if kind.ClearsAttention() {
			t.Errorf("%q should not clear a pending attention signal", kind)
		}
	}
}

func TestEnvelopeLineIsOneNewlineTerminatedJSONObject(t *testing.T) {
	line, err := Envelope{
		Version:   Version,
		Source:    SourceClaude,
		Event:     KindAttention,
		SessionID: "abc",
		Text:      "hi\nthere",
		At:        "2026-08-03T10:00:00Z",
	}.Line()
	if err != nil {
		t.Fatalf("Line: %v", err)
	}
	if got := line[len(line)-1]; got != '\n' {
		t.Fatalf("line does not end with a newline: %q", line)
	}
	// Embedded newlines must be escaped or framing breaks.
	if count := bytesCount(line, '\n'); count != 1 {
		t.Fatalf("line contains %d newlines, want 1: %q", count, line)
	}
	var decoded Envelope
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if decoded.Text != "hi\nthere" {
		t.Errorf("text did not survive the round trip: %q", decoded.Text)
	}
}

func TestTruncateKeepsTextValidUTF8(t *testing.T) {
	long := ""
	for len(long) < textLimit*2 {
		long += "刘海动画"
	}
	got := truncate(long)
	if len(got) > textLimit+len("…") {
		t.Fatalf("truncate returned %d bytes", len(got))
	}
	if !utf8Valid(got) {
		t.Fatalf("truncate split a multi-byte rune: %q", got)
	}
}

func TestDeliverFailsFastWhenTheAppIsNotRunning(t *testing.T) {
	// The load-bearing property of the whole design: with no listener, delivery
	// must fail immediately so the hook can exit 0 without stalling the agent.
	t.Setenv(SocketEnvVar, filepath.Join(t.TempDir(), "missing.sock"))
	started := time.Now()
	err := Deliver(Envelope{
		Version: Version, Source: SourceClaude, Event: KindAttention,
		SessionID: "abc", At: "2026-08-03T10:00:00Z",
	})
	if err == nil {
		t.Fatal("expected delivery to fail with no listener")
	}
	if elapsed := time.Since(started); elapsed > DeliverTimeout {
		t.Fatalf("delivery took %s, want well under %s", elapsed, DeliverTimeout)
	}
}

// shortSocketDir returns a temp dir short enough for sockaddr_un. The default
// t.TempDir() on macOS is already ~90 bytes, which blows the 103-byte limit as
// soon as a filename is appended.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "atmev")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestCheckSocketPathRejectsOverlongPaths(t *testing.T) {
	if err := CheckSocketPath(""); err == nil {
		t.Error("expected an error for an empty path")
	}
	if err := CheckSocketPath(filepath.Join("/tmp", strings.Repeat("a", 120))); err == nil {
		t.Error("expected an error for a path over the unix socket limit")
	}
	if err := CheckSocketPath("/tmp/atm/notch.sock"); err != nil {
		t.Errorf("expected a normal path to pass: %v", err)
	}
}

func TestDeliverWritesOneLineToTheListener(t *testing.T) {
	path := filepath.Join(shortSocketDir(t), "notch.sock")
	t.Setenv(SocketEnvVar, path)
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

	envelope := Envelope{
		Version: Version, Source: SourceClaude, Event: KindAttention,
		SessionID: "abc", Reason: "permission_prompt", At: "2026-08-03T10:00:00Z",
	}
	if err := Deliver(envelope); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	select {
	case line := <-received:
		var decoded Envelope
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("listener got undecodable line %q: %v", line, err)
		}
		if decoded != envelope {
			t.Errorf("listener got %+v, want %+v", decoded, envelope)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener never received the envelope")
	}
}

func bytesCount(data []byte, target byte) int {
	count := 0
	for _, b := range data {
		if b == target {
			count++
		}
	}
	return count
}

func utf8Valid(value string) bool {
	for _, r := range value {
		if r == 0xFFFD {
			return false
		}
	}
	return true
}
