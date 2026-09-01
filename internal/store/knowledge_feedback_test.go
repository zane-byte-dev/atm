package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func feedbackEvent(id, documentID, sessionID, query, outcome string) KnowledgeFeedbackEvent {
	return KnowledgeFeedbackEvent{
		ID: id, DocumentID: documentID, SessionID: sessionID, Query: query, Outcome: outcome,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// The append log deduped while reading itself: one retrieval per (document,
// session, query) and one verdict per (document, session). Those keys are now the
// schema's, so a repeat write updates instead of accumulating.
func TestKnowledgeFeedbackKeysDedupeOnWrite(t *testing.T) {
	withTempStore(t)
	if err := RecordKnowledgeFeedback([]KnowledgeFeedbackEvent{
		feedbackEvent("f1", "document:a", "session-1", "atm 架构", "retrieved"),
		feedbackEvent("f2", "document:a", "session-1", "atm 架构", "retrieved"),
		feedbackEvent("f3", "document:a", "session-1", "别的问题", "retrieved"),
		feedbackEvent("f4", "document:a", "session-2", "atm 架构", "retrieved"),
		feedbackEvent("f5", "document:a", "session-1", "", "adopted"),
		feedbackEvent("f6", "document:a", "session-1", "", "corrected"),
	}); err != nil {
		t.Fatal(err)
	}

	totals, err := KnowledgeFeedbackByDocument()
	if err != nil {
		t.Fatal(err)
	}
	total := totals["document:a"]
	// Three distinct retrievals; the repeated one collapsed.
	if total.Retrievals != 3 {
		t.Fatalf("retrievals = %d, want 3: %#v", total.Retrievals, total)
	}
	// One verdict per session, and the later one replaced the earlier.
	if total.Adopted != 0 || total.Corrected != 1 {
		t.Fatalf("verdicts = %#v", total)
	}
	if total.LastFeedback == "" {
		t.Fatal("last feedback timestamp missing")
	}
}

func TestKnowledgeFeedbackRejectsUnknownOutcome(t *testing.T) {
	withTempStore(t)
	err := RecordKnowledgeFeedback([]KnowledgeFeedbackEvent{
		feedbackEvent("f1", "document:a", "session-1", "", "helpful"),
	})
	if err == nil {
		t.Fatal("unknown outcome was accepted")
	}
}

// Feedback names markdown files and agent sessions, neither of which the database
// can enforce, so removal is explicit and orphans stay findable.
func TestKnowledgeFeedbackDeleteAndListDocuments(t *testing.T) {
	withTempStore(t)
	if err := RecordKnowledgeFeedback([]KnowledgeFeedbackEvent{
		feedbackEvent("f1", "document:a", "session-1", "q", "retrieved"),
		feedbackEvent("f2", "document:b", "session-1", "q", "retrieved"),
	}); err != nil {
		t.Fatal(err)
	}
	ids, err := KnowledgeFeedbackDocumentIDs()
	if err != nil || len(ids) != 2 {
		t.Fatalf("document ids = %#v, err=%v", ids, err)
	}
	if err := DeleteKnowledgeFeedback("document:a"); err != nil {
		t.Fatal(err)
	}
	if ids, err = KnowledgeFeedbackDocumentIDs(); err != nil || len(ids) != 1 || ids[0] != "document:b" {
		t.Fatalf("document ids after delete = %#v, err=%v", ids, err)
	}
}

func TestSessionReviewKeepsOneRowPerSession(t *testing.T) {
	withTempStore(t)
	for _, review := range []SessionReview{
		{SessionID: "s1", Outcome: "none", ReviewedAt: "2026-07-01T00:00:00Z"},
		{SessionID: "s1", Outcome: "memory", Note: "stored one decision", ReviewedAt: "2026-07-02T00:00:00Z"},
		{SessionID: "s2", Outcome: "knowledge", ReviewedAt: "2026-07-02T00:00:00Z"},
	} {
		if err := UpsertSessionReview(review); err != nil {
			t.Fatal(err)
		}
	}
	reviews, err := SessionReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 || reviews["s1"].Outcome != "memory" || reviews["s1"].Note != "stored one decision" {
		t.Fatalf("reviews = %#v", reviews)
	}
	if err := UpsertSessionReview(SessionReview{SessionID: "s3", Outcome: "maybe", ReviewedAt: "2026-07-02T00:00:00Z"}); err == nil {
		t.Fatal("unknown review outcome was accepted")
	}
}

// Reviews are written by whatever command happens to finish a session, and the
// App can be marking one while the CLI marks another. A single-writer SQLite
// file makes that contention real, so the upsert has to survive it rather than
// surfacing "database is locked" to whoever lost the race.
func TestConcurrentSessionReviewWrites(t *testing.T) {
	withTempStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for index := 0; index < 12; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs <- UpsertSessionReview(SessionReview{
				SessionID:  fmt.Sprintf("session-%d", index),
				Outcome:    "none",
				Note:       "no durable candidate",
				ReviewedAt: "2026-07-02T00:00:00Z",
			})
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reviews, err := SessionReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 12 {
		t.Fatalf("review count = %d, want 12", len(reviews))
	}
}
