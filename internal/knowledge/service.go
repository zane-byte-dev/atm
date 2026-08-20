package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// FeedbackTally is the persistence-neutral aggregate the application service
// needs to rank search results and build quality reports.
type FeedbackTally struct {
	Retrievals   int
	Adopted      int
	Corrected    int
	Rejected     int
	LastFeedback *time.Time
}

// FeedbackStore is the narrow persistence port used by the knowledge service.
// The Markdown corpus deliberately remains file-backed domain state; only the
// feedback ledger needs a store port.
type FeedbackStore interface {
	Record([]FeedbackEvent) error
	Totals() (map[string]FeedbackTally, error)
}

// ServiceOptions are the environment and persistence ports captured by a
// knowledge application service. Production uses the central Markdown corpus
// and SQLite feedback ledger; tests can supply a deterministic clock and an
// in-memory ledger.
type ServiceOptions struct {
	DataDir       string
	Now           func() time.Time
	FeedbackStore FeedbackStore
}

// Service owns knowledge use-case validation and orchestration. Adapters pass
// typed intent here instead of sequencing corpus reads and feedback writes.
type Service struct {
	dataDir  string
	now      func() time.Time
	feedback FeedbackStore
}

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.FeedbackStore == nil {
		options.FeedbackStore = sqliteFeedbackStore{}
	}
	return Service{dataDir: options.DataDir, now: options.Now, feedback: options.FeedbackStore}
}

type SearchInput struct {
	Query     string        `json:"query"`
	SessionID string        `json:"session_id,omitempty"`
	Options   SearchOptions `json:"options,omitempty"`
}

type SearchResult struct {
	Hits               []SearchHit `json:"hits"`
	RetrievalsRecorded int         `json:"retrievals_recorded"`
}

type ListInput struct {
	Collections []string `json:"collections,omitempty"`
}

type GetInput struct {
	DocumentID string `json:"document_id"`
}

type QualityInput struct {
	DocumentID string `json:"document_id,omitempty"`
	IssuesOnly bool   `json:"issues_only,omitempty"`
}

type QualityTotals struct {
	Documents  int `json:"documents"`
	Retrievals int `json:"retrievals"`
	Adopted    int `json:"adopted"`
	Corrected  int `json:"corrected"`
	Rejected   int `json:"rejected"`
}

type QualityResult struct {
	Qualities []KnowledgeQuality `json:"qualities"`
	Totals    QualityTotals      `json:"totals"`
}

// Search reads and ranks the corpus, then records at most one retrieval per
// returned document when a session is supplied. Keeping both steps in one use
// case prevents adapters from returning hits without the retrieval evidence
// that later quality scoring depends on.
func (service Service) Search(ctx context.Context, input SearchInput) (SearchResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return SearchResult{}, err
	}
	input.Query = strings.TrimSpace(input.Query)
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.Query == "" {
		return SearchResult{}, invalidKnowledgeArgument("query must not be empty", "query", input.Query)
	}

	documents, err := Discover(service.dataDir)
	if err != nil {
		return SearchResult{}, unavailableKnowledge("search knowledge", err)
	}
	tallies, err := service.feedback.Totals()
	if err != nil {
		// Quality feedback is an optional ranking signal, not part of the corpus.
		// A locked or damaged ledger must not make read-only Markdown knowledge
		// disappear. Retrieval recording below remains strict when a session asks
		// us to write new evidence.
		tallies = nil
	}
	hits := searchDocuments(documents, input.Query, input.Options, qualityIndexFromTallies(documents, tallies))
	events := retrievalFeedbackEvents(service.now(), input.SessionID, input.Query, hits)
	if len(events) > 0 {
		if err := knowledgeContextError(ctx); err != nil {
			return SearchResult{}, err
		}
		if err := service.feedback.Record(events); err != nil {
			return SearchResult{}, unavailableKnowledge("record knowledge retrievals", err)
		}
	}
	return SearchResult{Hits: hits, RetrievalsRecorded: len(events)}, nil
}

func (service Service) Feedback(ctx context.Context, input FeedbackInput) (FeedbackEvent, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return FeedbackEvent{}, err
	}
	input.DocumentID = strings.TrimSpace(input.DocumentID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Query = strings.TrimSpace(input.Query)
	input.Outcome = strings.TrimSpace(input.Outcome)
	input.Note = strings.TrimSpace(input.Note)
	if input.DocumentID == "" {
		return FeedbackEvent{}, invalidKnowledgeArgument("document id and session id are required", "document_id", input.DocumentID)
	}
	if input.SessionID == "" {
		return FeedbackEvent{}, invalidKnowledgeArgument("document id and session id are required", "session_id", input.SessionID)
	}
	if !validFeedbackOutcome(input.Outcome) || input.Outcome == "retrieved" {
		return FeedbackEvent{}, invalidKnowledgeArgument(
			fmt.Sprintf("invalid feedback outcome %q: use adopted, corrected, or rejected", input.Outcome),
			"outcome", input.Outcome,
		)
	}
	if _, err := service.Get(ctx, GetInput{DocumentID: input.DocumentID}); err != nil {
		return FeedbackEvent{}, err
	}

	event := FeedbackEvent{
		SchemaVersion: FeedbackSchemaVersion,
		ID:            newID("feedback"),
		DocumentID:    input.DocumentID,
		SessionID:     input.SessionID,
		Query:         input.Query,
		Outcome:       input.Outcome,
		Note:          input.Note,
		CreatedAt:     service.now().UTC(),
	}
	if err := knowledgeContextError(ctx); err != nil {
		return FeedbackEvent{}, err
	}
	if err := service.feedback.Record([]FeedbackEvent{event}); err != nil {
		return FeedbackEvent{}, unavailableKnowledge("record knowledge feedback", err)
	}
	return event, nil
}

func (service Service) Quality(ctx context.Context, input QualityInput) (QualityResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return QualityResult{}, err
	}
	documents, err := Discover(service.dataDir)
	if err != nil {
		return QualityResult{}, unavailableKnowledge("list knowledge quality", err)
	}
	tallies, err := service.feedback.Totals()
	if err != nil {
		return QualityResult{}, unavailableKnowledge("read knowledge quality", err)
	}
	values := knowledgeQualitiesFromTallies(documents, tallies)
	documentID := strings.TrimSpace(input.DocumentID)
	filtered := make([]KnowledgeQuality, 0, len(values))
	for _, value := range values {
		if documentID != "" && value.DocumentID != documentID {
			continue
		}
		if input.IssuesOnly && value.Corrected == 0 && value.Rejected == 0 && value.Score >= 0.5 {
			continue
		}
		filtered = append(filtered, value)
	}
	result := QualityResult{Qualities: filtered}
	result.Totals.Documents = len(filtered)
	for _, value := range filtered {
		result.Totals.Retrievals += value.Retrievals
		result.Totals.Adopted += value.Adopted
		result.Totals.Corrected += value.Corrected
		result.Totals.Rejected += value.Rejected
	}
	return result, nil
}

func (service Service) List(ctx context.Context, input ListInput) ([]DocumentSummary, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return nil, err
	}
	values, err := List(service.dataDir, input.Collections)
	if err != nil {
		return nil, unavailableKnowledge("list knowledge", err)
	}
	return values, nil
}

func (service Service) Catalog(ctx context.Context) ([]CollectionInfo, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return nil, err
	}
	values, err := Catalog(service.dataDir)
	if err != nil {
		return nil, unavailableKnowledge("catalog knowledge", err)
	}
	return values, nil
}

func (service Service) Get(ctx context.Context, input GetInput) (Document, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return Document{}, err
	}
	input.DocumentID = strings.TrimSpace(input.DocumentID)
	if input.DocumentID == "" {
		return Document{}, invalidKnowledgeArgument("knowledge document id must not be empty", "document_id", input.DocumentID)
	}
	document, err := Get(service.dataDir, input.DocumentID)
	if err == nil {
		return *document, nil
	}
	var notFound documentNotFoundError
	if errors.As(err, &notFound) {
		appErr := application.WrapError(application.CodeNotFound, err.Error(), err)
		appErr.Details = map[string]any{"document_id": input.DocumentID}
		return Document{}, appErr
	}
	return Document{}, unavailableKnowledge("read knowledge", err)
}

func knowledgeContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func invalidKnowledgeArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func unavailableKnowledge(operation string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, operation, cause)
	err.Retryable = true
	return err
}

type sqliteFeedbackStore struct{}

func (sqliteFeedbackStore) Record(events []FeedbackEvent) error {
	rows := make([]store.KnowledgeFeedbackEvent, 0, len(events))
	for _, event := range events {
		rows = append(rows, feedbackRow(event))
	}
	return store.RecordKnowledgeFeedback(rows)
}

func (sqliteFeedbackStore) Totals() (map[string]FeedbackTally, error) {
	stored, err := store.KnowledgeFeedbackByDocument()
	if err != nil {
		return nil, err
	}
	result := make(map[string]FeedbackTally, len(stored))
	for documentID, total := range stored {
		tally := FeedbackTally{
			Retrievals: total.Retrievals,
			Adopted:    total.Adopted,
			Corrected:  total.Corrected,
			Rejected:   total.Rejected,
		}
		if last, parseErr := time.Parse(time.RFC3339Nano, total.LastFeedback); parseErr == nil {
			value := last.UTC()
			tally.LastFeedback = &value
		}
		result[documentID] = tally
	}
	return result, nil
}
