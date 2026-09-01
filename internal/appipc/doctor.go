package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// doctor.check is read-only and takes no parameters: the self-check always reports
// on everything, because one that could be aimed would need the user to already
// know where the problem is.
//
// The typed Report is the shape `atm doctor --json` serializes as well, so the
// desktop and the CLI cannot drift on the key names — which is what replaced the
// hand-written map that used to guard against exactly that.
func registerDoctor(registry *ipc.Registry, dependencies Dependencies) {
	bindNoRequest(registry, "doctor.check", func(
		ctx context.Context,
		call application.Call,
	) (doctorapp.Report, error) {
		return dependencies.Doctor.Check(ctx, call, doctorapp.Input{})
	})
}
