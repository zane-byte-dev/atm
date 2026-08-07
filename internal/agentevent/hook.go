package agentevent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Supported hook sources. Claude Code, Codex, Grok Build, and Qoder ship
// Claude-style hook event names (compare ~/.claude/settings.json,
// ~/.codex/hooks.json, ~/.grok/hooks/*.json, and ~/.qoder/settings.json), so
// they share one mapping. Grok's stdin envelope is camelCase; Claude, Codex and
// Qoder use snake_case — FromHook accepts both. Pi has a different extension API
// and builds envelopes in TypeScript instead.
//
// Qoder covers both the IDE and Qoder CLI: they read the same
// ~/.qoder/settings.json, and their payloads are Claude-shaped down to the field
// names (session_id, cwd, hook_event_name, last_assistant_message). QoderWork is
// a different product with its own runtime and is not wired up here.
const (
	SourceClaude    = "claude"
	SourceCodex     = "codex"
	SourceGrokbuild = "grokbuild"
	SourceQoder     = "qoder"
	SourcePi        = "pi"
)

// SupportedSources lists the sources the CLI accepts, in install order.
func SupportedSources() []string {
	return []string{SourceClaude, SourceCodex, SourceGrokbuild, SourceQoder, SourcePi}
}

// Input is one hook invocation: the raw stdin payload plus the two things the
// payload cannot tell us.
type Input struct {
	// Source is which agent invoked the hook.
	Source string
	// Reason carries the notification matcher that fired. Claude Code selects
	// Notification hooks by matcher (permission_prompt, idle_prompt,
	// agent_needs_input, auth_success, …) but does not repeat the matcher in the
	// payload, so the installer bakes it into the command line and we read it
	// from here. Empty for every other event.
	Reason string
	Raw    []byte
	Now    time.Time
}

// preToolAttention lists the tools whose call *is* a dialog the user has to
// answer, keyed to the reason the notch should show.
//
// PreToolUse fires for every tool, so the payload's tool name — not the reason
// baked into the command line — decides here. AskUserQuestion earns its place
// because Claude Code emits no notification for it at all: in 2.1.211 the only
// notify call sites are the permission dialog, the two elicitation dialogs,
// idle_prompt, auth_success, and the background-job bands. The question dialog
// is not one of them, so without this hook the notch stays dark for the entire
// time the user is being asked something. idle_prompt does not cover the gap
// either — the turn is still in flight while the dialog is open, so its
// not-busy guard suppresses it.
var preToolAttention = map[string]string{
	"AskUserQuestion": "ask_user_question",
}

// notificationKinds maps Notification matchers onto normalized events. A
// matcher that is absent from this table is deliberately dropped: auth_success
// and the elicitation_* completions are not moments where the agent is waiting
// on you, and surfacing them would make the notch cry wolf.
//
// idle_prompt is dropped for the same reason, and it is worth spelling out
// because the name reads like the opposite. It does not fire while a turn is
// blocked — Claude Code guards it on not-busy, and Grok Build runs it off a
// timer (GROK_IDLE_NOTIFICATION_DELAY_MS, default ~60s) that only starts once
// the turn is over. Grok's payload says as much outright: notificationType
// "idle_prompt", message "Turn complete", level "info". So it never means "the
// agent is blocked on you"; it means "the agent finished and you have not come
// back yet". Surfacing it turned every completed turn orange a minute later,
// claiming the finished work still needed confirmation — and because attention
// outranks completion in the notch, it also displaced the completion card that
// was telling the truth.
var notificationKinds = map[string]Kind{
	"permission_prompt":    KindAttention,
	"agent_needs_input":    KindAttention,
	"elicitation_dialog":   KindAttention,
	"agent_completed":      KindCompleted,
	"idle_prompt":          "",
	"auth_success":         "",
	"elicitation_complete": "",
	"elicitation_response": "",
}

// FromHook converts a hook payload into an envelope. The boolean is false for
// events that are valid but carry no notch meaning, which the caller treats as
// success — a hook must never fail just because we ignore its event.
func FromHook(in Input) (Envelope, bool, error) {
	if !isHookSource(in.Source) {
		return Envelope{}, false, fmt.Errorf("unsupported source %q", in.Source)
	}
	var payload map[string]any
	if err := json.Unmarshal(in.Raw, &payload); err != nil {
		return Envelope{}, false, fmt.Errorf("hook payload is not JSON: %w", err)
	}

	kind, reason, ok := classify(payload, in.Reason)
	if !ok {
		return Envelope{}, false, nil
	}

	envelope := Envelope{
		Version:   Version,
		Source:    in.Source,
		Event:     kind,
		SessionID: sessionID(payload),
		CWD:       payloadStr(payload, "cwd"),
		Tool:      payloadStr(payload, "tool_name", "toolName"),
		Reason:    reason,
		Text:      truncate(eventText(payload)),
		At:        timestamp(in.Now),
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, false, err
	}
	return envelope, true, nil
}

func isHookSource(source string) bool {
	switch source {
	case SourceClaude, SourceCodex, SourceGrokbuild, SourceQoder:
		return true
	default:
		return false
	}
}

func classify(payload map[string]any, matcher string) (Kind, string, bool) {
	// Claude/Codex send PascalCase hook_event_name (SessionStart). Grok Build's
	// stdin envelope documents camelCase keys with snake_case values
	// (hookEventName: "pre_tool_use" / "stop"). Accept all three so a single
	// mapping serves every source.
	switch normalizeHookEventName(payloadStr(payload, "hook_event_name", "hookEventName")) {
	case "sessionstart":
		return KindSessionStart, "", true
	case "userpromptsubmit", "beforesubmitprompt":
		return KindStarted, "", true
	case "stop":
		// Grok also fires an observe-only Stop at session end
		// (reason: channel_closed / shutdown). That is not a turn completion.
		if reason := payloadStr(payload, "reason"); reason != "" && reason != "end_turn" {
			if reason == "channel_closed" || reason == "shutdown" {
				return KindSessionEnd, reason, true
			}
			// Other non-end_turn stops (interrupt, …) are not completions.
			return "", "", false
		}
		return KindCompleted, "", true
	case "sessionend":
		return KindSessionEnd, "", true
	case "pretooluse":
		tool := payloadStr(payload, "tool_name", "toolName")
		reason, ok := preToolAttention[tool]
		if !ok {
			return "", "", false
		}
		if matcher != "" {
			reason = matcher
		}
		return KindAttention, reason, true
	case "posttooluse", "posttoolusefailure":
		// The tool ran, so whatever was blocking it — a permission prompt, an
		// AskUserQuestion dialog — is resolved. A failure counts just the same:
		// the agent is unblocked either way.
		return KindResumed, "", true
	case "permissionrequest":
		// Reported but not decided in this phase: the hook stays non-blocking
		// and returns no JSON, so the agent falls through to its own permission
		// prompt exactly as if we were not installed.
		return KindAttention, "permission_request", true
	case "notification":
		if matcher == "" {
			// Installed without an explicit matcher, which is how Grok Build is
			// wired: its hook file selects no matcher, so nothing can be baked
			// into the command line. It does put the matcher in the payload
			// instead, under Claude's own vocabulary — notificationType
			// "idle_prompt" / "permission_prompt". Reading it is what lets one
			// table serve both agents; without it every Grok notification
			// collapsed onto the generic attention below.
			matcher = payloadStr(payload, "notificationType", "notification_type")
		}
		if matcher == "" {
			// Genuinely unclassifiable. A notification still means the agent
			// wants the user's eyes, so default to attention rather than
			// dropping a signal we cannot read.
			return KindAttention, "notification", true
		}
		kind, known := notificationKinds[matcher]
		if !known {
			return KindAttention, matcher, true
		}
		if kind == "" {
			return "", "", false
		}
		return kind, matcher, true
	}
	return "", "", false
}

// normalizeHookEventName folds Claude PascalCase, Cursor camelCase, and Grok
// snake_case event names onto one comparison key.
func normalizeHookEventName(name string) string {
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == '_' || r == '-' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sessionID prefers the agent's canonical session identifier and falls back to
// the Codex thread id, which some Codex hook events use instead. Grok Build
// emits camelCase sessionId.
func sessionID(payload map[string]any) string {
	return payloadStr(
		payload,
		"session_id", "sessionId",
		"thread_id", "threadId",
		"conversation_id", "conversationId",
	)
}

// eventText picks the most useful human-facing string available.
//
// last_assistant_message comes first because Stop carries it specifically so
// hooks do not have to read the transcript, which Claude Code documents as
// lagging behind the live conversation. Grok Build uses lastAssistantMessage.
func eventText(payload map[string]any) string {
	for _, key := range []string{
		"last_assistant_message", "lastAssistantMessage",
		"message", "prompt",
	} {
		if value := config.GetStr(payload, key); value != "" {
			return value
		}
	}
	return questionText(payload)
}

// questionText pulls the first question out of an AskUserQuestion call. That
// event carries no message field, so without this the notch would only know
// that something is being asked, not what — and "扫一眼" is the whole point of
// lighting it up.
func questionText(payload map[string]any) string {
	input := config.GetMap(payload, "tool_input")
	if input == nil {
		input = config.GetMap(payload, "toolInput")
	}
	if input == nil {
		return ""
	}
	questions, ok := input["questions"].([]any)
	if !ok || len(questions) == 0 {
		return ""
	}
	first, ok := questions[0].(map[string]any)
	if !ok {
		return ""
	}
	return config.GetStr(first, "question")
}

// payloadStr returns the first non-empty string among the given keys. Used to
// accept both Claude/Codex snake_case and Grok Build camelCase envelopes.
func payloadStr(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := config.GetStr(payload, key); value != "" {
			return value
		}
	}
	return ""
}
