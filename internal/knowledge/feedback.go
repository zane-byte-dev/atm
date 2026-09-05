package knowledge

import (
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

func retrievalFeedbackEvents(now time.Time, sessionID, query string, hits []SearchHit) []FeedbackEvent {
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
			Query: strings.TrimSpace(query), Outcome: "retrieved", CreatedAt: now.UTC(),
		})
	}
	return events
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
	totals, err := (sqliteFeedbackStore{}).Totals()
	if err != nil {
		return nil, err
	}
	return knowledgeQualitiesFromTallies(documents, totals), nil
}

func knowledgeQualitiesFromTallies(documents []Document, totals map[string]FeedbackTally) []KnowledgeQuality {
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
			if total.LastFeedback != nil {
				value := total.LastFeedback.UTC()
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
	return result
}

func knowledgeQualityIndex(dataDir string, documents []Document) map[string]float64 {
	qualities, err := knowledgeQualities(dataDir, documents)
	if err != nil {
		return nil
	}
	return qualityIndexFromQualities(qualities)
}

func qualityIndexFromTallies(documents []Document, totals map[string]FeedbackTally) map[string]float64 {
	return qualityIndexFromQualities(knowledgeQualitiesFromTallies(documents, totals))
}

func qualityIndexFromQualities(qualities []KnowledgeQuality) map[string]float64 {
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
