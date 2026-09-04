package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/presence"
	"github.com/zane-byte-dev/atm/internal/session"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

// These transport DTOs intentionally omit sync, source paths, thinking extraction,
// provider execution, and live billing. Browser reads consume the existing index.
type SessionListInput struct {
	Agent   string `json:"agent,omitempty"`
	Project string `json:"project,omitempty"`
	Days    int    `json:"days,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type SessionSearchInput struct {
	SessionListInput
	Keyword string `json:"keyword"`
}

type SessionShowInput struct {
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type SessionPage struct {
	session.ShowResult
	Offset         int  `json:"offset"`
	Limit          int  `json:"limit"`
	ContentLimited bool `json:"content_truncated"`
}

type SessionSearchPage struct {
	session.SearchResult
	Offset int `json:"offset"`
}

type ActivityAgent struct {
	Agent    string `json:"agent"`
	Sessions int    `json:"sessions"`
	LatestAt string `json:"latest_at,omitempty"`
}

type SessionStatus struct {
	Source       string                `json:"source"`
	GeneratedAt  string                `json:"generated_at"`
	MissingIndex bool                  `json:"missing_index"`
	Health       store.SyncHealth      `json:"health"`
	Agents       []ActivityAgent       `json:"agents"`
	Projects     []string              `json:"projects"`
	Bindings     []work.BindingContext `json:"bindings"`
	Presence     *presence.Snapshot    `json:"presence,omitempty"`
	AgentHooks   bool                  `json:"agent_hooks"`
}

type UsageInput struct {
	Range string `json:"range,omitempty"`
	Agent string `json:"agent,omitempty"`
}

type CachedQuotaInput struct {
	Agent string `json:"agent,omitempty"`
}

type CachedQuotaWindow struct {
	Agent         string            `json:"agent"`
	WindowMinutes int               `json:"window_minutes"`
	UsedPercent   float64           `json:"used_percent"`
	ResetsAt      int64             `json:"resets_at"`
	ObservedAt    string            `json:"observed_at"`
	Stale         bool              `json:"stale"`
	ResetElapsed  bool              `json:"reset_elapsed"`
	Source        string            `json:"source,omitempty"`
	Plan          string            `json:"plan,omitempty"`
	Trend         *store.QuotaTrend `json:"trend,omitempty"`
}

type CachedQuota struct {
	Source      string              `json:"source"`
	GeneratedAt string              `json:"generated_at"`
	Windows     []CachedQuotaWindow `json:"windows"`
}

func (h *Host) callActivity(ctx context.Context, call application.Call, method string, input json.RawMessage) (any, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return nil, err
	}
	switch method {
	case "session.list":
		return invoke(input, func(value SessionListInput) (any, error) { return activitySessions(ctx, value) })
	case "session.search":
		return invoke(input, func(value SessionSearchInput) (any, error) { return activitySearch(ctx, value) })
	case "session.show":
		return invoke(input, func(value SessionShowInput) (any, error) { return activityShow(ctx, value) })
	case "session.status":
		return invoke(input, func(struct{}) (any, error) {
			result, err := activityStatus(ctx)
			_, live := h.attachedRuntime()
			if live != nil {
				snapshot := live.Snapshot()
				result.Presence, result.AgentHooks = &snapshot, true
			}
			return result, err
		})
	case "usage.snapshot":
		return invoke(input, func(value UsageInput) (any, error) { return activityUsage(ctx, call, value) })
	case "quota.cached":
		return invoke(input, func(value CachedQuotaInput) (any, error) { return h.cachedQuota(ctx, value) })
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown activity API method")
	}
}

func activityListBounds(input *SessionListInput) error {
	if input.Days < 0 || input.Days > 365 || input.Offset < 0 || input.Offset > 10000 || input.Limit < 0 || input.Limit > 100 {
		return invalid("days must be 1–365, limit 1–100, and offset 0–10000")
	}
	if utf8.RuneCountInString(input.Project) > 200 || utf8.RuneCountInString(input.Agent) > 100 {
		return invalid("session filter is too long")
	}
	if input.Days == 0 {
		input.Days = 7
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	input.Agent, input.Project = strings.TrimSpace(input.Agent), strings.TrimSpace(input.Project)
	return nil
}

func activitySessions(ctx context.Context, input SessionListInput) (session.ListResult, error) {
	if err := activityListBounds(&input); err != nil {
		return session.ListResult{}, err
	}
	result, err := session.NewService(session.ServiceOptions{}).List(ctx, session.ListInput{
		Agent: input.Agent, Project: input.Project, Days: input.Days, Limit: input.Limit, Offset: input.Offset, Order: "activity-desc",
	})
	if errors.Is(err, store.ErrDatabaseMissing) {
		return session.ListResult{Sessions: []session.Summary{}, Days: input.Days, Limit: input.Limit, Offset: input.Offset}, nil
	}
	if err != nil {
		return result, err
	}
	for i := range result.Sessions {
		row := &result.Sessions[i]
		row.Summary = activityText(row.Summary, 2000)
		row.FirstQuestion = activityText(row.FirstQuestion, 2000)
		row.LatestProgress = activityText(row.LatestProgress, 2000)
		row.FinalResult = activityText(row.FinalResult, 2000)
		if row.Review != nil {
			row.Review.Note = activityText(row.Review.Note, 2000)
		}
	}
	return result, nil
}

func activitySearch(ctx context.Context, input SessionSearchInput) (SessionSearchPage, error) {
	if err := activityListBounds(&input.SessionListInput); err != nil {
		return SessionSearchPage{}, err
	}
	if input.Offset > 1000 {
		return SessionSearchPage{}, invalid("search offset must be 0–1000; narrow the search to see more results")
	}
	input.Keyword = strings.TrimSpace(input.Keyword)
	if input.Keyword == "" || utf8.RuneCountInString(input.Keyword) > 200 {
		return SessionSearchPage{}, invalid("search keyword must contain 1–200 characters")
	}
	result, err := session.NewService(session.ServiceOptions{}).Search(ctx, session.SearchInput{
		Keyword: input.Keyword, Agent: input.Agent, Project: input.Project, Days: input.Days, Limit: input.Offset + input.Limit, Snippet: 600,
	})
	if errors.Is(err, store.ErrDatabaseMissing) {
		return SessionSearchPage{SearchResult: session.SearchResult{Keyword: input.Keyword, Limit: input.Limit, Matches: []session.SearchHit{}}, Offset: input.Offset}, nil
	}
	if err != nil {
		return SessionSearchPage{}, err
	}
	start := min(input.Offset, len(result.Matches))
	result.Matches = result.Matches[start:]
	result.Returned, result.Limit = len(result.Matches), input.Limit
	result.Truncated = result.Total > input.Offset+result.Returned
	return SessionSearchPage{SearchResult: result, Offset: input.Offset}, nil
}

func activityShow(ctx context.Context, input SessionShowInput) (SessionPage, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.SessionID == "" || utf8.RuneCountInString(input.SessionID) > 256 || strings.ContainsAny(input.SessionID, "/\\\x00") {
		return SessionPage{}, invalid("a valid indexed session_id is required")
	}
	if input.Offset < 0 || input.Offset > 100000 || input.Limit < 0 || input.Limit > 50 {
		return SessionPage{}, invalid("turn limit must be 1–50 and offset 0–100000")
	}
	if input.Limit == 0 {
		input.Limit = 20
	}
	result, err := session.NewService(session.ServiceOptions{}).ShowPage(ctx, session.PageInput{SessionID: input.SessionID, Offset: input.Offset, Limit: input.Limit})
	if err != nil {
		return SessionPage{}, err
	}
	// Budget each turn independently: a long answer cannot consume the next
	// turn's page slot and make pagination silently skip that turn.
	limited := false
	for i := range result.QA {
		qa := &result.QA[i]
		budget := 16000
		take := func(value string) string {
			if utf8.RuneCountInString(value) > budget {
				limited = true
			}
			value = activityText(value, budget)
			budget -= utf8.RuneCountInString(value)
			return value
		}
		qa.Q = take(qa.Q)
		qa.A = take(qa.A)
		progress := []string{}
		for _, value := range qa.Progress {
			if next := take(value); next != "" {
				progress = append(progress, next)
			}
		}
		qa.Progress = progress
	}
	result.LatestProgress = activityText(result.LatestProgress, 4000)
	result.FinalResult = activityText(result.FinalResult, 8000)
	result.Truncated = result.Truncated || limited
	if result.QA == nil {
		result.QA = []session.QA{}
	}
	return SessionPage{ShowResult: result, Offset: input.Offset, Limit: input.Limit, ContentLimited: limited}, nil
}

func activityStatus(ctx context.Context) (SessionStatus, error) {
	result := SessionStatus{Source: "index", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Agents: []ActivityAgent{}, Projects: []string{}, Bindings: []work.BindingContext{}}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		result.MissingIndex = true
		return result, nil
	}
	if err != nil {
		return result, unavailable(err)
	}
	defer db.Close()
	result.Health, err = store.ReadSyncHealth(db, store.SyncScopeAll, time.Now(), store.DefaultSyncStaleAfter)
	if err != nil {
		return result, unavailable(err)
	}
	bindings, err := store.ListActiveTodoSessionBindings()
	if err != nil {
		return result, unavailable(err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return result, unavailable(err)
	}
	for _, item := range work.BuildBindingContexts(bindings, todos) {
		// Binding truth is deliberately separate from runtime process status.
		// Stale rows are retained with their classification rather than cleaned up.
		item.Binding.CWD = ""
		item.Binding.Reason = activityText(item.Binding.Reason, 1000)
		if item.Todo != nil {
			item.Todo.Title = activityText(item.Todo.Title, 500)
		}
		result.Bindings = append(result.Bindings, item)
		if len(result.Bindings) == 100 {
			break
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT agent, COUNT(*), MAX(COALESCE(NULLIF(last_ts, 0), created_ts)) FROM sessions WHERE is_internal = 0 GROUP BY agent ORDER BY agent LIMIT 100`)
	if err != nil {
		return result, unavailable(err)
	}
	for rows.Next() {
		var item ActivityAgent
		var latest int64
		if err = rows.Scan(&item.Agent, &item.Sessions, &latest); err != nil {
			rows.Close()
			return result, unavailable(err)
		}
		if latest > 0 {
			item.LatestAt = time.Unix(latest, 0).UTC().Format(time.RFC3339)
		}
		result.Agents = append(result.Agents, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, unavailable(err)
	}
	rows, err = db.QueryContext(ctx, `SELECT DISTINCT project FROM sessions WHERE is_internal = 0 AND project != '' ORDER BY project LIMIT 300`)
	if err != nil {
		return result, unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			return result, unavailable(err)
		}
		result.Projects = append(result.Projects, project)
	}
	return result, rows.Err()
}

func activityUsage(ctx context.Context, call application.Call, input UsageInput) (dashboard.Snapshot, error) {
	if input.Range == "" {
		input.Range = "last_7_days"
	}
	name, err := config.ParseMetricsRange(input.Range)
	if err != nil {
		return dashboard.Snapshot{}, invalid(err.Error())
	}
	if len(input.Agent) > 100 {
		return dashboard.Snapshot{}, invalid("agent filter is too long")
	}
	result, err := dashboard.NewService(nil).BuildSnapshot(ctx, call, dashboard.Request{
		Sections: []string{"stats"}, Ranges: []string{string(name)}, Compact: true, Agent: input.Agent,
	})
	if err != nil {
		return result, err
	}
	// Usage consumers have a separate paginated session API. Do not embed every
	// conversation (including its final answer) in a chart response.
	for key, value := range result.Ranges {
		value.Sessions = []dashboard.RangeSession{}
		result.Ranges[key] = value
	}
	result.TodoCompletions = []store.TodoCompletion{}
	return result, nil
}

func activityQuota(ctx context.Context, input CachedQuotaInput) (CachedQuota, error) {
	if utf8.RuneCountInString(input.Agent) > 100 {
		return CachedQuota{}, invalid("agent filter is too long")
	}
	now := time.Now().UTC()
	result := CachedQuota{Source: "quota_history", GeneratedAt: now.Format(time.RFC3339), Windows: []CachedQuotaWindow{}}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, unavailable(err)
	}
	defer db.Close()
	if err := store.BoundReadWait(ctx, db, 200*time.Millisecond); err != nil {
		return result, unavailable(err)
	}
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'quota_history'`).Scan(&exists); err != nil {
		return result, unavailable(err)
	}
	if exists == 0 {
		return result, nil
	}
	// Fixed local table, no provider process or transcript discovery. Keep the
	// observation unchanged after reset: expired readings are unknown, not zero.
	rows, err := db.QueryContext(ctx, `SELECT q.agent, q.window_minutes, q.used_percent, q.resets_at, q.ts FROM quota_history q JOIN (SELECT agent, window_minutes, MAX(ts) ts FROM quota_history GROUP BY agent, window_minutes) latest USING (agent, window_minutes, ts) WHERE (? = '' OR q.agent = ?) ORDER BY q.agent, q.window_minutes LIMIT 100`, input.Agent, input.Agent)
	if err != nil {
		return result, unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		var item CachedQuotaWindow
		var ts int64
		if err := rows.Scan(&item.Agent, &item.WindowMinutes, &item.UsedPercent, &item.ResetsAt, &ts); err != nil {
			return result, unavailable(err)
		}
		item.ObservedAt = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		item.Stale = now.Sub(time.Unix(ts, 0)) > store.DefaultSyncStaleAfter
		item.ResetElapsed = item.ResetsAt > 0 && item.ResetsAt <= now.Unix()
		result.Windows = append(result.Windows, item)
	}
	return result, rows.Err()
}

func (h *Host) cachedQuota(ctx context.Context, input CachedQuotaInput) (CachedQuota, error) {
	result, err := activityQuota(ctx, input)
	if err != nil {
		return result, err
	}
	// This is only the explicitly produced runtime cache. A page read never
	// starts a quota provider, and historical windows stay as a fallback for
	// agents omitted by a targeted refresh.
	cache, err := background.ReadQuotaCache(h.dataDir)
	if err != nil {
		return result, nil
	}
	now := time.Now().UTC()
	index := make(map[string]map[int]int)
	for i, window := range result.Windows {
		if index[window.Agent] == nil {
			index[window.Agent] = make(map[int]int)
		}
		index[window.Agent][window.WindowMinutes] = i
	}
	agents := make([]string, 0, len(cache.Snapshot.Agents))
	for agent := range cache.Snapshot.Agents {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	used := false
	for _, agent := range agents {
		if input.Agent != "" && input.Agent != agent {
			continue
		}
		observed, err := time.Parse(time.RFC3339, cache.UpdatedAtFor(agent))
		if err != nil || observed.After(now.Add(time.Minute)) {
			continue
		}
		agentQuota := cache.Snapshot.Agents[agent]
		for _, window := range agentQuota.Windows() {
			if window.WindowMinutes <= 0 {
				continue
			}
			next := CachedQuotaWindow{Agent: agent, WindowMinutes: window.WindowMinutes, UsedPercent: window.UsedPercent, ResetsAt: window.ResetsAt, ObservedAt: observed.UTC().Format(time.RFC3339), Stale: now.Sub(observed) > store.DefaultSyncStaleAfter, ResetElapsed: window.ResetsAt > 0 && window.ResetsAt <= now.Unix(), Source: agentQuota.Source, Plan: agentQuota.Plan, Trend: window.Trend}
			if existing, ok := index[agent][window.WindowMinutes]; ok {
				prior, _ := time.Parse(time.RFC3339, result.Windows[existing].ObservedAt)
				if !observed.Before(prior) {
					result.Windows[existing] = next
					used = true
				}
			} else if len(result.Windows) < 100 {
				if index[agent] == nil {
					index[agent] = make(map[int]int)
				}
				index[agent][window.WindowMinutes] = len(result.Windows)
				result.Windows = append(result.Windows, next)
				used = true
			}
		}
	}
	if used {
		result.Source = "runtime_quota_cache"
	}
	sort.Slice(result.Windows, func(i, j int) bool {
		if result.Windows[i].Agent != result.Windows[j].Agent {
			return result.Windows[i].Agent < result.Windows[j].Agent
		}
		return result.Windows[i].WindowMinutes < result.Windows[j].WindowMinutes
	})
	return result, nil
}

func activityText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
