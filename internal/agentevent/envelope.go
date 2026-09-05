// Package agentevent normalizes agent hook payloads into the single event
// shape the presence runtime consumes.
//
// Transcript keyword matching is both late (the transcript is written
// asynchronously) and blind to the case that matters most: a tool call blocked
// on a permission prompt writes no assistant text at all. Hooks report that
// moment directly, so this mapping is the activity feed's source of truth.
package agentevent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Version is the envelope schema version. The runtime rejects anything newer
// than it understands rather than guessing at unknown fields.
const Version = 1

// Kind is the normalized event. Every supported agent collapses onto these six
// so the runtime never branches on which client produced a signal.
type Kind string

const (
	// KindSessionStart means a session appeared. Carries no attention weight.
	KindSessionStart Kind = "session_start"
	// KindStarted means the user handed work to the agent, which also clears
	// any outstanding attention signal for that session.
	KindStarted Kind = "started"
	// KindAttention means the agent is blocked on the user.
	KindAttention Kind = "attention"
	// KindResumed means the agent is working again, which is the only evidence
	// there is that the user dealt with whatever blocked it.
	//
	// Resolving a permission prompt or answering a question is neither a new
	// prompt nor the end of a turn, so nothing else reports it: the agent simply
	// carries on. Without this the activity feed keeps claiming the agent is waiting for
	// the whole rest of the turn.
	KindResumed Kind = "resumed"
	// KindCompleted means the agent finished a turn and produced a result.
	KindCompleted Kind = "completed"
	// KindSessionEnd means the session is gone.
	KindSessionEnd Kind = "session_end"
)

// Valid reports whether the kind is one the runtime knows how to apply.
func (k Kind) Valid() bool {
	switch k {
	case KindSessionStart, KindStarted, KindAttention, KindResumed, KindCompleted, KindSessionEnd:
		return true
	}
	return false
}

// ClearsAttention reports whether receiving this event should retire a pending
// attention signal for the same session.
func (k Kind) ClearsAttention() bool {
	switch k {
	case KindStarted, KindResumed, KindCompleted, KindSessionEnd:
		return true
	}
	return false
}

// Envelope is one line of the presence socket protocol.
//
// Field names are deliberately short and stable: the Pi extension hand-builds
// this JSON in TypeScript, so any rename has to be made in two languages.
type Envelope struct {
	Version int    `json:"v"`
	Source  string `json:"source"`
	Event   Kind   `json:"event"`
	// SessionID is the agent's own session identifier. For Claude Code this
	// equals the transcript filename stem, which is what the parser already
	// stores; for Codex it is the full thread id, which the parser stores as
	// ResumeID rather than SessionID.
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Tool      string `json:"tool,omitempty"`
	// Reason explains an attention signal in the agent's own vocabulary, e.g.
	// "permission_prompt". Shown verbatim in the activity UI.
	Reason string `json:"reason,omitempty"`
	Text   string `json:"text,omitempty"`
	At     string `json:"at"`
}

// Validate rejects envelopes the runtime could not act on. Delivering a
// sessionless event would create an attention signal that can never be joined
// to a row or cleared, so it is an error rather than a silent drop.
func (e Envelope) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported envelope version %d", e.Version)
	}
	if e.Source == "" {
		return errors.New("missing source")
	}
	if !e.Event.Valid() {
		return fmt.Errorf("unsupported event %q", e.Event)
	}
	if e.SessionID == "" && e.CWD == "" {
		return errors.New("event carries neither session_id nor cwd")
	}
	return nil
}

// Line renders the envelope as the newline-terminated JSON the socket expects.
func (e Envelope) Line() ([]byte, error) {
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// textLimit keeps a runaway assistant message from filling the socket buffer.
// The activity UI renders one line, so anything beyond this is never seen.
const textLimit = 400

func truncate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= textLimit {
		return value
	}
	// Cut on a rune boundary so the JSON stays valid UTF-8.
	cut := textLimit
	for cut > 0 && !isRuneStart(value[cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

func timestamp(now time.Time) string {
	// Separate hook commands can be invoked during the same wall-clock second,
	// and the presence socket reads separate connections concurrently. Keeping
	// the caller's fractional seconds lets the receiver restore source order
	// when goroutine scheduling delivers those commands out of order. RFC3339Nano
	// is wire-compatible with the previous whole-second RFC3339 value.
	return now.Format(time.RFC3339Nano)
}
