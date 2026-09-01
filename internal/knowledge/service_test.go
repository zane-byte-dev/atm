package knowledge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

type stubFeedbackStore struct {
	tallies   map[string]FeedbackTally
	totalsErr error
	recordErr error
	recorded  []FeedbackEvent
	calls     int
}

func (store *stubFeedbackStore) Record(events []FeedbackEvent) error {
	store.calls++
	store.recorded = append(store.recorded, append([]FeedbackEvent(nil), events...)...)
	return store.recordErr
}

func (store *stubFeedbackStore) Totals() (map[string]FeedbackTally, error) {
	return store.tallies, store.totalsErr
}

func TestServiceSearchRecordsUniqueRetrievalsWithCapturedClock(t *testing.T) {
	dataDir := t.TempDir()
	first, err := Add(dataDir, AddDocumentInput{Title: "First", Content: "shared service marker", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Add(dataDir, AddDocumentInput{Title: "Second", Content: "shared service marker", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 3, 4, 5, 6, time.FixedZone("test", 8*60*60))
	ledger := &stubFeedbackStore{tallies: map[string]FeedbackTally{
		first.Metadata.ID:  {Adopted: 3},
		second.Metadata.ID: {Rejected: 3},
	}}
	service := NewService(ServiceOptions{DataDir: dataDir, Now: func() time.Time { return now }, FeedbackStore: ledger})

	result, err := service.Search(context.Background(), SearchInput{
		Query: "  shared service marker  ", SessionID: "  session-42  ", Options: SearchOptions{Limit: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 2 || result.Hits[0].DocumentID != first.Metadata.ID {
		t.Fatalf("ranked hits = %#v", result.Hits)
	}
	if result.RetrievalsRecorded != 2 || ledger.calls != 1 || len(ledger.recorded) != 2 {
		t.Fatalf("result = %#v, calls = %d, recorded = %#v", result, ledger.calls, ledger.recorded)
	}
	seen := map[string]bool{}
	for _, event := range ledger.recorded {
		if event.SessionID != "session-42" || event.Query != "shared service marker" || event.Outcome != "retrieved" || !event.CreatedAt.Equal(now.UTC()) {
			t.Fatalf("retrieval event = %#v", event)
		}
		if seen[event.DocumentID] {
			t.Fatalf("duplicate retrieval for %s", event.DocumentID)
		}
		seen[event.DocumentID] = true
	}
}

func TestServiceSearchAndFeedbackReturnTypedApplicationErrors(t *testing.T) {
	dataDir := t.TempDir()
	document, err := Add(dataDir, AddDocumentInput{Title: "Known", Content: "known service marker", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	ledger := &stubFeedbackStore{}
	service := NewService(ServiceOptions{DataDir: dataDir, FeedbackStore: ledger})

	if _, err := service.Search(context.Background(), SearchInput{}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty search error = %v", err)
	}
	if _, err := service.Feedback(context.Background(), FeedbackInput{DocumentID: document.Metadata.ID, Outcome: "adopted"}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("missing session error = %v", err)
	}
	if _, err := service.Feedback(context.Background(), FeedbackInput{DocumentID: "document:missing", SessionID: "s1", Outcome: "adopted"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing document error = %v", err)
	}

	ledger.recordErr = errors.New("database busy")
	if _, err := service.Search(context.Background(), SearchInput{Query: "known", SessionID: "s1"}); !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("retrieval write error = %v", err)
	}
}

func TestServiceSearchDegradesWhenOptionalQualityLedgerIsUnavailable(t *testing.T) {
	dataDir := t.TempDir()
	document, err := Add(dataDir, AddDocumentInput{
		Title: "Available corpus", Content: "ledger independent marker", Collection: "notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	ledger := &stubFeedbackStore{totalsErr: errors.New("quality database is locked")}
	service := NewService(ServiceOptions{DataDir: dataDir, FeedbackStore: ledger})

	result, err := service.Search(context.Background(), SearchInput{Query: "ledger independent marker"})
	if err != nil {
		t.Fatalf("read-only search failed with optional ledger: %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].DocumentID != document.Metadata.ID || result.Hits[0].Quality != 0.5 {
		t.Fatalf("neutral search result = %#v", result)
	}
	if ledger.calls != 0 || result.RetrievalsRecorded != 0 {
		t.Fatalf("read-only search unexpectedly wrote feedback: calls=%d result=%#v", ledger.calls, result)
	}
}

func TestServiceFeedbackQualityAndBrowseUseCases(t *testing.T) {
	dataDir := t.TempDir()
	good, err := Add(dataDir, AddDocumentInput{Title: "Good", Content: "useful body", Collection: "research"})
	if err != nil {
		t.Fatal(err)
	}
	weak, err := Add(dataDir, AddDocumentInput{Title: "Weak", Content: "uncertain body", Collection: "research"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	ledger := &stubFeedbackStore{tallies: map[string]FeedbackTally{
		good.Metadata.ID: {Retrievals: 2, Adopted: 2},
		weak.Metadata.ID: {Retrievals: 4, Rejected: 2},
	}}
	service := NewService(ServiceOptions{DataDir: dataDir, Now: func() time.Time { return now }, FeedbackStore: ledger})

	event, err := service.Feedback(context.Background(), FeedbackInput{
		DocumentID: good.Metadata.ID, SessionID: " s-feedback ", Query: " useful ", Outcome: "adopted",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.SessionID != "s-feedback" || event.Query != "useful" || !event.CreatedAt.Equal(now) || ledger.calls != 1 {
		t.Fatalf("feedback event = %#v, calls = %d", event, ledger.calls)
	}

	quality, err := service.Quality(context.Background(), QualityInput{IssuesOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(quality.Qualities) != 1 || quality.Qualities[0].DocumentID != weak.Metadata.ID {
		t.Fatalf("issues-only quality = %#v", quality)
	}
	if quality.Totals.Documents != 1 || quality.Totals.Retrievals != 4 || quality.Totals.Rejected != 2 {
		t.Fatalf("quality totals = %#v", quality.Totals)
	}

	listed, err := service.List(context.Background(), ListInput{Collections: []string{"research"}})
	if err != nil || len(listed) != 2 {
		t.Fatalf("list = %#v, err = %v", listed, err)
	}
	catalog, err := service.Catalog(context.Background())
	if err != nil || len(catalog) != 1 || catalog[0].ID != "research" || catalog[0].DocumentCount != 2 {
		t.Fatalf("catalog = %#v, err = %v", catalog, err)
	}
	got, err := service.Get(context.Background(), GetInput{DocumentID: "  " + good.Metadata.ID + "  "})
	if err != nil || got.Metadata.ID != good.Metadata.ID || got.Content != good.Content {
		t.Fatalf("get = %#v, err = %v", got, err)
	}
	if _, err := service.Get(context.Background(), GetInput{}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty get error = %v", err)
	}
}
