package knowledge

import (
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

const SessionReviewSchemaVersion = 1

type SessionReview struct {
	SchemaVersion int       `json:"schemaVersion"`
	SessionID     string    `json:"sessionId"`
	Outcome       string    `json:"outcome"`
	Note          string    `json:"note,omitempty"`
	ReviewedAt    time.Time `json:"reviewedAt"`
}

// Session reviews live in the database; there is no data directory to point at.
func MarkSessionReviewed(sessionID, outcome, note string) (*SessionReview, error) {
	sessionID = strings.TrimSpace(sessionID)
	outcome = strings.TrimSpace(outcome)
	note = strings.TrimSpace(note)
	if sessionID == "" {
		return nil, fmt.Errorf("session id must not be empty")
	}
	if !validSessionReviewOutcome(outcome) {
		return nil, fmt.Errorf("invalid review outcome %q: use none, memory, knowledge, or mixed", outcome)
	}

	// Re-reviewing with the same verdict keeps the original timestamp: the review
	// did not change, so neither should when it happened.
	existing, err := store.GetSessionReview(sessionID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Outcome == outcome && existing.Note == note {
		return sessionReview(*existing), nil
	}

	row := store.SessionReview{
		SessionID: sessionID, Outcome: outcome, Note: note,
		ReviewedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := store.UpsertSessionReview(row); err != nil {
		return nil, err
	}
	return sessionReview(row), nil
}

func SessionReviews() (map[string]SessionReview, error) {
	rows, err := store.SessionReviews()
	if err != nil {
		return nil, err
	}
	reviews := make(map[string]SessionReview, len(rows))
	for sessionID, row := range rows {
		reviews[sessionID] = *sessionReview(row)
	}
	return reviews, nil
}

func sessionReview(row store.SessionReview) *SessionReview {
	reviewedAt, _ := time.Parse(time.RFC3339Nano, row.ReviewedAt)
	return &SessionReview{
		SchemaVersion: SessionReviewSchemaVersion,
		SessionID:     row.SessionID,
		Outcome:       row.Outcome,
		Note:          row.Note,
		ReviewedAt:    reviewedAt.UTC(),
	}
}

func validSessionReviewOutcome(outcome string) bool {
	switch outcome {
	case "none", "memory", "knowledge", "mixed":
		return true
	default:
		return false
	}
}
