package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/presence"
)

func collectionJob(status string, completion *background.CollectionCompletion) background.Job {
	return background.Job{ID: "job-collection", Kind: background.CollectionRun, Status: status, Collection: completion}
}

func TestCollectionRuntimeNotificationsPreserveLegacyResultAndMuteSemantics(t *testing.T) {
	job := collectionJob("succeeded", &background.CollectionCompletion{
		Items: []background.CollectionNotificationItem{
			{ID: "ci-one", RunID: "cr-one", SourceName: "产品反馈群", Action: "create", Content: "修复登录页", UpdatedAt: 101},
		},
		Runs: []background.CollectionNotificationRun{
			{ID: "cr-one", Connector: "chat", SourceName: "产品反馈群", Status: "succeeded", Created: 1},
			{ID: "cr-muted", Connector: "chat", SourceName: "静音群", Status: "succeeded", Created: 8, Muted: true},
		},
	})
	notes := collectionRuntimeNotifications(job)
	if len(notes) != 1 {
		t.Fatalf("notifications=%+v, want the one concrete unmuted result", notes)
	}
	note := notes[0]
	if note.Kind != "collection_new" || note.ID != "collection-item-ci-one" || note.ObjectID != "ci-one" || note.Subtitle != "产品反馈群 · 新任务" || note.Body != "修复登录页" {
		t.Fatalf("wrong result notification: %+v", note)
	}

	fallback := collectionRuntimeNotifications(collectionJob("succeeded", &background.CollectionCompletion{Runs: []background.CollectionNotificationRun{
		{ID: "cr-old", Status: "succeeded", Created: 2, Appended: 1, Insight: 3},
		{ID: "cr-old-muted", Status: "succeeded", Created: 20, Muted: true},
	}}))
	if len(fallback) != 1 || fallback[0].Body != "新增 2 · 补充 1 · 结论 3" {
		t.Fatalf("fallback=%+v", fallback)
	}
}

func TestCollectionLoginReplacesGenericFailureAndUsesOutageIdentity(t *testing.T) {
	job := collectionJob("failed", &background.CollectionCompletion{Runs: []background.CollectionNotificationRun{
		{ID: "cr-auth", Connector: "dingtalk", SourceName: "产品群", Status: "failed", FailureKind: "auth_required", LoginActionable: true, Failed: 1},
		{ID: "cr-generic", Connector: "chat", SourceName: "需求群", Status: "failed", FailureKind: "error", Failed: 1},
		{ID: "cr-muted", Connector: "chat", SourceName: "静音群", Status: "failed", FailureKind: "error", Failed: 1, Muted: true},
		{ID: "cr-muted-auth", Connector: "muted-chat", SourceName: "静音登录", Status: "failed", FailureKind: "auth_required", LoginActionable: true, Failed: 1, Muted: true},
	}})
	notes := collectionRuntimeNotifications(job)
	if len(notes) != 2 {
		t.Fatalf("notifications=%+v, want one login and one generic failure", notes)
	}
	byKind := map[string]presence.Notification{}
	for _, note := range notes {
		byKind[note.Kind] = note
	}
	login := byKind["collection_login"]
	if login.ID != "collection-auth-dingtalk" || login.DedupKey != "collection:auth:dingtalk:cr-auth" || login.Subtitle != "dingtalk 需要重新登录" {
		t.Fatalf("wrong login notification: %+v", login)
	}
	failed := byKind["collection_failed"]
	if failed.ID != "collection-run-cr-generic" || failed.Subtitle != "需求群 · 收集失败" {
		t.Fatalf("wrong failure notification: %+v", failed)
	}
	for _, note := range notes {
		if stringsContain(note.ID, "cr-auth") && note.Kind == "collection_failed" {
			t.Fatalf("auth outage also emitted generic failure: %+v", note)
		}
	}

	// Without a declared login action, the legacy app leaves the problem in the
	// collection workspace and does not replace it with an inert system banner.
	quiet := collectionRuntimeNotifications(collectionJob("failed", &background.CollectionCompletion{Runs: []background.CollectionNotificationRun{
		{ID: "cr-auth-no-action", Connector: "chat", Status: "failed", FailureKind: "auth_required"},
	}}))
	if len(quiet) != 0 {
		t.Fatalf("non-actionable login emitted notifications: %+v", quiet)
	}
}

func TestCollectionOperationFailureBeforeAnyRunStillNotifies(t *testing.T) {
	notes := collectionRuntimeNotifications(collectionJob("failed", &background.CollectionCompletion{}))
	if len(notes) != 1 || notes[0].Kind != "collection_failed" || notes[0].DedupKey != "collection:job:job-collection:failed" {
		t.Fatalf("notifications=%+v", notes)
	}
	for _, status := range []string{"canceled", "interrupted"} {
		if notes := collectionRuntimeNotifications(collectionJob(status, &background.CollectionCompletion{})); len(notes) != 0 {
			t.Fatalf("%s emitted %+v", status, notes)
		}
	}
}

func TestCollectionRuntimePublicationDeduplicatesAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	socketDir, err := os.MkdirTemp("", "atm-cn-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	opts := presence.Options{DataDir: dir, SocketPath: filepath.Join(socketDir, "s"), InstanceID: "collection-notify-test"}
	runtime, err := presence.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	job := collectionJob("succeeded", &background.CollectionCompletion{Runs: []background.CollectionNotificationRun{
		{ID: "cr-one", Status: "succeeded", Created: 1},
	}})
	publishCollectionRuntimeNotifications(runtime, job)
	if feed := runtime.Notifications(0); feed.Cursor != 1 || len(feed.Notifications) != 1 {
		t.Fatalf("first publication=%+v", feed)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := presence.Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	publishCollectionRuntimeNotifications(restarted, job)
	if feed := restarted.Notifications(0); feed.Cursor != 1 || len(feed.Notifications) != 0 {
		t.Fatalf("restart replayed notification: %+v", feed)
	}
}

func stringsContain(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
