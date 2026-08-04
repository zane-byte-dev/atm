package knowledge

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

func RecordFeedback(dataDir string, input FeedbackInput) (*FeedbackEvent, error) {
	input.DocumentID = strings.TrimSpace(input.DocumentID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Query = strings.TrimSpace(input.Query)
	input.Outcome = strings.TrimSpace(input.Outcome)
	input.Note = strings.TrimSpace(input.Note)
	if input.DocumentID == "" || input.SessionID == "" {
		return nil, fmt.Errorf("document id and session id are required")
	}
	if !validFeedbackOutcome(input.Outcome) || input.Outcome == "retrieved" {
		return nil, fmt.Errorf("invalid feedback outcome %q: use adopted, corrected, or rejected", input.Outcome)
	}
	if _, err := Get(dataDir, input.DocumentID); err != nil {
		return nil, err
	}
	event := FeedbackEvent{
		SchemaVersion: FeedbackSchemaVersion,
		ID:            newID("feedback"),
		DocumentID:    input.DocumentID,
		SessionID:     input.SessionID,
		Query:         input.Query,
		Outcome:       input.Outcome,
		Note:          input.Note,
		CreatedAt:     time.Now().UTC(),
	}
	if err := store.RecordKnowledgeFeedback([]store.KnowledgeFeedbackEvent{feedbackRow(event)}); err != nil {
		return nil, err
	}
	return &event, nil
}

// RecordRetrievals takes no data directory: the hits already carry the document
// IDs, so nothing needs to be read back from the corpus.
func RecordRetrievals(sessionID, query string, hits []SearchHit) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(hits) == 0 {
		return nil
	}
	seen := make(map[string]bool)
	events := make([]FeedbackEvent, 0, len(hits))
	for _, hit := range hits {
		if hit.DocumentID == "" || seen[hit.DocumentID] {
			continue
		}
		seen[hit.DocumentID] = true
		events = append(events, FeedbackEvent{
			SchemaVersion: FeedbackSchemaVersion,
			ID:            newID("feedback"), DocumentID: hit.DocumentID, SessionID: sessionID,
			Query: strings.TrimSpace(query), Outcome: "retrieved", CreatedAt: time.Now().UTC(),
		})
	}
	rows := make([]store.KnowledgeFeedbackEvent, 0, len(events))
	for _, event := range events {
		rows = append(rows, feedbackRow(event))
	}
	return store.RecordKnowledgeFeedback(rows)
}

func feedbackRow(event FeedbackEvent) store.KnowledgeFeedbackEvent {
	return store.KnowledgeFeedbackEvent{
		ID: event.ID, DocumentID: event.DocumentID, SessionID: event.SessionID,
		Query: event.Query, Outcome: event.Outcome, Note: event.Note,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func KnowledgeQualities(dataDir string) ([]KnowledgeQuality, error) {
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	return knowledgeQualities(dataDir, documents)
}

// knowledgeQualities scores documents the caller has already read, so a search
// does not walk and parse the corpus a second time just to rank it. The tallies
// come from one GROUP BY instead of replaying and deduping an event log.
func knowledgeQualities(dataDir string, documents []Document) ([]KnowledgeQuality, error) {
	totals, err := store.KnowledgeFeedbackByDocument()
	if err != nil {
		return nil, err
	}
	values := make(map[string]*KnowledgeQuality, len(documents))
	for _, document := range documents {
		quality := &KnowledgeQuality{
			DocumentID: document.Metadata.ID, Title: document.Metadata.Title,
			Collection: document.Collection, Score: 0.5,
		}
		if total, recorded := totals[document.Metadata.ID]; recorded {
			quality.Retrievals = total.Retrievals
			quality.Adopted = total.Adopted
			quality.Corrected = total.Corrected
			quality.Rejected = total.Rejected
			if last, parseErr := time.Parse(time.RFC3339Nano, total.LastFeedback); parseErr == nil {
				value := last.UTC()
				quality.LastFeedback = &value
			}
		}
		values[document.Metadata.ID] = quality
	}
	result := make([]KnowledgeQuality, 0, len(values))
	for _, quality := range values {
		evidence := quality.Adopted + quality.Corrected + quality.Rejected
		quality.Score = (1 + float64(quality.Adopted) + 0.35*float64(quality.Corrected)) / float64(2+evidence)
		result = append(result, *quality)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].DocumentID < result[j].DocumentID
	})
	return result, nil
}

func knowledgeQualityIndex(dataDir string, documents []Document) map[string]float64 {
	qualities, err := knowledgeQualities(dataDir, documents)
	if err != nil {
		return nil
	}
	result := make(map[string]float64, len(qualities))
	for _, quality := range qualities {
		result[quality.DocumentID] = quality.Score
	}
	return result
}

func validFeedbackOutcome(value string) bool {
	switch value {
	case "retrieved", "adopted", "corrected", "rejected":
		return true
	}
	return false
}

// OrphanedFeedbackDocuments lists document IDs that feedback refers to but the
// corpus no longer contains. `knowledge delete` prunes as it goes; this catches
// files removed behind ATM's back, which no foreign key can prevent because the
// referent is a file.
func OrphanedFeedbackDocuments(dataDir string) ([]string, error) {
	recorded, err := store.KnowledgeFeedbackDocumentIDs()
	if err != nil {
		return nil, err
	}
	if len(recorded) == 0 {
		return nil, nil
	}
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(documents))
	for _, document := range documents {
		present[document.Metadata.ID] = true
	}
	var orphans []string
	for _, id := range recorded {
		if !present[id] {
			orphans = append(orphans, id)
		}
	}
	return orphans, nil
}

// MemoryEventCount reports how many memory events exist, forgotten ones included.
func MemoryEventCount() (int, error) {
	return store.CountMemoryEvents()
}
