package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// A review is keyed by the session's full ID and re-reviewing with the same
// verdict must not move the timestamp: the review did not change, so neither
// should when it happened.
func TestServiceReviewResolvesShortIDsAndStaysIdempotent(t *testing.T) {
	fixture := newServiceFixture(t)
	first, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "old", Outcome: "memory", Note: "  stored one decision  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Review.SessionID != "session-old-full" || first.Review.Outcome != "memory" ||
		first.Review.Note != "stored one decision" ||
		first.Review.SchemaVersion != SessionReviewSchemaVersion {
		t.Fatalf("first review = %#v", first.Review)
	}
	if !first.Review.ReviewedAt.Equal(fixture.now.UTC()) {
		t.Fatalf("reviewed at = %s, want the service clock %s", first.Review.ReviewedAt, fixture.now.UTC())
	}

	second, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "session-old-full", Outcome: "memory", Note: "stored one decision",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Review.ReviewedAt.Equal(first.Review.ReviewedAt) {
		t.Fatalf("idempotent review moved the timestamp: %s != %s",
			second.Review.ReviewedAt, first.Review.ReviewedAt)
	}

	changed, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "old", Outcome: "mixed", Note: "stored one decision",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Review.Outcome != "mixed" {
		t.Fatalf("changed review = %#v", changed.Review)
	}

	reviews, err := store.SessionReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 || reviews["session-old-full"].Outcome != "mixed" {
		t.Fatalf("stored reviews = %#v", reviews)
	}

	if _, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "old", Outcome: "temporary",
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid outcome error = %v", err)
	}
	if _, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "  ", Outcome: "none",
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty session id error = %v", err)
	}
	if _, err := fixture.service.Review(context.Background(), ReviewInput{
		SessionID: "missing", Outcome: "none",
	}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
}

// Forgetting only makes sense for retained history. While the last sync still
// found the transcript the next one would bring the session straight back, so
// planning refuses instead of raising a confirmation prompt that cannot deliver.
func TestServicePlanForgetRefusesSessionsWhoseSourceIsStillTracked(t *testing.T) {
	fixture := newServiceFixture(t)
	trackSessionSource(t, fixture.transcriptPath)

	_, err := fixture.service.PlanForget(context.Background(), PlanForgetInput{SessionID: "recent"})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("tracked source error = %v, want a conflict", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Details["file_path"] != fixture.transcriptPath {
		t.Fatalf("tracked source error = %v, details = %#v", err, appErr)
	}
}

func TestServiceForgetDropsConfirmedSessionAndEverythingDerivedFromIt(t *testing.T) {
	fixture := newServiceFixture(t)
	plan, err := fixture.service.PlanForget(context.Background(), PlanForgetInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.SessionID != "session-recent-full" || plan.ShortID != "recent" || plan.Messages != 2 {
		t.Fatalf("plan = %#v", plan)
	}

	if _, err := fixture.service.Forget(context.Background(), ForgetInput{Plan: plan}); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("unconfirmed forget = %v, want a refusal", err)
	}

	result, err := fixture.service.Forget(context.Background(), ForgetInput{Plan: plan, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.SessionID != plan.SessionID || result.Session.Messages != 2 {
		t.Fatalf("forget result = %#v", result.Session)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sessions, messages, usageEvents int
	if err := db.QueryRow(`SELECT
		(SELECT COUNT(*) FROM sessions WHERE id = 'session-recent-full'),
		(SELECT COUNT(*) FROM messages WHERE session_id = 'session-recent-full'),
		(SELECT COUNT(*) FROM usage_events WHERE session_id = 'session-recent-full')`).
		Scan(&sessions, &messages, &usageEvents); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || messages != 0 || usageEvents != 0 {
		t.Fatalf("after forget: %d sessions, %d messages, %d usage events; want all gone",
			sessions, messages, usageEvents)
	}

	if _, err := fixture.service.Forget(context.Background(), ForgetInput{Plan: plan, Confirmed: true}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("forget again = %v, want not found", err)
	}
	if _, err := fixture.service.Forget(context.Background(), ForgetInput{Confirmed: true}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("forget without a plan = %v", err)
	}
}

// A sync that re-adopts the transcript between the prompt and the delete must
// not be covered by the earlier confirmation.
func TestServiceForgetRechecksThePlanAgainstTheIndex(t *testing.T) {
	fixture := newServiceFixture(t)
	plan, err := fixture.service.PlanForget(context.Background(), PlanForgetInput{SessionID: "recent"})
	if err != nil {
		t.Fatal(err)
	}
	trackSessionSource(t, fixture.transcriptPath)

	if _, err := fixture.service.Forget(context.Background(), ForgetInput{Plan: plan, Confirmed: true}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("re-adopted source error = %v, want a conflict", err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var sessions int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = 'session-recent-full'").Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("session count after refused forget = %d, want 1", sessions)
	}
}

// trackSessionSource makes the last sync look like it still found the
// transcript, which is the state that blocks forgetting.
func trackSessionSource(t *testing.T, filePath string) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO sync_state (file_path, agent, mtime_unix, size_bytes, offset_bytes)
		VALUES (?, 'codex', ?, 2, 0)`, filePath, time.Now().Unix()); err != nil {
		t.Fatalf("track session source: %v", err)
	}
}
