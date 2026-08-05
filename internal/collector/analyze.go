package collector

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// AnalyzeOptions bounds one on-demand analysis. Automatic collection only ever
// looks forward from its checkpoint, so this is the only way to get a decision
// out of chat that already happened.
type AnalyzeOptions struct {
	Since time.Time
	Limit int
	// MaxBatches caps how many model calls one command can spend: batches are
	// "same conversation, gaps under 15 minutes", and each one is a call.
	MaxBatches int
	// Local analyses only messages already synced locally, without calling the connector.
	Local bool
	// Apply carries the decisions out instead of holding them for confirmation.
	Apply bool
}

type AnalyzeReport struct {
	SourceID   string                 `json:"source_id"`
	SourceName string                 `json:"source_name,omitempty"`
	Batches    int                    `json:"batches"`
	Analyzed   int                    `json:"analyzed"`
	Skipped    int                    `json:"skipped"`
	Proposed   int                    `json:"proposed"`
	Applied    int                    `json:"applied"`
	Insights   int                    `json:"insights"`
	Ignored    int                    `json:"ignored"`
	Failed     int                    `json:"failed"`
	Remaining  int                    `json:"remaining"`
	Items      []store.CollectionItem `json:"items"`
}

const (
	analyzeDefaultLimit      = 50
	analyzeDefaultMaxBatches = 20
)

// Analyze classifies a window of one source's chat. By default the decisions are
// held as pending proposals — nothing reaches the Todo list until a person
// confirms, which is what makes it safe to point at a week of history. The
// source checkpoint is never moved: this is a side path, and automatic
// collection must neither skip nor repeat work because of it.
func (service Service) Analyze(ctx context.Context, sourceID string,
	options AnalyzeOptions) (AnalyzeReport, error) {
	if service.Extractor == nil {
		return AnalyzeReport{}, fmt.Errorf("collector extractor is required")
	}
	if service.RegistryError != nil {
		return AnalyzeReport{}, service.RegistryError
	}
	if service.Now == nil {
		service.Now = func() time.Time { return time.Now().In(config.Loc) }
	}
	if options.Limit < 1 {
		options.Limit = analyzeDefaultLimit
	}
	if options.MaxBatches < 1 {
		options.MaxBatches = analyzeDefaultMaxBatches
	}
	db, err := store.Open()
	if err != nil {
		return AnalyzeReport{}, err
	}
	defer db.Close()
	source, err := store.GetCollectionSource(db, sourceID)
	if err != nil {
		return AnalyzeReport{}, err
	}
	report := AnalyzeReport{SourceID: source.ID, SourceName: source.Name, Items: []store.CollectionItem{}}
	messages, err := service.analyzeMessages(ctx, db, source, options)
	if err != nil {
		return report, err
	}
	batches := analysisBatches(source, messages)
	report.Batches = len(batches)
	for _, batch := range batches {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		// Already decided, or already waiting on a person: either way there is
		// nothing to spend a model call on.
		if existing, err := store.GetCollectionItemByFingerprint(db, source.Connector,
			batch.Fingerprint); err == nil && (existing.Status == "processed" || existing.ProposedAction != "") {
			report.Skipped++
			continue
		}
		if report.Analyzed >= options.MaxBatches {
			report.Remaining++
			continue
		}
		item, err := service.analyzeBatch(ctx, db, batch, options.Apply)
		if err != nil {
			report.Failed++
			continue
		}
		report.Analyzed++
		switch {
		case item.Action == "ignore":
			report.Ignored++
		case item.Action == "insight":
			report.Insights++
		case item.ProposedAction != "":
			report.Proposed++
		default:
			report.Applied++
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func (service Service) analyzeBatch(ctx context.Context, db *sql.DB, batch MessageBatch,
	apply bool) (store.CollectionItem, error) {
	item, _, err := store.PutCollectionItem(db, itemFromBatch(batch, service.Now().Unix()))
	if err != nil {
		return item, err
	}
	decision, err := service.decideBatch(ctx, batch)
	if err != nil {
		markItemFailed(db, &item, err)
		return item, err
	}
	// An ignore or an insight is already final — neither touches anything a
	// person owns, so there is nothing to confirm — and both are stored the way
	// automatic collection stores them. That also stops the next analysis from
	// paying for this batch again.
	if apply || decision.Action == "ignore" || decision.Action == "insight" {
		item, err = applyDecision(batch, item, decision)
		if err != nil {
			markItemFailed(db, &item, err)
			return item, err
		}
		return item, store.UpdateCollectionItem(db, item)
	}
	applyDecisionToItem(&item, decision)
	item.ProposedAction = decision.Action
	item.Action, item.Status, item.TodoID = "pending", "pending", ""
	return item, store.UpdateCollectionItem(db, item)
}

// analyzeMessages reads the window to analyse. Reading through a connector also syncs it, so
// analysing recent chat leaves the archive complete; --local analyses what is
// already there and never waits on the network.
func (service Service) analyzeMessages(ctx context.Context, db *sql.DB, source store.CollectionSource,
	options AnalyzeOptions) ([]Message, error) {
	if options.Local {
		query := store.CollectionMessageQuery{Connector: source.Connector,
			ConversationID: source.ExternalID, Limit: options.Limit}
		if !options.Since.IsZero() {
			query.SinceTS = options.Since.Unix()
		}
		stored, err := store.ListCollectionMessages(db, query)
		if err != nil {
			return nil, err
		}
		messages := make([]Message, 0, len(stored))
		for _, message := range stored {
			messages = append(messages, Message{ID: message.MessageID, ConversationID: message.ConversationID,
				Sender: message.Sender, CreatedAt: message.CreatedAt, Content: message.Content})
		}
		return messages, nil
	}
	historian, ok := service.historianFor(source)
	if !ok {
		return nil, fmt.Errorf("collection connector %s does not support history", source.Connector)
	}
	messages, err := historian.History(ctx, source, HistoryOptions{Since: options.Since, Limit: options.Limit})
	if err != nil {
		return nil, err
	}
	if _, err := store.PutCollectionMessages(db, CollectionMessagesFor(source, messages)); err != nil {
		return nil, err
	}
	return messages, nil
}

// Historian reports whether the legacy configured fetcher can also read a
// bounded window of history. Registry-backed callers use historianFor.
func (service Service) Historian() (Historian, bool) {
	historian, ok := service.Fetcher.(Historian)
	return historian, ok
}

func (service Service) historianFor(source store.CollectionSource) (Historian, bool) {
	if service.Connectors != nil {
		connector, err := service.Connectors.Resolve(source.Connector)
		if err != nil {
			return nil, false
		}
		historian, ok := connector.(Historian)
		return historian, ok
	}
	return service.Historian()
}
