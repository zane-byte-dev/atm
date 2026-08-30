package store

import (
	"database/sql"
	"math"
	"sort"
)

// Speed answers two different questions about how fast the work goes, because
// they have different answers and users care about both:
//
//   - throughput: output tokens per second inside one model request, which is
//     the model's own pace with tool execution excluded
//   - turn wait: how long a human waits between sending a message and the model
//     finishing its last reply to it, tool execution and every internal request
//     included
//
// Only Grok reports how long its calls took; every other agent's duration is
// derived from record timestamps, so some requests cannot be measured at all —
// see usage_events.duration_ms.

// Sampling window for throughput, applied per model call. A request whose
// measurement falls outside it is not a slow or fast model, it is a broken
// measurement: durations under speedMinDurationMS are dominated by the resolution
// of the log's own writes, and one over speedMaxDurationMS means the record
// before the response was not the request's real start (an idle session picked up
// again, an interrupted turn). Tiny responses are excluded too — per-request
// overhead swamps the generation rate and makes them read as absurdly slow.
//
// Per call, not per row, because a row can cover several: Grok reports one turn's
// API time across all the calls in it, and a five-call turn taking two minutes is
// an ordinary turn, not an outlier.
const (
	speedMinDurationMS   = 200
	speedMaxDurationMS   = 600_000
	speedMinOutputTokens = 20
)

// SpeedStatsResult is one model's throughput within the queried window.
type SpeedStatsResult struct {
	Client   string `json:"client"`
	Model    string `json:"model"`
	Requests int    `json:"requests"`
	// Sampled is how many of those requests carried a usable measurement, counted
	// the same way as Requests so the two can be compared.
	Sampled int `json:"sampled"`
	// TokensPerSecond percentiles describe one measurement each: a single request
	// for the agents that report per request, and one turn's calls together for
	// Grok, which reports a turn's API time as a whole. Weighted is the range's
	// overall rate — total sampled output over total sampled time — and is the
	// figure that stays correct when rows are aggregated further.
	TokensPerSecondP50      float64 `json:"tokens_per_second_p50"`
	TokensPerSecondP90      float64 `json:"tokens_per_second_p90"`
	TokensPerSecondWeighted float64 `json:"tokens_per_second_weighted"`
	DurationP50Seconds      float64 `json:"duration_p50_seconds"`
	DurationP90Seconds      float64 `json:"duration_p90_seconds"`
	// OutputTokens and SampledSeconds cover the sampled requests only. They are
	// the two sums a reader needs to combine rows — merging models, or merging a
	// filtered subset — without averaging rates against each other.
	OutputTokens   int64   `json:"output_tokens"`
	SampledSeconds float64 `json:"sampled_seconds"`
}

// TurnWaitStatsResult is one agent's human-facing wait within the queried window.
type TurnWaitStatsResult struct {
	Agent string `json:"agent"`
	Turns int    `json:"turns"`
	// Percentiles rather than a mean: an interrupted turn keeps running until the
	// next message arrives, so the tail is not a wait anyone actually sat through.
	WaitP50Seconds  float64 `json:"wait_p50_seconds"`
	WaitP90Seconds  float64 `json:"wait_p90_seconds"`
	WaitMaxSeconds  float64 `json:"wait_max_seconds"`
	RequestsPerTurn float64 `json:"requests_per_turn"`
}

// SpeedReport is everything `atm stats --by speed` shows, including what it had
// to leave out. Untimed and OutOfWindow are reported rather than dropped
// silently: they are the difference between "the model is this fast" and "this is
// as much as the transcripts could tell us".
type SpeedReport struct {
	Models []SpeedStatsResult    `json:"models"`
	Turns  []TurnWaitStatsResult `json:"turns"`
	// Untimed counts requests whose transcript did not bound a window at all.
	Untimed int `json:"untimed_requests"`
	// OutOfWindow counts timed requests rejected by the sampling window above.
	OutOfWindow int `json:"out_of_window_requests"`
}

type speedSample struct {
	durationSeconds float64
	outputTokens    int64
	// calls is how many model calls the sample covers: 1 for the agents that
	// report per request, a turn's worth for Grok. The rate is the same either
	// way; this is what keeps the sampled count comparable to the request count.
	calls int
}

// withinSpeedWindow judges a measurement per model call, so a row covering
// several is held to the same standard as a row covering one.
func withinSpeedWindow(durationMS, outputTokens int64, calls int) bool {
	if calls < 1 {
		calls = 1
	}
	perCall := durationMS / int64(calls)
	return perCall >= speedMinDurationMS && perCall <= speedMaxDurationMS &&
		outputTokens/int64(calls) >= speedMinOutputTokens
}

// GetSpeedStats collects both speed views for the window. Sessions are selected
// the way the other per-request stats do it, by the request's own timestamp, so
// a long-running session contributes to the days it was actually working.
func GetSpeedStats(db *sql.DB, startTS, endTS int64, agent string) (SpeedReport, error) {
	report := SpeedReport{}
	query := `SELECT s.agent, e.model, COALESCE(e.request_count, 1), e.output_tokens, e.duration_ms
		FROM usage_events e JOIN sessions s ON e.session_id = s.id
		WHERE s.is_internal = 0 AND e.ts >= ? AND e.ts < ? AND e.model != ''`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return report, err
	}
	defer rows.Close()

	type modelKey struct {
		client string
		model  string
	}
	requests := map[modelKey]int{}
	samples := map[modelKey][]speedSample{}
	order := []modelKey{}
	for rows.Next() {
		var client, model string
		var count int
		var outputTokens, durationMS int64
		if err := rows.Scan(&client, &model, &count, &outputTokens, &durationMS); err != nil {
			return report, err
		}
		if count <= 0 {
			count = 1
		}
		key := modelKey{client: client, model: model}
		if _, seen := requests[key]; !seen {
			order = append(order, key)
		}
		requests[key] += count
		switch {
		case durationMS <= 0:
			report.Untimed += count
		case !withinSpeedWindow(durationMS, outputTokens, count):
			report.OutOfWindow += count
		default:
			samples[key] = append(samples[key], speedSample{
				durationSeconds: float64(durationMS) / 1000,
				outputTokens:    outputTokens,
				calls:           count,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return report, err
	}

	for _, key := range order {
		result := SpeedStatsResult{
			Client:   key.client,
			Model:    key.model,
			Requests: requests[key],
		}
		rates := make([]float64, 0, len(samples[key]))
		durations := make([]float64, 0, len(samples[key]))
		var totalSeconds float64
		for _, sample := range samples[key] {
			rates = append(rates, float64(sample.outputTokens)/sample.durationSeconds)
			// Per call, so the column reads as one request's time whether the row
			// covered one call or a turn's worth.
			durations = append(durations, sample.durationSeconds/float64(sample.calls))
			totalSeconds += sample.durationSeconds
			result.OutputTokens += sample.outputTokens
			result.Sampled += sample.calls
		}
		sort.Float64s(rates)
		sort.Float64s(durations)
		result.TokensPerSecondP50 = percentile(rates, 0.5)
		result.TokensPerSecondP90 = percentile(rates, 0.9)
		result.DurationP50Seconds = percentile(durations, 0.5)
		result.DurationP90Seconds = percentile(durations, 0.9)
		result.SampledSeconds = totalSeconds
		if totalSeconds > 0 {
			result.TokensPerSecondWeighted = float64(result.OutputTokens) / totalSeconds
		}
		report.Models = append(report.Models, result)
	}
	sort.SliceStable(report.Models, func(i, j int) bool {
		if report.Models[i].Requests != report.Models[j].Requests {
			return report.Models[i].Requests > report.Models[j].Requests
		}
		if report.Models[i].Client != report.Models[j].Client {
			return report.Models[i].Client < report.Models[j].Client
		}
		return report.Models[i].Model < report.Models[j].Model
	})

	turns, err := getTurnWaitStats(db, startTS, endTS, agent)
	if err != nil {
		return report, err
	}
	report.Turns = turns
	return report, nil
}

// getTurnWaitStats reconstructs turns from what is already stored: a turn starts
// at a human message and ends at the end of the last model response before the
// next one. No turn table is needed — messages carries the human timestamps and
// usage_events carries the responses.
//
// A turn with no request in it is not counted. That covers messages the model
// never answered (the session ended, the request failed) as well as anything
// whose usage the transcript did not report, neither of which is a wait to
// average into the rest.
func getTurnWaitStats(db *sql.DB, startTS, endTS int64, agent string) ([]TurnWaitStatsResult, error) {
	query := `SELECT s.agent, s.id, m.ts
		FROM messages m JOIN sessions s ON m.session_id = s.id
		WHERE s.is_internal = 0 AND s.is_subagent = 0
			AND m.role = 'user' AND m.scope = 'local' AND m.kind = 'conversation'
			AND m.ts >= ? AND m.ts < ?`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY s.id, m.ts, m.seq"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type turnStart struct {
		agent string
		ts    int64
	}
	starts := map[string][]turnStart{}
	for rows.Next() {
		var client, sessionID string
		var ts int64
		if err := rows.Scan(&client, &sessionID, &ts); err != nil {
			return nil, err
		}
		if ts <= 0 {
			continue
		}
		starts[sessionID] = append(starts[sessionID], turnStart{agent: client, ts: ts})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(starts) == 0 {
		return nil, nil
	}

	endsBySession, err := requestEndsBySession(db, startTS, endTS, agent)
	if err != nil {
		return nil, err
	}

	sessionIDs := make([]string, 0, len(starts))
	for id := range starts {
		sessionIDs = append(sessionIDs, id)
	}
	sort.Strings(sessionIDs)

	waits := map[string][]float64{}
	turnRequests := map[string]int{}
	for _, sessionID := range sessionIDs {
		ends := endsBySession[sessionID]
		if len(ends) == 0 {
			continue
		}
		turns := starts[sessionID]
		for i, turn := range turns {
			limit := int64(1 << 62)
			if i+1 < len(turns) {
				limit = turns[i+1].ts
			}
			var lastEnd int64
			calls := 0
			for _, reply := range ends {
				if reply.ts < turn.ts || reply.ts >= limit {
					continue
				}
				// Model calls rather than rows: a Grok row covers a turn's worth.
				calls += reply.calls
				if reply.ts > lastEnd {
					lastEnd = reply.ts
				}
			}
			if calls == 0 || lastEnd <= turn.ts {
				continue
			}
			waits[turn.agent] = append(waits[turn.agent], float64(lastEnd-turn.ts))
			turnRequests[turn.agent] += calls
		}
	}

	agents := make([]string, 0, len(waits))
	for client := range waits {
		agents = append(agents, client)
	}
	sort.Strings(agents)
	results := make([]TurnWaitStatsResult, 0, len(agents))
	for _, client := range agents {
		values := waits[client]
		sort.Float64s(values)
		result := TurnWaitStatsResult{
			Agent:          client,
			Turns:          len(values),
			WaitP50Seconds: percentile(values, 0.5),
			WaitP90Seconds: percentile(values, 0.9),
			WaitMaxSeconds: values[len(values)-1],
		}
		result.RequestsPerTurn = float64(turnRequests[client]) / float64(len(values))
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Turns > results[j].Turns })
	return results, nil
}

// turnReply is one response inside a turn: when it finished, and how many model
// calls it accounts for.
type turnReply struct {
	ts    int64
	calls int
}

// requestEndsBySession reads every request in the window once, grouped by
// session, as the Unix second each response finished.
//
// A usage event's ts is when its transcript recorded the finished response — the
// assistant record for Claude and pi, the token_count for Codex — so it is
// already the end of the request and the measured window must not be added to
// it. That caps turn waits at second resolution, which a wait measured in tens of
// seconds can afford.
//
// Requests are bounded by the same window as the turn starts, so a turn opened
// just before the window closes reports only the replies inside it.
func requestEndsBySession(db *sql.DB, startTS, endTS int64, agent string) (map[string][]turnReply, error) {
	query := `SELECT COALESCE(root.id, e.session_id),
		e.ts, COALESCE(e.request_count, 1)
		FROM usage_events e JOIN sessions s ON e.session_id = s.id
		LEFT JOIN sessions root ON s.root_session_id <> ''
			AND (root.resume_id = s.root_session_id OR root.id = s.root_session_id)
		WHERE s.is_internal = 0 AND e.ts >= ? AND e.ts < ?`
	args := []any{startTS, endTS}
	if agent != "" {
		query += " AND s.agent = ?"
		args = append(args, agent)
	}
	query += " ORDER BY e.session_id, e.ts"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ends := map[string][]turnReply{}
	for rows.Next() {
		var sessionID string
		var ts int64
		var calls int
		if err := rows.Scan(&sessionID, &ts, &calls); err != nil {
			return nil, err
		}
		if ts <= 0 {
			continue
		}
		if calls < 1 {
			calls = 1
		}
		ends[sessionID] = append(ends[sessionID], turnReply{ts: ts, calls: calls})
	}
	return ends, rows.Err()
}

// percentile picks the nearest-rank value of a sorted slice: the smallest sample
// at or above which p of the samples fall. No interpolation between neighbours —
// with tens to thousands of requests that would add precision the underlying
// timestamps do not have. Ranking up rather than down matters at p90, where
// truncating would report the 3rd of 4 samples as the 90th percentile.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
