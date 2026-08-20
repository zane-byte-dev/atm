package collector

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

type stubConnector struct{ id string }

func (connector stubConnector) ID() string { return connector.id }
func (connector stubConnector) Fetch(context.Context, store.CollectionSource, int64) ([]Message, int64, error) {
	return nil, 0, nil
}

type routingConnector struct {
	id      string
	calls   int
	sources []string
}

func (connector *routingConnector) ID() string { return connector.id }
func (connector *routingConnector) Fetch(_ context.Context, source store.CollectionSource,
	_ int64) ([]Message, int64, error) {
	connector.calls++
	connector.sources = append(connector.sources, source.Connector)
	return []Message{{ID: connector.id + "-m1", ConversationID: source.ExternalID,
		CreatedAt: 100, Content: "status update"}}, 100, nil
}

func TestRegistryValidatesResolvesAndSortsConnectors(t *testing.T) {
	registry, err := NewRegistry(stubConnector{id: "slack"}, stubConnector{id: "github"})
	if err != nil {
		t.Fatal(err)
	}
	if got := registry.IDs(); !reflect.DeepEqual(got, []string{"github", "slack"}) {
		t.Fatalf("connector ids = %v", got)
	}
	if connector, err := registry.Resolve(" SLACK "); err != nil || connector.ID() != "slack" {
		t.Fatalf("resolve connector=%v err=%v", connector, err)
	}
	if err := registry.Register(stubConnector{id: "bad connector"}); err == nil {
		t.Fatal("invalid connector id was accepted")
	}
	if _, err := registry.Resolve("missing"); err == nil {
		t.Fatal("missing connector resolved")
	}
}

func TestCappedBufferBoundsConnectorOutput(t *testing.T) {
	buffer := cappedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 {
		t.Fatalf("write=%d err=%v", written, err)
	}
	if got := buffer.buffer.String(); got != "abcd" || !buffer.exceeded {
		t.Fatalf("buffer=%q exceeded=%v", got, buffer.exceeded)
	}
}

func TestCommandConnectorImplementsVersionedProtocol(t *testing.T) {
	script := filepath.Join(t.TempDir(), "connector")
	body := `#!/bin/sh
read request
case "$1" in
  fetch) printf '%s\n' '{"messages":[{"id":"m1","conversation_id":"C1","sender":"alice","created_at":42,"content":"hello","external_states_cover_message":true,"external_states":[{"kind":"code_review","reference":"https://code.example/review/42","state":"pending_review","disposition":"actionable","checked_at":43}]}],"cursor":50}' ;;
  history) printf '%s\n' '{"messages":[{"id":"h1","conversation_id":"C1","created_at":7,"content":"old"}]}' ;;
  search) printf '%s\n' '{"candidates":[{"kind":"channel","external_id":"C1","name":"general"}]}' ;;
  *) printf '%s\n' '{"error":"unsupported operation"}' ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	connector := CommandConnector{ConnectorID: "slack", Command: script, Timeout: 5 * time.Second}
	source := store.CollectionSource{Connector: "slack", Kind: "channel", ExternalID: "C1"}
	messages, cursor, err := connector.Fetch(context.Background(), source, 10)
	if err != nil || len(messages) != 1 || messages[0].ID != "m1" || cursor != 50 ||
		len(messages[0].ExternalStates) != 1 ||
		messages[0].ExternalStates[0].Disposition != ExternalDispositionActionable {
		t.Fatalf("fetch messages=%+v cursor=%d err=%v", messages, cursor, err)
	}
	history, err := connector.History(context.Background(), source, HistoryOptions{Limit: 5})
	if err != nil || len(history) != 1 || history[0].ID != "h1" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	candidates, err := connector.Search(context.Background(), "channel", "gen", 5)
	if err != nil || len(candidates) != 1 || candidates[0].ExternalID != "C1" {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
}

func TestCommandConnectorRejectsCoverageWithoutExternalState(t *testing.T) {
	script := filepath.Join(t.TempDir(), "connector")
	body := `#!/bin/sh
read request
printf '%s\n' '{"messages":[{"id":"m1","conversation_id":"C1","created_at":42,"content":"hello","external_states_cover_message":true}]}'
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	connector := CommandConnector{ConnectorID: "slack", Command: script, Timeout: 5 * time.Second}
	_, _, err := connector.Fetch(context.Background(), store.CollectionSource{
		Connector: "slack", Kind: "channel", ExternalID: "C1",
	}, 10)
	if err == nil || !strings.Contains(err.Error(), "covered by no external states") {
		t.Fatalf("invalid coverage err=%v", err)
	}
}

func TestCommandConnectorRejectsInvalidExternalDisposition(t *testing.T) {
	script := filepath.Join(t.TempDir(), "connector")
	body := `#!/bin/sh
read request
printf '%s\n' '{"messages":[{"id":"m1","conversation_id":"C1","created_at":42,"content":"hello","external_states":[{"kind":"code_review","reference":"review-42","state":"done","disposition":"probably","checked_at":43}]}]}'
`
	if err := os.WriteFile(script, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	connector := CommandConnector{ConnectorID: "slack", Command: script, Timeout: 5 * time.Second}
	_, _, err := connector.Fetch(context.Background(), store.CollectionSource{
		Connector: "slack", Kind: "channel", ExternalID: "C1",
	}, 10)
	if err == nil || !strings.Contains(err.Error(), "invalid external disposition") {
		t.Fatalf("invalid disposition err=%v", err)
	}
}

func TestServiceRoutesEachSourceThroughItsRegisteredConnector(t *testing.T) {
	withCollectorStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	webhookSource, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "webhook", Kind: "group", ExternalID: "cid-1", Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	slackSource, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "slack", Kind: "channel", ExternalID: "C1", Priority: "P2", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	webhook := &routingConnector{id: "webhook"}
	slack := &routingConnector{id: "slack"}
	registry, err := NewRegistry(webhook, slack)
	if err != nil {
		t.Fatal(err)
	}
	extractor := &fakeExtractor{decision: Decision{Action: "ignore", Reason: "test"}}
	service := Service{Connectors: registry, Extractor: extractor, Now: tickingClock()}
	if _, err := service.Run(context.Background(), webhookSource.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Run(context.Background(), slackSource.ID); err != nil {
		t.Fatal(err)
	}
	if webhook.calls != 1 || slack.calls != 1 ||
		!reflect.DeepEqual(webhook.sources, []string{"webhook"}) ||
		!reflect.DeepEqual(slack.sources, []string{"slack"}) {
		t.Fatalf("routing webhook=%+v slack=%+v", webhook, slack)
	}
}
