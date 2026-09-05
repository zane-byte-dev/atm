package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectSourceAndStatusCommandsExposeAuditContract(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	oldJSON := jsonOutput
	oldKind, oldExternalID, oldName := collectSourceKind, collectSourceExternalID, collectSourceName
	oldProject, oldPriority, oldDisabled := collectSourceProject, collectSourcePriority, collectSourceDisabled
	oldExclude := collectSourceExclude
	oldStrategy, oldInterval := collectSourceStrategy, collectSourceInterval
	oldLimit := collectLimit
	oldConnector, oldConnectors := collectSourceConnector, config.CollectionConnectors
	t.Cleanup(func() {
		jsonOutput = oldJSON
		collectSourceKind, collectSourceExternalID, collectSourceName = oldKind, oldExternalID, oldName
		collectSourceProject, collectSourcePriority, collectSourceDisabled = oldProject, oldPriority, oldDisabled
		collectSourceExclude = oldExclude
		collectSourceStrategy, collectSourceInterval = oldStrategy, oldInterval
		collectLimit = oldLimit
		collectSourceConnector, config.CollectionConnectors = oldConnector, oldConnectors
	})

	jsonOutput = true
	collectSourceConnector = "test"
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{
		"test": {Command: "/not/invoked"},
	}
	collectSourceKind = "group"
	collectSourceExternalID = "cid-command-test"
	collectSourceName = "命令测试群"
	collectSourceProject = "atm"
	collectSourcePriority = "P1"
	collectSourceExclude = "机器人通知"
	collectSourceStrategy = store.CollectionStrategyObserve
	collectSourceInterval = 60
	collectSourceDisabled = false
	var runErr error
	addedJSON := captureStdout(t, func() {
		runErr = collectSourceAddCmd.RunE(collectSourceAddCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("source add: %v", runErr)
	}
	var source store.CollectionSource
	if err := json.Unmarshal([]byte(addedJSON), &source); err != nil {
		t.Fatalf("decode added source: %v\n%s", err, addedJSON)
	}
	if source.ID == "" || !source.Enabled || source.ExternalID != "cid-command-test" ||
		source.ExcludePattern != "机器人通知" || source.Strategy != store.CollectionStrategyObserve ||
		source.IntervalMinutes != 60 {
		t.Fatalf("unexpected source: %+v", source)
	}

	collectLimit = 20
	statusJSON := captureStdout(t, func() {
		runErr = collectStatusCmd.RunE(collectStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("collect status: %v", runErr)
	}
	var status struct {
		Enabled         bool                        `json:"enabled"`
		IntervalMinutes int                         `json:"interval_minutes"`
		Summary         store.CollectionSummary     `json:"summary"`
		Sources         []store.CollectionSource    `json:"sources"`
		Items           []store.CollectionItem      `json:"items"`
		ConnectorHealth []collectionConnectorHealth `json:"connector_health"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusJSON)
	}
	if status.IntervalMinutes < 1 || status.Summary.Sources != 1 || status.Summary.Enabled != 1 ||
		len(status.Sources) != 1 || status.Items == nil || len(status.ConnectorHealth) != 1 ||
		status.ConnectorHealth[0].Status != "not_checked" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

// Status is where someone asks what collection still owes them, so the Todos it
// filed have to be counted by whether they are still open — not by the fact that
// a record was written once.
func TestCollectStatusReportsWhetherFiledTodosAreStillOpen(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldLimit := jsonOutput, collectLimit
	t.Cleanup(func() { jsonOutput, collectLimit = oldJSON, oldLimit })
	if err := seedTodos(
		store.Todo{ID: "t1", Title: "修部署脚本", Priority: "P1", Status: store.TodoStatusDone, Created: store.Today()},
		store.Todo{ID: "t2", Title: "补额度看板", Priority: "P2", Status: store.TodoStatusOpen, Created: store.Today()},
	); err != nil {
		t.Fatalf("seed todos: %v", err)
	}

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-followups", Priority: "P2", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	for _, item := range []store.CollectionItem{
		{SourceID: source.ID, Connector: "test", Fingerprint: "done-one", MessageIDs: []string{"m1"},
			Action: "create", Title: "修部署脚本", TodoID: "t1", Status: "processed"},
		{SourceID: source.ID, Connector: "test", Fingerprint: "open-one", MessageIDs: []string{"m2"},
			Action: "create", Title: "补额度看板", TodoID: "t2", Status: "processed"},
	} {
		if _, _, err := store.PutCollectionItem(db, item); err != nil {
			db.Close()
			t.Fatalf("put item: %v", err)
		}
	}
	db.Close()

	jsonOutput, collectLimit = false, 20
	text := captureStdout(t, func() {
		if err := collectStatusCmd.RunE(collectStatusCmd, nil); err != nil {
			t.Fatalf("collect status: %v", err)
		}
	})
	if !strings.Contains(text, "Filed Todos: 2 · 1 still open") {
		t.Fatalf("status did not report filed todos:\n%s", text)
	}

	jsonOutput = true
	statusJSON := captureStdout(t, func() {
		if err := collectStatusCmd.RunE(collectStatusCmd, nil); err != nil {
			t.Fatalf("collect status --json: %v", err)
		}
	})
	var status struct {
		Summary store.CollectionSummary `json:"summary"`
		Items   []store.CollectionItem  `json:"items"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusJSON)
	}
	if status.Summary.Followups != 2 || status.Summary.FollowupsClosed != 1 {
		t.Fatalf("unexpected follow-up counts: %+v", status.Summary)
	}
	byTodo := map[string]store.CollectionItem{}
	for _, item := range status.Items {
		byTodo[item.TodoID] = item
	}
	if byTodo["t1"].TodoStatus != store.TodoStatusDone || byTodo["t2"].TodoStatus != store.TodoStatusOpen {
		t.Fatalf("items did not carry their todo status: %+v", status.Items)
	}
}

func TestCollectStatusRejectsOldSchemaBeforeReading(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldLimit := jsonOutput, collectLimit
	t.Cleanup(func() {
		jsonOutput, collectLimit = oldJSON, oldLimit
	})

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 25`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	jsonOutput, collectLimit = true, 20
	var runErr error
	statusOut := captureStdout(t, func() {
		runErr = collectStatusCmd.RunE(collectStatusCmd, nil)
	})
	foundSchemaError := false
	for cause := runErr; cause != nil; cause = errors.Unwrap(cause) {
		if strings.Contains(cause.Error(), "database schema v25 is no longer supported") {
			foundSchemaError = true
			break
		}
	}
	if runErr == nil || !foundSchemaError {
		t.Fatalf("collect status against v25 database error = %v", runErr)
	}
	if statusOut != "" {
		t.Fatalf("old schema produced partial output: %q", statusOut)
	}
}

func withCollectHistoryFlags(t *testing.T) {
	t.Helper()
	oldKind, oldSince, oldLimit := collectHistoryKind, collectHistorySince, collectHistoryLimit
	oldLocal := collectHistoryLocal
	t.Cleanup(func() {
		collectHistoryKind, collectHistorySince, collectHistoryLimit = oldKind, oldSince, oldLimit
		collectHistoryLocal = oldLocal
	})
	collectHistoryKind, collectHistorySince, collectHistoryLimit, collectHistoryLocal = "all", "", 50, false
}

func TestCollectHistoryEnforcesRetentionEvenOnALocalRead(t *testing.T) {
	withTempAtmDir(t)
	withCollectHistoryFlags(t)
	oldRetention := config.CollectionMessageRetentionDays
	t.Cleanup(func() { config.CollectionMessageRetentionDays = oldRetention })
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(config.Loc)
	source, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "channel", ExternalID: "conversation-1",
		Name: "release", Priority: "P2", Enabled: true,
	})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := store.PutCollectionMessages(db, []store.CollectionMessage{
		{Connector: "test", ConversationID: "conversation-1", MessageID: "old",
			CreatedAt: now.AddDate(0, 0, -100).Unix(), Content: "一百天前"},
		{Connector: "test", ConversationID: "conversation-1", MessageID: "new",
			CreatedAt: now.AddDate(0, 0, -1).Unix(), Content: "昨天"},
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	// --local asks not to touch the network, not to skip housekeeping — and the
	// doctor suggestion tells people to read a conversation to trigger a prune.
	config.CollectionMessageRetentionDays, collectHistoryLocal, jsonOutput = 90, true, true
	captureStdout(t, func() {
		if err := collectHistoryCmd.RunE(collectHistoryCmd, []string{source.ID}); err != nil {
			t.Fatalf("local history read: %v", err)
		}
	})
	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stats, err := store.CollectionMessageStatsFor(db)
	if err != nil || stats.Total != 1 {
		t.Fatalf("retention was not enforced: %+v, err=%v", stats, err)
	}
}

func TestCollectStatusReportsTheSyncedArchive(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldLimit, oldRetention := jsonOutput, collectLimit, config.CollectionMessageRetentionDays
	t.Cleanup(func() {
		jsonOutput, collectLimit = oldJSON, oldLimit
		config.CollectionMessageRetentionDays = oldRetention
	})

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PutCollectionMessages(db, []store.CollectionMessage{{
		Connector: "test", ConversationID: "cid-1", MessageID: "m1",
		ConversationName: "示例研发工作群", Sender: "测试发布人", CreatedAt: 1_785_417_000, Content: "发布完毕",
	}}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	jsonOutput, collectLimit, config.CollectionMessageRetentionDays = true, 20, 30
	statusJSON := captureStdout(t, func() {
		if err := collectStatusCmd.RunE(collectStatusCmd, nil); err != nil {
			t.Fatalf("collect status: %v", err)
		}
	})
	var status struct {
		RetentionDays int                          `json:"message_retention_days"`
		Messages      store.CollectionMessageStats `json:"messages"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusJSON)
	}
	if status.RetentionDays != 30 || status.Messages.Total != 1 || status.Messages.Conversations != 1 ||
		status.Messages.Oldest != 1_785_417_000 {
		t.Fatalf("unexpected archive status: %+v", status)
	}
}

func TestCollectionFailureStatusDistinguishesLoginAndPermission(t *testing.T) {
	if got := collectionFailureStatus("not_authenticated; run connector auth login"); got != "auth_required" {
		t.Fatalf("auth status = %q", got)
	}
	if got := collectionFailureStatus("当前账号没有消息搜索权益"); got != "permission_required" {
		t.Fatalf("permission status = %q", got)
	}
	if got := collectionFailureStatus("connector timed out"); got != "error" {
		t.Fatalf("generic status = %q", got)
	}
}

// run builds one audit row. Runs are consumed newest-first, which is how the
// overview supplies them.
// The whole loop through the real command: a connector whose login expired
// attempts once, says what it skipped, and on the next background round attempts
// nothing at all. Before this, one outage wrote five identical failure rows every
// five minutes for as long as it lasted.
func TestBackgroundRunStopsAttemptingAConnectorWhoseLoginExpired(t *testing.T) {
	withTempAtmDir(t)
	withHumanCollectionCLI(t)
	connector := filepath.Join(t.TempDir(), "fake-connector")
	script := "#!/bin/sh\ncat >/dev/null\n" +
		`echo '{"error":"dws returned an error: 未登录，请先执行 dws auth login"}'` + "\n"
	if err := os.WriteFile(connector, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldConnectors, oldDue, oldJSON := config.CollectionConnectors, collectRunDue, jsonOutput
	config.CollectionConnectors = map[string]config.CollectionConnectorConfig{
		"fake": {Command: connector, LoginCommand: "/opt/fake/bin auth login"},
	}
	collectRunDue, jsonOutput = true, false
	t.Cleanup(func() {
		config.CollectionConnectors, collectRunDue, jsonOutput = oldConnectors, oldDue, oldJSON
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	for _, external := range []string{"c1", "c2"} {
		if _, err := store.UpsertCollectionSource(db, store.CollectionSource{
			Connector: "fake", Kind: "group", ExternalID: external,
			Name: external, Priority: "P2", Enabled: true,
		}); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()

	first := captureStdout(t, func() {
		if err := collectRunCmd.RunE(collectRunCmd, nil); err == nil {
			t.Fatalf("the source that was attempted failed and must be reported")
		}
	})
	if !strings.Contains(first, "跳过 1 个来源") || !strings.Contains(first, "重新登录：/opt/fake/bin auth login") {
		t.Fatalf("first round output = %q", first)
	}

	second := captureStdout(t, func() {
		if err := collectRunCmd.RunE(collectRunCmd, nil); err != nil {
			t.Fatalf("a skipped round is not a failure: %v", err)
		}
	})
	if !strings.Contains(second, "跳过 2 个来源") || !strings.Contains(second, "后再探测") {
		t.Fatalf("second round output = %q", second)
	}
	if strings.Contains(second, "No enabled collection sources") {
		t.Fatalf("skipping must not read as nothing to do: %q", second)
	}

	db, err = store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM collection_runs").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("run rows = %d, want the single attempt", rows)
	}
}

// The two statuses ATM stops retrying are the two whose line has to carry the
// thing that ends them.
func TestTheLineForAStuckLoginNamesHowToFixIt(t *testing.T) {
	health := collectionConnectorHealth{
		Connector: "dingtalk", Status: "auth_required", Error: "未登录",
		ConsecutiveFailures: 1, LoginCommand: "/Users/x/bin/dws auth login",
	}
	line := collectionHealthLine(health)
	if !strings.Contains(line, "重新登录：/Users/x/bin/dws auth login") {
		t.Fatalf("line = %q", line)
	}
	// A flaky connector is retrying on its own; offering a login there would be
	// advice for a problem it does not have.
	health.Status = "flaky"
	health.RecentRuns, health.RecentFailures = 20, 1
	if line := collectionHealthLine(health); strings.Contains(line, "重新登录") {
		t.Fatalf("flaky line = %q", line)
	}
}

// Sources that were deliberately left alone write no run row, so silence here
// used to read as "nothing was due" — the opposite of what happened.
func TestTheBlockedLineSaysWhatWasSkippedAndWhenItResumes(t *testing.T) {
	retryAt := time.Date(2026, 8, 25, 15, 39, 0, 0, config.Loc)
	line := collectionBlockedLine(collector.BlockedConnector{
		Connector: "dingtalk", Status: "auth_required", Error: "未登录",
		SkippedSources: 5, RetryAt: retryAt.Unix(), LoginCommand: "~/bin/dws auth login",
	})
	for _, want := range []string{"登录失效", "跳过 5 个来源", "15:39 后再探测", "重新登录：~/bin/dws auth login"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line = %q, want %q in it", line, want)
		}
	}
}

// A missing permission has been per-source in practice — one group this account
// cannot read — so it must not hold the connector's healthy siblings back, and it
// must not be answered with advice to log in again.
func TestAPermissionFailureNeitherBlocksTheConnectorNorAsksForALogin(t *testing.T) {
	health := collectionConnectorHealth{
		Connector: "dingtalk", Status: "permission_required", Error: "Permission denied",
		ConsecutiveFailures: 1, LoginCommand: "/Users/x/bin/dws auth login",
	}
	if line := collectionHealthLine(health); strings.Contains(line, "重新登录") {
		t.Fatalf("line = %q", line)
	}
}

func healthRun(connector, status, errorText string, finishedAt int64) store.CollectionRun {
	return store.CollectionRun{
		Connector: connector, Status: status, Error: errorText, FinishedAt: finishedAt,
	}
}

// The whole point of judging by a streak: these connectors return the occasional
// business error, and one of those between successes is noise that fixes itself at
// the next interval. Reporting it as `error` made a working connector look broken,
// and the workspace showed a card you had to dismiss by hand.
func TestOneFailureBetweenSuccessesIsFlakyNotBroken(t *testing.T) {
	overview := store.CollectionOverview{Runs: []store.CollectionRun{
		healthRun("dingtalk", "failed", "business error: success=false", 300),
		healthRun("dingtalk", "succeeded", "", 200),
		healthRun("dingtalk", "succeeded", "", 100),
	}}
	health := collectionHealth(overview)
	if len(health) != 1 {
		t.Fatalf("health = %+v", health)
	}
	got := health[0]
	if got.Status != "flaky" {
		t.Fatalf("status = %q, want flaky", got.Status)
	}
	if got.ConsecutiveFailures != 1 || got.RecentRuns != 3 || got.RecentFailures != 1 {
		t.Fatalf("counts = %+v", got)
	}
	// The rate is what tells a human whether to care; the latest message alone does not.
	line := collectionHealthLine(got)
	if !strings.Contains(line, "最近 3 次里失败 1 次") {
		t.Fatalf("line = %q", line)
	}
}

func TestASucceedingConnectorIsReadyAndCarriesNoStaleError(t *testing.T) {
	overview := store.CollectionOverview{Runs: []store.CollectionRun{
		healthRun("dingtalk", "succeeded", "", 300),
		healthRun("dingtalk", "failed", "business error", 200),
	}}
	got := collectionHealth(overview)[0]
	if got.Status != "ready" {
		t.Fatalf("status = %q", got.Status)
	}
	// A stale message beside "ready" is what made one hiccup read as a breakage.
	if got.Error != "" {
		t.Fatalf("error = %q, want empty once it is working again", got.Error)
	}
	if line := collectionHealthLine(got); !strings.Contains(line, "已恢复") {
		t.Fatalf("line = %q, want the recovery noted rather than hidden", line)
	}
}

func TestRepeatedFailuresAreReportedAsBroken(t *testing.T) {
	overview := store.CollectionOverview{Runs: []store.CollectionRun{
		healthRun("dingtalk", "failed", "business error", 400),
		healthRun("dingtalk", "failed", "business error", 300),
		healthRun("dingtalk", "succeeded", "", 200),
	}}
	got := collectionHealth(overview)[0]
	if got.Status != "error" {
		t.Fatalf("status = %q, want error once it stops recovering", got.Status)
	}
	if got.ConsecutiveFailures != 2 {
		t.Fatalf("streak = %d", got.ConsecutiveFailures)
	}
	if line := collectionHealthLine(got); !strings.Contains(line, "连续失败 2 次") {
		t.Fatalf("line = %q", line)
	}
}

// A login that expired will not fix itself, so waiting for a second sample only
// delays telling the user by one interval.
func TestAClassifiedFailureIsReportedOnTheFirstOccurrence(t *testing.T) {
	for _, test := range []struct {
		message string
		want    string
	}{
		{"dws: not_authenticated, run auth login", "auth_required"},
		{"forbidden: 没有权限", "permission_required"},
	} {
		overview := store.CollectionOverview{Runs: []store.CollectionRun{
			healthRun("dingtalk", "failed", test.message, 300),
			healthRun("dingtalk", "succeeded", "", 200),
		}}
		got := collectionHealth(overview)[0]
		if got.Status != test.want {
			t.Errorf("%q → %q, want %q", test.message, got.Status, test.want)
		}
	}
}

// A single failure with nothing behind it has no recovery to point at, so it is
// not called flaky — there is no evidence it recovers.
func TestTheVeryFirstRunFailingIsNotCalledFlaky(t *testing.T) {
	overview := store.CollectionOverview{Runs: []store.CollectionRun{
		healthRun("dingtalk", "failed", "business error", 100),
	}}
	got := collectionHealth(overview)[0]
	if got.Status != "error" {
		t.Fatalf("status = %q, want error", got.Status)
	}
}

// One source hiccupping must not condemn a connector that other sources are using
// successfully, and a running attempt is not evidence either way.
func TestRunningAttemptsAreIgnoredAndConnectorsStaySeparate(t *testing.T) {
	overview := store.CollectionOverview{Runs: []store.CollectionRun{
		healthRun("dingtalk", "running", "", 0),
		healthRun("dingtalk", "succeeded", "", 300),
		healthRun("slack", "failed", "business error", 290),
		healthRun("slack", "failed", "business error", 280),
	}}
	byConnector := map[string]collectionConnectorHealth{}
	for _, health := range collectionHealth(overview) {
		byConnector[health.Connector] = health
	}
	if byConnector["dingtalk"].Status != "ready" {
		t.Fatalf("dingtalk = %+v", byConnector["dingtalk"])
	}
	if byConnector["slack"].Status != "error" {
		t.Fatalf("slack = %+v", byConnector["slack"])
	}
}
