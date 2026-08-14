package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
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
	// decide, when set, answers per batch. A run that has to produce a different
	// decision for each message it sees cannot use a single canned one.
	decide  func(MessageBatch) Decision
	err     error
	calls   int
	batches []MessageBatch
}

type fakeTodoDispatcher struct {
	todoIDs  []string
	projects []string
	err      error
}

func (dispatcher *fakeTodoDispatcher) Dispatch(_ context.Context, todoID, project string) error {
	dispatcher.todoIDs = append(dispatcher.todoIDs, todoID)
	dispatcher.projects = append(dispatcher.projects, project)
	return dispatcher.err
}

func (extractor *fakeExtractor) Extract(_ context.Context, batch MessageBatch, _ []store.Todo) (Decision, error) {
	extractor.calls++
	extractor.batches = append(extractor.batches, batch)
	if extractor.decide != nil {
		return extractor.decide(batch), extractor.err
	}
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
	// Collection files work nobody asked for by hand, and the creator has to say
	// so: it is the difference between a Todo to review and a Todo I wrote.
	if todos.Items[0].Creator != store.TodoCreatorCollect {
		t.Fatalf("created Todo creator = %q, want %q", todos.Items[0].Creator, store.TodoCreatorCollect)
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

func TestServiceAutoDispatchesNewTodoOnce(t *testing.T) {
	withCollectorStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-auto", Name: "自动执行群",
		Project: "atm", Priority: "P1", Strategy: store.CollectionStrategyTasks,
		AutoDispatch: true, Enabled: true,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{messages: []Message{{ID: "auto-1", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 13_000, Content: "请实现自动派发"}}, newest: 13_000}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "实现自动派发",
		Summary: "采集后交给 Agent", ItemType: "requirement", Project: "/tmp/untrusted-message-project",
		Priority: "P1", Reason: "明确需求", Confidence: 0.98}}
	dispatcher := &fakeTodoDispatcher{}
	service := Service{Fetcher: fetcher, Extractor: extractor, Dispatcher: dispatcher, Now: tickingClock()}

	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("repeat run: %v", err)
	}
	if len(dispatcher.todoIDs) != 1 || dispatcher.todoIDs[0] == "" {
		t.Fatalf("dispatches = %v, want exactly one Todo", dispatcher.todoIDs)
	}
	if len(dispatcher.projects) != 1 || dispatcher.projects[0] != "atm" {
		t.Fatalf("dispatch projects = %v", dispatcher.projects)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || todos.Items[0].Project != "atm" {
		t.Fatalf("automatic Todo escaped configured project: %+v err=%v", todos, err)
	}
	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := store.ListCollectionItems(db, source.ID, 10)
	if err != nil || len(items) != 1 || items[0].DispatchStatus != "dispatched" || items[0].DispatchError != "" {
		t.Fatalf("items = %+v, err=%v", items, err)
	}
}

func TestServiceRecordsDispatchFailureWithoutLosingCreatedTodo(t *testing.T) {
	withCollectorStore(t)
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-auto-fail", Project: "atm",
		Priority: "P1", AutoDispatch: true, Enabled: true,
	})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Fetcher: &fakeFetcher{messages: []Message{{ID: "fail-1", ConversationID: source.ExternalID,
			CreatedAt: 14_000, Content: "实现失败可重试"}}, newest: 14_000},
		Extractor: &fakeExtractor{decision: Decision{Action: "create", Title: "实现失败重试",
			Summary: "保留 Todo 和证据", ItemType: "requirement", Project: "atm", Priority: "P1",
			Reason: "明确需求", Confidence: 0.98}},
		Dispatcher: &fakeTodoDispatcher{err: errors.New("codex unavailable")}, Now: tickingClock(),
	}
	report, err := service.Run(context.Background(), source.ID)
	if err == nil || len(report.Runs) != 1 || report.Runs[0].Status != "failed" {
		t.Fatalf("run=%+v err=%v", report, err)
	}
	todos, loadErr := store.LoadTodosReadOnly()
	if loadErr != nil || len(todos.Items) != 1 || todos.Items[0].Status != store.TodoStatusOpen {
		t.Fatalf("created Todo was lost or advanced: %+v err=%v", todos, loadErr)
	}
	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := store.ListCollectionItems(db, source.ID, 10)
	if err != nil || len(items) != 1 || items[0].Status != "processed" ||
		items[0].DispatchStatus != "failed" || !strings.Contains(items[0].DispatchError, "codex unavailable") {
		t.Fatalf("items = %+v, err=%v", items, err)
	}
	checkpoint, err := store.GetCollectionCheckpoint(db, source.ID)
	if err != nil || checkpoint.CursorTime != 14_000 {
		t.Fatalf("checkpoint = %+v, err=%v", checkpoint, err)
	}
}

func TestCollectionProjectWorkDirUsesConfiguredProjectRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, "mox", "atm")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := collectionProjectWorkDir("atm")
	if err != nil || got != want {
		t.Fatalf("work dir=%q err=%v, want %q", got, err, want)
	}
	if _, err := collectionProjectWorkDir(""); err == nil {
		t.Fatal("empty project resolved for automatic dispatch")
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

// A group returns to the same topic every few minutes, and each return is its own
// batch. Filing every one of them buries the single item somebody has to act on,
// so a follow-up lands on the Todo this same conversation already filed.
func TestServiceAppendsFollowUpToTheTodoTheSameChatFiled(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{ID: "m1", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 10_000, Content: "技能批测会命中默认 SKILL"}}, newest: 10_000}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "排查技能命中默认 SKILL",
		Summary: "批测有概率命中默认 SKILL", ItemType: "investigation", Project: "atm",
		Priority: "P1", Reason: "明确排查项", Confidence: 0.95}}
	service := Service{Fetcher: fetcher, Extractor: extractor, Now: tickingClock()}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("first run: %v", err)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 1 {
		t.Fatalf("first run should file one Todo: %+v", todos.Items)
	}
	filed := todos.Items[0].ID

	// 40 minutes later the group is still on the same topic, with a new finding.
	fetcher.messages = []Message{{ID: "m2", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 12_400, Content: "新结论：只注入了 definition，没在 prompt 里触发"}}
	fetcher.newest = 12_400
	extractor.decision = Decision{Action: "append", Title: "补充技能激活排查结论",
		Summary: "只注入了 skill definition，未在 prompt 里真正触发激活", ItemType: "investigation",
		Project: "atm", Priority: "P1", RelatedTodoID: filed, Reason: "同一件事的新进展", Confidence: 0.93}
	second, err := service.Run(context.Background(), source.ID)
	if err != nil || second.Runs[0].AppendedCount != 1 || second.Runs[0].CreatedCount != 0 {
		t.Fatalf("append run=%+v err=%v", second, err)
	}
	todos, _ = store.LoadTodosReadOnly()
	if len(todos.Items) != 1 {
		t.Fatalf("follow-up filed a duplicate Todo: %+v", todos.Items)
	}
	doc, err := store.ReadTodoDoc(filed)
	if err != nil {
		t.Fatalf("read Todo doc: %v", err)
	}
	if !strings.Contains(doc, "## 补充") ||
		!strings.Contains(doc, "只注入了 skill definition，未在 prompt 里真正触发激活") {
		t.Fatalf("append did not reach the Todo's 补充 section:\n%s", doc)
	}
	// The App strips exactly this marker out of the task timeline and keeps it on
	// disk to tie the entry back to its collection item.
	if !strings.Contains(doc, "<!-- [钉钉采集:") {
		t.Fatalf("append lost its traceability marker:\n%s", doc)
	}
	// The requirement is generated from the description on every metadata sync, so
	// an append must not have gone there.
	if todos.Items[0].Description != "批测有概率命中默认 SKILL" {
		t.Fatalf("append rewrote the Todo description: %q", todos.Items[0].Description)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 2 {
		t.Fatalf("expected one item per batch: %+v", items)
	}
	appended := items[0]
	if appended.Action != "append" || appended.TodoID != filed || appended.Status != "processed" {
		t.Fatalf("append item=%+v", appended)
	}
}

// The classifier reads untrusted chat, so the one write it can aim at an existing
// record is held to the thread that produced it. A Todo somebody wrote by hand is
// not editable by whatever a message claims to relate to — and the batch is filed
// rather than dropped, because a decision recorded nowhere is the worse failure.
func TestServiceRefusesToAppendOutsideTheConversationThatFiledTheTodo(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	if err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		transaction.Todos().Items = []store.Todo{{ID: "t9", Title: "手写的任务", Priority: "P1",
			Status: store.TodoStatusOpen, Project: "atm", Created: store.Today()}}
		return nil
	}); err != nil {
		t.Fatalf("seed Todo: %v", err)
	}
	fetcher := &fakeFetcher{messages: []Message{{ID: "m2", ConversationID: source.ExternalID,
		Sender: "测试用户", CreatedAt: 11_000, Content: "顺带说一下手写任务的事"}}, newest: 11_000}
	extractor := &fakeExtractor{decision: Decision{Action: "append", Title: "补充手写任务",
		Summary: "群里提到的补充信息", ItemType: "follow_up", Project: "atm", Priority: "P1",
		RelatedTodoID: "t9", Reason: "自称与 t9 有关", Confidence: 0.9}}
	service := Service{Fetcher: fetcher, Extractor: extractor, Now: tickingClock()}
	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].CreatedCount != 1 || report.Runs[0].AppendedCount != 0 {
		t.Fatalf("refused append run=%+v err=%v", report, err)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 2 {
		t.Fatalf("refused append should still file the batch: %+v", todos.Items)
	}
	if store.TodoDocExists("t9") {
		doc, _ := store.ReadTodoDoc("t9")
		if strings.Contains(doc, "群里提到的补充信息") {
			t.Fatalf("collector edited a Todo outside its conversation:\n%s", doc)
		}
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	// The record has to say what happened, not what was asked for: the run counted
	// a create, and Revert drops a Todo rather than writing a compensating note.
	if len(items) != 1 || items[0].Action != "create" {
		t.Fatalf("refused append item=%+v", items)
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

// Batching is time-based, so a noisy broadcast routinely shares a window with a
// real request. Excluding per batch dropped both — a CR bot's "有新增commits"
// silently took the "邀请你评审" three minutes before it down as well.
func TestSourceExclusionDropsOnlyTheMatchedMessages(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	db, _ := store.Open()
	source.ExcludePattern = "构建成功"
	var err error
	source, err = store.UpsertCollectionSource(db, source)
	db.Close()
	if err != nil {
		t.Fatalf("update source exclusion: %v", err)
	}
	extractor := &fakeExtractor{decision: Decision{Action: "create", Title: "修复登录超时",
		ItemType: "bug", Confidence: 0.9}}
	service := Service{Fetcher: &fakeFetcher{messages: []Message{
		{ID: "noise", ConversationID: source.ExternalID, Sender: "机器人",
			CreatedAt: 11_400, Content: "构建成功：主干流水线 #42"},
		{ID: "real", ConversationID: source.ExternalID, Sender: "测试发送人",
			CreatedAt: 11_500, Content: "登录超时需要修复一下"},
	}, newest: 11_500}, Extractor: extractor, Now: tickingClock()}
	report, err := service.Run(context.Background(), source.ID)
	if err != nil || report.Runs[0].CreatedCount != 1 || extractor.calls != 1 {
		t.Fatalf("mixed batch run=%+v calls=%d err=%v", report, extractor.calls, err)
	}
	batch := extractor.batches[0]
	if strings.Contains(batch.ActionContext, "构建成功") || !strings.Contains(batch.ActionContext, "登录超时") {
		t.Fatalf("action context still carries the excluded line: %q", batch.ActionContext)
	}
	// The excluded line stays readable as continuity, but only as [上下文]: the
	// prompt allows [新消息] lines alone to trigger a decision.
	if !strings.Contains(batch.RawContext, "[上下文] ") || !strings.Contains(batch.RawContext, "构建成功") ||
		!strings.Contains(batch.RawContext, "[新消息] ") {
		t.Fatalf("raw context lost the excluded line or its marker: %q", batch.RawContext)
	}
	db, _ = store.Open()
	defer db.Close()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	// Both IDs belong to the item, so the excluded message counts as handled and
	// is not re-fetched into a fresh batch on every later run.
	if len(items) != 1 || len(items[0].MessageIDs) != 2 {
		t.Fatalf("excluded message left unhandled: %+v", items)
	}
}

// A batch yields exactly one decision, so what a batch covers decides how many
// separate events can survive one window. Chat wants them merged — a request and
// the line clarifying it are one Todo. A notification feed wants them apart: two
// review invitations three minutes apart are two pieces of work, and grouping
// them keeps one and loses the other with no trace.
func TestDecisionUnitDecidesHowManyEventsSurviveOneWindow(t *testing.T) {
	messages := []Message{
		{ID: "first", ConversationID: "cid-product", Sender: "机器人",
			CreatedAt: 11_400, Content: "邀请你评审代码：登录重构"},
		{ID: "second", ConversationID: "cid-product", Sender: "机器人",
			CreatedAt: 11_520, Content: "邀请你评审代码：配额上报"},
	}
	titleFromLastLine := func(batch MessageBatch) Decision {
		lines := strings.Split(strings.TrimSpace(batch.ActionContext), "\n")
		return Decision{Action: "create", ItemType: "follow_up", Confidence: 0.9,
			Title: lines[len(lines)-1]}
	}

	for _, testCase := range []struct {
		unit      string
		wantCalls int
		wantTodos int
	}{
		{unit: store.CollectionDecisionUnitWindow, wantCalls: 1, wantTodos: 1},
		{unit: store.CollectionDecisionUnitMessage, wantCalls: 2, wantTodos: 2},
	} {
		t.Run(testCase.unit, func(t *testing.T) {
			withCollectorStore(t)
			source := addCollectorSource(t)
			db, _ := store.Open()
			source.DecisionUnit = testCase.unit
			var err error
			source, err = store.UpsertCollectionSource(db, source)
			db.Close()
			if err != nil {
				t.Fatalf("set decision unit %s: %v", testCase.unit, err)
			}
			extractor := &fakeExtractor{decide: titleFromLastLine}
			service := Service{Fetcher: &fakeFetcher{messages: messages, newest: 11_520},
				Extractor: extractor, Now: tickingClock()}
			report, err := service.Run(context.Background(), source.ID)
			if err != nil || extractor.calls != testCase.wantCalls ||
				report.Runs[0].CreatedCount != testCase.wantTodos {
				t.Fatalf("unit %s: run=%+v calls=%d err=%v",
					testCase.unit, report.Runs[0], extractor.calls, err)
			}
			db, _ = store.Open()
			defer db.Close()
			items, _ := store.ListCollectionItems(db, source.ID, 10)
			todos := map[string]struct{}{}
			for _, item := range items {
				todos[item.TodoID] = struct{}{}
			}
			if len(items) != testCase.wantTodos || len(todos) != testCase.wantTodos {
				t.Fatalf("unit %s: items=%+v distinct todos=%d", testCase.unit, items, len(todos))
			}
			if testCase.unit == store.CollectionDecisionUnitWindow {
				return
			}
			// Each message was decided alone, but still read next to the rest of
			// its window: the other line is present as context, not as a trigger.
			for index, batch := range extractor.batches {
				if strings.Count(batch.ActionContext, "邀请你评审代码") != 1 {
					t.Fatalf("batch %d decided on more than its own message: %q", index, batch.ActionContext)
				}
				if strings.Count(batch.RawContext, "邀请你评审代码") != 2 ||
					strings.Count(batch.RawContext, "[新消息] ") != 1 {
					t.Fatalf("batch %d lost its window as context: %q", index, batch.RawContext)
				}
			}
		})
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

// A batch that fails the same way every run must stop costing a model call, and
// must stop holding the checkpoint: an unbounded retry turns one broken message
// into a permanent tax on every later run.
func TestFailedBatchStopsRetryingWhenItsAttemptBudgetRunsOut(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{ID: "m9", ConversationID: source.ExternalID,
		Sender: "测试发送人", CreatedAt: 12_000, Content: "这段内容解析不了"}}, newest: 12_000}
	failing := &fakeExtractor{err: errors.New("model unavailable")}
	service := Service{Fetcher: fetcher, Extractor: failing, Now: tickingClock()}

	for attempt := 1; attempt <= store.MaxCollectionAttempts; attempt++ {
		if _, err := service.Run(context.Background(), source.ID); err == nil {
			t.Fatalf("attempt %d hid the model failure", attempt)
		}
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	checkpoint, _ := store.GetCollectionCheckpoint(db, source.ID)
	db.Close()
	if len(items) != 1 || items[0].Attempts != store.MaxCollectionAttempts || !store.CollectionRetriesExhausted(items[0]) {
		t.Fatalf("attempts were not counted: items=%+v", items)
	}
	if checkpoint.CursorTime != 0 {
		t.Fatalf("checkpoint advanced while retries remained: %+v", checkpoint)
	}

	// The run after the budget is spent must be clean, not another failure, or the
	// source stays permanently due and the re-read window keeps growing.
	report, err := service.Run(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("run after the budget ran out still failed: %v", err)
	}
	if failing.calls != store.MaxCollectionAttempts {
		t.Fatalf("retry kept spending model calls: calls=%d", failing.calls)
	}
	if report.Runs[0].Status != "succeeded" || report.Runs[0].FailedCount != 0 {
		t.Fatalf("retired item still failed the run: %+v", report.Runs[0])
	}
	db, _ = store.Open()
	items, _ = store.ListCollectionItems(db, source.ID, 10)
	checkpoint, _ = store.GetCollectionCheckpoint(db, source.ID)
	db.Close()
	if checkpoint.CursorTime != 12_000 {
		t.Fatalf("retired item kept the checkpoint pinned: %+v", checkpoint)
	}
	if len(items) != 1 || items[0].Action != "failed" {
		t.Fatalf("retired item was dropped from the ledger instead of kept: %+v", items)
	}
}

// Retiring an item stops the automatic retry, not every retry: asking for one
// explicitly is how someone says they fixed the cause.
func TestReprocessRestoresTheAutomaticRetryBudget(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{ID: "m10", ConversationID: source.ExternalID,
		Sender: "测试发送人", CreatedAt: 12_000, Content: "连接器刚刚挂了"}}, newest: 12_000}
	failing := &fakeExtractor{err: errors.New("model unavailable")}
	service := Service{Fetcher: fetcher, Extractor: failing, Now: tickingClock()}
	for attempt := 1; attempt <= store.MaxCollectionAttempts; attempt++ {
		service.Run(context.Background(), source.ID)
	}
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 1 || !store.CollectionRetriesExhausted(items[0]) {
		t.Fatalf("item was not retired before the reprocess: %+v", items)
	}

	service.Extractor = &fakeExtractor{decision: Decision{Action: "create", Title: "修好连接器后重试",
		Summary: "重新解析", ItemType: "bug", Priority: "P1", Confidence: 0.9}}
	item, err := service.Reprocess(context.Background(), items[0].ID)
	if err != nil {
		t.Fatalf("reprocess after retirement: %v", err)
	}
	if item.Action != "create" || item.TodoID == "" {
		t.Fatalf("reprocess did not carry out the decision: %+v", item)
	}
	if store.CollectionRetriesExhausted(item) {
		t.Fatalf("reprocess kept the item retired: %+v", item)
	}
}

// The retry rebuilds a batch from its messages, so one more message in the same
// conversation produces a different batch. The item left behind must not keep
// promising a retry that will never rebuild it.
func TestExpandedBatchRetiresTheFailedItemItAbsorbs(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	fetcher := &fakeFetcher{messages: []Message{{ID: "m11", ConversationID: source.ExternalID,
		Sender: "测试发送人", CreatedAt: 12_000, Content: "这个功能要改"}}, newest: 12_000}
	service := Service{Fetcher: fetcher, Extractor: &fakeExtractor{err: errors.New("model unavailable")},
		Now: tickingClock()}
	service.Run(context.Background(), source.ID)
	db, _ := store.Open()
	items, _ := store.ListCollectionItems(db, source.ID, 10)
	db.Close()
	if len(items) != 1 || items[0].Attempts != 1 {
		t.Fatalf("unexpected state after the first failure: %+v", items)
	}
	orphanID := items[0].ID

	fetcher.messages = append(fetcher.messages, Message{ID: "m12", ConversationID: source.ExternalID,
		Sender: "测试发送人", CreatedAt: 12_060, Content: "补充一下，优先级 P1"})
	fetcher.newest = 12_060
	service.Extractor = &fakeExtractor{decision: Decision{Action: "create", Title: "改这个功能",
		Summary: "按补充的优先级处理", ItemType: "requirement", Priority: "P1", Confidence: 0.9}}
	if _, err := service.Run(context.Background(), source.ID); err != nil {
		t.Fatalf("run over the expanded conversation: %v", err)
	}
	db, _ = store.Open()
	defer db.Close()
	orphan, err := store.GetCollectionItem(db, orphanID)
	if err != nil {
		t.Fatalf("read the absorbed item: %v", err)
	}
	if !store.CollectionRetriesExhausted(orphan) {
		t.Fatalf("absorbed item still advertises an automatic retry: %+v", orphan)
	}
	items, _ = store.ListCollectionItems(db, source.ID, 10)
	if !slices.ContainsFunc(items, func(item store.CollectionItem) bool {
		return item.ID != orphanID && item.Action == "create" && item.TodoID != ""
	}) {
		t.Fatalf("expanded batch did not produce its own decision: %+v", items)
	}
}

// An on-demand analysis holds its decisions for a person, and confirming a
// proposed append has to append. Promoting it into a new Todo would produce
// exactly the duplicate the proposal was avoiding.
func TestPromoteCarriesOutAProposedAppend(t *testing.T) {
	withCollectorStore(t)
	source := addCollectorSource(t)
	if err := workapp.Default.Mutate(func(transaction *workapp.Transaction) error {
		transaction.Todos().Items = []store.Todo{{ID: "t9", Title: "排查技能命中默认 SKILL",
			Description: "批测有概率命中默认 SKILL", Priority: "P1", Status: store.TodoStatusOpen,
			Project: "atm", Created: store.Today(), Source: "test:cid-product:m1",
			Creator: store.TodoCreatorCollect}}
		return nil
	}); err != nil {
		t.Fatalf("seed Todo: %v", err)
	}
	messages := []Message{{ID: "m2", ConversationID: source.ExternalID, Sender: "测试用户",
		CreatedAt: 11_000, Content: "新结论：没在 prompt 里触发激活"}}
	db, _ := store.Open()
	if _, err := store.PutCollectionMessages(db, CollectionMessagesFor(source, messages)); err != nil {
		t.Fatalf("seed messages: %v", err)
	}
	db.Close()
	extractor := &fakeExtractor{decision: Decision{Action: "append", Title: "补充激活排查结论",
		Summary: "未在 prompt 里真正触发激活", ItemType: "investigation", Project: "atm",
		Priority: "P1", RelatedTodoID: "t9", Reason: "同一件事的新进展", Confidence: 0.9}}
	service := Service{Extractor: extractor, Now: tickingClock()}

	report, err := service.Analyze(context.Background(), source.ID, AnalyzeOptions{Local: true})
	if err != nil || report.Proposed != 1 || report.Applied != 0 {
		t.Fatalf("analyze report=%+v err=%v", report, err)
	}
	proposal := report.Items[0]
	// The target has to survive the wait: TodoID carries it, ProposedAction is what
	// still says nothing has been written.
	if proposal.ProposedAction != "append" || proposal.TodoID != "t9" || proposal.Action != "pending" {
		t.Fatalf("proposal lost its append target: %+v", proposal)
	}
	if store.TodoDocExists("t9") {
		doc, _ := store.ReadTodoDoc("t9")
		if strings.Contains(doc, "未在 prompt 里真正触发激活") {
			t.Fatalf("analysis wrote before it was confirmed:\n%s", doc)
		}
	}

	promoted, err := service.Promote(proposal.ID, ItemCorrection{})
	if err != nil {
		t.Fatalf("promote proposed append: %v", err)
	}
	if promoted.Action != "append" || promoted.TodoID != "t9" || promoted.ProposedAction != "" {
		t.Fatalf("promoted item=%+v", promoted)
	}
	todos, _ := store.LoadTodosReadOnly()
	if len(todos.Items) != 1 {
		t.Fatalf("promoting an append filed a duplicate Todo: %+v", todos.Items)
	}
	doc, err := store.ReadTodoDoc("t9")
	if err != nil || !strings.Contains(doc, "未在 prompt 里真正触发激活") {
		t.Fatalf("confirmed append did not reach the Todo: err=%v\n%s", err, doc)
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

// What the classifier is allowed to do, and which candidates it can tell apart,
// is decided entirely by this prompt. A rolling discussion filed four separate
// Todos for one topic while append was absent from it.
func TestCollectionPromptOffersAppendAndMarksThisChatsOwnTodos(t *testing.T) {
	source := store.CollectionSource{Connector: "dingtalk", ExternalID: "cid-wanda",
		Name: "Project Wanda", Project: "wanda", Priority: "P2", Strategy: store.CollectionStrategyTasks}
	batch := MessageBatch{Source: source,
		Messages:   []Message{{ID: "m9", ConversationID: "cid-wanda"}},
		RawContext: "[新消息] 2026-08-06 [先酉] 还是有概率命中默认 SKILL"}
	prompt := collectionPrompt(batch, []store.Todo{
		{ID: "t210", Title: "排查技能命中默认 SKILL", Status: store.TodoStatusOpen, Project: "wanda",
			Description: "先酉反馈批测仍有概率命中默认 SKILL", Source: "dingtalk:cid-wanda:m1"},
		{ID: "t80", Title: "别的群的任务", Status: store.TodoStatusOpen, Project: "wanda",
			Source: "dingtalk:cid-other:m4"},
		{ID: "t70", Title: "已完成的任务", Status: store.TodoStatusDone, Source: "dingtalk:cid-wanda:m0"},
	})
	if !strings.Contains(prompt, "- append:") {
		t.Fatalf("prompt does not offer append:\n%s", prompt)
	}
	// The flag is the whole point: without it the classifier cannot tell "the group
	// is still on the same bug" from "a new bug was just reported".
	if !strings.Contains(prompt, `"id":"t210"`) ||
		!strings.Contains(prompt, `"status":"open","from_this_chat":true`) {
		t.Fatalf("this conversation's own Todo is not marked:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"id":"t80"`) ||
		!strings.Contains(prompt, `"status":"open","from_this_chat":false`) {
		t.Fatalf("another conversation's Todo is marked as this chat's:\n%s", prompt)
	}
	// Sameness cannot be judged from a title alone, so the description travels too.
	if !strings.Contains(prompt, "先酉反馈批测仍有概率命中默认 SKILL") {
		t.Fatalf("candidate summary missing:\n%s", prompt)
	}
	if strings.Contains(prompt, "t70") {
		t.Fatalf("closed Todo offered as a candidate:\n%s", prompt)
	}
}

// An append whose target or payload is missing writes nothing, and the batch would
// be marked handled having recorded nothing anywhere.
func TestValidateDecisionRequiresAnAppendTargetAndPayload(t *testing.T) {
	complete := Decision{Action: "append", Title: "补充结论", Summary: "新的排查结论",
		RelatedTodoID: "t210"}
	if err := validateDecision(complete); err != nil {
		t.Fatalf("complete append rejected: %v", err)
	}
	noTarget := complete
	noTarget.RelatedTodoID = ""
	if err := validateDecision(noTarget); err == nil {
		t.Fatal("append without related_todo_id accepted")
	}
	noPayload := complete
	noPayload.Summary = " "
	if err := validateDecision(noPayload); err == nil {
		t.Fatal("append without a summary accepted")
	}
}

// An observation source may not reach the Todo list at all. Adding append reopened
// that door, and the clamp is what keeps it shut.
func TestObservationSourceClampsAppendToInsight(t *testing.T) {
	observe := store.CollectionSource{Strategy: store.CollectionStrategyObserve}
	clamped := clampToStrategy(Decision{Action: "append", Title: "补充", Summary: "内容",
		ItemType: "follow_up", RelatedTodoID: "t210"}, observe)
	if clamped.Action != "insight" || clamped.RelatedTodoID != "" {
		t.Fatalf("observation append was not clamped: %+v", clamped)
	}
	prompt := collectionPrompt(MessageBatch{Source: observe}, nil)
	if strings.Contains(prompt, "- append:") {
		t.Fatalf("observation prompt offers append:\n%s", prompt)
	}
}

// stubTextModel replaces ATM's built-in text service for one test, so
// classification and digest tests never need a credential or a network.
func stubTextModel(t *testing.T, answer func(task, prompt string) (string, error)) {
	t.Helper()
	old := runTextModel
	t.Cleanup(func() { runTextModel = old })
	runTextModel = func(_ context.Context, task string, _ time.Duration, _, prompt string) ([]byte, error) {
		text, err := answer(task, prompt)
		if err != nil {
			return nil, err
		}
		return []byte(text), nil
	}
}

func TestAutomaticExtractorAcceptsSchemaConstrainedModelDecision(t *testing.T) {
	capturedTask, capturedPrompt := "", ""
	stubTextModel(t, func(task, prompt string) (string, error) {
		capturedTask, capturedPrompt = task, prompt
		return `{"action":"create","title":"实现自动收集","summary":"从聊天创建 Todo","item_type":"requirement","project":"atm","priority":"P1","related_todo_id":"","reason":"明确需求","confidence":0.98}`, nil
	})
	batch := MessageBatch{Source: store.CollectionSource{Name: "产品群", Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 想做自动收集并添加 Todo"}
	decision, err := (AutomaticExtractor{Timeout: 5 * time.Second}).Extract(context.Background(), batch, nil)
	if err != nil || decision.Action != "create" || decision.Title != "实现自动收集" || decision.Priority != "P1" {
		t.Fatalf("model decision=%+v err=%v", decision, err)
	}
	if capturedTask != textmodel.TaskDecision || !strings.Contains(capturedPrompt, "想做自动收集") {
		t.Fatalf("task=%q prompt=%q", capturedTask, capturedPrompt)
	}
}

// Classification writes to somebody's Todo list, so an unavailable model must
// leave the batch undecided. The run then holds its checkpoint and retries the
// same messages instead of filing a guess nobody can trace.
func TestAutomaticExtractorFailsClosedWhenTheModelIsUnavailable(t *testing.T) {
	stubTextModel(t, func(string, string) (string, error) {
		return "", fmt.Errorf("built-in DeepSeek text model is unavailable")
	})
	batch := MessageBatch{Source: store.CollectionSource{Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 我想实现自动需求收集"}
	decision, err := (AutomaticExtractor{Timeout: 5 * time.Second}).Extract(context.Background(), batch, nil)
	if err == nil {
		t.Fatalf("unavailable model produced a decision: %+v", decision)
	}
	if decision.Action != "" {
		t.Fatalf("failed classification still returned an action: %+v", decision)
	}
}

// The endpoint enforces JSON, not this schema, so a loose answer reaches
// validateDecision rather than being rejected by the transport. Nothing may be
// normalized into a supported action on the way through.
func TestAutomaticExtractorRejectsAnswersOutsideTheSchema(t *testing.T) {
	batch := MessageBatch{Source: store.CollectionSource{Project: "atm", Priority: "P2"},
		RawContext: "2026-07-31 [测试发送人] 我想实现自动需求收集"}
	for name, answer := range map[string]string{
		"unsupported action":      `{"action":"assign","title":"实现自动收集","summary":"x","item_type":"requirement","project":"atm","priority":"P1","related_todo_id":"","reason":"","confidence":0.9}`,
		"append without id":       `{"action":"append","title":"实现自动收集","summary":"x","item_type":"requirement","project":"atm","priority":"P1","related_todo_id":"","reason":"","confidence":0.9}`,
		"insight without summary": `{"action":"insight","title":"实现自动收集","summary":"","item_type":"insight","project":"atm","priority":"P1","related_todo_id":"","reason":"","confidence":0.9}`,
		"not an object":           "sorry, I cannot do that",
	} {
		stubTextModel(t, func(string, string) (string, error) { return answer, nil })
		if _, err := (AutomaticExtractor{Timeout: 5 * time.Second}).Extract(context.Background(), batch, nil); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestAutomaticSummarizerAsksForADigest(t *testing.T) {
	capturedTask := ""
	stubTextModel(t, func(task, _ string) (string, error) {
		capturedTask = task
		return `{"title":"产品群 2026-07-31 动态","body":"## 发布\n- 检查变绿"}`, nil
	})
	content, err := (AutomaticSummarizer{Timeout: 5 * time.Second}).Summarize(context.Background(), DigestInput{
		Source: store.CollectionSource{Name: "产品群"}, Date: "2026-07-31",
		Items: []store.CollectionItem{{Title: "发布检查变绿", Summary: "重跑后通过"}},
	})
	if err != nil || content.Title != "产品群 2026-07-31 动态" || !strings.Contains(content.Body, "检查变绿") {
		t.Fatalf("digest content=%+v err=%v", content, err)
	}
	if capturedTask != textmodel.TaskDigest {
		t.Fatalf("task=%q", capturedTask)
	}
}

func TestAutomaticSummarizerFailsClosedWhenTheModelIsUnavailable(t *testing.T) {
	stubTextModel(t, func(string, string) (string, error) {
		return "", fmt.Errorf("built-in DeepSeek text model is unavailable")
	})
	_, err := (AutomaticSummarizer{Timeout: 5 * time.Second}).Summarize(context.Background(), DigestInput{
		Source: store.CollectionSource{Name: "产品群"}, Date: "2026-07-31",
		Items: []store.CollectionItem{{Title: "发布检查变绿", Summary: "重跑后通过"}},
	})
	if err == nil {
		t.Fatal("unavailable model produced a digest")
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
