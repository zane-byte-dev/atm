package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

type ReviewInput struct {
	SessionID      string `json:"session_id"`
	Outcome        string `json:"outcome"`
	Note           string `json:"note,omitempty"`
	SyncBeforeRead bool   `json:"sync_before_read,omitempty"`
}

type ReviewResult struct {
	Review Review   `json:"review"`
	Meta   ReadMeta `json:"meta"`
}

// Review records that a session was mined for durable memory. The verdict is
// keyed by the session's full ID, so a short-ID review and a full-ID review of
// the same conversation are the same record — which is also why the lookup goes
// through the index instead of trusting the argument.
//
// Re-reviewing with the same verdict keeps the original timestamp: the review
// did not change, so neither should when it happened.
func (service Service) Review(ctx context.Context, input ReviewInput) (ReviewResult, error) {
	if err := contextError(ctx); err != nil {
		return ReviewResult{}, err
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Outcome = strings.TrimSpace(input.Outcome)
	input.Note = strings.TrimSpace(input.Note)
	if input.SessionID == "" {
		return ReviewResult{}, invalidArgument("session id must not be empty", "session_id", input.SessionID)
	}
	if !validReviewOutcome(input.Outcome) {
		return ReviewResult{}, invalidArgument(fmt.Sprintf(
			"invalid review outcome %q: use none, memory, knowledge, or mixed", input.Outcome,
		), "outcome", input.Outcome)
	}

	db, meta, err := service.openRead(ctx, input.SyncBeforeRead)
	if err != nil {
		return ReviewResult{}, err
	}
	stored, err := store.GetSession(db, input.SessionID)
	db.Close()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReviewResult{}, sessionNotFound(input.SessionID, err)
		}
		return ReviewResult{}, unavailable("resolve reviewed session", err)
	}

	existing, err := store.GetSessionReview(stored.FullID)
	if err != nil {
		return ReviewResult{}, unavailable("read session review state", err)
	}
	if existing != nil && existing.Outcome == input.Outcome && existing.Note == input.Note {
		return ReviewResult{Review: reviewFromStore(*existing), Meta: meta}, nil
	}
	row := store.SessionReview{
		SessionID: stored.FullID, Outcome: input.Outcome, Note: input.Note,
		ReviewedAt: service.now().UTC().Format(time.RFC3339Nano),
	}
	if err := store.UpsertSessionReview(row); err != nil {
		return ReviewResult{}, unavailable("record session review", err)
	}
	return ReviewResult{Review: reviewFromStore(row), Meta: meta}, nil
}

func validReviewOutcome(outcome string) bool {
	switch outcome {
	case "none", "memory", "knowledge", "mixed":
		return true
	default:
		return false
	}
}
