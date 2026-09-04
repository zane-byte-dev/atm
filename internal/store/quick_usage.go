package store

import (
	"context"
	"database/sql"
)

// QuickUsageSummary is the bounded scalar projection used by native quick
// surfaces. Sessions follows ListSessions' overlap semantics; the remaining
// fields follow GetDayStats' event-time usage and aggregate-fallback policy.
type QuickUsageSummary struct {
	Sessions        int     `json:"sessions"`
	Queries         int     `json:"queries"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

// ReadQuickUsageSummary reads one time range without materializing sessions,
// messages, or per-request rows. Detailed usage is attributed by event time;
// an aggregate usage row is used only for older sessions that have no detailed
// events at all, matching GetDayStats and GetStats.
func ReadQuickUsageSummary(ctx context.Context, db *sql.DB, startTS, endTS int64, agent string) (QuickUsageSummary, error) {
	sessionFilter := ` AND s.agent<>?`
	sessionArgs := []any{endTS, startTS, BuiltinAgent}
	periodFilter := ``
	queryArgs := []any{startTS, endTS}
	eventArgs := []any{startTS, endTS}
	fallbackArgs := []any{startTS, endTS}
	if agent != "" {
		sessionFilter = ` AND s.agent=?`
		sessionArgs = []any{endTS, startTS, agent}
		periodFilter = ` AND s.agent=?`
		queryArgs = append(queryArgs, agent)
		eventArgs = append(eventArgs, agent)
		fallbackArgs = append(fallbackArgs, agent)
	}

	query := `WITH session_total AS (
		SELECT COUNT(DISTINCT s.id) AS sessions
		FROM sessions s
		WHERE s.is_internal=0 AND s.created_ts<?
		AND CASE WHEN s.last_ts>0 THEN s.last_ts ELSE s.created_ts END>=?` + sessionFilter + `
	), query_total AS (
		SELECT COUNT(*) AS queries
		FROM messages m JOIN sessions s ON s.id=m.session_id
		WHERE s.is_internal=0 AND s.is_subagent=0
		AND m.role='user' AND m.scope='local' AND m.kind='conversation'
		AND m.ts>=? AND m.ts<?` + periodFilter + `
	), period_usage AS (
		SELECT e.input_tokens+e.cache_create_tokens+e.cache_read_tokens AS input_tokens,
			e.output_tokens AS output_tokens, e.cache_read_tokens AS cache_read_tokens,
			e.cost_usd AS cost_usd
		FROM usage_events e JOIN sessions s ON s.id=e.session_id
		WHERE s.is_internal=0 AND e.ts>=? AND e.ts<?` + periodFilter + `
		UNION ALL
		SELECT u.input_tokens+u.cache_create_tokens+u.cache_read_tokens,
			u.output_tokens, u.cache_read_tokens, u.cost_usd
		FROM usage u JOIN sessions s ON s.id=u.session_id
		WHERE s.is_internal=0 AND s.created_ts>=? AND s.created_ts<?
		AND NOT EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id=u.session_id)` + periodFilter + `
	), usage_total AS (
		SELECT COALESCE(SUM(input_tokens),0) AS input_tokens,
			COALESCE(SUM(output_tokens),0) AS output_tokens,
			COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens,
			COALESCE(SUM(cost_usd),0) AS cost_usd
		FROM period_usage
	)
	SELECT s.sessions,q.queries,u.input_tokens,u.output_tokens,u.cache_read_tokens,u.cost_usd
	FROM session_total s CROSS JOIN query_total q CROSS JOIN usage_total u`

	args := make([]any, 0, len(sessionArgs)+len(queryArgs)+len(eventArgs)+len(fallbackArgs))
	args = append(args, sessionArgs...)
	args = append(args, queryArgs...)
	args = append(args, eventArgs...)
	args = append(args, fallbackArgs...)

	var result QuickUsageSummary
	err := db.QueryRowContext(ctx, query, args...).Scan(
		&result.Sessions,
		&result.Queries,
		&result.InputTokens,
		&result.OutputTokens,
		&result.CacheReadTokens,
		&result.CostUSD,
	)
	return result, err
}
