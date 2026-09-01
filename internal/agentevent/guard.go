package agentevent

import (
	"encoding/json"
	"net"
	"time"
)

// TypeGuardRequest marks a guard message on the notch socket.
//
// The socket carries a discriminated union rather than a sixth Kind. A new Kind
// would be read by every consumer of an agent event — it would credit a session
// with turn-state reporting, join a cwd, and force a session refresh — none of
// which is true of an approval request, which is not about a session at all.
// Messages with no `type` are agent events, so every hook already installed keeps
// working byte for byte and an older app simply fails to decode the new shape and
// drops it.
const TypeGuardRequest = "guard_request"

// GuardEnvelope is one pending approval, pushed the moment it is created so the
// app can raise a banner instead of waiting up to a minute to notice the row.
//
// The socket is a courtesy channel, not the record: the request is already in the
// database before this is sent, and the app rebuilds the same list from the CLI
// on its own schedule. Anything undeliverable here is therefore a no-op, not a
// failure.
type GuardEnvelope struct {
	Version int    `json:"v"`
	Type    string `json:"type"`
	ID      string `json:"id"`
	Tool    string `json:"tool"`
	Label   string `json:"label,omitempty"`
	Target  string `json:"target,omitempty"`
	Title   string `json:"title,omitempty"`
	// Body is already capped by the caller at the same limit the database stores,
	// so the banner and the approval card cannot disagree about how much of a
	// message the user saw before deciding.
	Body      string `json:"body,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	Agent     string `json:"agent,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

// Line renders the envelope as the newline-terminated JSON the socket expects.
func (e GuardEnvelope) Line() ([]byte, error) {
	if e.Version == 0 {
		e.Version = Version
	}
	e.Type = TypeGuardRequest
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// DeliverGuard pushes one pending approval to the notch.
//
// Unlike Deliver, the error is worth looking at: the caller uses failure as the
// signal to raise a local banner itself, because the whole point of the request
// is that somebody has to see it. It must never take longer than DeliverTimeout,
// though — an agent is blocked on this call.
func DeliverGuard(envelope GuardEnvelope) error {
	line, err := envelope.Line()
	if err != nil {
		return err
	}
	path := SocketPath()
	if err := CheckSocketPath(path); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", path, DeliverTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(DeliverTimeout)); err != nil {
		return err
	}
	_, err = conn.Write(line)
	return err
}
