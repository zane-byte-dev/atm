package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestSearchMessagesOwnsSourceResolutionAndArchiveQuery(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PutCollectionMessages(db, []store.CollectionMessage{
		{Connector: source.Connector, ConversationID: source.ExternalID, MessageID: "m1",
			SourceID: source.ID, ConversationName: source.Name, Sender: "Alice",
			CreatedAt: 100, Content: "release completed"},
		{Connector: source.Connector, ConversationID: "somewhere-else", MessageID: "m2",
			Sender: "Alice", CreatedAt: 200, Content: "release completed elsewhere"},
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	result, err := (Service{}).SearchMessages(
		context.Background(), itemTestCall(), SearchMessagesInput{
			Keyword: "release", Source: source.Name, Sender: "ali", Limit: 10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Returned != 1 || len(result.Matches) != 1 || result.Matches[0].MessageID != "m1" ||
		result.Matches[0].ConversationName != source.Name {
		t.Fatalf("search result = %+v", result)
	}

	_, err = (Service{}).SearchMessages(
		context.Background(), itemTestCall(), SearchMessagesInput{Keyword: "release", Source: "missing"},
	)
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing source error = %v, want not_found", err)
	}
}

func TestAnalyzeCollectionResolvesAConfiguredSourceBeforeClassification(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PutCollectionMessages(db, []store.CollectionMessage{{
		Connector: source.Connector, ConversationID: source.ExternalID, MessageID: "m1",
		SourceID: source.ID, ConversationName: source.Name, Sender: "Alice",
		CreatedAt: 100, Content: "routine chatter",
	}})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	extractor := &fakeExtractor{decision: Decision{Action: "ignore", Reason: "no work"}}
	service := Service{Extractor: extractor, Now: tickingClock()}
	report, err := service.AnalyzeCollection(
		context.Background(), itemTestCall(), AnalyzeCollectionInput{
			Reference: source.ExternalID, Local: true, Limit: 10, MaxBatches: 10,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.SourceID != source.ID || report.Analyzed != 1 || report.Ignored != 1 || extractor.calls != 1 {
		t.Fatalf("analyze report = %+v, extractor calls = %d", report, extractor.calls)
	}
}

func TestCommandApplicationMethodsValidateCallsAndKeepConfigBehindAPort(t *testing.T) {
	var applied bool
	service := Service{ApplyCollectionEnabled: func(enabled bool) (bool, error) {
		applied = enabled
		return enabled, nil
	}}
	result, err := service.SetEnabled(
		context.Background(), itemTestCall(), SetEnabledInput{Enabled: true},
	)
	if err != nil || !result.Enabled || !applied {
		t.Fatalf("SetEnabled = %+v, %v; applied=%v", result, err, applied)
	}
	if _, err := service.SetEnabled(
		context.Background(), application.Call{}, SetEnabledInput{Enabled: false},
	); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid SetEnabled call = %v", err)
	}
	if _, err := service.DigestCollection(
		context.Background(), application.Call{}, DigestCollectionInput{},
	); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid DigestCollection call = %v", err)
	}
}
