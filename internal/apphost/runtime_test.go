package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/presence"
	"github.com/zane-byte-dev/atm/internal/quota"
	"github.com/zane-byte-dev/atm/internal/store"
)

func attachFixturePresence(t *testing.T, h *Host) *presence.Runtime {
	t.Helper()
	socketDir, err := os.MkdirTemp("", "atm-host-hooks-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	live, err := presence.Start(presence.Options{DataDir: h.dataDir, SocketPath: filepath.Join(socketDir, "notch.sock"), InstanceID: "host-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.AttachRuntime(nil, nil)
		if err := live.Close(); err != nil {
			t.Error(err)
		}
	})
	h.AttachRuntime(nil, live)
	return live
}

func attachFixtureJobs(t *testing.T, h *Host, execute background.Executor) *background.Manager {
	t.Helper()
	jobs, err := background.New(background.Options{DataDir: h.dataDir, Execute: execute, WithConfig: h.WithConfig})
	if err != nil {
		t.Fatal(err)
	}
	if err = jobs.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := jobs.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	_, live := h.attachedRuntime()
	h.AttachRuntime(jobs, live)
	return jobs
}

func TestRuntimeAPIsRejectUnknownFieldsAndDisabledExecution(t *testing.T) {
	h := testHost(t)
	for _, test := range []struct{ method, body string }{
		{"jobs.run", `{"kind":"session.sync","due_only":true}`},
		{"jobs.run", `{"kind":"session.sync","command":"anything"}`},
		{"jobs.run", `{"kind":"session.sync","actor":"human"}`},
		{"jobs.list", `{"limit":101}`},
		{"jobs.show", `{"job_id":"../../private"}`},
		{"jobs.cancel", `{"job_id":""}`},
		{"presence.snapshot", `{"path":"/tmp"}`},
	} {
		if _, err := h.CallRuntime(context.Background(), webCall(), test.method, json.RawMessage(test.body), "fixture"); !errors.Is(err, application.ErrInvalidArgument) {
			t.Errorf("%s %s: %v", test.method, test.body, err)
		}
	}
	if _, err := h.CallRuntime(context.Background(), webCall(), "jobs.run", json.RawMessage(`{"kind":"session.sync"}`), "fixture"); !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("disabled execution: %v", err)
	}
	if _, err := os.Stat(h.databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected request created database: %v", err)
	}
	for _, body := range []string{`{"client_id":"client"}`, `{"client_id":"client","notifications_enabled":true,"command":"ignored"}`} {
		if _, err := h.Companion(context.Background(), json.RawMessage(body), false); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("invalid native payload: %v", err)
		}
	}
}

func TestRuntimeJobsExposeDirectJobAndDurableList(t *testing.T) {
	h := testHost(t)
	started := make(chan struct{}, 1)
	jobs := attachFixtureJobs(t, h, func(ctx context.Context, _ application.Call, _ background.Request, _ func(string)) (any, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	value, err := h.CallRuntime(context.Background(), webCall(), "jobs.run", json.RawMessage(`{"kind":"session.sync"}`), "host-job")
	if err != nil {
		t.Fatal(err)
	}
	job, ok := value.(background.Job)
	if !ok || job.ID == "" {
		t.Fatalf("expected direct Job: %#v", value)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}
	value, err = h.CallRuntime(context.Background(), webCall(), "jobs.list", nil, "")
	if err != nil || len(value.(RuntimeJobList).Jobs) != 1 {
		t.Fatalf("jobs list: %#v %v", value, err)
	}
	if !h.RuntimeCapabilities()["runtime_jobs"] || !h.RuntimeCapabilities()["models"] || h.RuntimeCapabilities()["agent_hooks"] {
		t.Fatal("capabilities misstate attached services")
	}
	if got := h.workspaceRuntime(); got.Mode != "go" || !got.BackgroundSync || !got.Collection || !got.Models || got.AgentHooks {
		t.Fatalf("runtime truth: %+v", got)
	}
	body, _ := json.Marshal(RuntimeJobID{JobID: job.ID})
	value, err = h.CallRuntime(context.Background(), webCall(), "jobs.cancel", body, "")
	if err != nil || !value.(background.Job).CancelRequested {
		t.Fatalf("cancel: %#v %v", value, err)
	}
	if _, err = jobs.Get(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
}

func TestConfigWritersFailFastWithoutBlockingOtherReads(t *testing.T) {
	h := testHost(t)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- h.WithConfig(context.Background(), func(context.Context) error { close(entered); <-release; return nil })
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	checks := []func() error{
		func() error {
			_, err := h.SaveWorkspacePreferences(context.Background(), webCall(), WorkspacePreferencesInput{OwnerName: "fixture"})
			return err
		},
		func() error {
			_, err := h.SaveWorkspaceBusiness(context.Background(), webCall(), WorkspaceBusinessInput{})
			return err
		},
		func() error { _, err := h.saveWorkspaceCredential(context.Background(), webCall(), nil); return err },
	}
	for i, check := range checks {
		result := make(chan error, 1)
		go func() { result <- check() }()
		select {
		case err := <-result:
			if !errors.Is(err, application.ErrBusy) {
				t.Fatalf("writer %d: %v", i, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("writer %d queued behind job", i)
		}
	}
	collectionWrite := make(chan error, 1)
	go func() {
		_, err := h.callCollection(context.Background(), webCall(), "collect.item.read", json.RawMessage(`{"item_id":"ci_0000000000000000","read":true}`))
		collectionWrite <- err
	}()
	select {
	case err := <-collectionWrite:
		if errors.Is(err, application.ErrBusy) {
			t.Fatalf("collection write incorrectly shared the config writer gate: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("collection write queued behind config-bound background work")
	}
	settingsRead := make(chan error, 1)
	go func() { _, err := h.WorkspaceSettings(context.Background(), webCall()); settingsRead <- err }()
	select {
	case err := <-settingsRead:
		if err != nil {
			t.Fatalf("settings snapshot blocked by background work: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("settings snapshot queued behind background work")
	}
	read := make(chan error, 1)
	go func() { _, err := h.ListTodos(context.Background(), webCall(), ListInput{}); read <- err }()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("busy settings writer blocked unrelated page read")
	}
}

func TestSettingsReadUsesPinnedSnapshotDuringBackgroundWork(t *testing.T) {
	h := testHost(t)
	if err := os.WriteFile(config.ConfigPath, []byte(`{"owner_name":"Before"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := h.WorkspaceSettings(context.Background(), webCall())
	if err != nil || before.OwnerName != "Before" {
		t.Fatalf("initial settings: %+v, %v", before, err)
	}

	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- h.WithConfig(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	if err := os.WriteFile(config.ConfigPath, []byte(`{"owner_name":"After"}`), 0o600); err != nil {
		close(release)
		t.Fatal(err)
	}
	during, err := h.WorkspaceSettings(context.Background(), webCall())
	if err != nil || during.OwnerName != "Before" || during.Revision != before.Revision {
		close(release)
		t.Fatalf("background snapshot mixed generations: %+v, %v", during, err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	after, err := h.WorkspaceSettings(context.Background(), webCall())
	if err != nil || after.OwnerName != "After" || after.Revision == before.Revision {
		t.Fatalf("settings did not refresh after background work: %+v, %v", after, err)
	}
}

func TestPresenceRefreshAndNativeContract(t *testing.T) {
	h := testHost(t)
	live := attachFixturePresence(t, h)
	called := false
	h.SetPresenceLoader(func(ctx context.Context, agent string) (dashboard.LiveStatus, error) {
		called = true
		if h.gate.TryLock() {
			h.gate.Unlock()
			t.Error("presence loader ran without config gate")
		}
		return dashboard.LiveStatus{Sessions: []dashboard.LiveSession{{Tool: "Codex", SessionID: "short", ResumeID: "full-thread", ActivityState: "active"}}}, nil
	})
	if err := h.RefreshPresence(context.Background()); err != nil || !called {
		t.Fatalf("refresh: %v", err)
	}
	claim := json.RawMessage(`{"client_id":"native","after":0,"notifications_enabled":true}`)
	value, err := h.Companion(context.Background(), claim, false)
	if err != nil {
		t.Fatal(err)
	}
	initial := value.(CompanionResult)
	if initial.Snapshot.ActiveCount != 1 || len(initial.Feed.Notifications) != 0 || initial.Feed.LeaseUntil == nil {
		t.Fatalf("native initial view: %+v", initial)
	}
	event := agentevent.Envelope{Version: 1, Source: "codex", SessionID: "full-thread", Event: agentevent.KindAttention, Reason: "permission_prompt", At: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := live.Apply(event); err != nil {
		t.Fatal(err)
	}
	value, err = h.Companion(context.Background(), claim, false)
	if err != nil {
		t.Fatal(err)
	}
	attention := value.(CompanionResult)
	if attention.Snapshot.AttentionCount != 1 || len(attention.Feed.Notifications) != 1 || len(attention.AttentionNotificationIDs) != 1 {
		t.Fatalf("native attention contract: %+v", attention)
	}
	encoded, _ := json.Marshal(attention)
	var shape map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &shape)
	for _, key := range []string{"snapshot", "feed", "attention_notification_ids", "todos", "quota", "today_usage"} {
		if shape[key] == nil {
			t.Fatalf("missing native field %s", key)
		}
	}
	if shape["quick"] != nil || shape["guard_notification_ids"] != nil {
		t.Fatalf("removed quick-panel fields remain in native payload: %s", encoded)
	}
	if initial.Todos.Items == nil || initial.Quota.Windows == nil {
		t.Fatal("native summary arrays must encode as []")
	}
	if _, err := h.Companion(context.Background(), json.RawMessage(`{"client_id":"native","sequence":1}`), true); err != nil {
		t.Fatal(err)
	}
	value, err = h.Companion(context.Background(), json.RawMessage(`{"client_id":"native","after":0,"notifications_enabled":false}`), false)
	if err != nil || len(value.(CompanionResult).Feed.Notifications) != 1 || value.(CompanionResult).Feed.LeaseUntil != nil {
		t.Fatalf("disabled client lost in-app feed or retained display lease: %#v %v", value, err)
	}
	result, err := h.callActivity(context.Background(), webCall(), "session.status", nil)
	if err != nil || result.(SessionStatus).Presence == nil || !result.(SessionStatus).AgentHooks {
		t.Fatalf("activity did not expose live overlay: %#v %v", result, err)
	}
}

func TestCompanionTodoProjectionPreservesMenuBucketsWithoutDuplicates(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	longTitle := strings.Repeat("任务 ", 100)
	result := projectCompanionTodos([]store.Todo{
		{ID: "t1", Title: "lower review", Status: store.TodoStatusReview, Priority: "P2", Created: "2026-09-01"},
		{ID: "t2", Title: "due", Status: store.TodoStatusInProgress, Priority: "P1", Created: "2026-09-04", ReviewAt: "2026-09-04"},
		{ID: "t3", Title: longTitle, Status: store.TodoStatusReview, Priority: "P0", Created: "2026-08-01"},
		{ID: "t4", Title: "waiting", Status: store.TodoStatusInProgress, Priority: "P0", Created: "2026-09-03", ReviewAt: "2026-09-10", WakeCondition: "build completes"},
		{ID: "t5", Title: "working p0", Status: store.TodoStatusInProgress, Priority: "P0", Created: "2026-09-04"},
		{ID: "t6", Title: "working", Status: store.TodoStatusInProgress, Priority: "P1", Created: "2026-09-04"},
		{ID: "t7", Title: "not a current menu task", Status: store.TodoStatusOpen, Priority: "P0", Created: "2026-09-04"},
		{ID: "t8", Title: "closed", Status: store.TodoStatusDone, Priority: "P0", Created: "2026-09-04"},
	}, now)
	if result.Total != 6 || !result.Truncated || len(result.Items) != companionTodoLimit {
		t.Fatalf("bounds = %+v", result)
	}
	want := []string{"t3", "t1", "t2", "t4", "t5"}
	seen := map[string]bool{}
	for index, item := range result.Items {
		if item.ID != want[index] {
			t.Fatalf("rank[%d]=%s want %s: %+v", index, item.ID, want[index], result.Items)
		}
		if seen[item.ID] {
			t.Fatalf("duplicate projected task %s", item.ID)
		}
		seen[item.ID] = true
	}
	if result.Items[2].MenuState != "due" || result.Items[2].ReviewAt != "2026-09-04" || result.Items[3].MenuState != "waiting" || result.Items[4].MenuState != "working" {
		t.Fatalf("presentation state lost: %+v", result.Items)
	}
	if len([]rune(result.Items[0].Title)) != 160 || strings.Contains(result.Items[0].Title, "\n") {
		t.Fatalf("title is not bounded: %q", result.Items[0].Title)
	}
}

func TestCompanionQuotaProjectionIsFiniteBoundedAndNewestFirst(t *testing.T) {
	windows := []CachedQuotaWindow{
		{Agent: "bad-zero", WindowMinutes: 0, UsedPercent: 50, ObservedAt: "9999"},
		{Agent: "bad-nan", WindowMinutes: 60, UsedPercent: math.NaN(), ObservedAt: "9998"},
		{Agent: "newest", WindowMinutes: 300, UsedPercent: 125, ResetsAt: -1, ObservedAt: "2026-09-04T12:00:00Z"},
	}
	for index := range 12 {
		windows = append(windows, CachedQuotaWindow{Agent: "agent-" + string(rune('a'+index)), WindowMinutes: 60, UsedPercent: float64(index), ObservedAt: "2026-09-03T12:00:00Z"})
	}
	result := projectCompanionQuota(CachedQuota{Source: "runtime_quota_cache", GeneratedAt: "2026-09-04T12:01:00Z", Windows: windows})
	if !result.Truncated || len(result.Windows) != companionQuotaLimit {
		t.Fatalf("quota bounds = %+v", result)
	}
	first := result.Windows[0]
	if first.Agent != "newest" || first.UsedPercent != 100 || first.RemainingPercent != 0 || first.ResetsAt != 0 {
		t.Fatalf("quota normalization = %+v", first)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 32*1024 {
		t.Fatalf("bounded quota serialization: bytes=%d err=%v", len(encoded), err)
	}
}

func TestCompanionQuotaCacheProjectsProductsCardsAndTrendSafely(t *testing.T) {
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	cache := background.QuotaCache{
		UpdatedAt: now.Format(time.RFC3339),
		Snapshot: quota.Snapshot{
			Agents: map[string]*quota.AgentQuota{
				"codex": {
					Plan: "pro", Source: "cache",
					Primary:  &quota.Window{WindowMinutes: 300, UsedPercent: 40, ResetsAt: now.Add(time.Hour).Unix(), Trend: &store.QuotaTrend{PercentPerHour: 2, Samples: 3, SpanMinutes: 60, FromPercent: 38, ToPercent: 40, FullAt: now.Add(30 * time.Hour).Unix()}},
					Products: []quota.Product{{Name: "work", UsedPercent: 27}, {Name: "bad", UsedPercent: math.NaN()}},
					ProviderCards: []quota.ProviderCard{{
						ID: "credits", Agent: "codex", Provider: "fixture", Title: "Credits", URL: "file:///secret",
						Metrics: []quota.ProviderMetric{{ID: "remaining", Label: "Remaining", Used: 2, Limit: 10, UsedPercent: 20}, {ID: "bad", Label: "Bad", Used: math.Inf(1), Limit: 1, UsedPercent: 1}},
					}},
				},
			},
			Order: []string{"codex"},
		},
	}
	result := projectCompanionQuota(runtimeCachedQuota(cache, now, ""))
	projectCompanionQuotaCache(&result, cache, now)
	if len(result.Windows) != 1 || result.Windows[0].Plan != "pro" || result.Windows[0].Trend == nil || result.Windows[0].Trend.FullAt == "" {
		t.Fatalf("window cache projection: %+v", result.Windows)
	}
	if result.ProductsTotal != 1 || len(result.Products) != 1 || result.ProviderCardsTotal != 1 || len(result.ProviderCards) != 1 || len(result.ProviderCards[0].Metrics) != 1 || result.ProviderCards[0].URL != "" {
		t.Fatalf("provider cache was not filtered/bounded: %+v", result)
	}
}

func TestCachedQuotaMergeSelectsLatestObservationPerWindow(t *testing.T) {
	history := CachedQuota{Source: "quota_history", GeneratedAt: "2026-09-04T12:00:00Z", Windows: []CachedQuotaWindow{
		{Agent: "codex", WindowMinutes: 300, UsedPercent: 40, ObservedAt: "2026-09-04T12:00:00Z"},
	}}
	runtime := CachedQuota{Source: "runtime_quota_cache", GeneratedAt: "2026-09-04T13:00:00Z", Windows: []CachedQuotaWindow{
		{Agent: "codex", WindowMinutes: 300, UsedPercent: 20, ObservedAt: "2026-09-04T11:00:00Z"},
		{Agent: "grok", WindowMinutes: 120, UsedPercent: 60, ObservedAt: "2026-09-04T13:00:00Z"},
	}}
	merged := mergeCachedQuota(history, runtime)
	if len(merged.Windows) != 2 || merged.Windows[0].Agent != "codex" || merged.Windows[0].UsedPercent != 40 || merged.Windows[1].Agent != "grok" || merged.Windows[1].UsedPercent != 60 {
		t.Fatalf("latest merge = %+v", merged)
	}
	if merged.Source != runtime.Source || merged.GeneratedAt != runtime.GeneratedAt {
		t.Fatalf("merged source = %q at %q", merged.Source, merged.GeneratedAt)
	}
}

func TestCompanionSnapshotIsBoundedAndOmitsSessionPaths(t *testing.T) {
	h := testHost(t)
	live := attachFixturePresence(t, h)
	sessions := make([]presence.Session, 0, presence.MaxSessions)
	for index := 0; index < presence.MaxSessions; index++ {
		sessions = append(sessions, presence.Session{ID: "session-" + strings.Repeat("x", 480) + strconv.Itoa(index), Source: "codex", State: "active", CWD: "/private/secret/" + strings.Repeat("z", 4000)})
	}
	live.Merge(sessions)
	value, err := h.Companion(context.Background(), json.RawMessage(`{"client_id":"bounded","after":0,"notifications_enabled":false}`), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(CompanionResult)
	if len(result.Snapshot.Sessions) != 0 || result.Snapshot.ActiveCount != presence.MaxSessions {
		t.Fatalf("unbounded companion presence: %+v", result.Snapshot)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) >= 2*1024*1024 || strings.Contains(string(encoded), "/private/secret") {
		t.Fatalf("companion response boundary: bytes=%d err=%v", len(encoded), err)
	}
}

func TestTodayUsageCacheCanBeInvalidatedAfterRuntimeSync(t *testing.T) {
	h := testHost(t)
	want := CompanionTodayUsage{Sessions: 3, Queries: 2, TotalTokens: 42}
	h.quickUsageAt, h.quickUsageCache = time.Now(), want
	if err := os.Mkdir(config.AtmDB, 0700); err != nil {
		t.Fatal(err)
	}
	if got := h.companionTodayUsage(context.Background(), time.Now()); got.TotalTokens != 42 || got.Sessions != 3 || got.Queries != 2 || got.Error != "" {
		t.Fatalf("short cache was not used: %+v", got)
	}
	h.InvalidateQuickUsage()
	if got := h.companionTodayUsage(context.Background(), time.Now()); got.Error != "用量暂不可用" {
		t.Fatalf("sync invalidation did not force a fresh read: %+v", got)
	}
}

func TestQuotaReadersUseRuntimeCacheWhenHistoryIsUnreadable(t *testing.T) {
	h := testHost(t)
	now := time.Now().UTC().Truncate(time.Second)
	cache := background.QuotaCache{UpdatedAt: now.Format(time.RFC3339), Snapshot: quota.Snapshot{Agents: map[string]*quota.AgentQuota{"codex": {Primary: &quota.Window{UsedPercent: 73, WindowMinutes: 300, ResetsAt: now.Add(time.Hour).Unix()}}}}}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.dataDir, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dataDir, "runtime", "quota.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(h.databasePath, 0700); err != nil {
		t.Fatal(err)
	}
	browser, err := h.callActivity(context.Background(), webCall(), "quota.cached", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cached := browser.(CachedQuota); cached.Source != "runtime_quota_cache" || len(cached.Windows) != 1 || cached.Windows[0].UsedPercent != 73 {
		t.Fatalf("browser lost runtime cache with unreadable history: %+v", cached)
	}
	attachFixturePresence(t, h)
	value, err := h.Companion(context.Background(), json.RawMessage(`{"client_id":"cache-fallback","after":0,"notifications_enabled":false}`), false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(CompanionResult)
	if len(result.Quota.Windows) != 1 || result.Quota.Windows[0].UsedPercent != 73 || result.Quota.Error != "" {
		t.Fatalf("runtime cache was lost with unreadable index: %+v", result.Quota)
	}
	if result.TodayUsage.Error == "" || result.Feed.Notifications == nil {
		t.Fatalf("today usage failure isolation lost: %+v", result)
	}
}

func TestCompanionLockedSectionsDoNotDelayNotificationLease(t *testing.T) {
	h := testHost(t)
	seed(t, store.Todo{ID: "t1", Title: "working", Status: store.TodoStatusInProgress, Priority: "P1", Created: "2026-09-04"})
	live := attachFixturePresence(t, h)
	live.Merge([]presence.Session{{ID: "locked", ResumeID: "locked", Source: "codex", State: "active"}})
	claim := json.RawMessage(`{"client_id":"lock-reader","after":0,"notifications_enabled":true}`)
	if _, err := h.Companion(context.Background(), claim, false); err != nil {
		t.Fatal(err)
	}
	if err := live.Apply(agentevent.Envelope{Version: 1, Source: "codex", SessionID: "locked", Event: agentevent.KindAttention, Reason: "permission_prompt", At: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}

	writer, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		t.Fatal(err)
	}
	connection, err := writer.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer connection.ExecContext(context.Background(), "ROLLBACK")
	h.InvalidateQuickUsage()

	ctx, cancel := context.WithTimeout(context.Background(), 4500*time.Millisecond)
	defer cancel()
	started := time.Now()
	value, err := h.Companion(ctx, claim, false)
	if err != nil {
		t.Fatalf("locked companion read failed instead of isolating sections: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 4400*time.Millisecond {
		t.Fatalf("locked optional sections consumed native timeout: %s", elapsed)
	}
	result := value.(CompanionResult)
	if len(result.Feed.Notifications) != 1 || result.Feed.LeaseUntil == nil {
		t.Fatalf("notification lease/feed lost behind locked index: %+v", result.Feed)
	}
	if result.Todos.Error == "" || result.Quota.Error == "" || result.TodayUsage.Error == "" {
		t.Fatalf("locked optional sections were not isolated: %+v", result)
	}
}

func TestCompanionSummaryFailureDoesNotDropNotificationFeed(t *testing.T) {
	h := testHost(t)
	live := attachFixturePresence(t, h)
	if err := os.Mkdir(config.AtmDB, 0700); err != nil {
		t.Fatal(err)
	}
	live.Merge([]presence.Session{{ID: "summary-failure", ResumeID: "summary-failure", Source: "codex", State: "active"}})
	claim := json.RawMessage(`{"client_id":"native-failure","after":0,"notifications_enabled":true}`)
	if _, err := h.Companion(context.Background(), claim, false); err != nil {
		t.Fatal(err)
	}
	if err := live.Apply(agentevent.Envelope{Version: 1, Source: "codex", SessionID: "summary-failure", Event: agentevent.KindAttention, Reason: "permission_prompt", At: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	value, err := h.Companion(context.Background(), claim, false)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(CompanionResult)
	if result.Todos.Error != "任务暂不可用" || result.Quota.Error != "额度暂不可用" || result.TodayUsage.Error != "用量暂不可用" || len(result.Feed.Notifications) != 1 {
		t.Fatalf("independent companion sections = %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), config.AtmDB) {
		t.Fatalf("companion leaked internal read failure: %s, %v", encoded, err)
	}
}

func TestRuntimeQuotaCacheIsReadWithoutProviderExecution(t *testing.T) {
	h := testHost(t)
	now := time.Now().UTC().Truncate(time.Second)
	cache := background.QuotaCache{UpdatedAt: now.Format(time.RFC3339), Snapshot: quota.Snapshot{Agents: map[string]*quota.AgentQuota{"codex": {Primary: &quota.Window{UsedPercent: 37, WindowMinutes: 300, ResetsAt: now.Add(time.Hour).Unix()}}}}}
	data, _ := json.Marshal(cache)
	if err := os.MkdirAll(filepath.Join(config.AtmDir, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.AtmDir, "runtime", "quota.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := h.callActivity(context.Background(), webCall(), "quota.cached", nil)
	if err != nil {
		t.Fatal(err)
	}
	read := result.(CachedQuota)
	if read.Source != "runtime_quota_cache" || len(read.Windows) != 1 || read.Windows[0].UsedPercent != 37 || read.Windows[0].Stale {
		t.Fatalf("quota cache mapping: %+v", read)
	}
	if _, err := os.Stat(h.databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cache read materialized index")
	}
	attachFixturePresence(t, h)
	companion, err := h.Companion(context.Background(), json.RawMessage(`{"client_id":"quota-reader","after":0,"notifications_enabled":false}`), false)
	if err != nil {
		t.Fatal(err)
	}
	menuQuota := companion.(CompanionResult).Quota
	if menuQuota.Source != "runtime_quota_cache" || len(menuQuota.Windows) != 1 || menuQuota.Windows[0].UsedPercent != 37 || menuQuota.Windows[0].RemainingPercent != 63 {
		t.Fatalf("companion did not reuse cached quota: %+v", menuQuota)
	}
	if _, err := os.Stat(h.databasePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("companion cache read materialized index")
	}
	if err := os.WriteFile(path, []byte("invalid cache"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err = h.callActivity(context.Background(), webCall(), "quota.cached", nil)
	if err != nil || result.(CachedQuota).Source != "quota_history" {
		t.Fatalf("bad cache must retain safe historical fallback: %#v %v", result, err)
	}
}

func preserveConfigValue[T any](t *testing.T, target *T) {
	t.Helper()
	previous := *target
	t.Cleanup(func() { *target = previous })
}

func preserveHostBusinessConfig(t *testing.T) {
	t.Helper()
	preserveConfigValue(t, &config.Loc)
	preserveConfigValue(t, &config.Pricing)
	preserveConfigValue(t, &config.Subscriptions)
	preserveConfigValue(t, &config.ProjectAliases)
	preserveConfigValue(t, &config.OwnerName)
	preserveConfigValue(t, &config.GrokLiveQuota)
	preserveConfigValue(t, &config.CollectionEnabled)
	preserveConfigValue(t, &config.CollectionIntervalMinutes)
	preserveConfigValue(t, &config.CollectionLookbackMinutes)
	preserveConfigValue(t, &config.CollectionMessageRetentionDays)
	preserveConfigValue(t, &config.CollectionDigestCollection)
	preserveConfigValue(t, &config.CollectionDigestIntervalMinutes)
	preserveConfigValue(t, &config.CollectionConnectors)
	preserveConfigValue(t, &config.QuotaProviders)
	preserveConfigValue(t, &config.Guard)
	preserveConfigValue(t, &config.TextModelBaseURL)
	preserveConfigValue(t, &config.TextModelName)
	preserveConfigValue(t, &config.TextModelSource)
	preserveConfigValue(t, &config.TodoRefinePrompt)
	preserveConfigValue(t, &config.TodoRefineOnAdd)
}

func TestExplicitDataDirectoryResetsAllBusinessDefaults(t *testing.T) {
	_ = testHost(t)
	preserveHostBusinessConfig(t)
	t.Setenv("ATM_COLLECTION_ENABLED", "")
	t.Setenv("ATM_GROK_LIVE_QUOTA", "")
	t.Setenv("ATM_TODO_REFINE_ON_ADD", "")
	config.TextModelBaseURL, config.TextModelName, config.TextModelSource = "https://fixture.invalid", "private-model", "private-source"
	config.TodoRefinePrompt, config.TodoRefineOnAdd = "private policy", true
	config.CollectionEnabled, config.GrokLiveQuota = true, true
	config.CollectionIntervalMinutes, config.CollectionLookbackMinutes, config.CollectionMessageRetentionDays = 900, 901, 902
	config.CollectionDigestCollection, config.CollectionDigestIntervalMinutes = "private-collection", 903
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{"private": {Command: "do-not-run"}}
	config.QuotaProviders = map[string]config.QuotaProviderConfig{"private": {Command: "do-not-run"}}
	config.OwnerName = "private owner"
	home, codex, claude := config.Home, config.CodexSessions, config.ClaudeProjects
	if err := ConfigureDataDir(config.AtmDir); err != nil {
		t.Fatal(err)
	}
	if config.TextModelBaseURL != "https://api.deepseek.com" || config.TextModelName != "deepseek-v4-flash" || config.TextModelSource != "deepseek" || config.TodoRefinePrompt != config.DefaultTodoRefinePrompt || config.TodoRefineOnAdd {
		t.Fatal("isolated directory inherited model/refine policy")
	}
	if config.CollectionEnabled || config.GrokLiveQuota || config.CollectionIntervalMinutes != 5 || config.CollectionLookbackMinutes != 60 || config.CollectionMessageRetentionDays != 90 || config.CollectionDigestCollection != "inbox" || config.CollectionDigestIntervalMinutes != 60 || len(config.CollectionConnectors) != 0 || len(config.QuotaProviders) != 0 || config.OwnerName != "" {
		t.Fatal("isolated directory inherited collection/provider policy")
	}
	if config.Home != home || config.CodexSessions != codex || config.ClaudeProjects != claude {
		t.Fatal("data-directory isolation rewrote Agent sources")
	}
}

func TestRefreshConfigSkipsBusyAndAppliesExternalDeletion(t *testing.T) {
	h := testHost(t)
	preserveHostBusinessConfig(t)
	t.Setenv("ATM_COLLECTION_ENABLED", "")
	t.Setenv("ATM_TODO_REFINE_ON_ADD", "")
	config.CollectionEnabled = true
	config.TextModelName = "old-custom-model"
	config.TodoRefineOnAdd = true
	foreign := t.TempDir()
	data, _ := json.Marshal(map[string]any{"owner_name": "fresh-owner", "data_dir": foreign})
	if err := os.WriteFile(h.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	h.gate.RLock()
	if err := h.RefreshConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !config.CollectionEnabled || config.TextModelName != "old-custom-model" {
		t.Fatal("busy tick mutated live configuration")
	}
	h.gate.RUnlock()
	if err := h.RefreshConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if config.CollectionEnabled || config.TodoRefineOnAdd || config.TextModelName != "deepseek-v4-flash" || config.OwnerName != "fresh-owner" {
		t.Fatal("external deletion did not restore business defaults")
	}
	if config.AtmDir != h.dataDir || config.AtmDB != h.databasePath || config.ConfigPath != h.configPath {
		t.Fatal("external config redirected authenticated runtime")
	}
	if err := os.WriteFile(h.configPath, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.RefreshConfig(context.Background()); err == nil {
		t.Fatal("invalid external configuration not reported")
	}
	if err := h.RefreshConfig(context.Background()); err != nil {
		t.Fatal("unchanged external error repeated")
	}
	if config.OwnerName != "fresh-owner" {
		t.Fatal("invalid config replaced last usable values")
	}
	if err := os.Remove(h.configPath); err != nil {
		t.Fatal(err)
	}
	if err := h.RefreshConfig(context.Background()); err != nil {
		t.Fatal(err)
	}
	if config.OwnerName != "" {
		t.Fatal("removed config retained last owner's preferences")
	}
}
