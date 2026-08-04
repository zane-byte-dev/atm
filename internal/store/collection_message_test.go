package store

import (
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func syncedMessage(id string, at int64, sender, content string) CollectionMessage {
	return CollectionMessage{Connector: "test", ConversationID: "cid-1",
		MessageID: id, SourceID: "cs_1", ConversationName: "示例研发工作群",
		Sender: sender, CreatedAt: at, Content: content}
}

func TestPutCollectionMessagesIsIdempotentAndKeepsTheFirstSync(t *testing.T) {
	db := openTempDB(t)
	messages := []CollectionMessage{
		syncedMessage("m1", 1_000, "测试发布人", "发布完毕，观察下报错是否继续"),
		syncedMessage("m2", 2_000, "测试用户", "没cursor"),
	}
	inserted, err := PutCollectionMessages(db, messages)
	if err != nil || inserted != 2 {
		t.Fatalf("first sync inserted=%d err=%v", inserted, err)
	}
	// Re-reading a conversation must not duplicate it, and must not rewrite what
	// was already stored: a chat message never changes.
	edited := syncedMessage("m1", 1_000, "测试发布人", "被改写的内容")
	inserted, err = PutCollectionMessages(db, []CollectionMessage{edited, syncedMessage("m3", 3_000, "宜谦", "[图片]")})
	if err != nil || inserted != 1 {
		t.Fatalf("second sync inserted=%d err=%v", inserted, err)
	}
	stored, err := ListCollectionMessages(db, CollectionMessageQuery{ConversationID: "cid-1", Limit: 10})
	if err != nil || len(stored) != 3 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if stored[0].MessageID != "m1" || stored[2].MessageID != "m3" {
		t.Fatalf("messages are not oldest first: %+v", stored)
	}
	if stored[0].Content != "发布完毕，观察下报错是否继续" {
		t.Fatalf("a re-sync rewrote history: %q", stored[0].Content)
	}
	if stored[0].ConversationName != "示例研发工作群" || stored[0].SyncedAt == 0 {
		t.Fatalf("unexpected stored message: %+v", stored[0])
	}
	// Rows without an identity are skipped rather than stored unaddressable.
	if inserted, err := PutCollectionMessages(db, []CollectionMessage{{Connector: "example"}}); err != nil || inserted != 0 {
		t.Fatalf("identity-less message inserted=%d err=%v", inserted, err)
	}
}

func TestListCollectionMessagesKeepsTheNewestEndOfTheWindow(t *testing.T) {
	db := openTempDB(t)
	if _, err := PutCollectionMessages(db, []CollectionMessage{
		syncedMessage("m1", 1_000, "a", "一"),
		syncedMessage("m2", 2_000, "b", "二"),
		syncedMessage("m3", 3_000, "c", "三"),
	}); err != nil {
		t.Fatal(err)
	}
	// "The recent messages" means the newest ones, still read oldest first.
	stored, err := ListCollectionMessages(db, CollectionMessageQuery{ConversationID: "cid-1", Limit: 2})
	if err != nil || len(stored) != 2 || stored[0].MessageID != "m2" || stored[1].MessageID != "m3" {
		t.Fatalf("limited list=%+v err=%v", stored, err)
	}
	stored, err = ListCollectionMessages(db, CollectionMessageQuery{ConversationID: "cid-1", SinceTS: 2_500, Limit: 10})
	if err != nil || len(stored) != 1 || stored[0].MessageID != "m3" {
		t.Fatalf("since list=%+v err=%v", stored, err)
	}
	other, err := ListCollectionMessages(db, CollectionMessageQuery{ConversationID: "cid-other", Limit: 10})
	if err != nil || len(other) != 0 {
		t.Fatalf("other conversation list=%+v err=%v", other, err)
	}
}

func TestCollectionMessageQueriesKeepConnectorsIsolated(t *testing.T) {
	db := openTempDB(t)
	webhook := syncedMessage("shared-id", 1_000, "a", "Webhook 内容")
	slack := webhook
	slack.Connector, slack.Content = "slack", "Slack 内容"
	if _, err := PutCollectionMessages(db, []CollectionMessage{webhook, slack}); err != nil {
		t.Fatal(err)
	}
	messages, err := ListCollectionMessages(db, CollectionMessageQuery{
		Connector: "slack", ConversationID: "cid-1", Limit: 10,
	})
	if err != nil || len(messages) != 1 || messages[0].Content != "Slack 内容" {
		t.Fatalf("connector-scoped messages=%+v err=%v", messages, err)
	}
	matches, err := SearchCollectionMessages(db, "内容", CollectionMessageQuery{
		Connector: "test", ConversationID: "cid-1", Limit: 10,
	})
	if err != nil || len(matches) != 1 || matches[0].Content != "Webhook 内容" {
		t.Fatalf("connector-scoped search=%+v err=%v", matches, err)
	}
	stats, err := CollectionMessageStatsFor(db)
	if err != nil || stats.Conversations != 2 {
		t.Fatalf("connector-aware stats=%+v err=%v", stats, err)
	}
}

func TestSearchCollectionMessagesFiltersLikeSessionSearch(t *testing.T) {
	db := openTempDB(t)
	elsewhere := syncedMessage("m9", 4_000, "测试发布人", "另一个群里的发布")
	elsewhere.ConversationID, elsewhere.ConversationName = "cid-2", "另一个群"
	if _, err := PutCollectionMessages(db, []CollectionMessage{
		syncedMessage("m1", 1_000, "测试发布人", "发布完毕"),
		syncedMessage("m2", 2_000, "测试用户", "FaBu 大写也该命中"),
		syncedMessage("m3", 3_000, "测试发布人", "无关内容"),
		elsewhere,
	}); err != nil {
		t.Fatal(err)
	}
	matches, err := SearchCollectionMessages(db, "发布", CollectionMessageQuery{Limit: 10})
	if err != nil || len(matches) != 2 {
		t.Fatalf("keyword matches=%+v err=%v", matches, err)
	}
	// Newest first, and the match from another conversation is included until scoped.
	if matches[0].MessageID != "m9" || matches[1].MessageID != "m1" {
		t.Fatalf("matches are not newest first: %+v", matches)
	}
	matches, err = SearchCollectionMessages(db, "发布", CollectionMessageQuery{ConversationID: "cid-1", Limit: 10})
	if err != nil || len(matches) != 1 || matches[0].MessageID != "m1" {
		t.Fatalf("conversation-scoped matches=%+v err=%v", matches, err)
	}
	matches, err = SearchCollectionMessages(db, "fabu", CollectionMessageQuery{Limit: 10})
	if err != nil || len(matches) != 1 || matches[0].MessageID != "m2" {
		t.Fatalf("case-insensitive matches=%+v err=%v", matches, err)
	}
	matches, err = SearchCollectionMessages(db, "内容", CollectionMessageQuery{Sender: "测试发布人", Limit: 10})
	if err != nil || len(matches) != 1 || matches[0].MessageID != "m3" {
		t.Fatalf("sender-scoped matches=%+v err=%v", matches, err)
	}
	matches, err = SearchCollectionMessages(db, "发布", CollectionMessageQuery{SinceTS: 3_500, Limit: 10})
	if err != nil || len(matches) != 1 || matches[0].MessageID != "m9" {
		t.Fatalf("since-scoped matches=%+v err=%v", matches, err)
	}
}

func TestPruneCollectionMessagesHonoursRetentionWindow(t *testing.T) {
	db := openTempDB(t)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, config.Loc)
	old := now.AddDate(0, 0, -100).Unix()
	recent := now.AddDate(0, 0, -10).Unix()
	if _, err := PutCollectionMessages(db, []CollectionMessage{
		syncedMessage("m-old", old, "测试发布人", "一百天前"),
		syncedMessage("m-new", recent, "测试发布人", "十天前"),
	}); err != nil {
		t.Fatal(err)
	}
	// Zero days means keep everything, so it must not be treated as "cutoff now".
	if cutoff := RetentionCutoff(0, now); cutoff != 0 {
		t.Fatalf("retention 0 produced cutoff %d", cutoff)
	}
	if removed, err := PruneCollectionMessages(db, RetentionCutoff(0, now)); err != nil || removed != 0 {
		t.Fatalf("prune with no retention removed=%d err=%v", removed, err)
	}
	removed, err := PruneCollectionMessages(db, RetentionCutoff(90, now))
	if err != nil || removed != 1 {
		t.Fatalf("prune removed=%d err=%v", removed, err)
	}
	stats, err := CollectionMessageStatsFor(db)
	if err != nil || stats.Total != 1 || stats.Conversations != 1 || stats.Oldest != recent {
		t.Fatalf("stats after prune = %+v, err=%v", stats, err)
	}
	empty := openTempDB(t)
	if stats, err := CollectionMessageStatsFor(empty); err != nil || stats.Total != 0 || stats.Oldest != 0 {
		t.Fatalf("empty archive stats = %+v, err=%v", stats, err)
	}
}
