package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
	quotaapp "github.com/zane-byte-dev/atm/internal/quota"
)

// QuotaRequest narrows the read. An empty Agent asks for every source ATM can
// read, which is what the desktop's dashboard wants.
//
// Live is deliberately absent: it opts into a billing endpoint over the network,
// and that choice belongs to the user's configuration rather than to a request a
// replayable transport can carry. The composition root supplies the configured
// value.
type QuotaRequest struct {
	Agent string `json:"agent,omitempty"`
}

// quota.snapshot answers with the agent map alone rather than the whole snapshot
// value. That map is the published quota shape — one nullable object per agent —
// and the App has decoded it since before this method existed. Provider warnings
// stay on the CLI's stderr, which is where the App already ignores them.
func registerQuota(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "quota.snapshot", func(
		ctx context.Context,
		call application.Call,
		input QuotaRequest,
	) (map[string]*quotaapp.AgentQuota, error) {
		snapshot, err := dependencies.Quota.Snapshot(ctx, call, quotaapp.Input{
			Agent: input.Agent,
			Live:  dependencies.QuotaLiveBilling,
		})
		if err != nil {
			return nil, err
		}
		return snapshot.Agents, nil
	})
}
