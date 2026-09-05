package collector

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// SnapshotInput bounds the audit rows returned with collection's current
// control-plane state. It is a stable application query, not the old `status`
// command's flag set.
type SnapshotInput struct {
	ItemLimit int `json:"item_limit,omitempty"`
}

// ConnectorHealth describes what the recent durable run ledger says about one
// connector. Error is the latest actionable connector failure, when one exists.
type ConnectorHealth struct {
	Connector           string `json:"connector"`
	Status              string `json:"status"`
	Error               string `json:"error,omitempty"`
	CheckedAt           int64  `json:"checked_at,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures,omitempty"`
	RecentRuns          int    `json:"recent_runs,omitempty"`
	RecentFailures      int    `json:"recent_failures,omitempty"`
	// LoginCommand is what this connector declared as its way back in. It travels
	// with health so the CLI line and browser banner can offer the action from
	// the same read, instead of each reaching into config for it.
	LoginCommand string `json:"login_command,omitempty"`
}

// Snapshot is the collection workspace's read model. CLI and Web adapters both
// consume this shape; neither adapter reconstructs health from storage rows.
type Snapshot struct {
	Enabled         bool                         `json:"enabled"`
	IntervalMinutes int                          `json:"interval_minutes"`
	LookbackMinutes int                          `json:"lookback_minutes"`
	RetentionDays   int                          `json:"message_retention_days"`
	Model           string                       `json:"model"`
	ConnectorHealth []ConnectorHealth            `json:"connector_health"`
	Messages        store.CollectionMessageStats `json:"messages"`
	store.CollectionOverview
}

func (service Service) Snapshot(
	ctx context.Context,
	call application.Call,
	input SnapshotInput,
) (Snapshot, error) {
	if _, err := validateSourceCall(ctx, call, false); err != nil {
		return Snapshot{}, err
	}
	limit := input.ItemLimit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 10_000 {
		return Snapshot{}, sourceInvalidArgument(
			"collection item limit must be between 1 and 10000", "item_limit", input.ItemLimit,
		)
	}

	// Snapshot is commonly the first collection read after an upgrade. Opening
	// writable lets pending schema migrations complete before any query runs.
	db, err := store.Open()
	if err != nil {
		return Snapshot{}, sourceUnavailable("load collection snapshot", err)
	}
	defer db.Close()
	if err := reconcileInterruptedRunsForSnapshot(ctx, db); err != nil {
		return Snapshot{}, sourceUnavailable("reconcile collection snapshot", err)
	}
	overview, err := store.LoadCollectionOverview(db, limit)
	if err != nil {
		return Snapshot{}, sourceUnavailable("load collection snapshot", err)
	}
	messages, err := store.CollectionMessageStatsFor(db)
	if err != nil {
		return Snapshot{}, sourceUnavailable("load collection message statistics", err)
	}
	return Snapshot{
		Enabled:            config.CollectionEnabled,
		IntervalMinutes:    config.CollectionIntervalMinutes,
		LookbackMinutes:    config.CollectionLookbackMinutes,
		RetentionDays:      config.CollectionMessageRetentionDays,
		Model:              config.TextModelName,
		ConnectorHealth:    service.connectorHealth(overview),
		Messages:           messages,
		CollectionOverview: overview,
	}, nil
}

// reconcileInterruptedRunsForSnapshot makes a crashed run converge on the next
// overview read. A live collector holds the same OS lock, in which case the
// short attempt is skipped and its running row remains visible as it should.
func reconcileInterruptedRunsForSnapshot(ctx context.Context, db *sql.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	lock, err := acquireCollectionLock(lockCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return nil
		}
		return err
	}
	defer lock.Close()
	_, err = store.ReconcileInterruptedCollectionRuns(db, time.Now().In(config.Loc).Unix())
	return err
}

func (service Service) connectorHealth(overview store.CollectionOverview) []ConnectorHealth {
	connectorIDs := map[string]bool{}
	registry := service.Connectors
	if registry == nil && service.RegistryError == nil {
		registry, _ = DefaultRegistry()
	}
	if registry != nil {
		for _, id := range registry.IDs() {
			connectorIDs[id] = true
		}
	}
	for _, source := range overview.Sources {
		connectorIDs[source.Connector] = true
	}
	for _, run := range overview.Runs {
		connectorIDs[run.Connector] = true
	}
	healthByID := map[string]ConnectorHealth{}
	streakOpen := map[string]bool{}
	for id := range connectorIDs {
		healthByID[id] = ConnectorHealth{Connector: id, Status: "not_checked"}
		streakOpen[id] = true
	}
	// Runs are newest first. The uninterrupted failure prefix answers whether a
	// connector is currently broken; the whole window records flakiness.
	for _, run := range overview.Runs {
		health, ok := healthByID[run.Connector]
		if !ok || run.Status == "running" {
			continue
		}
		health.RecentRuns++
		failed := run.Status != "succeeded"
		if failed {
			health.RecentFailures++
		}
		if health.CheckedAt == 0 {
			health.CheckedAt = run.FinishedAt
			if failed {
				health.Error = run.Error
			}
		}
		if streakOpen[run.Connector] {
			if failed {
				health.ConsecutiveFailures++
			} else {
				streakOpen[run.Connector] = false
			}
		}
		healthByID[run.Connector] = health
	}
	for id, health := range healthByID {
		if health.RecentRuns > 0 {
			health = ResolveConnectorHealth(health)
		}
		health.LoginCommand = ConnectorLoginCommand(id)
		healthByID[id] = health
	}
	ids := make([]string, 0, len(healthByID))
	for id := range healthByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]ConnectorHealth, 0, len(ids))
	for _, id := range ids {
		result = append(result, healthByID[id])
	}
	return result
}

// ConnectorHealthFor exposes the deterministic projection for adapter tests
// and alternate read models without exposing storage access.
func (service Service) ConnectorHealthFor(overview store.CollectionOverview) []ConnectorHealth {
	return service.connectorHealth(overview)
}

func ResolveConnectorHealth(health ConnectorHealth) ConnectorHealth {
	if health.ConsecutiveFailures == 0 {
		health.Status = "ready"
		health.Error = ""
		return health
	}
	classified := CollectionFailureStatus(health.Error)
	if classified != "error" {
		health.Status = classified
		return health
	}
	if health.ConsecutiveFailures == 1 && health.RecentRuns > 1 {
		health.Status = "flaky"
		return health
	}
	health.Status = "error"
	return health
}

func CollectionFailureStatus(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "not_authenticated") || strings.Contains(lower, "auth login") ||
		strings.Contains(lower, "未登录") || strings.Contains(lower, "登录失效") {
		return "auth_required"
	}
	if strings.Contains(lower, "permission") || strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "权益") || strings.Contains(lower, "权限") {
		return "permission_required"
	}
	return "error"
}
