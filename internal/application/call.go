package application

import "strings"

// ActorKind describes whose authority an application use case runs with.
//
// It is an authorization and audit identity, not proof of operating-system
// identity. Adapters are responsible for deriving it from their trusted call
// context rather than accepting it from ordinary request parameters.
type ActorKind string

const (
	ActorHuman      ActorKind = "human"
	ActorAgent      ActorKind = "agent"
	ActorController ActorKind = "controller"
)

// Valid reports whether kind is one of the stable actor kinds.
func (kind ActorKind) Valid() bool {
	switch kind {
	case ActorHuman, ActorAgent, ActorController:
		return true
	default:
		return false
	}
}

// Origin identifies the adapter through which a use case was invoked. It is
// kept separate from ActorKind: for example, a human action in the desktop app
// has ActorHuman with OriginIPC.
type Origin string

const (
	OriginCLI        Origin = "cli"
	OriginIPC        Origin = "ipc"
	OriginController Origin = "controller"
	OriginHook       Origin = "hook"
)

// Valid reports whether origin is one of the stable application origins.
func (origin Origin) Valid() bool {
	switch origin {
	case OriginCLI, OriginIPC, OriginController, OriginHook:
		return true
	default:
		return false
	}
}

// Actor carries the provenance needed for authorization and audit. Empty
// identifiers mean that the corresponding concept does not apply to this call.
// BindingID is the durable database identifier; zero means no binding.
type Actor struct {
	Kind      ActorKind `json:"kind"`
	Origin    Origin    `json:"origin"`
	SessionID string    `json:"session_id,omitempty"`
	BindingID int64     `json:"binding_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Agent     string    `json:"agent,omitempty"`
}

// Validate checks only the universal shape of an actor. Use-case-specific
// policy, such as requiring an Agent session for plan updates, belongs in the
// application service that owns that use case.
func (actor Actor) Validate() error {
	if !actor.Kind.Valid() {
		return invalidCallField("actor.kind", string(actor.Kind), "unknown actor kind")
	}
	if !actor.Origin.Valid() {
		return invalidCallField("actor.origin", string(actor.Origin), "unknown call origin")
	}
	if actor.BindingID < 0 {
		return invalidCallField("actor.binding_id", actor.BindingID, "binding ID cannot be negative")
	}
	return nil
}

// Call is the context supplied to every application use case. RequestID is
// generated at the adapter edge and is suitable for correlation and mutation
// idempotency; it must not be derived from mutable business data.
type Call struct {
	RequestID string `json:"request_id"`
	Actor     Actor  `json:"actor"`
}

// Validate checks the transport-independent call shape.
func (call Call) Validate() error {
	if strings.TrimSpace(call.RequestID) == "" {
		return invalidCallField("request_id", call.RequestID, "request ID is required")
	}
	return call.Actor.Validate()
}

func invalidCallField(field string, value any, message string) *Error {
	return &Error{
		Code:    CodeInvalidArgument,
		Message: message,
		Details: map[string]any{
			"field": field,
			"value": value,
		},
	}
}
