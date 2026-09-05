package cmd

import (
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/presence"
)

// collectionRuntimeNotifications translates the executor's private completion
// projection into durable presence transitions. A scheduled no-op stays quiet;
// authentication failures replace generic failures with an actionable result.
func collectionRuntimeNotifications(job background.Job) []presence.Notification {
	if job.Kind != background.CollectionRun || !job.Terminal() || job.Collection == nil {
		return nil
	}
	if job.Status == "canceled" || job.Status == "interrupted" {
		return nil
	}

	completion := job.Collection
	notifications := make([]presence.Notification, 0, len(completion.Items)+len(completion.Runs)+1)
	failedItemsByRun := map[string]bool{}
	normalItems := 0
	for _, item := range completion.Items {
		kind, label := "collection_new", collectionActionLabel(item.Action)
		if item.Failed {
			kind, label = "collection_failed", "收集失败"
			failedItemsByRun[item.RunID] = true
		} else {
			normalItems++
		}
		notifications = append(notifications, presence.Notification{
			ID: "collection-item-" + item.ID, Kind: kind, Action: "post",
			Title: "ATM · 收集", Subtitle: item.SourceName + " · " + label,
			Body: item.Content, ObjectID: item.ID,
			DedupKey: fmt.Sprintf("collection:item:%s:%s:%d:%s:%t", item.RunID, item.ID, item.UpdatedAt, item.Action, item.Failed),
		})
	}

	created, appended, insights := 0, 0, 0
	hadAuthFailure, hadGenericFailure := false, false
	publishedLogin := map[string]bool{}
	for _, run := range completion.Runs {
		if !run.Muted {
			created += run.Created
			appended += run.Appended
			insights += run.Insight
		}
		if run.Muted {
			continue
		}
		if run.FailureKind == "auth_required" {
			hadAuthFailure = true
			if run.LoginActionable && !publishedLogin[run.Connector] {
				publishedLogin[run.Connector] = true
				notifications = append(notifications, presence.Notification{
					ID: "collection-auth-" + run.Connector, Kind: "collection_login", Action: "post",
					Title: "ATM · 收集", Subtitle: run.Connector + " 需要重新登录",
					Body: "收集已暂停，重新登录后继续。", ObjectID: run.Connector,
					DedupKey: "collection:auth:" + run.Connector + ":" + run.ID,
				})
			}
			continue
		}
		if run.Status == "succeeded" || run.Muted {
			continue
		}
		hadGenericFailure = true
		if failedItemsByRun[run.ID] {
			continue
		}
		source := strings.TrimSpace(run.SourceName)
		if source == "" {
			source = run.Connector
		}
		notifications = append(notifications, presence.Notification{
			ID: "collection-run-" + run.ID, Kind: "collection_failed", Action: "post",
			Title: "ATM · 收集", Subtitle: source + " · 收集失败",
			Body:     "这次收集没有完成，请打开收集查看详情。",
			DedupKey: "collection:run:" + run.ID + ":failed",
		})
	}

	// Older or very large ledgers may not leave a matching item inside the
	// bounded callback projection. Preserve the legacy count summary as a
	// fallback, without duplicating the concrete item notifications.
	if normalItems == 0 && created+appended+insights > 0 {
		notifications = append(notifications, presence.Notification{
			ID: "collection-job-" + job.ID, Kind: "collection_new", Action: "post",
			Title: "ATM · 收集", Subtitle: "有新的收集待查看",
			Body:     fmt.Sprintf("新增 %d · 补充 %d · 结论 %d", created, appended, insights),
			DedupKey: "collection:job:" + job.ID + ":results",
		})
	}
	// An operation can fail before a connector writes a run row (opening the
	// ledger, loading the registry, and similar failures). Auth failures stay
	// quiet here because their actionable login banner, when available, is the
	// single notification for that outage.
	if job.Status == "failed" && !hadAuthFailure && !hadGenericFailure && len(completion.Runs) == 0 {
		notifications = append(notifications, presence.Notification{
			ID: "collection-job-" + job.ID, Kind: "collection_failed", Action: "post",
			Title: "ATM · 收集", Subtitle: "收集失败",
			Body:     "这次收集没有完成，请打开收集查看详情。",
			DedupKey: "collection:job:" + job.ID + ":failed",
		})
	}
	return notifications
}

func publishCollectionRuntimeNotifications(runtime *presence.Runtime, job background.Job) {
	if runtime == nil {
		return
	}
	for _, notification := range collectionRuntimeNotifications(job) {
		_, _ = runtime.Publish(notification)
	}
}

func collectionActionLabel(action string) string {
	switch action {
	case "create":
		return "新任务"
	case "append":
		return "任务补充"
	case "insight":
		return "新结论"
	default:
		return "新收集"
	}
}
