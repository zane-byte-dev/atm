package store

import (
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
)

type ListResult struct {
	FullID    string
	ShortID   string
	Agent     string
	Project   string
	CreatedAt string
	CreatedTS int64
	LastTS    int64
	QCount    int
	FirstQ    string
	Summary   string
}

type SearchHit struct {
	FullID    string
	Agent     string
	Project   string
	ShortID   string
	CreatedAt string
	CreatedTS int64
	Role      string
	Content   string
}

type SearchOptions struct {
	Agent   string
	Project string
	Role    string
	SinceTS int64
	Limit   int
	// Keep drops matches the caller considers noise before the limit applies.
	// It runs here rather than at the call site so Total counts the same set the
	// caller will see, instead of a raw row count the caller then shrinks.
	Keep func(content string) bool
}

// SearchMatches is a bounded page plus the honest size of the set it came from.
// Returning only the page invites the caller to print its length as the match
// count, which reads as "this is everything" when it is the first 50 of 1110.
type SearchMatches struct {
	Hits      []SearchHit
	Total     int
	Truncated bool
}

type ShowResult struct {
	// Agent is the display name ("Grok Build"). AgentKey is the stored key
	// ("grokbuild"), which is what callers must switch on: routing on the display
	// name silently matched nothing and made every transcript read as Claude's.
	Agent    string
	AgentKey string
	Project  string
	FullID   string
	FilePath string
	Inputs   []string
	Outputs  []string
	Tools    map[string]int
}

type ReportResult struct {
	ShortID   string
	Agent     string
	Project   string
	CreatedAt string
	Summary   string
	FilePath  string
	Inputs    []string
	Outputs   []string
	Tools     map[string]int
}

func ListSessions(db *sql.DB, startTS, endTS int64, agent, project string) ([]ListResult, error) {
	query := `SELECT s.id, s.short_id, s.agent, s.project, s.created_at, s.created_ts, s.last_ts,
		(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') AS q_count,
		COALESCE((SELECT m.content FROM messages m WHERE m.session_id = s.id AND m.role = 'user'
				ORDER BY m.seq LIMIT 1), '') AS first_q,
		s.summary
		FROM sessions s
		WHERE s.created_ts < ? AND CASE WHEN s.last_ts > 0 THEN s.last_ts ELSE s.created_ts END >= ?`
	args := []any{endTS, startTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY s.created_at"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ListResult
	for rows.Next() {
		var r ListResult
		if err := rows.Scan(&r.FullID, &r.ShortID, &r.Agent, &r.Project, &r.CreatedAt, &r.CreatedTS, &r.LastTS, &r.QCount, &r.FirstQ, &r.Summary); err != nil {
			return nil, err
		}
		r.Project = config.CanonicalProject(r.Project)
		if !config.ProjectMatches(r.Project, project) {
			continue
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func SearchMessages(db *sql.DB, keyword string, agent string) ([]SearchHit, error) {
	matches, err := SearchMessagesWithOptions(db, keyword, SearchOptions{Agent: agent})
	return matches.Hits, err
}

func SearchMessagesWithOptions(db *sql.DB, keyword string, options SearchOptions) (SearchMatches, error) {
	query := `SELECT s.id, s.agent, s.project, s.short_id, s.created_at, s.created_ts, m.role, m.content
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE instr(lower(m.content), lower(?)) > 0`
	args := []any{keyword}
	if options.Agent != "" {
		query += " AND s.agent = ?"
		args = append(args, options.Agent)
	}
	if options.Project != "" {
		query += " AND instr(lower(s.project), lower(?)) > 0"
		args = append(args, options.Project)
	}
	if options.Role != "" {
		query += " AND m.role = ?"
		args = append(args, options.Role)
	}
	if options.SinceTS > 0 {
		query += " AND CASE WHEN m.ts > 0 THEN m.ts ELSE s.created_ts END >= ?"
		args = append(args, options.SinceTS)
	}
	query += " ORDER BY CASE WHEN m.ts > 0 THEN m.ts ELSE s.created_ts END DESC, s.created_ts DESC, m.seq DESC"

	// The limit is applied only after evaluating every accepted row rather than
	// as SQL LIMIT: relevance cannot be decided from recency alone. The scanner
	// retains only the best bounded page, so a broad query does not retain every
	// matching transcript in memory.
	matches, err := scanSearchHits(db, keyword, options, query, args...)
	if err != nil {
		return SearchMatches{}, err
	}
	return matches, nil
}

func scanSearchHits(db *sql.DB, keyword string, options SearchOptions, query string, args ...any) (SearchMatches, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return SearchMatches{}, err
	}
	defer rows.Close()

	type rankedSearchHit struct {
		hit   SearchHit
		score float64
		order int
	}
	better := func(left, right rankedSearchHit) bool {
		if left.score != right.score {
			return left.score > right.score
		}
		if left.hit.CreatedTS != right.hit.CreatedTS {
			return left.hit.CreatedTS > right.hit.CreatedTS
		}
		return left.order < right.order
	}

	var matches SearchMatches
	var ranked []rankedSearchHit
	order := 0
	for rows.Next() {
		var r SearchHit
		if err := rows.Scan(&r.FullID, &r.Agent, &r.Project, &r.ShortID, &r.CreatedAt, &r.CreatedTS, &r.Role, &r.Content); err != nil {
			return SearchMatches{}, err
		}
		if options.Keep != nil && !options.Keep(r.Content) {
			continue
		}
		matches.Total++
		r.Project = config.CanonicalProject(r.Project)
		candidate := rankedSearchHit{hit: r, score: searchMessageScore(r, keyword), order: order}
		order++
		if options.Limit <= 0 || len(ranked) < options.Limit {
			ranked = append(ranked, candidate)
			continue
		}
		worst := 0
		for index := 1; index < len(ranked); index++ {
			if better(ranked[worst], ranked[index]) {
				worst = index
			}
		}
		if better(candidate, ranked[worst]) {
			ranked[worst] = candidate
		}
	}
	if err := rows.Err(); err != nil {
		return SearchMatches{}, err
	}
	sort.Slice(ranked, func(i, j int) bool {
		return better(ranked[i], ranked[j])
	})
	matches.Hits = make([]SearchHit, len(ranked))
	for index := range ranked {
		matches.Hits[index] = ranked[index].hit
	}
	matches.Truncated = matches.Total > len(matches.Hits)
	return matches, nil
}

// searchMessageScore rewards a focused occurrence of the whole phrase. Session
// search already requires that literal occurrence, so token overlap would add
// no signal; density, position and the author's intent are what distinguish a
// direct query from a large transcript or generated report that mentions it in
// passing. Recency is deliberately only a tie-breaker in scanSearchHits.
func searchMessageScore(hit SearchHit, keyword string) float64 {
	content := strings.ToLower(strings.TrimSpace(hit.Content))
	needle := strings.ToLower(strings.TrimSpace(keyword))
	if content == "" || needle == "" {
		return 0
	}
	contentRunes := len([]rune(content))
	needleRunes := len([]rune(needle))
	occurrences := strings.Count(content, needle)
	position := strings.Index(content, needle)
	positionRunes := contentRunes
	if position >= 0 {
		positionRunes = len([]rune(content[:position]))
	}

	score := 1000 * float64(occurrences*needleRunes) / float64(max(contentRunes, 1))
	score += 100 / (1 + float64(positionRunes)/20)
	if hit.Role == "user" {
		score += 100
	}
	if content == needle {
		score += 500
	}
	return score
}

// sessionLookupArgs binds the id-or-short-id-prefix pair every session lookup
// matches on. All four of them order by last_ts DESC LIMIT 1: a prefix can match
// several sessions, and the index keeps history whose transcript is already
// gone, so the most recent match is the one the caller means. They have to agree
// — GetSession once matched short_id alone and could not resolve a full id that
// `atm session timeline` accepted.
func sessionLookupArgs(idOrPrefix string) []any {
	return []any{idOrPrefix, idOrPrefix + "%"}
}

func GetSession(db *sql.DB, idOrPrefix string) (*ShowResult, error) {
	var fullID, agent, project, filePath string
	err := db.QueryRow("SELECT id, agent, project, file_path FROM sessions WHERE id = ? OR short_id LIKE ? ORDER BY last_ts DESC LIMIT 1",
		sessionLookupArgs(idOrPrefix)...).Scan(&fullID, &agent, &project, &filePath)
	if err != nil {
		return nil, err
	}
	project = config.CanonicalProject(project)

	inputs, outputs, err := scanMessages(db, "SELECT role, content FROM messages WHERE session_id = ? ORDER BY seq", fullID)
	if err != nil {
		return nil, err
	}

	tools, err := scanTools(db, fullID)
	if err != nil {
		return nil, err
	}

	agentDisplay := AgentDisplayName(agent)
	return &ShowResult{
		Agent:    agentDisplay,
		AgentKey: agent,
		Project:  project,
		FullID:   fullID,
		FilePath: filePath,
		Inputs:   inputs,
		Outputs:  outputs,
		Tools:    tools,
	}, nil
}

func GetReport(db *sql.DB, startTS, endTS int64, agent string) ([]ReportResult, error) {
	query := `SELECT id, short_id, agent, project, created_at, summary, file_path FROM sessions
		WHERE (created_ts >= ? AND created_ts < ?) OR (last_ts >= ? AND last_ts < ?)`
	args := []any{startTS, endTS, startTS, endTS}
	if agent != "" {
		query += " AND agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY agent, created_at"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type sessionMeta struct {
		id, shortID, agent, project, createdAt, summary, filePath string
	}
	var metas []sessionMeta
	for rows.Next() {
		var m sessionMeta
		if err := rows.Scan(&m.id, &m.shortID, &m.agent, &m.project, &m.createdAt, &m.summary, &m.filePath); err != nil {
			return nil, err
		}
		metas = append(metas, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var results []ReportResult
	for _, m := range metas {
		inputs, outputs, err := scanMessages(db,
			"SELECT role, content FROM messages WHERE session_id = ? AND ts >= ? AND ts < ? ORDER BY seq",
			m.id, startTS, endTS)
		if err != nil {
			return nil, err
		}

		tools, err := scanTools(db, m.id)
		if err != nil {
			return nil, err
		}

		results = append(results, ReportResult{
			ShortID:   m.shortID,
			Agent:     m.agent,
			Project:   m.project,
			CreatedAt: m.createdAt,
			Summary:   m.summary,
			FilePath:  m.filePath,
			Inputs:    inputs,
			Outputs:   outputs,
			Tools:     tools,
		})
	}
	return results, nil
}

func scanMessages(db *sql.DB, query string, args ...any) ([]string, []string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var inputs, outputs []string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			return nil, nil, err
		}
		if role == "user" {
			inputs = append(inputs, content)
		} else {
			outputs = append(outputs, content)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return inputs, outputs, nil
}

func scanTools(db *sql.DB, sessionID string) (map[string]int, error) {
	rows, err := db.Query("SELECT name, count FROM tools WHERE session_id = ?", sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tools := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		tools[name] = count
	}
	return tools, rows.Err()
}

type ExportRow struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	TS        int64  `json:"ts"`
}

func ExportMessages(db *sql.DB, startTS, endTS int64, agent string) ([]ExportRow, error) {
	query := `SELECT s.short_id, s.agent, s.project, s.created_at, m.role, m.content, m.ts
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE s.created_ts < ? AND CASE WHEN s.last_ts > 0 THEN s.last_ts ELSE s.created_ts END >= ?`
	args := []any{endTS, startTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY s.created_at, m.seq"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ExportRow
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.SessionID, &r.Agent, &r.Project, &r.CreatedAt, &r.Role, &r.Content, &r.TS); err != nil {
			return nil, err
		}
		r.Project = config.CanonicalProject(r.Project)
		results = append(results, r)
	}
	return results, rows.Err()
}

type StatsResult struct {
	Project         string  `json:"project"`
	Agent           string  `json:"agent"`
	Sessions        int     `json:"sessions"`
	Queries         int     `json:"queries"`
	ToolCalls       int     `json:"tool_calls"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

func GetStats(db *sql.DB, startTS, endTS int64, agent string) ([]StatsResult, error) {
	query := `WITH active_sessions AS (
			SELECT id, project, agent FROM sessions WHERE created_ts >= ? AND created_ts < ?
			UNION
			SELECT s.id, s.project, s.agent
			FROM messages m JOIN sessions s ON s.id = m.session_id
			WHERE m.role = 'user' AND m.ts >= ? AND m.ts < ?
			UNION
			SELECT s.id, s.project, s.agent
			FROM usage_events e JOIN sessions s ON s.id = e.session_id
			WHERE e.ts >= ? AND e.ts < ?
		),
		period_usage AS (
			SELECT s.project, s.agent,
				e.input_tokens + e.cache_create_tokens + e.cache_read_tokens AS input_tokens,
				e.output_tokens, e.cache_read_tokens, e.cost_usd
			FROM usage_events e JOIN sessions s ON s.id = e.session_id
			WHERE e.ts >= ? AND e.ts < ?
			UNION ALL
			SELECT s.project, s.agent,
				u.input_tokens + u.cache_create_tokens + u.cache_read_tokens,
				u.output_tokens, u.cache_read_tokens, u.cost_usd
			FROM usage u JOIN sessions s ON s.id = u.session_id
			WHERE s.created_ts >= ? AND s.created_ts < ?
			AND NOT EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id = u.session_id)
		),
		usage_totals AS (
			SELECT project, agent, SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens, SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(cost_usd) AS cost_usd
			FROM period_usage GROUP BY project, agent
		),
		query_totals AS (
			SELECT s.project, s.agent, COUNT(*) AS queries
			FROM messages m JOIN sessions s ON s.id = m.session_id
			WHERE m.role = 'user' AND m.ts >= ? AND m.ts < ?
			GROUP BY s.project, s.agent
		),
		tool_totals AS (
			SELECT a.project, a.agent, SUM(t.count) AS tool_calls
			FROM active_sessions a JOIN tools t ON t.session_id = a.id
			GROUP BY a.project, a.agent
		)
		SELECT a.project, a.agent, COUNT(DISTINCT a.id) AS sessions,
			COALESCE(q.queries, 0), COALESCE(t.tool_calls, 0),
			COALESCE(u.input_tokens, 0), COALESCE(u.output_tokens, 0),
			COALESCE(u.cache_read_tokens, 0), COALESCE(u.cost_usd, 0)
		FROM active_sessions a
		LEFT JOIN query_totals q ON q.project = a.project AND q.agent = a.agent
		LEFT JOIN tool_totals t ON t.project = a.project AND t.agent = a.agent
		LEFT JOIN usage_totals u ON u.project = a.project AND u.agent = a.agent
		GROUP BY a.project, a.agent
		ORDER BY sessions DESC, a.project`
	rows, err := db.Query(query,
		startTS, endTS,
		startTS, endTS,
		startTS, endTS,
		startTS, endTS,
		startTS, endTS,
		startTS, endTS,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []StatsResult
	for rows.Next() {
		var r StatsResult
		if err := rows.Scan(&r.Project, &r.Agent, &r.Sessions, &r.Queries, &r.ToolCalls,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		r.Project = config.CanonicalProject(r.Project)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	merged := make([]StatsResult, 0, len(results))
	indexes := map[string]int{}
	for _, result := range results {
		if agent != "" && result.Agent != agent {
			continue
		}
		key := result.Agent + "\x00" + result.Project
		if index, ok := indexes[key]; ok {
			merged[index].Sessions += result.Sessions
			merged[index].Queries += result.Queries
			merged[index].ToolCalls += result.ToolCalls
			merged[index].InputTokens += result.InputTokens
			merged[index].OutputTokens += result.OutputTokens
			merged[index].CacheReadTokens += result.CacheReadTokens
			merged[index].CostUSD += result.CostUSD
			continue
		}
		indexes[key] = len(merged)
		merged = append(merged, result)
	}
	return merged, nil
}

type ModelStatsResult struct {
	Client          string  `json:"client"`
	Model           string  `json:"model"`
	Sessions        int     `json:"sessions"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	// CostEstimated marks a row whose rate ATM guessed, either from the model's
	// family or from the Opus-tier default it falls back to. Without it an
	// invented number is indistinguishable from a known one; see PricingSource.
	CostEstimated bool          `json:"cost_estimated"`
	PricingSource PricingSource `json:"pricing_source"`
}

type SkillStatsResult struct {
	Skill    string `json:"skill"`
	Calls    int    `json:"calls"`
	Sessions int    `json:"sessions"`
	Agents   int    `json:"agents"`
}

func GetSkillStats(db *sql.DB, startTS, endTS int64, agent string) ([]SkillStatsResult, error) {
	query := `SELECT e.name, COUNT(*), COUNT(DISTINCT e.session_id), COUNT(DISTINCT s.agent)
		FROM skill_events e
		JOIN sessions s ON s.id = e.session_id
		WHERE e.ts >= ? AND e.ts < ?`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " GROUP BY e.name ORDER BY COUNT(*) DESC, e.name"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SkillStatsResult
	for rows.Next() {
		var result SkillStatsResult
		if err := rows.Scan(&result.Skill, &result.Calls, &result.Sessions, &result.Agents); err != nil {
			return nil, err
		}
		if !parser.IsValidSkillName(result.Skill) {
			continue
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func GetModelStats(db *sql.DB, startTS, endTS int64, agent string) ([]ModelStatsResult, error) {
	query := `SELECT s.agent, u.model, COUNT(DISTINCT u.session_id),
		SUM(u.input_tokens + u.cache_create_tokens + u.cache_read_tokens),
		SUM(u.output_tokens), SUM(u.cache_read_tokens), SUM(u.cost_usd)
		FROM (
			SELECT e.session_id, e.model, e.ts, e.input_tokens, e.output_tokens,
				e.cache_create_tokens, e.cache_read_tokens, e.cost_usd
			FROM usage_events e
			UNION ALL
			SELECT x.session_id, x.model, s0.created_ts, x.input_tokens, x.output_tokens,
				x.cache_create_tokens, x.cache_read_tokens, x.cost_usd
			FROM usage x JOIN sessions s0 ON s0.id = x.session_id
			WHERE NOT EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id=x.session_id)
		) u
		JOIN sessions s ON u.session_id = s.id
		WHERE u.ts >= ? AND u.ts < ? AND u.model != ''`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += ` GROUP BY s.agent, u.model
		ORDER BY SUM(u.input_tokens + u.cache_create_tokens + u.cache_read_tokens + u.output_tokens) DESC,
			s.agent, u.model`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModelStatsResult
	for rows.Next() {
		var r ModelStatsResult
		if err := rows.Scan(&r.Client, &r.Model, &r.Sessions, &r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		_, r.PricingSource = PricingFor(r.Model)
		r.CostEstimated = r.PricingSource.Estimated()
		results = append(results, r)
	}
	return results, rows.Err()
}

type ModelDayStatsResult struct {
	Date            string  `json:"date"`
	Client          string  `json:"client"`
	Model           string  `json:"model"`
	Sessions        int     `json:"sessions"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	// See ModelStatsResult: whether this row's rate was ATM's guess.
	CostEstimated bool          `json:"cost_estimated"`
	PricingSource PricingSource `json:"pricing_source"`
	// Output tokens and milliseconds of the requests in this bucket whose
	// generation window was measurable. Their ratio is the bucket's throughput;
	// carried separately so any further grouping divides sums, not rates.
	MeasuredOutputTokens int64 `json:"measured_output_tokens"`
	MeasuredDurationMS   int64 `json:"measured_duration_ms"`
}

func GetModelDayStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]ModelDayStatsResult, error) {
	return modelPeriodStats(db, startTS, endTS, agent, loc, dayLayout)
}

func GetModelHourStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]ModelDayStatsResult, error) {
	return modelPeriodStats(db, startTS, endTS, agent, loc, hourLayout)
}

type ProjectDayStatsResult struct {
	Date            string  `json:"date"`
	Client          string  `json:"client"`
	Project         string  `json:"project"`
	Sessions        int     `json:"sessions"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	// See ModelDayStatsResult: measured-speed components, kept as sums.
	MeasuredOutputTokens int64 `json:"measured_output_tokens"`
	MeasuredDurationMS   int64 `json:"measured_duration_ms"`
}

func GetProjectDayStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]ProjectDayStatsResult, error) {
	return projectPeriodStats(db, startTS, endTS, agent, loc, dayLayout)
}

func GetProjectHourStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]ProjectDayStatsResult, error) {
	return projectPeriodStats(db, startTS, endTS, agent, loc, hourLayout)
}

const (
	dayLayout  = "2006-01-02"
	hourLayout = "2006-01-02 15:00"
)

func modelPeriodStats(
	db *sql.DB,
	startTS, endTS int64,
	agent string,
	loc *time.Location,
	layout string,
) ([]ModelDayStatsResult, error) {
	// A usage row with no model cannot appear in a model breakdown.
	stats, err := periodStats(db, startTS, endTS, agent, loc, layout, periodStatsLabel{
		column:   "u.model",
		required: true,
	})
	if err != nil {
		return nil, err
	}
	results := make([]ModelDayStatsResult, 0, len(stats))
	for _, stat := range stats {
		_, source := PricingFor(stat.Label)
		results = append(results, ModelDayStatsResult{
			Date:                 stat.Date,
			Client:               stat.Client,
			Model:                stat.Label,
			Sessions:             stat.Sessions,
			InputTokens:          stat.InputTokens,
			OutputTokens:         stat.OutputTokens,
			CacheReadTokens:      stat.CacheReadTokens,
			CostUSD:              stat.CostUSD,
			CostEstimated:        source.Estimated(),
			PricingSource:        source,
			MeasuredOutputTokens: stat.MeasuredOutputTokens,
			MeasuredDurationMS:   stat.MeasuredDurationMS,
		})
	}
	return results, nil
}

func projectPeriodStats(
	db *sql.DB,
	startTS, endTS int64,
	agent string,
	loc *time.Location,
	layout string,
) ([]ProjectDayStatsResult, error) {
	// Every usage row belongs to a session, so no row is dropped here; keeping
	// them makes the project series add up to the totals in GetDayStats.
	stats, err := periodStats(db, startTS, endTS, agent, loc, layout, periodStatsLabel{
		column:    "s.project",
		normalize: config.CanonicalProject,
	})
	if err != nil {
		return nil, err
	}
	results := make([]ProjectDayStatsResult, 0, len(stats))
	for _, stat := range stats {
		results = append(results, ProjectDayStatsResult{
			Date:                 stat.Date,
			Client:               stat.Client,
			Project:              stat.Label,
			Sessions:             stat.Sessions,
			InputTokens:          stat.InputTokens,
			OutputTokens:         stat.OutputTokens,
			CacheReadTokens:      stat.CacheReadTokens,
			CostUSD:              stat.CostUSD,
			MeasuredOutputTokens: stat.MeasuredOutputTokens,
			MeasuredDurationMS:   stat.MeasuredDurationMS,
		})
	}
	return results, nil
}

type periodStat struct {
	Date            string
	Client          string
	Label           string
	Sessions        int
	InputTokens     int64
	OutputTokens    int64
	CacheReadTokens int64
	CostUSD         float64
	// The two measured-speed components. They are carried as sums rather than a
	// rate because sums stay correct under further aggregation: a chart that
	// merges models into clients, or hours into days, divides the totals it has
	// instead of averaging averages.
	MeasuredOutputTokens int64
	MeasuredDurationMS   int64
}

// periodStatsLabel selects the second grouping dimension. column is a trusted
// column expression, never user input; normalize folds aliases together before
// aggregation so two spellings of one project do not become two series.
type periodStatsLabel struct {
	column    string
	required  bool
	normalize func(string) string
}

// periodStats keeps the request timestamp together with the client and one more
// label -- the model or the project -- so a caller can chart either breakdown
// over time. Aggregate-only legacy sessions are attributed to their creation
// time bucket.
func periodStats(
	db *sql.DB,
	startTS, endTS int64,
	agent string,
	loc *time.Location,
	layout string,
	label periodStatsLabel,
) ([]periodStat, error) {
	// A request only contributes to the speed sums when its measurement passed the
	// sampling window; the aggregate-only fallback rows have no measurement at all.
	// The window is judged per model call — see withinSpeedWindow — because a row
	// can report a whole turn's calls together.
	inWindow := `e.duration_ms / MAX(COALESCE(e.request_count, 1), 1) BETWEEN ? AND ?
			AND e.output_tokens / MAX(COALESCE(e.request_count, 1), 1) >= ?`
	measuredTokens := `CASE WHEN ` + inWindow + ` THEN e.output_tokens ELSE 0 END`
	measuredDuration := `CASE WHEN ` + inWindow + ` THEN e.duration_ms ELSE 0 END`
	query := `SELECT u.session_id, s.agent, ` + label.column + `, u.ts,
		u.input_tokens + u.cache_create_tokens + u.cache_read_tokens,
		u.output_tokens, u.cache_read_tokens, u.cost_usd,
		u.measured_output_tokens, u.measured_duration_ms
		FROM (
			SELECT e.session_id, e.model, e.ts, e.input_tokens, e.output_tokens,
				e.cache_create_tokens, e.cache_read_tokens, e.cost_usd,
				` + measuredTokens + ` AS measured_output_tokens,
				` + measuredDuration + ` AS measured_duration_ms
			FROM usage_events e
			UNION ALL
			SELECT x.session_id, x.model, s0.created_ts, x.input_tokens, x.output_tokens,
				x.cache_create_tokens, x.cache_read_tokens, x.cost_usd, 0, 0
			FROM usage x JOIN sessions s0 ON s0.id = x.session_id
			WHERE NOT EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id=x.session_id)
		) u
		JOIN sessions s ON u.session_id = s.id
		WHERE u.ts >= ? AND u.ts < ?`
	args := []any{
		speedMinDurationMS, speedMaxDurationMS, speedMinOutputTokens,
		speedMinDurationMS, speedMaxDurationMS, speedMinOutputTokens,
		startTS, endTS,
	}
	if label.required {
		query += ` AND ` + label.column + ` != ''`
	}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY u.ts, s.agent, " + label.column

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type periodKey struct {
		date   string
		client string
		label  string
	}
	aggregates := make(map[periodKey]*periodStat)
	sessions := make(map[periodKey]map[string]struct{})
	for rows.Next() {
		var sessionID, client, value string
		var ts, input, output, cacheRead, measuredOutput, measuredDuration int64
		var cost float64
		if err := rows.Scan(&sessionID, &client, &value, &ts, &input, &output, &cacheRead, &cost,
			&measuredOutput, &measuredDuration); err != nil {
			return nil, err
		}
		if label.normalize != nil {
			value = label.normalize(value)
		}
		key := periodKey{
			date:   time.Unix(ts, 0).In(loc).Format(layout),
			client: client,
			label:  value,
		}
		result := aggregates[key]
		if result == nil {
			result = &periodStat{Date: key.date, Client: key.client, Label: key.label}
			aggregates[key] = result
			sessions[key] = make(map[string]struct{})
		}
		result.InputTokens += input
		result.OutputTokens += output
		result.CacheReadTokens += cacheRead
		result.CostUSD += cost
		result.MeasuredOutputTokens += measuredOutput
		result.MeasuredDurationMS += measuredDuration
		sessions[key][sessionID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	results := make([]periodStat, 0, len(aggregates))
	for key, result := range aggregates {
		result.Sessions = len(sessions[key])
		results = append(results, *result)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Date != results[j].Date {
			return results[i].Date < results[j].Date
		}
		if results[i].Client != results[j].Client {
			return results[i].Client < results[j].Client
		}
		return results[i].Label < results[j].Label
	})
	return results, nil
}

type SessionStatsResult struct {
	ShortID      string  `json:"short_id"`
	Project      string  `json:"project"`
	Model        string  `json:"model"`
	Queries      int     `json:"queries"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// SessionUsageStatsResult is one session's usage inside an event-time window.
// Unlike SessionStatsResult, which is the legacy whole-session rollup selected
// by session creation time, these values only include requests whose own
// timestamps fall inside [startTS, endTS). StartedTS and LastTS are likewise the
// first and last requests in that window, not the lifetime session boundaries.
type SessionUsageStatsResult struct {
	SessionID         string  `json:"session_id"`
	ShortID           string  `json:"short_id"`
	Agent             string  `json:"agent"`
	Project           string  `json:"project"`
	Model             string  `json:"model"`
	StartedTS         int64   `json:"started_ts"`
	LastTS            int64   `json:"last_ts"`
	Requests          int     `json:"requests"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	CacheCreateTokens int64   `json:"cache_create_tokens"`
	CacheReadTokens   int64   `json:"cache_read_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	CostUSD           float64 `json:"cost_usd"`
	Share             float64 `json:"share"`
}

type RequestStatsResult struct {
	SessionID string `json:"session_id"`
	Agent     string `json:"agent"`
	Project   string `json:"project"`
	Model     string `json:"model"`
	TS        int64  `json:"ts"`
	// RequestCount is how many model calls this row aggregates. Always >= 1.
	// Grok turn_completed rows often cover several modelCalls; other agents
	// are typically 1. Token fields are the total for the whole row, not per call.
	RequestCount int     `json:"request_count"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CacheTokens  int64   `json:"cache_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

func GetRequestStats(db *sql.DB, startTS, endTS int64, agent, session string) ([]RequestStatsResult, error) {
	query := `SELECT s.short_id,s.agent,s.project,e.model,e.ts,COALESCE(e.request_count,1),e.input_tokens,e.output_tokens,e.cache_create_tokens+e.cache_read_tokens,e.cost_usd FROM usage_events e JOIN sessions s ON e.session_id=s.id WHERE s.created_ts>=? AND s.created_ts<?`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent=?"
		args = append(args, agent)
	}
	if session != "" {
		query += " AND (s.id=? OR s.short_id LIKE ?)"
		args = append(args, sessionLookupArgs(session)...)
	}
	query += " ORDER BY e.ts DESC,e.id DESC"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestStatsResult
	for rows.Next() {
		var r RequestStatsResult
		if err := rows.Scan(&r.SessionID, &r.Agent, &r.Project, &r.Model, &r.TS, &r.RequestCount, &r.InputTokens, &r.OutputTokens, &r.CacheTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		if r.RequestCount <= 0 {
			r.RequestCount = 1
		}
		r.Project = config.CanonicalProject(r.Project)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSessionUsageStats groups usage by session using request timestamps. The
// fallback branch keeps older indexed sessions useful when a parser supplied a
// lifetime usage rollup but no request-level events; those rows can only be
// attributed to the session creation time because no better timestamp exists.
func GetSessionUsageStats(db *sql.DB, startTS, endTS int64, agent string) ([]SessionUsageStatsResult, error) {
	query := `WITH usage_rows AS (
			SELECT e.session_id, s.short_id, s.agent, s.project,
				COALESCE(NULLIF(e.model, ''), 'unknown') AS model,
				e.ts, max(COALESCE(e.request_count, 1), 1) AS request_count,
				e.input_tokens, e.output_tokens, e.cache_create_tokens,
				e.cache_read_tokens, e.cost_usd
			FROM usage_events e
			JOIN sessions s ON s.id = e.session_id
			WHERE e.ts >= ? AND e.ts < ?
			UNION ALL
			SELECT u.session_id, s.short_id, s.agent, s.project,
				COALESCE(NULLIF(u.model, ''), 'unknown') AS model,
				s.created_ts, max(COALESCE(u.request_count, 1), 1) AS request_count,
				u.input_tokens, u.output_tokens, u.cache_create_tokens,
				u.cache_read_tokens, u.cost_usd
			FROM usage u
			JOIN sessions s ON s.id = u.session_id
			WHERE s.created_ts >= ? AND s.created_ts < ?
				AND NOT EXISTS (
					SELECT 1 FROM usage_events e WHERE e.session_id = u.session_id
				)
		),
		per_session AS (
			SELECT session_id, short_id, agent, project,
				MIN(ts) AS started_ts, MAX(ts) AS last_ts,
				SUM(request_count) AS requests,
				SUM(input_tokens) AS input_tokens,
				SUM(output_tokens) AS output_tokens,
				SUM(cache_create_tokens) AS cache_create_tokens,
				SUM(cache_read_tokens) AS cache_read_tokens,
				SUM(input_tokens + output_tokens + cache_create_tokens + cache_read_tokens) AS total_tokens,
				SUM(cost_usd) AS cost_usd
			FROM usage_rows
			GROUP BY session_id, short_id, agent, project
		),
		per_model AS (
			SELECT session_id, model,
				SUM(input_tokens + output_tokens + cache_create_tokens + cache_read_tokens) AS model_tokens
			FROM usage_rows
			GROUP BY session_id, model
		),
		ranked_model AS (
			SELECT session_id, model,
				ROW_NUMBER() OVER (
					PARTITION BY session_id
					ORDER BY model_tokens DESC, model ASC
				) AS model_rank
			FROM per_model
		)
		SELECT ps.session_id, ps.short_id, ps.agent, ps.project,
			COALESCE(rm.model, 'unknown'), ps.started_ts, ps.last_ts,
			ps.requests, ps.input_tokens, ps.output_tokens,
			ps.cache_create_tokens, ps.cache_read_tokens,
			ps.total_tokens, ps.cost_usd
		FROM per_session ps
		LEFT JOIN ranked_model rm
			ON rm.session_id = ps.session_id AND rm.model_rank = 1
		WHERE ps.total_tokens > 0`
	args := []any{startTS, endTS, startTS, endTS}
	if agent != "" {
		query += " AND ps.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY ps.total_tokens DESC, ps.last_ts DESC, ps.session_id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionUsageStatsResult
	var grandTotal int64
	for rows.Next() {
		var result SessionUsageStatsResult
		if err := rows.Scan(
			&result.SessionID, &result.ShortID, &result.Agent, &result.Project,
			&result.Model, &result.StartedTS, &result.LastTS, &result.Requests,
			&result.InputTokens, &result.OutputTokens, &result.CacheCreateTokens,
			&result.CacheReadTokens, &result.TotalTokens, &result.CostUSD,
		); err != nil {
			return nil, err
		}
		result.Project = config.CanonicalProject(result.Project)
		grandTotal += result.TotalTokens
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if grandTotal > 0 {
		for index := range results {
			results[index].Share = float64(results[index].TotalTokens) / float64(grandTotal)
		}
	}
	return results, nil
}

type TimelineEvent struct {
	Kind         string  `json:"kind"`
	Role         string  `json:"role,omitempty"`
	Content      string  `json:"content,omitempty"`
	Model        string  `json:"model,omitempty"`
	TS           int64   `json:"ts"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	CacheTokens  int64   `json:"cache_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

func GetSessionTimeline(db *sql.DB, sid string) ([]TimelineEvent, error) {
	var fullID string
	if err := db.QueryRow(`SELECT id FROM sessions WHERE id=? OR short_id LIKE ? ORDER BY last_ts DESC LIMIT 1`,
		sessionLookupArgs(sid)...).Scan(&fullID); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT kind,role,content,model,ts,input_tokens,output_tokens,cache_tokens,cost_usd FROM (
		SELECT 'message' kind,role,content,'' model,ts,0 input_tokens,0 output_tokens,0 cache_tokens,0.0 cost_usd,seq sort_id FROM messages WHERE session_id=?
		UNION ALL SELECT 'request','','',model,ts,input_tokens,output_tokens,cache_create_tokens+cache_read_tokens,cost_usd,1000000+id FROM usage_events WHERE session_id=?) ORDER BY ts,sort_id`, fullID, fullID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimelineEvent
	for rows.Next() {
		var e TimelineEvent
		if err := rows.Scan(&e.Kind, &e.Role, &e.Content, &e.Model, &e.TS, &e.InputTokens, &e.OutputTokens, &e.CacheTokens, &e.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func GetSessionStats(db *sql.DB, startTS, endTS int64, agent string) ([]SessionStatsResult, error) {
	query := `SELECT s.short_id, s.project, u.model,
		COALESCE(u.request_count, 0) AS queries,
		u.input_tokens, u.output_tokens,
		u.cache_create_tokens + u.cache_read_tokens AS cache_tokens,
		u.cost_usd
		FROM usage u
		JOIN sessions s ON u.session_id = s.id
		WHERE s.created_ts >= ? AND s.created_ts < ?
		AND (u.input_tokens > 0 OR u.output_tokens > 0)`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY (u.input_tokens + u.output_tokens + u.cache_create_tokens + u.cache_read_tokens) DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionStatsResult
	for rows.Next() {
		var r SessionStatsResult
		if err := rows.Scan(&r.ShortID, &r.Project, &r.Model, &r.Queries,
			&r.InputTokens, &r.OutputTokens, &r.CacheTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		r.Project = config.CanonicalProject(r.Project)
		results = append(results, r)
	}
	return results, rows.Err()
}

type DayStatsResult struct {
	Date            string  `json:"date"`
	Sessions        int     `json:"sessions"`
	Queries         int     `json:"queries"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

func GetDayStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]DayStatsResult, error) {
	return getPeriodStats(db, startTS, endTS, agent, loc, false)
}

func GetHourStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location) ([]DayStatsResult, error) {
	return getPeriodStats(db, startTS, endTS, agent, loc, true)
}

// forEachPeriodRow runs one of the period-stats queries and hands each row to
// scan. All of them bind the same window and take the same optional agent
// filter, and each one used to repeat rows.Close() on all three of its exit
// paths; the defer here means a new error path cannot leak the cursor. The query
// must end in a WHERE clause, because the agent filter is appended with AND.
func forEachPeriodRow(db *sql.DB, query string, startTS, endTS int64, agent string, scan func(*sql.Rows) error) error {
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func getPeriodStats(db *sql.DB, startTS, endTS int64, agent string, loc *time.Location, hourly bool) ([]DayStatsResult, error) {
	dayMap := make(map[string]*DayStatsResult)
	sessionsByDay := make(map[string]map[string]struct{})
	layout := "2006-01-02"
	if hourly {
		layout = "2006-01-02 15:00"
	}
	ensureDay := func(ts int64) (*DayStatsResult, string) {
		day := time.Unix(ts, 0).In(loc).Format(layout)
		d, ok := dayMap[day]
		if !ok {
			d = &DayStatsResult{Date: day}
			dayMap[day] = d
		}
		return d, day
	}
	addSession := func(day, id string) {
		if sessionsByDay[day] == nil {
			sessionsByDay[day] = make(map[string]struct{})
		}
		sessionsByDay[day][id] = struct{}{}
	}

	// The two usage queries differ only in which table they read, so they
	// accumulate through one scan.
	accumulateUsage := func(rows *sql.Rows) error {
		var ts, input, output, cacheRead int64
		var id string
		var cost float64
		if err := rows.Scan(&ts, &id, &input, &output, &cacheRead, &cost); err != nil {
			return err
		}
		d, day := ensureDay(ts)
		d.InputTokens += input
		d.OutputTokens += output
		d.CacheReadTokens += cacheRead
		d.CostUSD += cost
		addSession(day, id)
		return nil
	}

	// A session is active on the day it is created, receives a user message,
	// or records token usage. This keeps cross-midnight/resumed work visible.
	err := forEachPeriodRow(db, `SELECT s.created_ts, s.id FROM sessions s
		WHERE s.created_ts >= ? AND s.created_ts < ?`, startTS, endTS, agent,
		func(rows *sql.Rows) error {
			var ts int64
			var id string
			if err := rows.Scan(&ts, &id); err != nil {
				return err
			}
			_, day := ensureDay(ts)
			addSession(day, id)
			return nil
		})
	if err != nil {
		return nil, err
	}

	err = forEachPeriodRow(db, `SELECT m.ts, m.session_id FROM messages m JOIN sessions s ON s.id = m.session_id
		WHERE m.role = 'user' AND m.ts >= ? AND m.ts < ?`, startTS, endTS, agent,
		func(rows *sql.Rows) error {
			var ts int64
			var id string
			if err := rows.Scan(&ts, &id); err != nil {
				return err
			}
			d, day := ensureDay(ts)
			d.Queries++
			addSession(day, id)
			return nil
		})
	if err != nil {
		return nil, err
	}

	err = forEachPeriodRow(db, `SELECT e.ts, e.session_id,
		e.input_tokens + e.cache_create_tokens + e.cache_read_tokens,
		e.output_tokens, e.cache_read_tokens, e.cost_usd
		FROM usage_events e JOIN sessions s ON s.id = e.session_id
		WHERE e.ts >= ? AND e.ts < ?`, startTS, endTS, agent, accumulateUsage)
	if err != nil {
		return nil, err
	}

	// Preserve totals for older/imported sessions that only have the aggregate
	// usage row. Once per-request events exist, they are the source of truth.
	err = forEachPeriodRow(db, `SELECT s.created_ts, s.id,
		u.input_tokens + u.cache_create_tokens + u.cache_read_tokens,
		u.output_tokens, u.cache_read_tokens, u.cost_usd
		FROM usage u JOIN sessions s ON s.id = u.session_id
		WHERE s.created_ts >= ? AND s.created_ts < ?
		AND NOT EXISTS (SELECT 1 FROM usage_events e WHERE e.session_id = u.session_id)`,
		startTS, endTS, agent, accumulateUsage)
	if err != nil {
		return nil, err
	}

	for day, sessions := range sessionsByDay {
		dayMap[day].Sessions = len(sessions)
	}

	// Fill missing periods so charts show real zero-usage gaps.
	startValue := time.Unix(startTS, 0).In(loc)
	lastIncludedTS := endTS
	if endTS > startTS {
		lastIncludedTS--
	}
	endValue := time.Unix(lastIncludedTS, 0).In(loc)
	startDay := time.Date(startValue.Year(), startValue.Month(), startValue.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(endValue.Year(), endValue.Month(), endValue.Day(), 0, 0, 0, 0, loc)
	if hourly {
		startDay = time.Date(startValue.Year(), startValue.Month(), startValue.Day(), startValue.Hour(), 0, 0, 0, loc)
		endDay = time.Date(endValue.Year(), endValue.Month(), endValue.Day(), endValue.Hour(), 0, 0, 0, loc)
	}
	var results []DayStatsResult
	for d := startDay; !d.After(endDay); {
		key := d.Format(layout)
		if r, ok := dayMap[key]; ok {
			results = append(results, *r)
		} else {
			results = append(results, DayStatsResult{Date: key})
		}
		if hourly {
			d = d.Add(time.Hour)
		} else {
			d = d.AddDate(0, 0, 1)
		}
	}
	return results, nil
}

type TodoBoundSession struct {
	SessionID string `json:"session_id"`
	// IndexedID is the transcript index's own id for the session, which differs
	// from SessionID for codex (the ledger stores the thread uuid, the index the
	// rollout filename). Empty when the session was never indexed.
	IndexedID    string  `json:"indexed_id,omitempty"`
	ShortID      string  `json:"short_id"`
	Agent        string  `json:"agent"`
	Project      string  `json:"project"`
	Summary string `json:"summary,omitempty"`
	// LatestResult is the session's last assistant message, so a Todo can show
	// what the Agent concluded without depending on the session still being
	// inside the live-status window. Capped because a bound-session list is a
	// list, not a transcript.
	LatestResult string `json:"latest_result,omitempty"`
	Indexed      bool   `json:"indexed"`
	CWD          string `json:"cwd,omitempty"`
	StartedAt    int64   `json:"started_at,omitempty"`
	LastAt       int64   `json:"last_at,omitempty"`
	BindingCount int     `json:"binding_count"`
	FirstBoundAt int64   `json:"first_bound_at"`
	BoundAt      int64   `json:"bound_at"`
	UnboundAt    *int64  `json:"unbound_at,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	Queries      int     `json:"queries"`
	ToolCalls    int     `json:"tool_calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

// FindSessionsForTodo returns the distinct sessions explicitly bound to a todo.
// The binding ledger is authoritative: a session need not have been indexed yet,
// and deleting an indexed transcript must not erase the work-state relationship.
// Session metadata and usage are therefore attached with a LEFT JOIN. Codex
// bindings use the thread UUID while the transcript index keeps the rollout
// filename as its id, whose final component is that same UUID.
func FindSessionsForTodo(db *sql.DB, todoID string) ([]TodoBoundSession, error) {
	query := `SELECT b.session_id,
		COALESCE(s.id, '') AS indexed_id,
		COALESCE(NULLIF(s.short_id, ''), SUBSTR(b.session_id, 1, 8)) AS short_id,
		COALESCE(NULLIF(b.agent, ''), s.agent, '') AS agent,
		COALESCE(NULLIF(b.project, ''), s.project, '') AS project,
		COALESCE(s.summary, '') AS summary,
		COALESCE((SELECT SUBSTR(m.content, 1, 2000) FROM messages m
			WHERE m.session_id = s.id AND m.role = 'assistant'
			ORDER BY m.seq DESC LIMIT 1), '') AS latest_result,
		CASE WHEN s.id IS NULL THEN 0 ELSE 1 END AS indexed,
		b.cwd,
		COALESCE(s.created_ts, 0) AS started_at,
		COALESCE(s.last_ts, 0) AS last_at,
		(SELECT COUNT(*) FROM todo_session_bindings bc
			WHERE bc.todo_id = b.todo_id AND bc.session_id = b.session_id) AS binding_count,
		(SELECT MIN(bf.bound_at) FROM todo_session_bindings bf
			WHERE bf.todo_id = b.todo_id AND bf.session_id = b.session_id) AS first_bound_at,
		b.bound_at, b.unbound_at, b.reason,
		(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id AND m.role = 'user') AS queries,
		COALESCE((SELECT SUM(t.count) FROM tools t WHERE t.session_id = s.id), 0) AS tool_calls,
		COALESCE((SELECT u.input_tokens + u.cache_create_tokens + u.cache_read_tokens FROM usage u WHERE u.session_id = s.id), 0) AS input_tokens,
		COALESCE((SELECT u.output_tokens FROM usage u WHERE u.session_id = s.id), 0) AS output_tokens,
		COALESCE((SELECT u.cost_usd FROM usage u WHERE u.session_id = s.id), 0) AS cost_usd
		FROM todo_session_bindings b
		LEFT JOIN sessions s ON s.id = (
			SELECT sm.id FROM sessions sm
			WHERE sm.id = b.session_id
				OR (sm.agent = 'codex' AND sm.id LIKE '%-' || b.session_id)
			ORDER BY CASE WHEN sm.id = b.session_id THEN 0 ELSE 1 END, sm.last_ts DESC
			LIMIT 1)
		WHERE b.todo_id = ?
			AND b.id = (SELECT MAX(bl.id) FROM todo_session_bindings bl
				WHERE bl.todo_id = b.todo_id AND bl.session_id = b.session_id)
		ORDER BY first_bound_at, b.id`

	rows, err := db.Query(query, todoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TodoBoundSession
	for rows.Next() {
		var r TodoBoundSession
		if err := rows.Scan(&r.SessionID, &r.IndexedID, &r.ShortID, &r.Agent, &r.Project, &r.Summary,
			&r.LatestResult,
			&r.Indexed, &r.CWD, &r.StartedAt, &r.LastAt, &r.BindingCount, &r.FirstBoundAt, &r.BoundAt,
			&r.UnboundAt, &r.Reason,
			&r.Queries, &r.ToolCalls, &r.InputTokens, &r.OutputTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		r.Project = config.CanonicalProject(r.Project)
		results = append(results, r)
	}
	return results, rows.Err()
}

// EarliestUserMessages returns the first perSession user messages of each given
// session, oldest first. Callers name a session from its opening prompt, and
// the opening prompt is often not the first stored message: agents inject
// plugin lists and instruction preambles as "user" turns, so more than one
// candidate has to come back for the caller to filter.
func EarliestUserMessages(db *sql.DB, sessionIDs []string, perSession int) (map[string][]string, error) {
	messages := make(map[string][]string, len(sessionIDs))
	if len(sessionIDs) == 0 || perSession <= 0 {
		return messages, nil
	}

	placeholders := make([]string, 0, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	if len(args) == 0 {
		return messages, nil
	}

	query := `SELECT session_id, content FROM messages
		WHERE role = 'user' AND session_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY session_id, seq`
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID, content string
		if err := rows.Scan(&sessionID, &content); err != nil {
			return nil, err
		}
		if len(messages[sessionID]) >= perSession {
			continue
		}
		messages[sessionID] = append(messages[sessionID], content)
	}
	return messages, rows.Err()
}

// AgentDisplayName is the one place an agent key becomes a human label. It was
// two: internal/cmd carried a copy that knew about pi but matched case
// sensitively, while this one folded case but had never learned pi, so the same
// agent read as "Pi" in one command and "pi" in another.
func AgentDisplayName(agent string) string {
	switch strings.ToLower(agent) {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "copilot":
		return "GitHub Copilot"
	case "pi":
		return "Pi"
	case "qoder":
		return "Qoder"
	case "qodercli":
		return "Qoder CLI"
	case "qoderwork":
		return "QoderWork"
	case "grokbuild":
		return "Grok Build"
	}
	return agent
}
