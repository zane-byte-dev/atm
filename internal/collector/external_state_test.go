package collector

import (
	"context"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/store"
)

func externalStateMessage(source store.CollectionSource, disposition, state string) Message {
	return Message{ID: "review-1", ConversationID: source.ExternalID, Sender: "Code助手",
		CreatedAt: 12_000, Content: "邀请您评审代码：https://code.example/review/42",
		ExternalStatesCoverMessage: true,
		ExternalStates: []ExternalState{{Kind: "code_review", Reference: "https://code.example/review/42",
			State: state, Disposition: disposition, CheckedAt: 12_100}}}
}

func TestSettledExternalStateSkipsModelAndTodoCreation(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "不应创建"}}
	service := Service{Connectors: testRegistry(&fakeFetcher{messages: []Message{
		externalStateMessage(source, ExternalDispositionSettled, "not_pending_review"),
	}, newest: 12_000}), Extractor: extractor, Now: tickingClock()}

	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].IgnoredCount != 1 || report.Runs[0].CreatedCount != 0 {
		t.Fatalf("settled run=%+v err=%v", report, err)
	}
	if extractor.calls != 0 {
		t.Fatalf("settled external work called extractor %d time(s)", extractor.calls)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 0 {
		t.Fatalf("settled external work created Todos: %+v", todos.Items)
	}
	db, _ := store.Open()
	defer db.Close()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	checkpoint, _ := store.GetCollectionCheckpoint(db, source.ID)
	if len(items) != 1 || items[0].Action != "ignore" ||
		!strings.Contains(items[0].Reason, "not_pending_review") || checkpoint.CursorTime != 12_000 {
		t.Fatalf("settled audit=%+v checkpoint=%+v", items, checkpoint)
	}
}

func TestActionableExternalStateStillAllowsTodoCreation(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "评审 CR",
		Summary: "仍待当前用户评审", ItemType: "follow_up", Confidence: 0.9}}
	service := Service{Connectors: testRegistry(&fakeFetcher{messages: []Message{
		externalStateMessage(source, ExternalDispositionActionable, "pending_review"),
	}, newest: 12_000}), Extractor: extractor, Now: tickingClock()}

	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].CreatedCount != 1 || extractor.calls != 1 {
		t.Fatalf("actionable run=%+v calls=%d err=%v", report, extractor.calls, err)
	}
}

func TestUnknownExternalStateFailsClosedAndRetries(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "不应创建"}}
	service := Service{Connectors: testRegistry(&fakeFetcher{messages: []Message{
		externalStateMessage(source, ExternalDispositionUnknown, "lookup_failed"),
	}, newest: 12_000}), Extractor: extractor, Now: tickingClock()}

	report, err := service.Run(context.Background(), source.ID)
	if err == nil || !strings.Contains(err.Error(), "state is unknown") ||
		len(report.Runs) != 1 || report.Runs[0].FailedCount != 1 {
		t.Fatalf("unknown run=%+v err=%v", report, err)
	}
	if extractor.calls != 0 {
		t.Fatalf("unknown external state called extractor %d time(s)", extractor.calls)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 0 {
		t.Fatalf("unknown external state created Todos: %+v", todos.Items)
	}
	db, _ := store.Open()
	defer db.Close()
	checkpoint, _ := store.GetCollectionCheckpoint(db, source.ID)
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	if checkpoint.CursorTime != 0 || len(items) != 1 || items[0].Action != "failed" {
		t.Fatalf("unknown audit=%+v checkpoint=%+v", items, checkpoint)
	}
}

func TestSettledStateDoesNotHideAnUnannotatedMessageInTheSameUnit(t *testing.T) {
	batch := MessageBatch{Messages: []Message{
		externalStateMessage(store.CollectionSource{}, ExternalDispositionSettled, "merged"),
		{ID: "request-2", Content: "还有一个独立请求"},
	}}
	if _, settled, err := externalStateDecision(batch); err != nil || settled {
		t.Fatalf("mixed unit settled=%v err=%v", settled, err)
	}
}

func TestSettledStateWithoutWholeMessageCoverageStillReachesClassifier(t *testing.T) {
	message := externalStateMessage(store.CollectionSource{}, ExternalDispositionSettled, "merged")
	message.ExternalStatesCoverMessage = false
	message.Content += "，另外请补发布说明"
	if _, settled, err := externalStateDecision(MessageBatch{Messages: []Message{message}}); err != nil || settled {
		t.Fatalf("partially covered message settled=%v err=%v", settled, err)
	}
}

func TestUnknownStateFailsClosedEvenBesideActionableWork(t *testing.T) {
	actionable := externalStateMessage(store.CollectionSource{}, ExternalDispositionActionable, "pending_review")
	unknown := externalStateMessage(store.CollectionSource{}, ExternalDispositionUnknown, "lookup_failed")
	unknown.ID = "review-2"
	batch := MessageBatch{Messages: []Message{actionable, unknown}}
	if _, _, err := externalStateDecision(batch); err == nil || !strings.Contains(err.Error(), "state is unknown") {
		t.Fatalf("mixed actionable/unknown err=%v", err)
	}
}
