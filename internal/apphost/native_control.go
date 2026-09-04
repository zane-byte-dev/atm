package apphost

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
)

const NativeControlSessionSync = "session.sync"

// NativeSessionSyncInput deliberately has no job kind or scheduler-only fields.
// The authenticated adapter can enqueue a session index refresh, never choose an
// arbitrary background operation.
type NativeSessionSyncInput struct {
	IdempotencyKey string `json:"idempotency_key"`
	Agent          string `json:"agent,omitempty"`
}

// NativeControl is reachable only through web.Server's bearer capability and
// instance binding. It repeats that boundary check so an alternate adapter
// cannot trust a caller-supplied presentation identity.
func (h *Host) NativeControl(ctx context.Context, call application.Call, method string, raw json.RawMessage) (any, error) {
	if err := validate(ctx, call); err != nil {
		return nil, err
	}
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginNativeControl ||
		call.Actor.SessionID != "" || call.Actor.BindingID != 0 || call.Actor.Agent != "" {
		return nil, application.NewError(application.CodeForbidden, "native control requires the local control capability")
	}
	if method != NativeControlSessionSync {
		return nil, application.NewError(application.CodeNotFound, "unknown native control method")
	}
	return invoke(raw, func(input NativeSessionSyncInput) (any, error) {
		if !validNativeIdempotencyKey(input.IdempotencyKey) {
			return nil, invalid("idempotency_key is required and must be a bounded opaque identifier")
		}
		if !validNativeAgent(input.Agent) {
			return nil, invalid("agent must be a bounded agent name")
		}
		jobs, _ := h.attachedRuntime()
		if jobs == nil {
			return nil, runtimeUnavailable()
		}
		var job background.Job
		err := h.WithConfig(ctx, func(ctx context.Context) error {
			var runErr error
			job, runErr = jobs.Run(ctx, call, background.Request{Kind: background.SessionSync, Agent: strings.TrimSpace(input.Agent)}, input.IdempotencyKey)
			return runErr
		})
		return job, err
	})
}

func validNativeIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 160 || strings.TrimSpace(key) != key || !utf8.ValidString(key) {
		return false
	}
	for _, value := range key {
		if !(value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' ||
			value == '-' || value == '_' || value == '.' || value == ':') {
			return false
		}
	}
	return true
}

func validNativeAgent(agent string) bool {
	if len(agent) > 100 || strings.TrimSpace(agent) != agent || strings.ContainsRune(agent, '\x00') {
		return false
	}
	return utf8.ValidString(agent)
}
