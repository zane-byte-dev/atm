package apphost

import (
	"context"
	"errors"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

const (
	companionTodoLimit           = 5
	companionQuotaLimit          = 12
	companionLinkLimit           = 4
	companionProductLimit        = 32
	companionProviderCardLimit   = 24
	companionProviderMetricLimit = 8
	companionSectionReadTimeout  = 650 * time.Millisecond
)

type CompanionTodoLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// CompanionTodo is the bounded task projection needed by the menu companion.
// It excludes descriptions, documents and bindings. Links are restricted to
// ordinary browser URLs and ETag is the service-owned optimistic revision.
type CompanionTodo struct {
	ID            string              `json:"id"`
	Title         string              `json:"title"`
	Status        string              `json:"status"`
	Priority      string              `json:"priority"`
	Project       string              `json:"project"`
	ReviewAt      string              `json:"review_at,omitempty"`
	WakeCondition string              `json:"wake_condition,omitempty"`
	MenuState     string              `json:"menu_state"`
	Links         []CompanionTodoLink `json:"links,omitempty"`
	ETag          string              `json:"etag,omitempty"`
}

type CompanionTodos struct {
	Items     []CompanionTodo `json:"items"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated"`
	Error     string          `json:"error,omitempty"`
}

type CompanionQuotaWindow struct {
	Agent            string               `json:"agent"`
	WindowMinutes    int                  `json:"window_minutes"`
	UsedPercent      float64              `json:"used_percent"`
	RemainingPercent float64              `json:"remaining_percent"`
	ResetsAt         int64                `json:"resets_at"`
	ObservedAt       string               `json:"observed_at"`
	Stale            bool                 `json:"stale"`
	ResetElapsed     bool                 `json:"reset_elapsed"`
	Source           string               `json:"source,omitempty"`
	Plan             string               `json:"plan,omitempty"`
	Trend            *CompanionQuotaTrend `json:"trend,omitempty"`
}

type CompanionQuotaTrend struct {
	PercentPerHour  float64 `json:"percent_per_hour"`
	Samples         int     `json:"samples"`
	SpanMinutes     int     `json:"span_minutes"`
	FromPercent     float64 `json:"from_percent"`
	ToPercent       float64 `json:"to_percent"`
	FullAt          string  `json:"full_at,omitempty"`
	FullBeforeReset bool    `json:"full_before_reset"`
}

type CompanionQuotaProduct struct {
	Agent       string  `json:"agent"`
	Product     string  `json:"product"`
	UsedPercent float64 `json:"used_percent"`
}

type CompanionQuotaMetric struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	UsedPercent float64 `json:"used_percent"`
	Unit        string  `json:"unit,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	Precision   int     `json:"precision,omitempty"`
}

type CompanionProviderQuotaCard struct {
	ID                string                 `json:"id"`
	Agent             string                 `json:"agent"`
	Provider          string                 `json:"provider"`
	Title             string                 `json:"title"`
	Period            string                 `json:"period,omitempty"`
	ObservedAt        string                 `json:"observed_at,omitempty"`
	Source            string                 `json:"source,omitempty"`
	URL               string                 `json:"url,omitempty"`
	Unavailable       bool                   `json:"unavailable"`
	UnavailableReason string                 `json:"unavailable_reason,omitempty"`
	Metrics           []CompanionQuotaMetric `json:"metrics"`
}

type CompanionQuota struct {
	Source             string                       `json:"source"`
	GeneratedAt        string                       `json:"generated_at"`
	Windows            []CompanionQuotaWindow       `json:"windows"`
	Truncated          bool                         `json:"truncated"`
	Error              string                       `json:"error,omitempty"`
	Products           []CompanionQuotaProduct      `json:"products,omitempty"`
	ProviderCards      []CompanionProviderQuotaCard `json:"provider_cards,omitempty"`
	ProductsTotal      int                          `json:"products_total,omitempty"`
	ProviderCardsTotal int                          `json:"provider_cards_total,omitempty"`
}

// companionSummaries isolates failures by section. Presence and notification
// delivery stay available when the index is being migrated or briefly busy.
func (h *Host) companionSummaries(ctx context.Context) (CompanionTodos, CompanionQuota, error) {
	todos := CompanionTodos{Items: []CompanionTodo{}}
	quota := CompanionQuota{Windows: []CompanionQuotaWindow{}}
	err := h.WithConfig(ctx, func(ctx context.Context) error {
		now := time.Now()
		taskCtx, cancelTasks := context.WithTimeout(ctx, companionSectionReadTimeout)
		db, openErr := store.OpenQuickReadOnly()
		var readErr error
		if openErr == nil {
			waitErr := store.BoundReadWait(taskCtx, db, 200*time.Millisecond)
			menu, menuErr := store.QuickTodoSnapshot{}, waitErr
			if waitErr == nil {
				menu, menuErr = store.ReadQuickTodos(taskCtx, db, now.In(config.Loc).Format("2006-01-02"), companionTodoLimit)
			}
			_ = db.Close()
			readErr = menuErr
			if menuErr == nil {
				todos = projectStoredCompanionTodos(menu)
			}
		} else {
			readErr = openErr
		}
		cancelTasks()
		if readErr != nil && !errors.Is(readErr, store.ErrDatabaseMissing) {
			todos.Error = "任务暂不可用"
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		quotaCtx, cancelQuota := context.WithTimeout(ctx, companionSectionReadTimeout)
		cached, cache, readErr := h.readCachedQuotaSources(quotaCtx, now.UTC(), "", true)
		cancelQuota()
		if readErr == nil {
			quota = projectCompanionQuota(cached)
		} else {
			quota.Error = "额度暂不可用"
		}
		if cache != nil {
			projectCompanionQuotaCache(&quota, *cache, now.UTC())
		}
		return ctx.Err()
	})
	if err != nil {
		return todos, quota, err
	}
	return todos, quota, ctx.Err()
}

type rankedCompanionTodo struct {
	todo      store.Todo
	menuState string
	rank      int
}

func projectStoredCompanionTodos(value store.QuickTodoSnapshot) CompanionTodos {
	rows := make([]store.QuickTodoRow, 0, len(value.NeedsAction)+len(value.Working)+len(value.Waiting))
	rows = append(rows, value.NeedsAction...)
	rows = append(rows, value.Working...)
	rows = append(rows, value.Waiting...)
	ranked := make([]rankedCompanionTodo, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if seen[row.Todo.ID] {
			continue
		}
		seen[row.Todo.ID] = true
		rank := 3
		switch row.MenuState {
		case "review":
			rank = 0
		case "due":
			rank = 1
		}
		ranked = append(ranked, rankedCompanionTodo{todo: row.Todo, menuState: row.MenuState, rank: rank})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank != ranked[j].rank {
			return ranked[i].rank < ranked[j].rank
		}
		left, right := companionPriorityRank(ranked[i].todo.Priority), companionPriorityRank(ranked[j].todo.Priority)
		if left != right {
			return left < right
		}
		if ranked[i].todo.Created != ranked[j].todo.Created {
			return ranked[i].todo.Created < ranked[j].todo.Created
		}
		return ranked[i].todo.ID < ranked[j].todo.ID
	})
	result := projectCompanionTodosRanked(ranked)
	result.Total = value.Summary.Review + value.Summary.Working
	result.Truncated = result.Total > len(result.Items)
	return result
}

func projectCompanionTodos(values []store.Todo, now time.Time) CompanionTodos {
	return projectCompanionTodosRanked(rankCompanionTodos(values, now))
}

func rankCompanionTodos(values []store.Todo, now time.Time) []rankedCompanionTodo {
	today := now.Format("2006-01-02")
	ranked := make([]rankedCompanionTodo, 0, len(values))
	for _, todo := range values {
		item := rankedCompanionTodo{todo: todo}
		switch todo.Status {
		case store.TodoStatusReview:
			item.rank, item.menuState = 0, "review"
		case store.TodoStatusInProgress:
			if todo.ReviewAt != "" && todo.ReviewAt <= today {
				item.rank, item.menuState = 1, "due"
			} else if strings.TrimSpace(todo.WakeCondition) != "" || todo.ReviewAt != "" {
				item.rank, item.menuState = 2, "waiting"
			} else {
				item.rank, item.menuState = 2, "working"
			}
		default:
			continue
		}
		ranked = append(ranked, item)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].rank != ranked[j].rank {
			return ranked[i].rank < ranked[j].rank
		}
		left, right := companionPriorityRank(ranked[i].todo.Priority), companionPriorityRank(ranked[j].todo.Priority)
		if left != right {
			return left < right
		}
		if ranked[i].todo.Created != ranked[j].todo.Created {
			return ranked[i].todo.Created < ranked[j].todo.Created
		}
		return ranked[i].todo.ID < ranked[j].todo.ID
	})
	return ranked
}

func projectCompanionTodosRanked(ranked []rankedCompanionTodo) CompanionTodos {
	result := CompanionTodos{Items: []CompanionTodo{}, Total: len(ranked), Truncated: len(ranked) > companionTodoLimit}
	for _, rankedTodo := range ranked[:min(len(ranked), companionTodoLimit)] {
		result.Items = append(result.Items, projectCompanionTodo(rankedTodo))
	}
	return result
}

func projectCompanionTodo(rankedTodo rankedCompanionTodo) CompanionTodo {
	todo := rankedTodo.todo
	result := CompanionTodo{ID: boundedCompanionText(todo.ID, 40), Title: boundedCompanionText(todo.Title, 160), Status: boundedCompanionText(todo.Status, 32), Priority: boundedCompanionText(todo.Priority, 8), Project: boundedCompanionText(todo.Project, 80), ReviewAt: boundedCompanionText(todo.ReviewAt, 32), WakeCondition: boundedCompanionText(todo.WakeCondition, 160), MenuState: rankedTodo.menuState, ETag: work.TodoETag(todo)}
	for _, link := range todo.Links {
		if len(result.Links) == companionLinkLimit {
			break
		}
		parsed, err := url.Parse(strings.TrimSpace(link.URL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || len(link.URL) > 2048 {
			continue
		}
		result.Links = append(result.Links, CompanionTodoLink{Title: boundedCompanionText(link.Title, 100), URL: link.URL})
	}
	return result
}

func companionPriorityRank(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	default:
		return 99
	}
}

func projectCompanionQuota(value CachedQuota) CompanionQuota {
	windows := make([]CachedQuotaWindow, 0, len(value.Windows))
	for _, window := range value.Windows {
		if window.WindowMinutes <= 0 || math.IsNaN(window.UsedPercent) || math.IsInf(window.UsedPercent, 0) {
			continue
		}
		windows = append(windows, window)
	}
	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].ObservedAt != windows[j].ObservedAt {
			return windows[i].ObservedAt > windows[j].ObservedAt
		}
		if windows[i].Agent != windows[j].Agent {
			return windows[i].Agent < windows[j].Agent
		}
		return windows[i].WindowMinutes < windows[j].WindowMinutes
	})
	result := CompanionQuota{
		Source:      boundedCompanionText(value.Source, 64),
		GeneratedAt: boundedCompanionText(value.GeneratedAt, 64),
		Windows:     []CompanionQuotaWindow{},
		Truncated:   len(windows) > companionQuotaLimit,
	}
	for _, window := range windows {
		if len(result.Windows) == companionQuotaLimit {
			break
		}
		used := clampCompanionPercent(window.UsedPercent)
		result.Windows = append(result.Windows, CompanionQuotaWindow{
			Agent:            boundedCompanionText(window.Agent, 100),
			WindowMinutes:    window.WindowMinutes,
			UsedPercent:      used,
			RemainingPercent: clampCompanionPercent(100 - used),
			ResetsAt:         max(window.ResetsAt, 0),
			ObservedAt:       boundedCompanionText(window.ObservedAt, 64),
			Stale:            window.Stale,
			ResetElapsed:     window.ResetElapsed,
			Source:           boundedCompanionText(window.Source, 64),
			Plan:             boundedCompanionText(window.Plan, 80),
			Trend:            projectCompanionQuotaTrend(window.Trend),
		})
	}
	return result
}

func projectCompanionQuotaTrend(value *QuotaTrend) *CompanionQuotaTrend {
	if value == nil || !finiteCompanionNumber(value.PercentPerHour) || !finiteCompanionNumber(value.FromPercent) || !finiteCompanionNumber(value.ToPercent) {
		return nil
	}
	result := &CompanionQuotaTrend{PercentPerHour: value.PercentPerHour, Samples: max(value.Samples, 0), SpanMinutes: max(value.SpanMinutes, 0), FromPercent: clampCompanionPercent(value.FromPercent), ToPercent: clampCompanionPercent(value.ToPercent), FullBeforeReset: value.FullBeforeReset}
	if value.FullAt > 0 {
		result.FullAt = time.Unix(value.FullAt, 0).UTC().Format(time.RFC3339)
	}
	return result
}

func finiteCompanionNumber(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func clampCompanionPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func boundedCompanionText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
