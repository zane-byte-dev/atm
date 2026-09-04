package background

import (
	"context"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/collector"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/quota"
	"github.com/zane-byte-dev/atm/internal/store"
	syncapp "github.com/zane-byte-dev/atm/internal/sync"
)

// DefaultExecutor calls typed application services directly. Configured
// connectors retain their own fixed JSON protocol, login backoff and timeouts.
func DefaultExecutor(dataDir string, refineOptions ...TodoRefineOptions) Executor {
	var refinement TodoRefineOptions
	if len(refineOptions) > 0 {
		refinement = refineOptions[0]
	}
	return func(ctx context.Context, call application.Call, input Request, progress func(string)) (any, error) {
		switch input.Kind {
		case TodoRefine:
			return executeTodoRefine(ctx, call, input, progress, refinement)
		case SessionSync:
			progress("同步会话索引与额度历史")
			result, err := syncapp.Default.Run(ctx, call, syncapp.RunInput{Agent: input.Agent})
			return map[string]any{"synced_files": result.SyncedFiles, "warning_count": len(result.Warnings)}, err
		case CollectionRun:
			if input.DueOnly && !config.CollectionEnabled {
				progress("自动收集已关闭，本轮跳过")
				return map[string]any{"skipped": true, "reason": "collection_disabled"}, nil
			}
			progress("读取已启用的来源并处理待收集消息")
			report, err := collector.DefaultService().RunCollection(ctx, call, collector.RunInput{SourceID: input.SourceID, DueOnly: input.DueOnly})
			// Never copy connector error text, login commands or message bodies into
			// the job feed. Source health remains on the collection workspace.
			return makeCollectionJobResult(report), err
		case CollectionReprocess:
			progress("重新处理收集条目")
			result, err := collector.DefaultService().Reprocess(ctx, call, collector.ReprocessInput{ItemID: input.ItemID})
			return map[string]any{"item_id": result.Item.ID}, err
		case DayRebuild:
			progress("生成 AI Day 统计与徽章")
			if call.Actor.Kind == application.ActorController {
				result, _, err := aiday.Default.Dashboard(ctx, aiday.DashboardInput{Days: 30, Sync: false})
				return map[string]any{"day": result.Today.Day}, err
			}
			result, _, err := aiday.Default.Rebuild(ctx, aiday.RebuildInput{From: input.From, To: input.To, Sync: false})
			return map[string]any{"from": result.From, "to": result.To, "days": result.Count}, err
		case QuotaRefresh:
			progress("刷新配置允许的额度来源")
			// Live remains the user's configuration choice, never a request flag.
			result, err := quota.Default.Snapshot(ctx, call, quota.Input{Agent: input.Agent, Live: config.GrokLiveQuota})
			if err != nil {
				return nil, err
			}
			progress("保存额度快照")
			if err := saveQuotaSnapshot(ctx, dataDir, result, input.Agent); err != nil {
				return nil, err
			}
			return map[string]any{"agents": len(result.Agents), "warning_count": len(result.Warnings)}, nil
		default:
			return nil, invalid("unsupported background job kind")
		}
	}
}

// collectionDue mirrors the native policy: global opt-in, at least one enabled
// source, min(global poll interval, fastest source), then per-source due rules.
// The collector performs the authoritative due/auth checks again after its lock.
func collectionDue(ctx context.Context, lastAttempt time.Time) (bool, error) {
	if !config.CollectionEnabled {
		return false, nil
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return false, err
	}
	defer db.Close()
	sources, err := store.ListCollectionSources(db, "", true)
	if err != nil {
		return false, err
	}
	if len(sources) == 0 {
		return false, nil
	}
	minutes := max(config.CollectionIntervalMinutes, 1)
	for _, source := range sources {
		minutes = min(minutes, max(source.IntervalMinutes, 1))
	}
	now := time.Now()
	if !lastAttempt.IsZero() && now.Sub(lastAttempt) < time.Duration(minutes)*time.Minute {
		return false, nil
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		due, err := store.CollectionSourceDue(db, source, now)
		if err != nil {
			return false, err
		}
		if due {
			return true, nil
		}
	}
	return false, nil
}

func (m *Manager) scheduler() {
	defer m.wg.Done()
	m.scheduleTick()
	ticker := time.NewTicker(m.options.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.scheduleTick()
		}
	}
}

func (m *Manager) scheduleTick() {
	if m.ctx.Err() != nil {
		return
	}
	now := m.options.Now()
	if m.lastSync.IsZero() || now.Sub(m.lastSync) >= m.options.SyncInterval {
		if m.schedule(Request{Kind: SessionSync}) {
			m.lastSync = now
		}
	}
	if m.lastDay.IsZero() || now.Sub(m.lastDay) >= m.options.DayInterval {
		if m.schedule(Request{Kind: DayRebuild}) {
			m.lastDay = now
		}
	}
	var due bool
	err := m.options.WithConfig(m.ctx, func(ctx context.Context) error {
		var err error
		due, err = m.options.CollectionDue(ctx, m.lastCollection)
		return err
	})
	if err == nil && due {
		if m.schedule(Request{Kind: CollectionRun, DueOnly: true}) {
			m.lastCollection = now
		}
	}
}

func (m *Manager) schedule(request Request) bool {
	id, err := randomID()
	if err != nil {
		return false
	}
	call := application.Call{RequestID: id, Actor: application.Actor{Kind: application.ActorController, Origin: application.OriginController}}
	_, err = m.Run(m.ctx, call, request, "auto-"+id)
	return err == nil
}
