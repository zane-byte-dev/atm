package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

func registerAIDay(registry *ipc.Registry, dependencies Dependencies) {
	bindNoRequest(registry, "day.snapshot", func(
		ctx context.Context,
		_ application.Call,
	) (aiday.Dashboard, error) {
		result, _, err := dependencies.AIDay.Dashboard(ctx, aiday.DashboardInput{Days: 180, Sync: false})
		return result, err
	})
	bind(registry, "day.show", func(
		ctx context.Context,
		_ application.Call,
		input aiday.DayInput,
	) (aiday.Result, error) {
		return dependencies.AIDay.Show(ctx, input.Day)
	})
	bind(registry, "day.feedback", func(
		ctx context.Context,
		_ application.Call,
		input aiday.FeedbackInput,
	) (aiday.Result, error) {
		return dependencies.AIDay.Feedback(ctx, input)
	})
	bind(registry, "day.source.set", func(
		ctx context.Context,
		_ application.Call,
		input aiday.SourceInput,
	) (aiday.Privacy, error) {
		return dependencies.AIDay.SetSource(ctx, input)
	})
	bind(registry, "day.source.delete", func(
		ctx context.Context,
		_ application.Call,
		input aiday.SourceDeleteInput,
	) (aiday.SourceDeleteResult, error) {
		return dependencies.AIDay.DeleteSource(ctx, input.Source, input.Confirmed)
	})
	bind(registry, "day.privacy.set", func(
		ctx context.Context,
		_ application.Call,
		patch aiday.PrivacyPatch,
	) (aiday.Privacy, error) {
		return dependencies.AIDay.SetPrivacy(ctx, patch)
	})
	bind(registry, "day.data.delete", func(
		ctx context.Context,
		_ application.Call,
		input aiday.DeleteInput,
	) (aiday.DeleteSummary, error) {
		return dependencies.AIDay.Delete(ctx, input)
	})
	bindNoRequest(registry, "day.data.export", func(
		ctx context.Context,
		_ application.Call,
	) (aiday.Export, error) {
		return dependencies.AIDay.Export(ctx)
	})
}
