package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

type fakeFetcher struct {
	messages []Message
	newest   int64
	err      error
	since    []int64
}

func (fetcher *fakeFetcher) Fetch(_ context.Context, _ store.CollectionSource, since int64) ([]Message, int64, error) {
	fetcher.since = append(fetcher.since, since)
	return append([]Message(nil), fetcher.messages...), fetcher.newest, fetcher.err
}

type fakeExtractor struct {
	decision Decision
	err      error
	calls    int
	batches  []MessageBatch
}

func (extractor *fakeExtractor) Extract(_ context.Context, batch MessageBatch, _ []store.Todo) (Decision, error) {
	extractor.calls++
	extractor.batches = append(extractor.batches, batch)
	return extractor.decision, extractor.err
}

func withCollectorStore(t *testing.T) {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir, config.AtmDB = dir, filepath.Join(dir, "atm.db")
	t.Cleanup(func() { config.AtmDir, config.AtmDB = oldDir, oldDB })
}

func addCollectorSource(t *testing.T) store.CollectionSource {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-product",
		Name: "产品讨论", Project: "atm", Priority: "P1", Enabled: true,
	})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}
	return source
}

func tickingClock() func() time.Time {
	now := time.Unix(20_000, 0)
	return func() time.Time {
		now = now.Add(time.Second)
		return now
	}
}

func TestServiceCreatesOnceAndAdvancesCheckpoint(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{
		ID: "m1", ConversationID: source.ExternalID, Sender: "测试发送人", CreatedAt: 10_000,
		Content: "我想把需求收集做成全自动的",
	}}, newest: 10_000}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "实现全自动需求收集",
		Summary: "自动读取聊天并创建 Todo", ItemType: "requirement", Project: "atm",
		Priority: "P1", Reason: "明确需求", Confidence: 0.96}}
	service := Service{Fetcher: fetcher, Extractor: extractor, Now: tickingClock()}

	first, err := service.Run(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("first collection run: %v", err)
	}
	if len(first.Runs) != 1 || first.Runs[0].CreatedCount != 1 || first.Runs[0].Status != "succeeded" {
		t.Fatalf("unexpected first run: %+v", first)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 {
		t.Fatalf("todos after first run: %+v, %v", todos, err)
	}
	if todos.Items[0].Status != store.TodoStatusOpen || !strings.HasPrefix(todos.Items[0].Source, "test:cid-product:m1") {
		t.Fatalf("created Todo is not traceable/open: %+v", todos.Items[0])
	}
	if todos.Items[0].Description != "自动读取聊天并创建 Todo" ||
		strings.Contains(todos.Items[0].Description, "我想把需求收集做成全自动的") {
		t.Fatalf("created Todo copied raw conversation into its description: %q", todos.Items[0].Description)
	}

	second, err := service.Run(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("second collection run: %v", err)
	}
	if second.Runs[0].CreatedCount != 0 || extractor.calls != 1 {
		t.Fatalf("duplicate messages were analyzed or created again: run=%+v calls=%d", second.Runs[0], extractor.calls)
	}
	todos, _ = store.LoadTodosReadOnly()
	if len(todos.Items) != 1 {
		t.Fatalf("duplicate Todo created: %+v", todos.Items)
	}
	db, _ := store.Open()
	defer db.Close()
	checkpoint, err := store.GetCollectionCheckpoint(db, source.ID)
	if err != nil || checkpoint.CursorTime != 10_000 {
		t.Fatalf("checkpoint = %+v, err=%v", checkpoint, err)
	}
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	if len(items) != 1 || items[0].Status != "processed" || items[0].TodoID != todos.Items[0].ID {
		t.Fatalf("unexpected audit items: %+v", items)
	}
	if !strings.Contains(items[0].RawContext, "我想把需求收集做成全自动的") {
		t.Fatalf("collection audit lost raw conversation: %+v", items[0])
	}
}

func TestServiceRelatedWorkCreatesNewTodoAndKeepsExistingTodoUntouched(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	if err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		transaction.Todos().Items = []store.Todo{{ID: "t9", Title: "实现需求收集", Priority: "P1",
			Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()}}
		return nil
	}); err != nil {
		t.Fatalf("seed Todo: %v", err)
	}
	fetcher := &fakeFetcher{messages: []Message{{ID: "m2", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 11_000, Content: "补充：使用 connector 增量拉取"}}, newest: 11_000}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "实现需求收集",
		Summary: "使用 connector 增量拉取", ItemType: "requirement", Project: "atm", Priority: "P1",
		RelatedTodoID: "t9", Reason: "新的可执行事项", Confidence: 0.95}}
	service := Service{Fetcher: fetcher, Extractor: extractor, Now: tickingClock()}
	first, err := service.Run(context.Background(), source.ID)
	if err != nil || first.Runs[0].CreatedCount != 1 || first.Runs[0].AppendedCount != 0 {
		t.Fatalf("create related run=%+v err=%v", first, err)
	}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("repeat related run: %v", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 2 {
		t.Fatalf("related work should create one new Todo: %+v, %v", todos, err)
	}
	existing := store.FindTodo(todos, "t9")
	if existing == nil || existing.Description != "" {
		t.Fatalf("existing Todo was modified: %+v", existing)
	}
	var created *store.Todo
	for index := range todos.Items {
		if todos.Items[index].ID != "t9" {
			created = &todos.Items[index]
		}
	}
	if created == nil || !strings.Contains(created.Description, "使用 connector 增量拉取") ||
		!strings.Contains(created.Description, "相关历史 Todo：t9") {
		t.Fatalf("new Todo did not preserve the historical relation: %+v", created)
	}
}

func TestServiceDoesNotRegroupHandledMessagesWhenConversationExpands(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	if err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		transaction.Todos().Items = []store.Todo{{ID: "t9", Title: "实现需求收集", Priority: "P1",
			Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()}}
		return nil
	}); err != nil {
		t.Fatalf("seed Todo: %v", err)
	}
	fetcher := &fakeFetcher{messages: []Message{
		{ID: "m1", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_000, Content: "实现需求收集"},
		{ID: "m2", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_030, Content: "先使用 connector"},
	}, newest: 10_030}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "实现需求收集",
		Summary: "使用 connector 增量拉取", ItemType: "requirement", Project: "atm", Priority: "P1",
		RelatedTodoID: "t9", Reason: "新的事项与历史任务有关", Confidence: 0.95}}
	service := Service{Fetcher: fetcher, Extractor: extractor, Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// The overlap returns m1 and m2 again, while m3 extends the same 15-minute
	// conversation. Only m3 should form the next batch; hashing all three would
	// repeat the original decision under a different fingerprint.
	fetcher.messages = append(fetcher.messages, Message{ID: "m3", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 10_060, Content: "补充保留 20 分钟重叠窗口"})
	fetcher.newest = 10_060
	second, err := service.Run(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("expanded run: %v", err)
	}
	if extractor.calls != 2 || second.Runs[0].AnalyzedCount != 1 {
		t.Fatalf("expanded run reprocessed overlap: calls=%d run=%+v", extractor.calls, second.Runs[0])
	}
	db, _ := store.Open()
	items, err := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if err != nil || len(items) != 2 {
		t.Fatalf("expanded items = %+v, %v", items, err)
	}
	var newest store.CollectionItem
	for _, item := range items {
		if slices.Contains(item.MessageIDs, "m3") {
			newest = item
			break
		}
	}
	if len(newest.MessageIDs) != 1 || newest.MessageIDs[0] != "m3" {
		t.Fatalf("new batch still contains handled messages: %+v", newest.MessageIDs)
	}
	lastBatch := extractor.batches[len(extractor.batches)-1]
	if strings.Contains(lastBatch.ActionContext, "先使用 connector") {
		t.Fatalf("handled lines can still trigger work: %q", lastBatch.ActionContext)
	}
	if !strings.Contains(lastBatch.RawContext, "[上下文]") ||
		!strings.Contains(lastBatch.RawContext, "先使用 connector") ||
		!strings.Contains(lastBatch.RawContext, "[新消息]") {
		t.Fatalf("expanded batch lost conversation continuity: %q", lastBatch.RawContext)
	}
}

// An observation source has no authority to file work, so a create it asks for
// becomes an insight. The judgement that the content matters survives; the Todo
// does not get written.
func TestObservationSourceKeepsInsightAndNeverWritesTodo(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "大家在聊 AI 工具",
		Summary: "讨论不同工具的体验", ItemType: "requirement", Priority: "P1", Confidence: 0.9}}
	service := Service{Fetcher: &fakeFetcher{messages: []Message{
		{ID: "m-observe-1", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_000,
			Content: "最近都在用什么 AI？"},
	},
		newest: 10_000}, Extractor: extractor, Now: tickingClock()}
	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].InsightCount != 1 || report.Runs[0].CreatedCount != 0 {
		t.Fatalf("observation run=%+v err=%v", report, err)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 0 {
		t.Fatalf("observation source wrote todos: %+v", todos.Items)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	// item_type is left as the model classified the content: the clamp changes
	// where the item goes, not what it is, and Promote later files a Todo of the
	// right kind because of it.
	if len(items) != 1 || items[0].Action != "insight" || items[0].TodoID != "" ||
		items[0].ItemType != "requirement" || items[0].Title != "大家在聊 AI 工具" {
		t.Fatalf("observation item=%+v", items)
	}
	if !strings.Contains(items[0].Reason, "只观察不建 Todo") {
		t.Fatalf("clamped item should say why it was downgraded: %q", items[0].Reason)
	}
}

// A window holding one thing worth keeping and one joke has to be able to answer
// differently about each, which is why observation sources are grouped by topic
// rather than collapsed into a single batch per run.
func TestObservationSourceDecidesPerTopic(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	extractor := &fakeExtractor{decision: Decision{Action: "insight", Title: "记一笔",
		Summary: "值得留下的内容", ItemType: "insight", Confidence: 0.8}}
	// 33 minutes apart: past the 15-minute gap that separates one topic from the next.
	service := Service{Fetcher: &fakeFetcher{messages: []Message{
		{ID: "m-topic-1", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_000,
			Content: "connector 的增量拉取用 --since"},
		{ID: "m-topic-2", ConversationID: source.ExternalID, Sender: "临遥", CreatedAt: 12_000,
			Content: "午饭去哪里吃？"},
	},
		newest: 12_000}, Extractor: extractor, Now: tickingClock()}
	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].AnalyzedCount != 2 || extractor.calls != 2 {
		t.Fatalf("observation run=%+v calls=%d err=%v", report, extractor.calls, err)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 2 {
		t.Fatalf("expected one item per topic, got %+v", items)
	}
}

// Downgrading a decision is about what a source may do on its own. A person
// pointing at one of those items and saying "this is work" is a different act,
// and must not be clamped back to an insight.
func TestPromoteTurnsObservationInsightIntoTodo(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	service := Service{Fetcher: &fakeFetcher{messages: []Message{
		{ID: "m-promote-1", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_000,
			Content: "收集来源要走白名单"},
	},
		newest: 10_000},
		Extractor: &fakeExtractor{decision: Decision{Action: "insight", Title: "来源走白名单",
			Summary: "只采集显式添加的群和人", ItemType: "insight", Confidence: 0.9}},
		Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("observation run: %v", err)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 1 || items[0].Action != "insight" {
		t.Fatalf("expected one insight item, got %+v", items)
	}
	promoted, err := service.Promote(items[0].ID, ItemCorrection{})
	if err != nil {
		t.Fatalf("promote insight: %v", err)
	}
	if promoted.Action != "create" || promoted.TodoID == "" {
		t.Fatalf("promoted item=%+v", promoted)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 1 || todos.Items[0].Title != "来源走白名单" {
		t.Fatalf("promote should write exactly the Todo asked for: %+v", todos.Items)
	}
}

func observationSource(t *testing.T) store.CollectionSource {
	t.Helper()
	source := addCollectorSource(t)
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	source.Strategy, source.IntervalMinutes = store.CollectionStrategyObserve, 60
	source, err = store.UpsertCollectionSource(db, source)
	if err != nil {
		t.Fatalf("make source observation-only: %v", err)
	}
	return source
}

func TestRunDueHonorsSourceCadenceWhileManualRunForces(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, _ := store.Open()
	source.Strategy, source.IntervalMinutes = store.CollectionStrategyObserve, 60
	source, _ = store.UpsertCollectionSource(db, source)
	now := time.Unix(20_000, 0)
	if err := store.SaveCollectionRun(db, store.CollectionRun{ID: "recent-observation",
		Connector: source.Connector, SourceID: source.ID, Status: "succeeded",
		StartedAt: now.Add(-30 * time.Minute).Unix()}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	fetcher := &fakeFetcher{newest: now.Unix()}
	service := Service{Fetcher: fetcher, Extractor: &fakeExtractor{}, Now: func() time.Time { return now }}
	report, err := service.RunDue(context.Background(), source.ID)
	if err != nil || len(report.Runs) != 0 || len(fetcher.since) != 0 {
		t.Fatalf("not-due background run=%+v fetches=%v err=%v", report, fetcher.since, err)
	}
	report, err = service.Run(context.Background(), source.ID)
	if err != nil || len(report.Runs) != 1 || len(fetcher.since) != 1 {
		t.Fatalf("manual run did not force source: run=%+v fetches=%v err=%v", report, fetcher.since, err)
	}
}

func TestSourceExclusionIsAuditedWithoutCallingModel(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, _ := store.Open()
	source.ExcludePattern = "机器人通知, 构建成功"
	var err error
	source, err = store.UpsertCollectionSource(db, source)
	db.Close()
	if err != nil {
		t.Fatalf("update source exclusion: %v", err)
	}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "不应调用"}}
	service := Service{Fetcher: &fakeFetcher{messages: []Message{{ID: "bot1",
		ConversationID: source.ExternalID, Sender: "机器人", CreatedAt: 11_500,
		Content: "机器人通知：构建成功"}}, newest: 11_500}, Extractor: extractor, Now: tickingClock()}
	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].IgnoredCount != 1 || extractor.calls != 0 {
		t.Fatalf("excluded run=%+v calls=%d err=%v", report, extractor.calls, err)
	}
	db, _ = store.Open()
	defer db.Close()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	if len(items) != 1 || items[0].Action != "ignore" || !strings.Contains(items[0].Reason, "排除规则") {
		t.Fatalf("excluded audit item: %+v", items)
	}
}

func TestServiceFailureDoesNotAdvanceCheckpointAndCanRecover(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{ID: "m3", ConversationID: source.ExternalID,
		Sender: "测试发送人", CreatedAt: 12_000, Content: "这里有个 bug 需要修复"}}, newest: 12_000}
	failing := &fakeExtractor{err: errors.New("model unavailable")}
	service := Service{Fetcher: fetcher, Extractor: failing, Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err == nil || !strings.Contains(err.Error(), "model unavailable") {
		t.Fatalf("model failure was not surfaced: %v", err)
	}
	db, _ := store.Open()
	checkpoint, _ := store.GetCollectionCheckpoint(db, source.ID)
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if checkpoint.CursorTime != 0 || len(items) != 1 || items[0].Action != "failed" {
		t.Fatalf("failed run advanced/lost state: checkpoint=%+v items=%+v", checkpoint, items)
	}

	service.Extractor = &fakeExtractor{decision: Decision{Action: "create", Title: "修复聊天中的 bug",
		Summary: "复现并修复", ItemType: "bug", Priority: "P1", Confidence: 0.9}}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("retry run: %v", err)
	}
	db, _ = store.Open()
	checkpoint, _ = store.GetCollectionCheckpoint(db, source.ID)
	items, _ = store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if checkpoint.CursorTime != 12_000 || items[0].Action != "create" || items[0].TodoID == "" {
		t.Fatalf("failed item did not recover: checkpoint=%+v item=%+v", checkpoint, items[0])
	}
}

func TestFetcherFailurePreservesExistingCheckpoint(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, _ := store.Open()
	if err := store.SaveCollectionCheckpoint(db, store.CollectionCheckpoint{SourceID: source.ID, CursorTime: 5_000}); err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
	db.Close()
	fetcher := &fakeFetcher{err: errors.New("not_authenticated")}
	service := Service{Fetcher: fetcher, Extractor: &fakeExtractor{}, Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err == nil || !strings.Contains(err.Error(), "not_authenticated") {
		t.Fatalf("fetcher error was not surfaced: %v", err)
	}
	db, _ = store.Open()
	defer db.Close()
	checkpoint, _ := store.GetCollectionCheckpoint(db, source.ID)
	if checkpoint.CursorTime != 5_000 || len(fetcher.since) != 1 || fetcher.since[0] != 3_800 {
		t.Fatalf("checkpoint/overlap changed: checkpoint=%+v since=%v", checkpoint, fetcher.since)
	}
}

func TestItemPromotionCorrectionAndRevert(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	service := Service{Fetcher: &fakeFetcher{messages: []Message{{ID: "m4",
		ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 13_000,
		Content: "可以先看看这个想法"}}, newest: 13_000},
		Extractor: &fakeExtractor{decision: Decision{Action: "ignore", ItemType: "conversation",
			Reason: "没有明确行动", Confidence: 0.7}}, Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("ignore run: %v", err)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 1 || items[0].Action != "ignore" {
		t.Fatalf("missing ignored audit item: %+v", items)
	}
	title := "评估需求自动收集方案"
	promoted, err := service.Promote(items[0].ID, ItemCorrection{Title: &title})
	if err != nil || promoted.Action != "create" || promoted.TodoID == "" {
		t.Fatalf("promote item=%+v err=%v", promoted, err)
	}
	correctedTitle, project, priority := "实现需求自动收集方案", "platform", "P0"
	corrected, err := service.Correct(promoted.ID, ItemCorrection{
		Title: &correctedTitle, Project: &project, Priority: &priority,
	})
	if err != nil || corrected.Title != correctedTitle || corrected.Project != project || corrected.Priority != priority {
		t.Fatalf("correct item=%+v err=%v", corrected, err)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 1 || todos.Items[0].Title != correctedTitle ||
		todos.Items[0].Project != project || todos.Items[0].Priority != priority {
		t.Fatalf("Todo correction did not stay in sync: %+v", todos.Items)
	}
	reverted, err := service.Revert(corrected.ID)
	if err != nil || reverted.Action != "reverted" {
		t.Fatalf("revert item=%+v err=%v", reverted, err)
	}
	todos, _ = store.LoadTodosReadOnly()
	if len(todos.Items) != 1 || todos.Items[0].Status != store.TodoStatusDropped || todos.Items[0].ClosedReason == nil {
		t.Fatalf("created Todo was not safely dropped: %+v", todos.Items)
	}
	oldTodoID := todos.Items[0].ID
	service.Extractor = &fakeExtractor{decision: Decision{Action: "create", Title: "重新判断后的事项",
		Summary: "用户撤销后要求重新处理", ItemType: "follow_up", Priority: "P1", Confidence: 0.9}}
	reprocessed, err := service.Reprocess(context.Background(), reverted.ID)
	if err != nil || reprocessed.Action != "create" || reprocessed.TodoID == oldTodoID {
		t.Fatalf("reprocess reverted item=%+v err=%v", reprocessed, err)
	}
	todos, _ = store.LoadTodosReadOnly()
	if len(todos.Items) != 2 || todos.Items[1].Status != store.TodoStatusOpen {
		t.Fatalf("reprocess should create a new active Todo: %+v", todos.Items)
	}
}

func TestRunArchivesFetchedChatWhateverTheDecisionIs(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetched := []Message{
		{ID: "m1", ConversationID: source.ExternalID, Sender: "测试发送人", CreatedAt: 10_000,
			Content: "我想把需求收集做成全自动的"},
		// Chatter the classifier will not act on: the archive is the only place it
		// survives, which is the whole point of keeping it.
		{ID: "m2", ConversationID: source.ExternalID, Sender: "测试用户", CreatedAt: 10_060,
			Content: "顺便说下午饭吃什么"},
	}
	service := Service{
		Fetcher:   &fakeFetcher{messages: fetched, newest: 10_060},
		Extractor: &fakeExtractor{decision: Decision{Action: "ignore", Reason: "闲聊"}},
		Now:       tickingClock(),
	}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("collection run: %v", err)
	}
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored, err := store.ListCollectionMessages(db, store.CollectionMessageQuery{
		ConversationID: source.ExternalID, Limit: 10,
	})
	if err != nil || len(stored) != 2 {
		t.Fatalf("archived messages=%+v err=%v", stored, err)
	}
	if stored[0].MessageID != "m1" || stored[1].Content != "顺便说下午饭吃什么" {
		t.Fatalf("unexpected archived messages: %+v", stored)
	}
	if stored[0].SourceID != source.ID || stored[0].ConversationName != source.Name {
		t.Fatalf("archived message lost its source: %+v", stored[0])
	}
	// A second run over the same window must not duplicate the archive.
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("second collection run: %v", err)
	}
	stats, err := store.CollectionMessageStatsFor(db)
	if err != nil || stats.Total != 2 {
		t.Fatalf("archive stats after re-run = %+v, err=%v", stats, err)
	}
}

func TestAutomaticExtractorFailsClosedUnlessRuleModeIsExplicit(t *testing.T) {
	batch := MessageBatch{Source: store.CollectionSource{Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 我想实现自动需求收集"}
	if _, err := (AutomaticExtractor{ModelCommand: filepath.Join(t.TempDir(), "missing")}).Extract(context.Background(), batch, nil); err == nil {
		t.Fatal("missing model command silently fell back")
	}
	decision, err := (AutomaticExtractor{ModelCommand: "rule"}).Extract(context.Background(), batch, nil)
	if err != nil || decision.Action != "create" || decision.Project != "atm" || decision.Priority != "P2" {
		t.Fatalf("explicit rule decision=%+v err=%v", decision, err)
	}
}

// A rate-limited primary model is the whole reason the chain exists: the run
// must continue on the next CLI instead of failing the source.
func TestAutomaticExtractorFallsBackToTheNextModelInTheChain(t *testing.T) {
	rateLimited := writeFakeModel(t, "rate-limited", "echo 'usage limit reached' >&2\nexit 1\n")
	working := writeFakeModel(t, "working",
		`printf '%s' '{"action":"create","title":"实现自动收集","summary":"从聊天创建 Todo","item_type":"requirement","project":"atm","priority":"P1","related_todo_id":"","reason":"明确需求","confidence":0.9}'`)
	batch := MessageBatch{Source: store.CollectionSource{Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 想做自动收集"}
	decision, err := (AutomaticExtractor{ModelCommand: rateLimited + "," + working, Timeout: 5 * time.Second}).
		Extract(context.Background(), batch, nil)
	if err != nil || decision.Action != "create" || decision.Priority != "P1" {
		t.Fatalf("chain decision=%+v err=%v", decision, err)
	}
}

func TestAutomaticExtractorDegradesToRulesOnlyAtTheEndOfTheChain(t *testing.T) {
	rateLimited := writeFakeModel(t, "rate-limited", "echo 'usage limit reached' >&2\nexit 1\n")
	batch := MessageBatch{Source: store.CollectionSource{Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 我想实现自动需求收集"}
	if _, err := (AutomaticExtractor{ModelCommand: rateLimited, Timeout: 5 * time.Second}).
		Extract(context.Background(), batch, nil); err == nil {
		t.Fatal("a chain without rule must fail closed when the model fails")
	}
	decision, err := (AutomaticExtractor{ModelCommand: rateLimited + ",rule", Timeout: 5 * time.Second}).
		Extract(context.Background(), batch, nil)
	if err != nil || decision.Action != "create" {
		t.Fatalf("rule fallback decision=%+v err=%v", decision, err)
	}
	if !strings.Contains(decision.Reason, "降级") || !strings.Contains(decision.Reason, "usage limit reached") {
		t.Fatalf("degraded decision should say why: %q", decision.Reason)
	}
}

func TestAutomaticExtractorAcceptsSchemaConstrainedModelDecision(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-codex")
	body := `#!/bin/sh
printf '%s\n' '{"action":"create","title":"实现自动收集","summary":"从聊天创建 Todo","item_type":"requirement","project":"atm","priority":"P1","related_todo_id":"","reason":"明确需求","confidence":0.98}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write model command: %v", err)
	}
	batch := MessageBatch{Source: store.CollectionSource{Name: "产品群", Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 想做自动收集并添加 Todo"}
	decision, err := (AutomaticExtractor{ModelCommand: script, Timeout: 5 * time.Second}).Extract(
		context.Background(), batch, nil,
	)
	if err != nil || decision.Action != "create" || decision.Title != "实现自动收集" || decision.Priority != "P1" {
		t.Fatalf("model decision=%+v err=%v", decision, err)
	}
}

type fakeSummarizer struct {
	content DigestContent
	err     error
	calls   int
	inputs  []DigestInput
}

func (summarizer *fakeSummarizer) Summarize(_ context.Context, input DigestInput) (DigestContent, error) {
	summarizer.calls++
	summarizer.inputs = append(summarizer.inputs, input)
	return summarizer.content, summarizer.err
}

// insightItem stores an insight decision directly, so digest tests do not have to
// go through a fetch and a classification to get their input.
func insightItem(t *testing.T, source store.CollectionSource, id, title string, occurredAt int64) {
	t.Helper()
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()
	if _, _, err := store.PutCollectionItem(db, store.CollectionItem{
		SourceID: source.ID, Connector: source.Connector, ConversationID: source.ExternalID,
		Fingerprint: "fp-" + id, MessageIDs: []string{id}, Sender: "测试用户",
		OccurredAt: occurredAt, Title: title, Summary: title + " 的细节",
		ItemType: "insight", Action: "insight", Status: "processed",
	}); err != nil {
		t.Fatalf("store insight: %v", err)
	}
}

func digestDayFor(occurredAt int64) string {
	return time.Unix(occurredAt, 0).In(config.Loc).Format("2006-01-02")
}

// Running a digest twice for the same day must rewrite one document rather than
// file a second: a background caller runs it while the day is still going.
func TestDigestRewritesOneDocumentPerSourcePerDay(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	occurred := time.Now().In(config.Loc).Add(-2 * time.Hour).Unix()
	insightItem(t, source, "m-digest-1", "connector 增量拉取用 --since", occurred)
	summarizer := &fakeSummarizer{content: DigestContent{Title: "产品讨论 动态", Body: "## 采集\n\n用 --since 增量拉取。"}}
	service := Service{Summarizer: summarizer, Now: func() time.Time { return time.Now().In(config.Loc) }}
	date := digestDayFor(occurred)

	first, err := service.Digest(context.Background(), source.ID, DigestOptions{Date: date})
	if err != nil || len(first.Results) != 1 || first.Results[0].Status != "created" {
		t.Fatalf("first digest=%+v err=%v", first, err)
	}
	documentID := first.Results[0].DocumentID
	if documentID == "" || first.Results[0].Collection != config.CollectionDigestCollection {
		t.Fatalf("first digest result=%+v", first.Results[0])
	}

	// Nothing new: the digest already covers every insight, so no model call.
	repeat, err := service.Digest(context.Background(), source.ID, DigestOptions{Date: date})
	if err != nil || repeat.Results[0].Status != "skipped" || summarizer.calls != 1 {
		t.Fatalf("unchanged digest=%+v calls=%d err=%v", repeat, summarizer.calls, err)
	}

	// A later insight makes it due again, and rewrites the same document.
	insightItem(t, source, "m-digest-2", "白名单只采集显式来源", occurred+600)
	second, err := service.Digest(context.Background(), source.ID, DigestOptions{Date: date})
	if err != nil || second.Results[0].Status != "updated" ||
		second.Results[0].DocumentID != documentID || second.Results[0].ItemCount != 2 {
		t.Fatalf("second digest=%+v err=%v", second, err)
	}
	if summarizer.calls != 2 || len(summarizer.inputs[1].Items) != 2 {
		t.Fatalf("a regenerated digest must see the whole day: calls=%d input=%+v",
			summarizer.calls, summarizer.inputs)
	}
	documents, err := knowledge.List(config.AtmDir, nil)
	if err != nil {
		t.Fatalf("list knowledge: %v", err)
	}
	if len(documents) != 1 {
		t.Fatalf("expected one digest document, got %+v", documents)
	}
}

func TestDigestSkipsSourceWithoutInsights(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	summarizer := &fakeSummarizer{content: DigestContent{Title: "x", Body: "y"}}
	service := Service{Summarizer: summarizer, Now: func() time.Time { return time.Now().In(config.Loc) }}
	report, err := service.Digest(context.Background(), source.ID, DigestOptions{})
	if err != nil || report.Results[0].Status != "skipped" || summarizer.calls != 0 {
		t.Fatalf("empty digest=%+v calls=%d err=%v", report, summarizer.calls, err)
	}
}

// --due exists so a background caller can poll far more often than a day's chat
// changes without paying for a model call every time.
func TestDigestDueWaitsOutTheInterval(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	occurred := time.Now().In(config.Loc).Add(-2 * time.Hour).Unix()
	insightItem(t, source, "m-due-1", "第一条", occurred)
	summarizer := &fakeSummarizer{content: DigestContent{Title: "产品讨论 动态", Body: "## 采集\n\n内容。"}}
	service := Service{Summarizer: summarizer, Now: func() time.Time { return time.Now().In(config.Loc) }}
	date := digestDayFor(occurred)
	if _, err := service.Digest(context.Background(), source.ID, DigestOptions{Date: date}); err != nil {
		t.Fatalf("seed digest: %v", err)
	}
	insightItem(t, source, "m-due-2", "第二条", occurred+600)

	held, err := service.Digest(context.Background(), source.ID,
		DigestOptions{Date: date, DueOnly: true})
	if err != nil || held.Results[0].Status != "skipped" || summarizer.calls != 1 {
		t.Fatalf("due digest should wait: %+v calls=%d err=%v", held, summarizer.calls, err)
	}
	// The same state without --due is a manual "do it now" and goes through.
	forced, err := service.Digest(context.Background(), source.ID, DigestOptions{Date: date})
	if err != nil || forced.Results[0].Status != "updated" || summarizer.calls != 2 {
		t.Fatalf("manual digest should force: %+v calls=%d err=%v", forced, summarizer.calls, err)
	}
}

func TestDigestDryRunReturnsBodyWithoutWriting(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	occurred := time.Now().In(config.Loc).Add(-time.Hour).Unix()
	insightItem(t, source, "m-dry-1", "只读演练", occurred)
	summarizer := &fakeSummarizer{content: DigestContent{Title: "产品讨论 动态", Body: "## 采集\n\n正文。"}}
	service := Service{Summarizer: summarizer, Now: func() time.Time { return time.Now().In(config.Loc) }}
	report, err := service.Digest(context.Background(), source.ID,
		DigestOptions{Date: digestDayFor(occurred), DryRun: true})
	if err != nil || report.Results[0].Status != "skipped" || report.Results[0].Body == "" {
		t.Fatalf("dry run=%+v err=%v", report, err)
	}
	if !strings.Contains(report.Results[0].Body, "只读演练") {
		t.Fatalf("dry-run body should carry the source list: %q", report.Results[0].Body)
	}
	documents, _ := knowledge.List(config.AtmDir, nil)
	if len(documents) != 0 {
		t.Fatalf("dry run wrote knowledge: %+v", documents)
	}
	db, _ := store.Open()
	digest, _ := store.GetCollectionDigest(db, source.ID, digestDayFor(occurred))
	db.Close()
	if digest.DocumentID != "" {
		t.Fatalf("dry run recorded a digest: %+v", digest)
	}
}

// A source can name the collection its digest belongs in, so a 1:1 with a
// colleague can file into a project's knowledge while a noisy group stays apart.
func TestDigestHonorsSourceKnowledgeCollection(t *testing.T) {
	withCollectorStore(t)
	source := observationSource(t)
	db, _ := store.Open()
	source.KnowledgeCollection = "atm"
	source, _ = store.UpsertCollectionSource(db, source)
	db.Close()
	occurred := time.Now().In(config.Loc).Add(-time.Hour).Unix()
	insightItem(t, source, "m-collection-1", "归到 atm", occurred)
	service := Service{Now: func() time.Time { return time.Now().In(config.Loc) },
		Summarizer: &fakeSummarizer{content: DigestContent{Title: "产品讨论 动态", Body: "正文"}}}
	report, err := service.Digest(context.Background(), source.ID,
		DigestOptions{Date: digestDayFor(occurred)})
	if err != nil || report.Results[0].Collection != "atm" {
		t.Fatalf("digest collection=%+v err=%v", report, err)
	}
}
