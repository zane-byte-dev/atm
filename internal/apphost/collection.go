package apphost

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
)

// Collection reads use collector.QueryService rather than Snapshot or History:
// the latter can migrate or prune. This adapter now only validates Web input,
// composes runtime status and maps application results to the browser contract.
type CollectionOverviewResult struct {
	Enabled         bool                   `json:"enabled"`
	IntervalMinutes int                    `json:"interval_minutes"`
	LookbackMinutes int                    `json:"lookback_minutes"`
	RetentionDays   int                    `json:"message_retention_days"`
	WorkerOwned     bool                   `json:"worker_owned"`
	WorkerStatus    string                 `json:"worker_status"`
	Summary         CollectionSummary      `json:"summary"`
	Sources         []CollectionSource     `json:"sources"`
	Runs            []CollectionRun        `json:"runs"`
	Messages        CollectionMessageStats `json:"messages"`
}

type CollectionSource = collector.WorkspaceCollectionSource
type CollectionRun = collector.WorkspaceCollectionRun
type CollectionSummary = collector.WorkspaceCollectionSummary
type CollectionMessageStats = collector.WorkspaceCollectionMessageStats

type CollectionListInput struct {
	SourceID string `json:"source_id,omitempty"`
	State    string `json:"state,omitempty"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

type CollectionItemSummary = collector.WorkspaceCollectionItemSummary
type CollectionListResult = collector.WorkspaceCollectionList

type CollectionItemInput struct {
	ItemID string `json:"item_id"`
}
type CollectionItemResult struct {
	Item CollectionItem `json:"item"`
}
type CollectionReadInput struct {
	ItemID string `json:"item_id"`
	Read   *bool  `json:"read"`
}
type CollectionArchiveInput struct {
	ItemID   string `json:"item_id"`
	Archived *bool  `json:"archived"`
}
type CollectionSourceEnabledInput struct {
	SourceID string `json:"source_id"`
	Enabled  *bool  `json:"enabled"`
}
type CollectionSourceMutedInput struct {
	SourceID string `json:"source_id"`
	Muted    *bool  `json:"muted"`
}
type CollectionSourceResult struct {
	Source CollectionSource `json:"source"`
}
type CollectionHistoryInput struct {
	SourceID string `json:"source_id"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type CollectionHistoryResult = collector.WorkspaceCollectionHistory
type CollectionItem = collector.WorkspaceCollectionItem
type CollectionMessage = collector.WorkspaceCollectionMessage

func (h *Host) callCollection(ctx context.Context, call application.Call, method string, input json.RawMessage) (any, error) {
	write := method == "collect.item.read" || method == "collect.item.archive" ||
		method == "collect.source.enabled" || method == "collect.source.muted" ||
		method == "collect.source.save" || method == "collect.source.delete"
	// Collection state is owned by SQLite and collector.Service, not config.json.
	// Keep only a shared config-generation pin while reading collection settings;
	// ordinary ledger mutations use their own transaction and must not compete
	// with settings writers for this process-wide gate.
	h.gate.RLock()
	defer h.gate.RUnlock()
	if write {
		if err := validateWrite(ctx, call); err != nil {
			return nil, err
		}
	} else {
		if err := validate(ctx, call); err != nil {
			return nil, err
		}
	}
	switch method {
	case "collect.overview":
		return invoke(input, func(struct{}) (any, error) {
			result, err := collectionOverview(ctx)
			jobs, _ := h.attachedRuntime()
			if jobs != nil {
				result.WorkerOwned, result.WorkerStatus = true, "idle"
				if !result.Enabled {
					result.WorkerStatus = "disabled"
				}
				if recent, jobErr := jobs.List(ctx, 30); jobErr == nil {
					for _, job := range recent {
						if (job.Kind == background.CollectionRun || job.Kind == background.CollectionReprocess) && !job.Terminal() {
							result.WorkerStatus = "running"
							break
						}
					}
				}
			}
			return result, err
		})
	case "collect.items":
		return invoke(input, func(value CollectionListInput) (any, error) { return collectionItems(ctx, value) })
	case "collect.item.show":
		return invoke(input, func(value CollectionItemInput) (any, error) { return collectionItem(ctx, value) })
	case "collect.history":
		return invoke(input, func(value CollectionHistoryInput) (any, error) { return collectionHistory(ctx, value) })
	case "collect.item.read":
		return invoke(input, func(value CollectionReadInput) (any, error) {
			if value.Read == nil {
				return nil, invalid("read is required")
			}
			return changeCollectionItem(ctx, call, value.ItemID, *value.Read, false)
		})
	case "collect.item.archive":
		return invoke(input, func(value CollectionArchiveInput) (any, error) {
			if value.Archived == nil {
				return nil, invalid("archived is required")
			}
			return changeCollectionItem(ctx, call, value.ItemID, *value.Archived, true)
		})
	case "collect.source.enabled":
		return invoke(input, func(value CollectionSourceEnabledInput) (any, error) {
			if value.Enabled == nil {
				return nil, invalid("enabled is required")
			}
			return changeCollectionSource(ctx, call, value.SourceID, *value.Enabled, false)
		})
	case "collect.source.muted":
		return invoke(input, func(value CollectionSourceMutedInput) (any, error) {
			if value.Muted == nil {
				return nil, invalid("muted is required")
			}
			return changeCollectionSource(ctx, call, value.SourceID, *value.Muted, true)
		})
	case "collect.source.save":
		return invoke(input, func(value collector.SaveSourceInput) (any, error) {
			if len(value.ExternalID) > 2000 || len(value.Name) > 500 || len(value.Project) > 500 || len(value.ExcludePattern) > 2000 || len(value.Instruction) > 16000 {
				return nil, invalid("source fields exceed their supported size")
			}
			if value.KnowledgeCollection != "" && !managedCollectionID(value.KnowledgeCollection) {
				return nil, invalid("invalid knowledge collection ID")
			}
			query := collector.QueryService{}
			if err := query.VerifyCurrentSchema(ctx); err != nil {
				return nil, err
			}
			saved, err := (collector.Service{}).SaveSource(ctx, call, value)
			if err != nil {
				return nil, err
			}
			source, err := query.Source(ctx, saved.Source.ID)
			return CollectionSourceResult{Source: source}, err
		})
	case "collect.source.delete":
		return invoke(input, func(value collector.DeleteSourceInput) (any, error) {
			if !collectionID(value.SourceID, "cs_") || !value.Confirmed {
				return nil, invalid("source_id and explicit deletion confirmation are required")
			}
			query := collector.QueryService{}
			if err := query.VerifyCurrentSchema(ctx); err != nil {
				return nil, err
			}
			source, err := query.Source(ctx, value.SourceID)
			if err != nil {
				return nil, err
			}
			if _, err := (collector.Service{}).DeleteSource(ctx, call, value); err != nil {
				return nil, err
			}
			return CollectionSourceResult{Source: source}, nil
		})
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown collection API method")
	}
}

func collectionOverview(ctx context.Context) (CollectionOverviewResult, error) {
	result := CollectionOverviewResult{
		Enabled: config.CollectionEnabled, IntervalMinutes: config.CollectionIntervalMinutes,
		LookbackMinutes: config.CollectionLookbackMinutes, RetentionDays: config.CollectionMessageRetentionDays,
		WorkerStatus: "external_status_unknown", Sources: []CollectionSource{}, Runs: []CollectionRun{},
	}
	overview, err := (collector.QueryService{}).Overview(ctx)
	if err != nil {
		return result, err
	}
	result.Summary, result.Sources, result.Runs, result.Messages = overview.Summary, overview.Sources, overview.Runs, overview.Messages
	return result, nil
}

func collectionItems(ctx context.Context, input CollectionListInput) (CollectionListResult, error) {
	if input.Limit < 0 || input.Limit > 100 || input.Offset < 0 || len(input.Query) > 500 {
		return CollectionListResult{}, invalid("limit must be 1–100, offset non-negative, and query at most 500 bytes")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.SourceID != "" && !collectionID(input.SourceID, "cs_") {
		return CollectionListResult{}, invalid("invalid source_id")
	}
	return (collector.QueryService{}).Items(ctx, collector.WorkspaceCollectionListInput{
		SourceID: input.SourceID, State: input.State, Query: input.Query, Limit: input.Limit, Offset: input.Offset,
	})
}

func collectionItem(ctx context.Context, input CollectionItemInput) (CollectionItemResult, error) {
	if !collectionID(input.ItemID, "ci_") {
		return CollectionItemResult{}, invalid("invalid item_id")
	}
	item, err := (collector.QueryService{}).Item(ctx, input.ItemID)
	return CollectionItemResult{Item: item}, err
}

func collectionHistory(ctx context.Context, input CollectionHistoryInput) (CollectionHistoryResult, error) {
	if !collectionID(input.SourceID, "cs_") || input.Limit < 0 || input.Limit > 200 || len(input.Query) > 500 {
		return CollectionHistoryResult{}, invalid("a valid source_id, limit between 1–200, and query at most 500 bytes are required")
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	return (collector.QueryService{}).History(ctx, collector.WorkspaceCollectionHistoryInput{
		SourceID: input.SourceID, Query: input.Query, Limit: input.Limit,
	})
}

func changeCollectionItem(ctx context.Context, call application.Call, id string, value, archive bool) (CollectionItemResult, error) {
	if !collectionID(id, "ci_") {
		return CollectionItemResult{}, invalid("invalid item_id")
	}
	query := collector.QueryService{}
	if err := query.VerifyCurrentSchema(ctx); err != nil {
		return CollectionItemResult{}, err
	}
	if _, err := query.Item(ctx, id); err != nil {
		return CollectionItemResult{}, err
	}
	service := collector.Service{}
	if archive {
		if _, err := service.SetItemsArchived(ctx, call, collector.SetItemsArchivedInput{ItemIDs: []string{id}, Archived: value}); err != nil {
			return CollectionItemResult{}, err
		}
	} else if _, err := service.SetItemsRead(ctx, call, collector.SetItemsReadInput{ItemIDs: []string{id}, Read: value}); err != nil {
		return CollectionItemResult{}, err
	}
	item, err := query.Item(ctx, id)
	return CollectionItemResult{Item: item}, err
}

func changeCollectionSource(ctx context.Context, call application.Call, id string, value, mute bool) (CollectionSourceResult, error) {
	if !collectionID(id, "cs_") {
		return CollectionSourceResult{}, invalid("invalid source_id")
	}
	query := collector.QueryService{}
	if err := query.VerifyCurrentSchema(ctx); err != nil {
		return CollectionSourceResult{}, err
	}
	if _, err := query.Source(ctx, id); err != nil {
		return CollectionSourceResult{}, err
	}
	service := collector.Service{}
	var err error
	if mute {
		_, err = service.SetSourceMuted(ctx, call, collector.SetSourceMutedInput{SourceID: id, Muted: value})
	} else {
		_, err = service.SetSourceEnabled(ctx, call, collector.SetSourceEnabledInput{SourceID: id, Enabled: value})
	}
	if err != nil {
		return CollectionSourceResult{}, err
	}
	source, err := query.Source(ctx, id)
	return CollectionSourceResult{Source: source}, err
}

func collectionID(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+16 {
		return false
	}
	for _, char := range value[len(prefix):] {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}
