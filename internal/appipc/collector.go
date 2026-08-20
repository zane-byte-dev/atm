package appipc

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/ipc"
)

// CollectionHistoryRequest starts from a configured source. Connector/name
// discovery remains a broader public CLI feature, not desktop protocol policy.
type CollectionHistoryRequest struct {
	SourceID string `json:"source_id"`
	Limit    int    `json:"limit,omitempty"`
	Local    bool   `json:"local,omitempty"`
}

func registerCollector(registry *ipc.Registry, dependencies Dependencies) {
	bind(registry, "collect.snapshot", func(
		ctx context.Context,
		call application.Call,
		input collector.SnapshotInput,
	) (collector.Snapshot, error) {
		return dependencies.Collector.Snapshot(ctx, call, input)
	})
	bind(registry, "collect.run", func(
		ctx context.Context,
		call application.Call,
		input collector.RunInput,
	) (collector.RunReport, error) {
		return dependencies.Collector.RunCollection(ctx, call, input)
	})
	bind(registry, "collect.history", func(
		ctx context.Context,
		call application.Call,
		input CollectionHistoryRequest,
	) (collector.HistoryResult, error) {
		return dependencies.Collector.History(ctx, call, collector.HistoryInput{
			Reference: input.SourceID,
			Limit:     input.Limit,
			Local:     input.Local,
		})
	})

	bind(registry, "collect.source.save", func(
		ctx context.Context,
		call application.Call,
		input collector.SaveSourceInput,
	) (collector.SaveSourceResult, error) {
		return dependencies.Collector.SaveSource(ctx, call, input)
	})
	bind(registry, "collect.source.search", func(
		ctx context.Context,
		call application.Call,
		input collector.SearchSourcesInput,
	) (collector.SearchSourcesResult, error) {
		return dependencies.Collector.SearchSources(ctx, call, input)
	})
	bind(registry, "collect.source.enabled", func(
		ctx context.Context,
		call application.Call,
		input collector.SetSourceEnabledInput,
	) (collector.SetSourceEnabledResult, error) {
		return dependencies.Collector.SetSourceEnabled(ctx, call, input)
	})
	bind(registry, "collect.source.muted", func(
		ctx context.Context,
		call application.Call,
		input collector.SetSourceMutedInput,
	) (collector.SetSourceMutedResult, error) {
		return dependencies.Collector.SetSourceMuted(ctx, call, input)
	})
	bind(registry, "collect.source.delete", func(
		ctx context.Context,
		call application.Call,
		input collector.DeleteSourceInput,
	) (collector.DeleteSourceResult, error) {
		return dependencies.Collector.DeleteSource(ctx, call, input)
	})

	bind(registry, "collect.item.reprocess", func(
		ctx context.Context,
		call application.Call,
		input collector.ReprocessInput,
	) (collector.ItemResult, error) {
		return dependencies.Collector.Reprocess(ctx, call, input)
	})
	bind(registry, "collect.item.save_conclusion", func(
		ctx context.Context,
		call application.Call,
		input collector.SaveConclusionInput,
	) (collector.ItemResult, error) {
		return dependencies.Collector.SaveConclusion(ctx, call, input)
	})
	bind(registry, "collect.item.promote", func(
		ctx context.Context,
		call application.Call,
		input collector.PromoteInput,
	) (collector.ItemResult, error) {
		return dependencies.Collector.Promote(ctx, call, input)
	})
	bind(registry, "collect.item.correct", func(
		ctx context.Context,
		call application.Call,
		input collector.CorrectInput,
	) (collector.ItemResult, error) {
		return dependencies.Collector.Correct(ctx, call, input)
	})
	bind(registry, "collect.item.revert", func(
		ctx context.Context,
		call application.Call,
		input collector.RevertInput,
	) (collector.ItemResult, error) {
		return dependencies.Collector.Revert(ctx, call, input)
	})
	bind(registry, "collect.item.read", func(
		ctx context.Context,
		call application.Call,
		input collector.SetItemsReadInput,
	) (collector.SetItemsReadResult, error) {
		return dependencies.Collector.SetItemsRead(ctx, call, input)
	})
	bind(registry, "collect.item.archive", func(
		ctx context.Context,
		call application.Call,
		input collector.SetItemsArchivedInput,
	) (collector.SetItemsArchivedResult, error) {
		return dependencies.Collector.SetItemsArchived(ctx, call, input)
	})
	bind(registry, "collect.item.delete", func(
		ctx context.Context,
		call application.Call,
		input collector.DeleteItemsInput,
	) (collector.DeleteItemsResult, error) {
		return dependencies.Collector.DeleteItems(ctx, call, input)
	})
}
