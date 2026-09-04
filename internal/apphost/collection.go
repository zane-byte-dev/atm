package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Collection reads deliberately do not use collector.Snapshot or History:
// Snapshot opens the migration-capable store and even local History prunes it.
// This adapter only reads the durable ledger, never a connector or model.
type CollectionOverviewResult struct {
	Enabled         bool                         `json:"enabled"`
	IntervalMinutes int                          `json:"interval_minutes"`
	LookbackMinutes int                          `json:"lookback_minutes"`
	RetentionDays   int                          `json:"message_retention_days"`
	WorkerOwned     bool                         `json:"worker_owned"`
	WorkerStatus    string                       `json:"worker_status"`
	Summary         store.CollectionSummary      `json:"summary"`
	Sources         []store.CollectionSource     `json:"sources"`
	Runs            []store.CollectionRun        `json:"runs"`
	Messages        store.CollectionMessageStats `json:"messages"`
}

type CollectionListInput struct {
	SourceID string `json:"source_id,omitempty"`
	State    string `json:"state,omitempty"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

type CollectionItemSummary struct {
	ID             string `json:"id"`
	SourceID       string `json:"source_id"`
	Connector      string `json:"connector"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Sender         string `json:"sender"`
	Action         string `json:"action"`
	Status         string `json:"status"`
	Project        string `json:"project"`
	TodoID         string `json:"todo_id"`
	ReadAt         int64  `json:"read_at"`
	ArchivedAt     int64  `json:"archived_at"`
	OccurredAt     int64  `json:"occurred_at"`
	UpdatedAt      int64  `json:"updated_at"`
	ProposedAction string `json:"proposed_action"`
}

type CollectionListResult struct {
	Items  []CollectionItemSummary `json:"items"`
	Total  int                     `json:"total"`
	Limit  int                     `json:"limit"`
	Offset int                     `json:"offset"`
}

type CollectionItemInput struct {
	ItemID string `json:"item_id"`
}
type CollectionItemResult struct {
	Item store.CollectionItem `json:"item"`
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
	Source store.CollectionSource `json:"source"`
}
type CollectionHistoryInput struct {
	SourceID string `json:"source_id"`
	Query    string `json:"query,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}
type CollectionHistoryResult struct {
	Source   store.CollectionSource    `json:"source"`
	Messages []store.CollectionMessage `json:"messages"`
	Local    bool                      `json:"local"`
	Limit    int                       `json:"limit"`
}

func (h *Host) callCollection(ctx context.Context, call application.Call, method string, input json.RawMessage) (any, error) {
	write := method == "collect.item.read" || method == "collect.item.archive" ||
		method == "collect.source.enabled" || method == "collect.source.muted" ||
		method == "collect.source.save" || method == "collect.source.delete"
	if write {
		if !h.gate.TryLock() {
			return nil, configBusy()
		}
		defer h.gate.Unlock()
		if err := validateWrite(ctx, call); err != nil {
			return nil, err
		}
	} else {
		h.gate.RLock()
		defer h.gate.RUnlock()
		if err := validate(ctx, call); err != nil {
			return nil, err
		}
	}
	switch method {
	case "collect.overview":
		return invoke(input, func(struct{}) (any, error) {
			result, err := collectionOverview()
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
		return invoke(input, func(value CollectionItemInput) (any, error) { return collectionItem(value) })
	case "collect.history":
		return invoke(input, func(value CollectionHistoryInput) (any, error) { return collectionHistory(value) })
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
			db, err := collectionWriteDB(ctx)
			if err != nil {
				return nil, err
			}
			db.Close()
			return (collector.Service{}).SaveSource(ctx, call, value)
		})
	case "collect.source.delete":
		return invoke(input, func(value collector.DeleteSourceInput) (any, error) {
			if !collectionID(value.SourceID, "cs_") || !value.Confirmed {
				return nil, invalid("source_id and explicit deletion confirmation are required")
			}
			db, err := collectionWriteDB(ctx)
			if err != nil {
				return nil, err
			}
			db.Close()
			return (collector.Service{}).DeleteSource(ctx, call, value)
		})
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown collection API method")
	}
}

func collectionOverview() (CollectionOverviewResult, error) {
	result := CollectionOverviewResult{
		Enabled: config.CollectionEnabled, IntervalMinutes: config.CollectionIntervalMinutes,
		LookbackMinutes: config.CollectionLookbackMinutes, RetentionDays: config.CollectionMessageRetentionDays,
		WorkerStatus: "external_status_unknown", Sources: []store.CollectionSource{}, Runs: []store.CollectionRun{},
	}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, unavailable(err)
	}
	defer db.Close()
	overview, err := store.LoadCollectionOverview(db, 1)
	if err != nil {
		return result, unavailable(err)
	}
	result.Summary, result.Sources, result.Runs = overview.Summary, overview.Sources, overview.Runs
	result.Messages, err = store.CollectionMessageStatsFor(db)
	return result, collectionError(err)
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
	where := " WHERE (?='' OR source_id=?)"
	args := []any{input.SourceID, input.SourceID}
	switch input.State {
	case "", "active":
		where += " AND archived_at=0"
	case "unread":
		where += " AND archived_at=0 AND read_at=0"
	case "read":
		where += " AND archived_at=0 AND read_at>0"
	case "archived":
		where += " AND archived_at>0"
	case "all":
	default:
		return CollectionListResult{}, invalid("invalid collection state")
	}
	if query := strings.TrimSpace(input.Query); query != "" {
		where += " AND instr(lower(title || char(10) || summary || char(10) || raw_context || char(10) || sender || char(10) || project),lower(?))>0"
		args = append(args, query)
	}
	result := CollectionListResult{Items: []CollectionItemSummary{}, Limit: input.Limit, Offset: input.Offset}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, unavailable(err)
	}
	defer db.Close()
	// Count and page share a snapshot when the external collector commits while
	// a browser request is running. Results have a deterministic tie breaker.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, unavailable(err)
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM collection_items"+where, args...).Scan(&result.Total); err != nil {
		return result, unavailable(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,source_id,connector,title,substr(summary,1,480),sender,action,status,
		project,COALESCE(todo_id,''),read_at,archived_at,occurred_at,updated_at,proposed_action
		FROM collection_items`+where+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, input.Limit, input.Offset)...)
	if err != nil {
		return result, unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CollectionItemSummary
		if err := rows.Scan(&item.ID, &item.SourceID, &item.Connector, &item.Title, &item.Summary, &item.Sender,
			&item.Action, &item.Status, &item.Project, &item.TodoID, &item.ReadAt, &item.ArchivedAt,
			&item.OccurredAt, &item.UpdatedAt, &item.ProposedAction); err != nil {
			return result, unavailable(err)
		}
		result.Items = append(result.Items, item)
	}
	return result, collectionError(rows.Err())
}

func collectionItem(input CollectionItemInput) (CollectionItemResult, error) {
	if !collectionID(input.ItemID, "ci_") {
		return CollectionItemResult{}, invalid("invalid item_id")
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return CollectionItemResult{}, collectionError(err)
	}
	defer db.Close()
	item, err := store.GetCollectionItem(db, input.ItemID)
	return CollectionItemResult{Item: item}, collectionError(err)
}

func collectionHistory(input CollectionHistoryInput) (CollectionHistoryResult, error) {
	if !collectionID(input.SourceID, "cs_") || input.Limit < 0 || input.Limit > 200 || len(input.Query) > 500 {
		return CollectionHistoryResult{}, invalid("a valid source_id, limit between 1–200, and query at most 500 bytes are required")
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	result := CollectionHistoryResult{Messages: []store.CollectionMessage{}, Local: true, Limit: input.Limit}
	db, err := store.OpenReadOnly()
	if err != nil {
		return result, collectionError(err)
	}
	defer db.Close()
	result.Source, err = store.GetCollectionSource(db, input.SourceID)
	if err != nil {
		return result, collectionError(err)
	}
	query := store.CollectionMessageQuery{Connector: result.Source.Connector, ConversationID: result.Source.ExternalID, Limit: input.Limit}
	if strings.TrimSpace(input.Query) != "" {
		result.Messages, err = store.SearchCollectionMessages(db, strings.TrimSpace(input.Query), query)
	} else {
		result.Messages, err = store.ListCollectionMessages(db, query)
	}
	return result, collectionError(err)
}

// Verify the existing schema without opening a migration-capable handle. The
// application mutation is only invoked for a current account, while the host
// gate serializes its own writes. An older account remains fully read-only.
func collectionWriteDB(ctx context.Context) (*sql.DB, error) {
	db, err := store.OpenReadOnly()
	if err != nil {
		return nil, unavailable(err)
	}
	var version int
	if err = db.QueryRowContext(ctx, "SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		db.Close()
		return nil, unavailable(err)
	}
	if version != store.SchemaVersion {
		db.Close()
		return nil, application.NewError(application.CodeForbidden, "collection changes require the current database schema; this account remains read-only")
	}
	return db, nil
}

func changeCollectionItem(ctx context.Context, call application.Call, id string, value, archive bool) (CollectionItemResult, error) {
	if !collectionID(id, "ci_") {
		return CollectionItemResult{}, invalid("invalid item_id")
	}
	db, err := collectionWriteDB(ctx)
	if err != nil {
		return CollectionItemResult{}, err
	}
	defer db.Close()
	if _, err := store.GetCollectionItem(db, id); err != nil {
		return CollectionItemResult{}, collectionError(err)
	}
	service := collector.Service{}
	if archive {
		result, err := service.SetItemsArchived(ctx, call, collector.SetItemsArchivedInput{ItemIDs: []string{id}, Archived: value})
		if err != nil {
			return CollectionItemResult{}, err
		}
		return CollectionItemResult{Item: result.Items[0]}, nil
	}
	result, err := service.SetItemsRead(ctx, call, collector.SetItemsReadInput{ItemIDs: []string{id}, Read: value})
	if err != nil {
		return CollectionItemResult{}, err
	}
	return CollectionItemResult{Item: result.Items[0]}, nil
}

func changeCollectionSource(ctx context.Context, call application.Call, id string, value, mute bool) (CollectionSourceResult, error) {
	if !collectionID(id, "cs_") {
		return CollectionSourceResult{}, invalid("invalid source_id")
	}
	db, err := collectionWriteDB(ctx)
	if err != nil {
		return CollectionSourceResult{}, err
	}
	defer db.Close()
	if _, err := store.GetCollectionSource(db, id); err != nil {
		return CollectionSourceResult{}, collectionError(err)
	}
	service := collector.Service{}
	if mute {
		_, err = service.SetSourceMuted(ctx, call, collector.SetSourceMutedInput{SourceID: id, Muted: value})
	} else {
		_, err = service.SetSourceEnabled(ctx, call, collector.SetSourceEnabledInput{SourceID: id, Enabled: value})
	}
	if err != nil {
		return CollectionSourceResult{}, collectionError(err)
	}
	source, err := store.GetCollectionSource(db, id)
	return CollectionSourceResult{Source: source}, collectionError(err)
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

func collectionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrDatabaseMissing) {
		return application.NewError(application.CodeNotFound, "collection record not found")
	}
	return unavailable(err)
}
