package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// GuardListRequest deliberately exposes only the read path. `_ipc` is locally
// replayable and cannot authorize Guard approve/deny decisions.
type GuardListRequest struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
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
}
