package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type sourceSearchTestConnector struct {
	id         string
	candidates []Candidate
	err        error
	kind       string
	keyword    string
	limit      int
}

type sourceFetchOnlyTestConnector struct{ id string }

func (connector sourceFetchOnlyTestConnector) ID() string { return connector.id }

func (sourceFetchOnlyTestConnector) Fetch(
	context.Context,
	store.CollectionSource,
	int64,
) ([]Message, int64, error) {
	return nil, 0, nil
}

func (connector *sourceSearchTestConnector) ID() string { return connector.id }

func (*sourceSearchTestConnector) Fetch(
	context.Context,
	store.CollectionSource,
	int64,
) ([]Message, int64, error) {
	return nil, 0, nil
}

func (connector *sourceSearchTestConnector) Search(
	_ context.Context,
	kind, keyword string,
	limit int,
) ([]Candidate, error) {
	connector.kind, connector.keyword, connector.limit = kind, keyword, limit
	return connector.candidates, connector.err
}

func sourceApplicationCall(kind application.ActorKind, origin application.Origin) application.Call {
	return application.Call{
		RequestID: "source-application-test",
		Actor: application.Actor{
			Kind:   kind,
			Origin: origin,
		},
	}
}

func TestSourceApplicationServiceOwnsLifecycleAndPreservesAudit(t *testing.T) {
	withCollectorStore(t)
	connector := &sourceSearchTestConnector{id: "test"}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Connectors: registry}
	human := sourceApplicationCall(application.ActorHuman, application.OriginCLI)

	saved, err := service.SaveSource(context.Background(), human, SaveSourceInput{
		Connector: " TEST ", Kind: " Group ", ExternalID: " conversation-1 ",
		Name: "产品群", Project: "atm", Priority: "p1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if saved.Source.ID == "" || saved.Source.Connector != "test" || saved.Source.Kind != "group" ||
		saved.Source.ExternalID != "conversation-1" || saved.Source.Priority != "P1" ||
		saved.Source.Strategy != SourceStrategyTasks ||
		saved.Source.DecisionUnit != SourceDecisionUnitWindow || saved.Source.IntervalMinutes != 5 {
		t.Fatalf("saved source = %+v", saved.Source)
	}

	listed, err := service.ListSources(context.Background(), human, ListSourcesInput{})
	if err != nil || len(listed.Sources) != 1 || listed.Sources[0].ID != saved.Source.ID {
		t.Fatalf("ListSources = %+v, %v", listed, err)
	}
	if _, err := service.SetSourceMuted(context.Background(), human, SetSourceMutedInput{
		SourceID: saved.Source.ID, Muted: true,
	}); err != nil {
		t.Fatalf("SetSourceMuted: %v", err)
	}
	if _, err := service.SetSourceEnabled(context.Background(), human, SetSourceEnabledInput{
		SourceID: saved.Source.ID, Enabled: false,
	}); err != nil {
		t.Fatalf("SetSourceEnabled: %v", err)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.GetCollectionSource(db, saved.Source.ID)
	if err != nil || !updated.Muted || updated.Enabled {
		db.Close()
		t.Fatalf("updated source = %+v, %v", updated, err)
	}
	item, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: saved.Source.ID, Connector: saved.Source.Connector,
		Fingerprint: "source-audit-survives", MessageIDs: []string{"m1"},
		Action: "ignore", Status: "processed",
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.DeleteSource(context.Background(), human, DeleteSourceInput{
		SourceID: saved.Source.ID,
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed delete error = %v, want invalid_argument", err)
	}
	if _, err := service.DeleteSource(context.Background(), human, DeleteSourceInput{
		SourceID: saved.Source.ID, Confirmed: true,
	}); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	db, err = store.OpenReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := store.GetCollectionSource(db, saved.Source.ID); err == nil {
		t.Fatal("deleted source still exists")
	}
	if _, err := store.GetCollectionItem(db, item.ID); err != nil {
		t.Fatalf("source deletion erased audit item: %v", err)
	}
}

func TestSourceApplicationPolicyRejectsNonHumanMutation(t *testing.T) {
	service := Service{}
	input := SaveSourceInput{Connector: "test", Kind: "group", ExternalID: "c1", Enabled: true}
	for _, call := range []application.Call{
		sourceApplicationCall(application.ActorAgent, application.OriginCLI),
		sourceApplicationCall(application.ActorController, application.OriginController),
		sourceApplicationCall(application.ActorHuman, application.OriginHook),
	} {
		if _, err := service.SaveSource(context.Background(), call, input); !errors.Is(err, application.ErrForbidden) {
			t.Errorf("SaveSource(%+v) error = %v, want forbidden", call.Actor, err)
		}
	}
	if _, err := service.SaveSource(context.Background(), application.Call{}, input); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid call error = %v, want invalid_argument", err)
	}
}

func TestSourceSearchIsTypedAndAvailableToAgentReaders(t *testing.T) {
	connector := &sourceSearchTestConnector{
		id:         "test",
		candidates: []Candidate{{Kind: "group", ExternalID: "g1", Name: "产品群"}},
	}
	registry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Connectors: registry}
	agent := sourceApplicationCall(application.ActorAgent, application.OriginCLI)
	result, err := service.SearchSources(context.Background(), agent, SearchSourcesInput{
		Connector: "test", Keyword: " 产品 ", Limit: 7,
	})
	if err != nil {
		t.Fatalf("SearchSources: %v", err)
	}
	if len(result.Candidates) != 1 || connector.kind != DirectoryKindAll ||
		connector.keyword != "产品" || connector.limit != 7 {
		t.Fatalf("result/call = %+v, kind=%q keyword=%q limit=%d",
			result, connector.kind, connector.keyword, connector.limit)
	}

	if _, err := service.SearchSources(context.Background(), agent, SearchSourcesInput{
		Connector: "missing", Keyword: "x",
	}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing connector error = %v, want not_found", err)
	}
	if _, err := service.SearchSources(context.Background(), agent, SearchSourcesInput{
		Connector: "test", Kind: "room", Keyword: "x",
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid kind error = %v, want invalid_argument", err)
	}
}

func TestSourceSearchReturnsTypedCapabilityAndConnectorErrors(t *testing.T) {
	fetchOnlyRegistry, err := NewRegistry(sourceFetchOnlyTestConnector{id: "fetch-only"})
	if err != nil {
		t.Fatal(err)
	}
	agent := sourceApplicationCall(application.ActorAgent, application.OriginCLI)
	_, err = (Service{Connectors: fetchOnlyRegistry}).SearchSources(
		context.Background(),
		agent,
		SearchSourcesInput{Connector: "fetch-only", Keyword: "x"},
	)
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("missing search capability error = %v, want conflict", err)
	}

	connector := &sourceSearchTestConnector{id: "failing", err: errors.New("connector credential expired")}
	failingRegistry, err := NewRegistry(connector)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (Service{Connectors: failingRegistry}).SearchSources(
		context.Background(),
		agent,
		SearchSourcesInput{Connector: "failing", Keyword: "x"},
	)
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("connector failure error = %v, want unavailable", err)
	}
	var appErr *application.Error
	if !errors.As(err, &appErr) || !appErr.Retryable {
		t.Fatalf("connector failure = %#v, want retryable application error", err)
	}
	if appErr.Message == "connector credential expired" {
		t.Fatalf("connector secret-bearing cause leaked into public message: %q", appErr.Message)
	}
}

func TestSourceApplicationReturnsTypedNotFound(t *testing.T) {
	withCollectorStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	service := Service{}
	_, err = service.SetSourceEnabled(
		context.Background(),
		sourceApplicationCall(application.ActorHuman, application.OriginWeb),
		SetSourceEnabledInput{SourceID: "cs_missing", Enabled: true},
	)
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing source error = %v, want not_found", err)
	}
}
