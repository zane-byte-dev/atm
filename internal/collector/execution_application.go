package collector

import (
	"context"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
)

type RunInput struct {
	SourceID string `json:"source_id,omitempty"`
	DueOnly  bool   `json:"due_only,omitempty"`
}

// RunCollection is the typed application entry point shared by CLI and Web.
// The older Run and RunDue methods remain the domain execution primitives.
func (service Service) RunCollection(
	ctx context.Context,
	call application.Call,
	input RunInput,
) (RunReport, error) {
	ctx, err := validateSourceCall(ctx, call, false)
	if err != nil {
		return RunReport{}, err
	}
	sourceID := strings.TrimSpace(input.SourceID)
	var report RunReport
	if input.DueOnly {
		report, err = service.RunDue(ctx, sourceID)
	} else {
		report, err = service.Run(ctx, sourceID)
	}
	if err == nil {
		return report, nil
	}
	if strings.Contains(err.Error(), "enabled collection source not found") {
		notFound := application.WrapError(
			application.CodeNotFound,
			"enabled collection source not found: "+sourceID,
			err,
		)
		notFound.Details = map[string]any{"source_id": sourceID}
		return report, notFound
	}
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return report, appErr
	}
	unavailable := sourceUnavailable("run collection", err)
	if sourceID != "" {
		unavailable.Details = map[string]any{"source_id": sourceID}
	}
	return report, unavailable
}
