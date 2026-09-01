package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// DashboardRequest keeps sync and agent filtering out of the desktop contract.
// The App schedules sync separately and consumes the all-agent projection.
type DashboardRequest struct {
	Sections  []string `json:"sections,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
}

func registerDashboard(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "dashboard.snapshot", func(
		ctx context.Context,
		call application.Call,
		input DashboardRequest,
	) (dashboard.Snapshot, error) {
		return dependencies.Dashboard.BuildSnapshot(ctx, call, dashboard.Request{
			Sections:  input.Sections,
			SessionID: input.SessionID,
		})
	})
}
