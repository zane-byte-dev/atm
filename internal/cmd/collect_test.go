package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestCollectSourceAndStatusCommandsExposeAuditContract(t *testing.T) {
	withTempAtmDir(t)
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

func TestCollectStatusMigratesV25DatabaseBeforeReading(t *testing.T) {
	withTempAtmDir(t)
	oldJSON, oldLimit := jsonOutput, collectLimit
	t.Cleanup(func() {
		jsonOutput, collectLimit = oldJSON, oldLimit
	})

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertCollectionSource(db, store.CollectionSource{
		Connector: "test", Kind: "group", ExternalID: "cid-old-v25",
		Name: "old v25 source", Priority: "P2", Enabled: true,
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE collection_sources DROP COLUMN exclude_pattern`); err != nil {
		db.Close()
		t.Fatalf("prepare v25 collection_sources: %v", err)
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
	statusJSON := captureStdout(t, func() {
		runErr = collectStatusCmd.RunE(collectStatusCmd, nil)
	})
	if runErr != nil {
		t.Fatalf("collect status against v25 database: %v", runErr)
	}
	var status struct {
		Summary store.CollectionSummary  `json:"summary"`
		Sources []store.CollectionSource `json:"sources"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		t.Fatalf("decode status: %v\n%s", err, statusJSON)
	}
	if status.Summary.Sources != 1 || len(status.Sources) != 1 || status.Sources[0].ExternalID != "cid-old-v25" {
		t.Fatalf("unexpected migrated status: %+v", status)
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

func TestCollectionRetentionIssuesReportAStuckPrune(t *testing.T) {
	withTempAtmDir(t)
	oldRetention := config.CollectionMessageRetentionDays
	t.Cleanup(func() { config.CollectionMessageRetentionDays = oldRetention })

	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ancient := time.Now().In(config.Loc).AddDate(0, 0, -200).Unix()
	if _, err := store.PutCollectionMessages(db, []store.CollectionMessage{{
		Connector: "test", ConversationID: "cid-1", MessageID: "m1",
		CreatedAt: ancient, Content: "两百天前",
	}}); err != nil {
		t.Fatal(err)
	}
	config.CollectionMessageRetentionDays = 90
	issues := collectionRetentionIssues(db)
	if len(issues) != 1 || issues[0].Code != "collection_messages_past_retention" {
		t.Fatalf("stuck prune was not reported: %+v", issues)
	}
	// Keeping chat on purpose is not a problem to report.
	config.CollectionMessageRetentionDays = 0
	if issues := collectionRetentionIssues(db); len(issues) != 0 {
		t.Fatalf("retention 0 reported an issue: %+v", issues)
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
