package background

import (
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/store"
)

// CollectionCompletion is the small, in-memory handoff from the collection
// executor to the serve runtime's notification producer. It is never persisted
// as part of a background Job, so item text cannot leak through the generic job
// API. Run and item IDs give presence.Publish stable transition identities.
type CollectionCompletion struct {
	Runs  []CollectionNotificationRun
	Items []CollectionNotificationItem
}

type CollectionNotificationRun struct {
	ID              string
	SourceID        string
	Connector       string
	SourceName      string
	Status          string
	FailureKind     string
	LoginActionable bool
	Muted           bool
	Created         int
	Appended        int
	Insight         int
	Failed          int
}

type CollectionNotificationItem struct {
	ID         string
	RunID      string
	SourceID   string
	SourceName string
	Action     string
	Content    string
	Failed     bool
	UpdatedAt  int64
}

// collectionJobResult is intentionally sparse on the wire. The completion
// projection is callback-only; connector errors, login commands and collected
// content therefore do not enter background job history.
type collectionJobResult struct {
	Runs              int `json:"runs"`
	Succeeded         int `json:"succeeded"`
	Failed            int `json:"failed"`
	BlockedConnectors int `json:"blocked_connectors"`
	completion        *CollectionCompletion
}

func makeCollectionJobResult(report collector.RunReport) collectionJobResult {
	result := collectionJobResult{Runs: len(report.Runs), BlockedConnectors: len(report.Blocked)}
	for _, run := range report.Runs {
		if run.Status == "succeeded" {
			result.Succeeded++
		} else {
			result.Failed++
		}
	}
	result.completion = loadCollectionCompletion(report)
	return result
}

func loadCollectionCompletion(report collector.RunReport) *CollectionCompletion {
	completion := &CollectionCompletion{Runs: make([]CollectionNotificationRun, 0, len(report.Runs))}
	db, err := store.OpenReadOnly()
	if err != nil {
		for _, run := range report.Runs {
			completion.Runs = append(completion.Runs, notificationRun(run, store.CollectionSource{}))
		}
		return completion
	}
	defer db.Close()

	sources, sourceErr := store.ListCollectionSources(db, "", false)
	sourceByID := make(map[string]store.CollectionSource, len(sources))
	if sourceErr == nil {
		for _, source := range sources {
			sourceByID[source.ID] = source
		}
	}
	for _, run := range report.Runs {
		notification := notificationRun(run, sourceByID[run.SourceID])
		if notification.FailureKind == "auth_required" {
			// Authentication belongs to the connector. A muted source can block
			// unmuted siblings, so silence the login prompt only when every enabled
			// source that depends on this connector is muted.
			notification.Muted = connectorNotificationsMuted(run.Connector, sources)
		}
		completion.Runs = append(completion.Runs, notification)
	}

	// A due run may skip a connector whose preceding run already proved the
	// login expired. Attach that originating run ID so every skipped cycle uses
	// the same persisted deduplication key as the original outage.
	if len(report.Blocked) > 0 {
		latest, _ := store.ListLatestCollectionRunsBySource(db)
		for _, block := range report.Blocked {
			if run, ok := latestAuthRun(block.Connector, latest); ok {
				if !completionHasRun(completion.Runs, run.ID) {
					notification := notificationRun(run, sourceByID[run.SourceID])
					notification.Muted = connectorNotificationsMuted(run.Connector, sources)
					completion.Runs = append(completion.Runs, notification)
				}
			}
		}
	}

	items, itemErr := store.ListCollectionItems(db, "", 500)
	if itemErr != nil {
		return completion
	}
	seen := map[string]bool{}
	// Runs are normally returned in source order, whereas notification order is
	// chronological. Stable sorting keeps simultaneous callback retries equal.
	runs := append([]CollectionNotificationRun(nil), completion.Runs...)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	for _, run := range runs {
		if run.Muted {
			continue
		}
		original, ok := reportRun(run.ID, report.Runs)
		if !ok {
			continue
		}
		finished := original.FinishedAt
		if finished == 0 {
			finished = int64(^uint64(0) >> 1)
		}
		for _, item := range items {
			if seen[item.ID] || item.SourceID != run.SourceID || item.UpdatedAt < original.StartedAt || item.UpdatedAt > finished || !notifiableCollectionItem(item) {
				continue
			}
			seen[item.ID] = true
			completion.Items = append(completion.Items, CollectionNotificationItem{
				ID: item.ID, RunID: run.ID, SourceID: item.SourceID,
				SourceName: run.SourceName, Action: item.Action,
				Content:   collectionItemContent(item),
				Failed:    item.Status == "failed" || strings.TrimSpace(item.Error) != "",
				UpdatedAt: item.UpdatedAt,
			})
		}
	}
	sort.SliceStable(completion.Items, func(i, j int) bool {
		if completion.Items[i].UpdatedAt == completion.Items[j].UpdatedAt {
			return completion.Items[i].ID < completion.Items[j].ID
		}
		return completion.Items[i].UpdatedAt < completion.Items[j].UpdatedAt
	})
	return completion
}

func notificationRun(run store.CollectionRun, source store.CollectionSource) CollectionNotificationRun {
	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = run.Connector
	}
	failureKind := ""
	if run.Status != "succeeded" {
		failureKind = collector.CollectionFailureStatus(run.Error)
	}
	return CollectionNotificationRun{
		ID: run.ID, SourceID: run.SourceID, Connector: run.Connector,
		SourceName: name, Status: run.Status, FailureKind: failureKind,
		LoginActionable: failureKind == "auth_required" && strings.TrimSpace(collector.ConnectorLoginCommand(run.Connector)) != "",
		Muted:           source.ID != "" && source.Muted,
		Created:         run.CreatedCount, Appended: run.AppendedCount,
		Insight: run.InsightCount, Failed: run.FailedCount,
	}
}

func connectorNotificationsMuted(connector string, sources []store.CollectionSource) bool {
	found := false
	for _, source := range sources {
		if source.Connector != connector || !source.Enabled {
			continue
		}
		found = true
		if !source.Muted {
			return false
		}
	}
	return found
}

func latestAuthRun(connector string, runs []store.CollectionRun) (store.CollectionRun, bool) {
	for _, run := range runs {
		if run.Connector == connector && run.Status != "running" && collector.CollectionFailureStatus(run.Error) == "auth_required" {
			return run, true
		}
	}
	return store.CollectionRun{}, false
}

func completionHasRun(runs []CollectionNotificationRun, id string) bool {
	for _, run := range runs {
		if run.ID == id {
			return true
		}
	}
	return false
}

func reportRun(id string, runs []store.CollectionRun) (store.CollectionRun, bool) {
	for _, run := range runs {
		if run.ID == id {
			return run, true
		}
	}
	return store.CollectionRun{}, false
}

func notifiableCollectionItem(item store.CollectionItem) bool {
	if item.Status == "failed" || strings.TrimSpace(item.Error) != "" {
		return true
	}
	switch item.Action {
	case "create", "append", "insight":
		return true
	default:
		return false
	}
}

func collectionItemContent(item store.CollectionItem) string {
	for _, value := range []string{item.Title, item.Summary, item.Error, item.RawContext} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "打开收集查看详情"
}
