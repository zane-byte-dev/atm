package aiday

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// Refresh is the zero-configuration entry point used by the browser AI Day
// snapshot. It fills in any missing day in the baseline window and
// always rebuilds the current day, but leaves already-built past days alone.
// Rebuilding the whole window on every read meant opening the workspace could silently
// rewrite last month's badges; changing history stays an explicit `day rebuild`.
func Refresh(ctx context.Context, db *sql.DB, now time.Time, loc *time.Location, windowDays int) (RebuildSummary, error) {
	today := dayStart(now, loc)
	if err := EnsureCollectionStart(ctx, db, today.Format(time.DateOnly)); err != nil {
		return RebuildSummary{}, err
	}
	built, err := builtDays(ctx, db, today.AddDate(0, 0, -windowDays).Format(time.DateOnly), today.Format(time.DateOnly))
	if err != nil {
		return RebuildSummary{}, err
	}
	summary := RebuildSummary{SchemaVersion: ContractVersion, From: today.AddDate(0, 0, -windowDays).Format(time.DateOnly), To: today.Format(time.DateOnly)}
	for i := windowDays; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		key := day.Format(time.DateOnly)
		if i > 0 && built[key] {
			continue
		}
		result, err := RebuildDay(ctx, db, day, loc)
		if err != nil {
			return RebuildSummary{}, fmt.Errorf("refresh %s: %w", key, err)
		}
		summary.Days = append(summary.Days, result)
	}
	summary.Count = len(summary.Days)
	if _, err := PruneEvents(ctx, db, now); err != nil {
		return RebuildSummary{}, err
	}
	if len(summary.Days) == 0 {
		result, err := Load(ctx, db, today.Format(time.DateOnly))
		if err != nil {
			return RebuildSummary{}, err
		}
		summary.Days, summary.Count = []Result{result}, 1
	}
	return summary, nil
}

func builtDays(ctx context.Context, db *sql.DB, from, to string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT day FROM ai_day_features WHERE day>=? AND day<=?`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	built := map[string]bool{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return nil, err
		}
		built[day] = true
	}
	return built, rows.Err()
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
	provisional := isProvisional(start, time.Now(), loc)
	coverage, err := dayCoverage(ctx, db, start, end)
	if err != nil {
		return Result{}, err
	}
	result := selectReward(ctx, db, features, baseline, provisional, coverage, time.Now().Unix())
	if err := save(ctx, db, result); err != nil {
		return Result{}, err
	}
	if err := saveRewardState(ctx, db, result); err != nil {
		return Result{}, err
	}
	return Load(ctx, db, result.Day)
}

// isProvisional reports whether the day has not yet ended in the user's
// timezone, i.e. whether more of it is still expected to arrive.
func isProvisional(dayStartLocal, now time.Time, loc *time.Location) bool {
	return now.In(loc).Before(dayStartLocal.AddDate(0, 0, 1))
}

// dayCoverage compares the sources that produced events on this day against
// those active in the surrounding week. AI Day reads a mirror that other
// processes fill in as sessions flush, so "no tool calls today" routinely means
// "not synced yet" rather than "you used no tools".
func dayCoverage(ctx context.Context, db *sql.DB, start, end time.Time) (*Coverage, error) {
	weekStart := start.AddDate(0, 0, -7).Unix()
	// Paused sources are excluded. Pausing stops ingestion going forward but
	// leaves already-derived events in place, so a source the user switched off
	// still has events in the trailing week and would be reported missing every
	// day until those events aged out — turning a deliberate choice into a
	// standing warning that the data is incomplete.
	rows, err := db.QueryContext(ctx, `
		SELECT e.source,
		       MAX(CASE WHEN e.occurred_at >= ? AND e.occurred_at < ? THEN 1 ELSE 0 END) AS present
		FROM ai_day_events e
		LEFT JOIN ai_day_sources s ON s.source = e.source
		WHERE e.occurred_at >= ? AND e.occurred_at < ?
		  AND COALESCE(s.enabled, 1) = 1
		GROUP BY e.source ORDER BY e.source`, start.Unix(), end.Unix(), weekStart, end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	coverage := &Coverage{Complete: true}
	for rows.Next() {
		var source string
		var present int
		if err := rows.Scan(&source, &present); err != nil {
			return nil, err
		}
		coverage.ExpectedSource++
		if present == 1 {
			coverage.PresentSource++
		} else {
			coverage.MissingSources = append(coverage.MissingSources, source)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	coverage.Complete = len(coverage.MissingSources) == 0
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(occurred_at),0) FROM ai_day_events WHERE occurred_at < ?`, end.Unix()).Scan(&coverage.DataThrough)
	return coverage, nil
}

func Load(ctx context.Context, db *sql.DB, day string) (Result, error) {
	var result Result
	var conceptID, title, explanation, tagsJSON, evidenceJSON, origin, computedID string
	var confidence, evidenceStrength float64
	err := db.QueryRowContext(ctx, `
		SELECT f.day, f.timezone, f.session_count, f.turn_count, f.tool_calls,
		       f.source_count, f.input_tokens, f.output_tokens,
		       f.cache_create_tokens, f.cache_read_tokens, f.generation_seconds,
		       f.built_at, f.feature_version,
		       r.state, r.concept_id, r.title, r.explanation, r.tags_json,
		       r.evidence_json, r.confidence, r.evidence_strength, r.origin,
		       r.computed_badge_id, r.baseline_days,
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
		&confidence, &evidenceStrength, &origin, &computedID, &result.BaselineDays,
		&result.GeneratedAt, &result.EngineVersion,
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
		concept := &Concept{ID: conceptID, Title: title, Explanation: explanation, Confidence: confidence, Origin: origin, EvidenceStrength: evidenceStrength, ComputedID: computedID}
		if computedID != "" {
			if d, ok := definition(computedID); ok {
				concept.ComputedTitle = d.Name
			}
		}
		if concept.Origin == "" {
			concept.Origin = "computed"
		}
		if err := json.Unmarshal([]byte(tagsJSON), &concept.Tags); err != nil {
			return Result{}, fmt.Errorf("decode tags: %w", err)
		}
		if err := json.Unmarshal([]byte(evidenceJSON), &concept.Evidence); err != nil {
			return Result{}, fmt.Errorf("decode evidence: %w", err)
		}
		result.Concept = concept
	}

	parsed, err := time.ParseInLocation(time.DateOnly, day, locationOf(result.Timezone))
	if err != nil {
		return Result{}, fmt.Errorf("decode day: %w", err)
	}
	loc := locationOf(result.Timezone)
	result.Provisional = isProvisional(parsed, time.Now(), loc)
	if result.State != "empty" && !result.Provisional {
		baseline, err := loadBaseline(ctx, db, day, 30)
		if err != nil {
			return Result{}, err
		}
		result.Percentiles = percentiles(result.Features, baseline)
	}
	if result.State != "empty" {
		coverage, err := dayCoverage(ctx, db, parsed, parsed.AddDate(0, 0, 1))
		if err != nil {
			return Result{}, err
		}
		result.Coverage = coverage
	}
	feedback, err := loadFeedback(ctx, db, day)
	if err != nil {
		return Result{}, err
	}
	result.Feedback = feedback
	if err := loadFeatureDetails(ctx, db, &result); err != nil {
		return Result{}, err
	}
	if err := loadSelectedBadge(ctx, db, &result); err != nil {
		return Result{}, err
	}
	if err := loadCandidates(ctx, db, &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

// loadCandidates restores the badges that qualified for the day. The correction
// flow needs them: "which badge was today really" is a far better question when
// the alternatives the engine actually considered are visible.
func loadCandidates(ctx context.Context, db *sql.DB, result *Result) error {
	rows, err := db.QueryContext(ctx, `SELECT badge_id,score,evidence_json FROM ai_day_badge_days WHERE day=? AND qualified=1 ORDER BY score DESC, badge_id`, result.Day)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, evidenceJSON string
		var score float64
		if err := rows.Scan(&id, &score, &evidenceJSON); err != nil {
			return err
		}
		d, ok := definition(id)
		if !ok {
			continue
		}
		badge := Badge{ID: d.ID, Name: d.Name, Description: d.Description, Family: d.Family, Kind: d.Kind, Score: score, QualifiedDates: []string{}}
		_ = json.Unmarshal([]byte(evidenceJSON), &badge.Evidence)
		result.Candidates = append(result.Candidates, badge)
	}
	return rows.Err()
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

func percentiles(current Features, baseline []Features) map[string]float64 {
	result := map[string]float64{
		"turn_count":   0.5,
		"tool_calls":   0.5,
		"total_tokens": 0.5,
		"work_tokens":  0.5,
	}
	if len(baseline) == 0 {
		return result
	}
	turns := make([]int64, 0, len(baseline))
	tools := make([]int64, 0, len(baseline))
	tokens := make([]int64, 0, len(baseline))
	work := make([]int64, 0, len(baseline))
	for _, f := range baseline {
		turns = append(turns, f.TurnCount)
		tools = append(tools, f.ToolCalls)
		tokens = append(tokens, f.TotalTokens())
		work = append(work, f.WorkTokens())
	}
	result["turn_count"] = rank(current.TurnCount, turns)
	result["tool_calls"] = rank(current.ToolCalls, tools)
	result["total_tokens"] = rank(current.TotalTokens(), tokens)
	result["work_tokens"] = rank(current.WorkTokens(), work)
	return result
}

func defaultOrigin(origin string) string {
	if origin == "" {
		return "computed"
	}
	return origin
}

// locationOf resolves a stored timezone name, falling back to UTC so a result
// row written under a since-removed zone still loads.
func locationOf(name string) *time.Location {
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func loadFeedback(ctx context.Context, db *sql.DB, day string) (*Feedback, error) {
	var f Feedback
	var raw string
	err := db.QueryRowContext(ctx, `SELECT day,verdict,corrected_badge_id,semantic_labels_json,updated_at FROM ai_day_feedback WHERE day=?`, day).
		Scan(&f.Day, &f.Verdict, &f.CorrectedBadge, &raw, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(raw), &f.SemanticLabels)
	return &f, nil
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
		 confidence, evidence_strength, origin, computed_badge_id,
		 baseline_days, generated_at, engine_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET
		 state=excluded.state, concept_id=excluded.concept_id, title=excluded.title,
		 explanation=excluded.explanation, tags_json=excluded.tags_json,
		 evidence_json=excluded.evidence_json, confidence=excluded.confidence,
		 evidence_strength=excluded.evidence_strength, origin=excluded.origin,
		 computed_badge_id=excluded.computed_badge_id,
		 baseline_days=excluded.baseline_days, generated_at=excluded.generated_at,
		 engine_version=excluded.engine_version`,
		result.Day, result.State, concept.ID, concept.Title, concept.Explanation,
		string(tagsJSON), string(evidenceJSON), concept.Confidence,
		concept.EvidenceStrength, defaultOrigin(concept.Origin), concept.ComputedID,
		result.BaselineDays, result.GeneratedAt, result.EngineVersion)
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
