package apphost

import (
	"context"
	"errors"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/presence"
	"github.com/zane-byte-dev/atm/internal/store"
)

const companionTodayUsageTTL = time.Minute

// CompanionTodayUsage is the only usage projection needed by the ordinary
// menu. It deliberately excludes sessions, messages, model rows and costs.
type CompanionTodayUsage struct {
	TotalTokens int64  `json:"total_tokens"`
	Sessions    int    `json:"sessions"`
	Queries     int    `json:"queries"`
	Error       string `json:"error,omitempty"`
}

// companionTodayUsage keeps usage failure independent from notification
// delivery. The config gate only pins the local-day boundary and never covers
// a provider refresh or write.
func (h *Host) companionTodayUsage(ctx context.Context, now time.Time) CompanionTodayUsage {
	result := CompanionTodayUsage{}
	err := h.WithConfig(ctx, func(ctx context.Context) error {
		result = h.readCompanionTodayUsage(ctx, now)
		return nil
	})
	if err != nil {
		result.Error = "用量暂不可用"
	}
	return result
}

func (h *Host) readCompanionTodayUsage(ctx context.Context, now time.Time) CompanionTodayUsage {
	h.quickUsageMu.Lock()
	defer h.quickUsageMu.Unlock()
	if !h.quickUsageAt.IsZero() && now.Sub(h.quickUsageAt) >= 0 && now.Sub(h.quickUsageAt) < companionTodayUsageTTL {
		return h.quickUsageCache
	}
	result := CompanionTodayUsage{}
	db, err := store.OpenQuickReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result
	}
	if err != nil {
		result.Error = "用量暂不可用"
		return result
	}
	defer db.Close()
	if err := store.BoundReadWait(ctx, db, 200*time.Millisecond); err != nil {
		result.Error = "用量暂不可用"
		return result
	}
	startTS, endTS := config.RangeToday.UnixBounds(now)
	read, err := store.ReadQuickUsageSummary(ctx, db, startTS, endTS, "")
	if err != nil {
		result.Error = "用量暂不可用"
		return result
	}
	result.TotalTokens = max(read.InputTokens+read.OutputTokens, 0)
	result.Sessions = max(read.Sessions, 0)
	result.Queries = max(read.Queries, 0)
	h.quickUsageAt, h.quickUsageCache = now, result
	return result
}

// InvalidateQuickUsage makes a completed sync visible on the next menu poll.
func (h *Host) InvalidateQuickUsage() {
	h.quickUsageMu.Lock()
	h.quickUsageAt = time.Time{}
	h.quickUsageCache = CompanionTodayUsage{}
	h.quickUsageMu.Unlock()
}

func compactCompanionSnapshot(value presence.Snapshot) presence.Snapshot {
	// Session details include local paths and belong to Web's authenticated
	// activity API. The native control surface only needs aggregate counts.
	value.Sessions = []presence.Session{}
	return value
}
