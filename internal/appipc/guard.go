package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// Guard exposes read paths here and nothing else. `_ipc` is locally replayable
// and carries no proof that ATM.app rather than an Agent launched it, so the
// service refuses OriginIPC for approve/deny and for every installation or rule
// change; see the policy comment in guard.Service.Decide. Reading which actions
// are pending, which shims are installed and which rules exist tells the desktop
// what to render without letting the transport decide anything.
//
// This is the opposite call from Todo lifecycle, which does admit OriginIPC: the
// question is whether the transport can escalate. `atm todo done` is already
// something an Agent may run from a plain terminal. `atm guard approve` exists
// precisely to require a human an Agent cannot impersonate.
type GuardListRequest struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GuardStatusRequest reports shim installation state. Tools nil means every
// registered tool; Bin is the search root used to locate a tool's real binary.
type GuardStatusRequest struct {
	Tools []string `json:"tools,omitempty"`
	Bin   string   `json:"bin,omitempty"`
}

// GuardRuleListRequest uses a pointer so "every tool" stays distinguishable from
// an explicitly empty tool name, which is invalid.
type GuardRuleListRequest struct {
	Tool *string `json:"tool,omitempty"`
}

func registerGuard(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "guard.list", func(
		ctx context.Context,
		call application.Call,
		input GuardListRequest,
	) (guard.ListResult, error) {
		result, _, err := dependencies.Guard.List(ctx, call, guard.ListInput{
			Status: input.Status,
			Limit:  input.Limit,
		})
		return result, err
	})
	bind(registry, "guard.status", func(
		ctx context.Context,
		call application.Call,
		input GuardStatusRequest,
	) (guard.ShimResult, error) {
		return dependencies.Guard.StatusTools(ctx, call, guard.ShimInput{
			Tools: input.Tools,
			Bin:   input.Bin,
		})
	})
	bind(registry, "guard.rule.list", func(
		ctx context.Context,
		call application.Call,
		input GuardRuleListRequest,
	) (guard.ListRulesResult, error) {
		return dependencies.Guard.ListRules(ctx, call, guard.ListRulesInput{Tool: input.Tool})
	})
}
