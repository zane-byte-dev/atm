package aiday

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"
)

var ErrDayNotBuilt = errors.New("AI Day has not been built")

// Rebuild regenerates every local calendar day in the inclusive range. Days are
// processed oldest-first so a range rebuild has the same rolling baseline as a
// sequence of one-day rebuilds.
func Rebuild(ctx context.Context, db *sql.DB, from, to time.Time, loc *time.Location) (RebuildSummary, error) {
	from = dayStart(from, loc)
	to = dayStart(to, loc)
	if to.Before(from) {
		return RebuildSummary{}, fmt.Errorf("end day %s is before start day %s", to.Format(time.DateOnly), from.Format(time.DateOnly))
	}

	summary := RebuildSummary{
		SchemaVersion: ContractVersion,
		From:          from.Format(time.DateOnly),
		To:            to.Format(time.DateOnly),
	}
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		result, err := RebuildDay(ctx, db, day, loc)
		if err != nil {
			return RebuildSummary{}, fmt.Errorf("rebuild %s: %w", day.Format(time.DateOnly), err)
		}
		summary.Days = append(summary.Days, result)
	}
	summary.Count = len(summary.Days)
	if _, err := PruneEvents(ctx, db, time.Now()); err != nil {
		return RebuildSummary{}, err
	}
	return summary, nil
}

func RebuildDay(ctx context.Context, db *sql.DB, day time.Time, loc *time.Location) (Result, error) {
	start := dayStart(day, loc)
	end := start.AddDate(0, 0, 1)
	if err := normalizeDay(ctx, db, start, end); err != nil {
		return Result{}, fmt.Errorf("normalize events: %w", err)
	}
	features, err := aggregate(ctx, db, start, end, loc)
	if err != nil {
		return Result{}, err
	}
	baseline, err := loadBaseline(ctx, db, features.Day, 30)
	if err != nil {
		return Result{}, err
	}
	result := selectReward(ctx, db, features, baseline, time.Now().Unix())
	if err := save(ctx, db, result); err != nil {
		return Result{}, err
	}
	if err := saveRewardState(ctx, db, result); err != nil {
		return Result{}, err
	}
	return Load(ctx, db, result.Day)
}

func Load(ctx context.Context, db *sql.DB, day string) (Result, error) {
	var result Result
	var conceptID, title, explanation, tagsJSON, evidenceJSON string
	var confidence float64
	err := db.QueryRowContext(ctx, `
		SELECT f.day, f.timezone, f.session_count, f.turn_count, f.tool_calls,
		       f.source_count, f.input_tokens, f.output_tokens,
		       f.cache_create_tokens, f.cache_read_tokens, f.generation_seconds,
		       f.built_at, f.feature_version,
		       r.state, r.concept_id, r.title, r.explanation, r.tags_json,
		       r.evidence_json, r.confidence, r.baseline_days,
		       r.generated_at, r.engine_version
		FROM ai_day_features f
		JOIN ai_day_results r ON r.day = f.day
		WHERE f.day = ?`, day).Scan(
		&result.Day, &result.Timezone,
		&result.Features.SessionCount, &result.Features.TurnCount, &result.Features.ToolCalls,
		&result.Features.SourceCount, &result.Features.InputTokens, &result.Features.OutputTokens,
		&result.Features.CacheCreateTokens, &result.Features.CacheReadTokens,
		&result.Features.GenerationSeconds, &result.Features.BuiltAt, &result.Features.FeatureVersion,
		&result.State, &conceptID, &title, &explanation, &tagsJSON, &evidenceJSON,
		&confidence, &result.BaselineDays, &result.GeneratedAt, &result.EngineVersion,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrDayNotBuilt
	}
	if err != nil {
		return Result{}, err
	}

	result.SchemaVersion = ContractVersion
	result.Features.Day = result.Day
	result.Features.Timezone = result.Timezone
	if conceptID != "" {
		concept := &Concept{ID: conceptID, Title: title, Explanation: explanation, Confidence: confidence}
		if err := json.Unmarshal([]byte(tagsJSON), &concept.Tags); err != nil {
			return Result{}, fmt.Errorf("decode tags: %w", err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &concept.Evidence); err != nil {
			return Result{}, fmt.Errorf("decode evidence: %w", err)
		}
		result.Concept = concept
	}
	if result.State != "empty" {
		baseline, err := loadBaseline(ctx, db, day, 30)
		if err != nil {
			return Result{}, err
		}
		result.Percentiles = percentiles(result.Features, baseline)
	}
	if err := loadFeatureDetails(ctx, db, &result); err != nil {
		return Result{}, err
	}
	if err := loadSelectedBadge(ctx, db, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func aggregateLegacy(ctx context.Context, db *sql.DB, start, end time.Time, loc *time.Location) (Features, error) {
	f := Features{
		Day:            start.Format(time.DateOnly),
		Timezone:       loc.String(),
		BuiltAt:        time.Now().Unix(),
		FeatureVersion: FeatureVersion,
	}
	startTS, endTS := start.Unix(), end.Unix()

	// A session is active on a day if the session itself, one of its messages, or
	// one of its usage events lands in that day. UNION prevents one session from
	// being counted more than once when all three are present.
	err := db.QueryRowContext(ctx, `
		WITH active AS (
			SELECT id, agent FROM sessions WHERE created_ts >= ? AND created_ts < ?
			UNION
			SELECT s.id, s.agent FROM sessions s JOIN messages m ON m.session_id = s.id
			 WHERE m.ts >= ? AND m.ts < ?
			UNION
			SELECT s.id, s.agent FROM sessions s JOIN usage_events u ON u.session_id = s.id
			 WHERE u.ts >= ? AND u.ts < ?
		)
		SELECT COUNT(*), COUNT(DISTINCT agent) FROM active`,
		startTS, endTS, startTS, endTS, startTS, endTS).
		Scan(&f.SessionCount, &f.SourceCount)
	if err != nil {
		return Features{}, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE role = 'user' AND ts >= ? AND ts < ?`, startTS, endTS).Scan(&f.TurnCount); err != nil {
		return Features{}, err
	}
	// Tool rows are per-session aggregates and have no event timestamp in the
	// current parser contract. Attribute them only to sessions first created on
	// this day; this is deterministic and avoids duplicating a long session's
	// lifetime total across every day it spans.
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(t.count), 0)
		FROM tools t JOIN sessions s ON s.id = t.session_id
		WHERE s.created_ts >= ? AND s.created_ts < ?`, startTS, endTS).Scan(&f.ToolCalls); err != nil {
		return Features{}, err
	}
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cache_create_tokens), 0), COALESCE(SUM(cache_read_tokens), 0),
		       COALESCE(SUM(duration_ms), 0) / 1000
		FROM usage_events WHERE ts >= ? AND ts < ?`, startTS, endTS).Scan(
		&f.InputTokens, &f.OutputTokens, &f.CacheCreateTokens, &f.CacheReadTokens, &f.GenerationSeconds,
	); err != nil {
		return Features{}, err
	}
	return f, nil
}

func loadBaseline(ctx context.Context, db *sql.DB, before string, limit int) ([]Features, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT day, timezone, session_count, turn_count, tool_calls, source_count,
		       input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
		       generation_seconds, built_at, feature_version
		FROM ai_day_features
		WHERE day < ? AND (session_count > 0 OR turn_count > 0 OR tool_calls > 0
		                   OR input_tokens > 0 OR output_tokens > 0
		                   OR cache_create_tokens > 0 OR cache_read_tokens > 0)
		ORDER BY day DESC LIMIT ?`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Features
	for rows.Next() {
		var f Features
		if err := rows.Scan(&f.Day, &f.Timezone, &f.SessionCount, &f.TurnCount, &f.ToolCalls,
			&f.SourceCount, &f.InputTokens, &f.OutputTokens, &f.CacheCreateTokens,
			&f.CacheReadTokens, &f.GenerationSeconds, &f.BuiltAt, &f.FeatureVersion); err != nil {
			return nil, err
		}
		values = append(values, f)
	}
	return values, rows.Err()
}

func selectConcept(features Features, baseline []Features, generatedAt int64) Result {
	result := Result{
		SchemaVersion: ContractVersion,
		Day:           features.Day,
		State:         "ready",
		Timezone:      features.Timezone,
		Features:      features,
		BaselineDays:  len(baseline),
		GeneratedAt:   generatedAt,
		EngineVersion: EngineVersion,
	}
	if features.Empty() {
		result.State = "empty"
		return result
	}
	p := percentiles(features, baseline)
	result.Percentiles = p
	confidence := math.Min(0.55+float64(len(baseline))*0.4/30, 0.95)

	type candidate struct {
		score   float64
		concept Concept
	}
	var candidates []candidate
	add := func(score float64, id, title, explanation string, tags []string, evidence []Evidence) {
		candidates = append(candidates, candidate{score: score, concept: Concept{
			ID: id, Title: title, Explanation: explanation, Tags: tags,
			Evidence: evidence, Confidence: round(confidence),
		}})
	}

	if features.SourceCount >= 2 {
		add(0.72+math.Min(float64(features.SourceCount-2)*0.03, 0.12),
			"multi_agent_orchestrator", "多智能体编排的一天",
			"你在多个 AI 来源之间切换并推进工作，今天的核心能力是协调与编排。",
			[]string{"multi-agent", "orchestration"}, []Evidence{
				{Metric: "source_count", Value: float64(features.SourceCount), Unit: "agents"},
				{Metric: "session_count", Value: float64(features.SessionCount), Unit: "sessions"},
			})
	}
	if (len(baseline) >= 5 && p["turn_count"] >= 0.8) || features.TurnCount >= 20 {
		add(0.65+p["turn_count"]*0.2, "deep_collaboration", "深度共创的一天",
			"连续多轮交互构成了今天的主旋律，你和 AI 共同把问题推向了更深处。",
			[]string{"collaboration", "deep-work"}, []Evidence{
				{Metric: "turn_count", Value: float64(features.TurnCount), Unit: "turns", Comparison: percentileText(p["turn_count"])},
				{Metric: "generation_seconds", Value: float64(features.GenerationSeconds), Unit: "seconds"},
			})
	}
	if (len(baseline) >= 5 && p["total_tokens"] >= 0.8) || (len(baseline) < 5 && features.TotalTokens() >= 1_000_000) {
		add(0.6+p["total_tokens"]*0.25, "high_load", "高负载推进的一天",
			"今天处理的信息量明显偏高，AI 承担了密集的理解与生成工作。",
			[]string{"high-load", "throughput"}, []Evidence{
				{Metric: "total_tokens", Value: float64(features.TotalTokens()), Unit: "tokens", Comparison: percentileText(p["total_tokens"])},
				{Metric: "output_tokens", Value: float64(features.OutputTokens), Unit: "tokens"},
			})
	}
	if len(baseline) >= 5 && p["total_tokens"] <= 0.2 && p["turn_count"] <= 0.2 {
		add(0.7, "ai_rest_day", "AI 低负载的一天",
			"相对你近期的使用节奏，今天更轻量，给注意力留出了恢复空间。",
			[]string{"low-load", "recovery"}, []Evidence{
				{Metric: "total_tokens", Value: float64(features.TotalTokens()), Unit: "tokens", Comparison: percentileText(p["total_tokens"])},
				{Metric: "turn_count", Value: float64(features.TurnCount), Unit: "turns", Comparison: percentileText(p["turn_count"])},
			})
	}
	if len(candidates) == 0 {
		add(0.55, "steady_collaboration", "稳定共创的一天",
			"今天保持了稳定的 AI 协作节奏，推进来自持续而清晰的互动。",
			[]string{"steady", "collaboration"}, []Evidence{
				{Metric: "session_count", Value: float64(features.SessionCount), Unit: "sessions"},
				{Metric: "turn_count", Value: float64(features.TurnCount), Unit: "turns"},
			})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].concept.ID < candidates[j].concept.ID
		}
		return candidates[i].score > candidates[j].score
	})
	result.Concept = &candidates[0].concept
	return result
}

func percentiles(current Features, baseline []Features) map[string]float64 {
	result := map[string]float64{
		"turn_count":   0.5,
		"tool_calls":   0.5,
		"total_tokens": 0.5,
	}
	if len(baseline) == 0 {
		return result
	}
	turns := make([]int64, 0, len(baseline))
	tools := make([]int64, 0, len(baseline))
	tokens := make([]int64, 0, len(baseline))
	for _, f := range baseline {
		turns = append(turns, f.TurnCount)
		tools = append(tools, f.ToolCalls)
		tokens = append(tokens, f.TotalTokens())
	}
	result["turn_count"] = rank(current.TurnCount, turns)
	result["tool_calls"] = rank(current.ToolCalls, tools)
	result["total_tokens"] = rank(current.TotalTokens(), tokens)
	return result
}

func rank(value int64, baseline []int64) float64 {
	belowOrEqual := 0
	for _, candidate := range baseline {
		if candidate <= value {
			belowOrEqual++
		}
	}
	return round(float64(belowOrEqual) / float64(len(baseline)))
}

func save(ctx context.Context, db *sql.DB, result Result) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	f := result.Features
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_day_features
		(day, timezone, session_count, turn_count, tool_calls, source_count,
		 input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
		 generation_seconds, built_at, feature_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET
		 timezone=excluded.timezone, session_count=excluded.session_count,
		 turn_count=excluded.turn_count, tool_calls=excluded.tool_calls,
		 source_count=excluded.source_count, input_tokens=excluded.input_tokens,
		 output_tokens=excluded.output_tokens, cache_create_tokens=excluded.cache_create_tokens,
		 cache_read_tokens=excluded.cache_read_tokens,
		 generation_seconds=excluded.generation_seconds, built_at=excluded.built_at,
		 feature_version=excluded.feature_version`,
		f.Day, f.Timezone, f.SessionCount, f.TurnCount, f.ToolCalls, f.SourceCount,
		f.InputTokens, f.OutputTokens, f.CacheCreateTokens, f.CacheReadTokens,
		f.GenerationSeconds, f.BuiltAt, f.FeatureVersion)
	if err != nil {
		return err
	}
	concept := Concept{Tags: []string{}, Evidence: []Evidence{}}
	if result.Concept != nil {
		concept = *result.Concept
	}
	tagsJSON, err := json.Marshal(concept.Tags)
	if err != nil {
		return err
	}
	evidenceJSON, err := json.Marshal(concept.Evidence)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_day_results
		(day, state, concept_id, title, explanation, tags_json, evidence_json,
		 confidence, baseline_days, generated_at, engine_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET
		 state=excluded.state, concept_id=excluded.concept_id, title=excluded.title,
		 explanation=excluded.explanation, tags_json=excluded.tags_json,
		 evidence_json=excluded.evidence_json, confidence=excluded.confidence,
		 baseline_days=excluded.baseline_days, generated_at=excluded.generated_at,
		 engine_version=excluded.engine_version`,
		result.Day, result.State, concept.ID, concept.Title, concept.Explanation,
		string(tagsJSON), string(evidenceJSON), concept.Confidence, result.BaselineDays,
		result.GeneratedAt, result.EngineVersion)
	if err != nil {
		return err
	}
	semanticJSON, err := json.Marshal(f.SemanticCounts)
	if err != nil {
		return err
	}
	modalityJSON, err := json.Marshal(f.ModalityCounts)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ai_day_feature_details(day,event_count,active_seconds,foreground_seconds,background_seconds,semantic_counts_json,modality_counts_json) VALUES (?,?,?,?,?,?,?) ON CONFLICT(day) DO UPDATE SET event_count=excluded.event_count,active_seconds=excluded.active_seconds,foreground_seconds=excluded.foreground_seconds,background_seconds=excluded.background_seconds,semantic_counts_json=excluded.semantic_counts_json,modality_counts_json=excluded.modality_counts_json`, f.Day, f.EventCount, f.ActiveSeconds, f.ForegroundSeconds, f.BackgroundSeconds, string(semanticJSON), string(modalityJSON))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func dayStart(value time.Time, loc *time.Location) time.Time {
	value = value.In(loc)
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, loc)
}

func round(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func percentileText(value float64) string {
	return "recent_p" + strconv.Itoa(int(math.Round(value*100)))
}
