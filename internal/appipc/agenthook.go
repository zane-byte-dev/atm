package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// Hook registration is a write the desktop is allowed to make. Unlike Guard
// installation, which refuses OriginIPC because an approval must require a human
// an Agent cannot impersonate, everything installed here is a reporting hook: it
// cannot block a tool call or answer a permission prompt, so a replayable
// transport cannot escalate anything by calling it.
//
// AgentHookRequest narrows the work to one agent. Empty means every agent ATM
// knows how to wire up, which is what the settings pane's three buttons ask for.
type AgentHookRequest struct {
	Source string `json:"source,omitempty"`
}

func registerAgentHook(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "agent.hook.install", func(
		ctx context.Context,
		call application.Call,
		input AgentHookRequest,
	) (agentevent.RegistrationReport, error) {
		return dependencies.AgentHook.Install(ctx, call, agentevent.RegistrationInput{Source: input.Source})
	})
	bind(registry, "agent.hook.uninstall", func(
		ctx context.Context,
		call application.Call,
		input AgentHookRequest,
	) (agentevent.RegistrationReport, error) {
		return dependencies.AgentHook.Uninstall(ctx, call, agentevent.RegistrationInput{Source: input.Source})
	})
	bind(registry, "agent.hook.status", func(
		ctx context.Context,
		call application.Call,
		input AgentHookRequest,
	) (agentevent.RegistrationReport, error) {
		return dependencies.AgentHook.Status(ctx, call, agentevent.RegistrationInput{Source: input.Source})
	})
}
