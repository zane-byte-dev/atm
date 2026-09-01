package aiday

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"time"
)

type badgeDefinition struct{ ID, Name, Description, Family, Kind string }

var badgeCatalog = []badgeDefinition{
	{"autopilot", "自动驾驶", "你把连续执行交给了 AI，自己专注于方向与验收。", "orbit", "growth"},
	{"deep_collaboration", "深度共创", "多轮互动把一个问题持续推向更深处。", "crystal", "growth"},
	{"model_conductor", "模型指挥家", "你在多个 AI 来源之间协调任务与上下文。", "orbit", "instant"},
	{"visual_director", "视觉导演", "今天的视觉生成与审美判断格外集中。", "prism", "growth"},
	{"code_architect", "代码架构师", "代码、工具与结构化执行构成了主旋律。", "grid", "growth"},
	{"quality_inspector", "AI 质检员", "你通过重试与修正持续提高交付质量。", "lens", "growth"},
	{"follow_up", "追问者", "你没有停在第一版，而是持续细化答案。", "crystal", "growth"},
	{"detail_microscope", "细节显微镜", "高密度问题和解释让细节被充分看见。", "lens", "instant"},
	{"generalist", "全能协作者", "多种任务模态在同一天里自然切换。", "prism", "instant"},
	{"hard_to_fool", "不易被糊弄", "你主动识别偏差并明确纠正方向。", "grid", "growth"},
	{"first_draft_accepted", "一稿即中", "今天的首轮产出获得了直接确认。", "crystal", "instant"},
	{"streak", "持续同行", "稳定使用 AI 的节奏已经形成连续轨迹。", "orbit", "streak"},
}

func definition(id string) (badgeDefinition, bool) {
	for _, d := range badgeCatalog {
		if d.ID == id {
			return d, true
		}
	}
	return badgeDefinition{}, false
}

type scoredBadge struct {
	definition                             badgeDefinition
	significance, rarity, freshness, score float64
	evidence                               []Evidence
}

func selectReward(ctx context.Context, db *sql.DB, f Features, baseline []Features, provisional bool, coverage *Coverage, generatedAt int64) Result {
	r := Result{SchemaVersion: ContractVersion, Day: f.Day, State: "ready", Timezone: f.Timezone, Features: f, BaselineDays: len(baseline), Provisional: provisional, Coverage: coverage, GeneratedAt: generatedAt, EngineVersion: EngineVersion}
	if f.Empty() {
		r.State = "empty"
		return r
	}
	p := percentiles(f, baseline)
	// A day still in progress cannot be ranked against completed days, so the
	// percentile-gated branches are withheld rather than allowed to read as
	// "today is your quietest day in a month" every morning.
	if !provisional {
		r.Percentiles = p
	}
	candidates := qualifyBadges(ctx, db, f, p, len(baseline), provisional)
	seen := map[string]bool{}
	deduped := candidates[:0]
	for _, c := range candidates {
		if !seen[c.definition.ID] {
			seen[c.definition.ID] = true
			deduped = append(deduped, c)
		}
	}
	candidates = deduped
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].definition.ID < candidates[j].definition.ID
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) == 0 {
		return r
	}

	selected, computed := candidates[0], candidates[0]
	origin := "computed"
	// A correction says which badge the day deserves. It does not make the
	// engine more certain and it does not erase the behavioural evidence, so the
	// corrected badge keeps whatever evidence it qualified with (or the day's
	// shape when it did not qualify at all) and the original pick is preserved.
	if corrected := correctedBadge(ctx, db, f.Day); corrected != "" {
		if d, ok := definition(corrected); ok && d.ID != selected.definition.ID {
			origin = "user_corrected"
			replacement, found := scoredBadge{}, false
			for _, c := range candidates {
				if c.definition.ID == d.ID {
					replacement, found = c, true
					break
				}
			}
			if !found {
				replacement = scoredBadge{
					definition: d,
					rarity:     badgeRarity(ctx, db, d.ID, f.Day),
					evidence: []Evidence{
						{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns"},
						{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"},
					},
				}
				candidates = append(candidates, replacement)
			}
			selected = replacement
		}
	}

	// Cap after selection so a corrected badge that did not place in the top
	// five is still visible in the candidate list it now leads.
	ordered := make([]scoredBadge, 0, len(candidates))
	ordered = append(ordered, selected)
	for _, c := range candidates {
		if c.definition.ID != selected.definition.ID {
			ordered = append(ordered, c)
		}
	}
	if len(ordered) > 5 {
		ordered = ordered[:5]
	}
	for _, c := range ordered {
		r.Candidates = append(r.Candidates, badgeFromScore(c))
	}

	concept := &Concept{
		ID:               selected.definition.ID,
		Title:            selected.definition.Name,
		Explanation:      selected.definition.Description,
		Tags:             []string{selected.definition.Family, selected.definition.Kind},
		Evidence:         selected.evidence,
		EvidenceStrength: round(selected.significance),
		Confidence:       conceptConfidence(selected.significance, len(baseline), coverage, provisional),
		Origin:           origin,
	}
	if origin == "user_corrected" {
		concept.ComputedID, concept.ComputedTitle = computed.definition.ID, computed.definition.Name
	}
	r.Concept = concept
	b := badgeFromScore(selected)
	r.Badge = &b
	return r
}

// conceptConfidence answers "how much should the user trust this card", which
// depends on how much history there is to compare against, how strong today's
// signal is, and whether the day's inputs look complete. The old formula used
// baseline length alone, so every day past the first month reported 93%.
func conceptConfidence(strength float64, baselineDays int, coverage *Coverage, provisional bool) float64 {
	baselineTerm := clamp(float64(baselineDays) / 30)
	coverageTerm := 1.0
	if coverage != nil && coverage.ExpectedSource > 0 {
		coverageTerm = clamp(float64(coverage.PresentSource) / float64(coverage.ExpectedSource))
	}
	value := 0.30*baselineTerm + 0.50*clamp(strength) + 0.20*coverageTerm
	if provisional {
		// The day is not over; the same evidence may look different by tonight.
		value *= 0.8
	}
	if value > 0.95 {
		value = 0.95
	}
	return round(value)
}

// streakMilestones are the only consecutive-day counts worth announcing. The
// previous rule qualified from two days onward, which on any continuous stretch
// of use meant "持续同行" sat in the candidate pool every single day.
var streakMilestones = map[int]bool{7: true, 14: true, 30: true, 60: true, 100: true, 200: true, 365: true}

func qualifyBadges(ctx context.Context, db *sql.DB, f Features, p map[string]float64, baselineDays int, provisional bool) []scoredBadge {
	semantic := func(keys ...string) int64 {
		var n int64
		for _, k := range keys {
			n += f.SemanticCounts[k]
		}
		return n
	}
	modalities := 0
	for _, n := range f.ModalityCounts {
		if n > 0 {
			modalities++
		}
	}
	// Percentile comparisons are only meaningful for a finished day.
	ranked := baselineDays >= 5 && !provisional
	rankOf := func(key string) float64 {
		if !ranked {
			return 0
		}
		return p[key]
	}
	comparison := func(key string) string {
		if !ranked {
			return ""
		}
		return percentileText(p[key])
	}
	turns := float64(f.TurnCount)
	if turns < 1 {
		turns = 1
	}
	streak := priorStreak(ctx, db, f.Day) + 1

	type raw struct {
		id       string
		strength float64
		evidence []Evidence
	}
	var raws []raw
	add := func(ok bool, id string, strength float64, evidence ...Evidence) {
		if ok {
			raws = append(raws, raw{id, clamp(strength), evidence})
		}
	}

	toolRank, workTokenRank := 0.0, 0.0
	// A zero can rank at the 100th percentile when every baseline day is also
	// zero. Percentiles describe relative volume; they must not manufacture an
	// execution signal that did not happen at all.
	if f.ToolCalls > 0 {
		toolRank = rankOf("tool_calls")
	}
	if f.WorkTokens() > 0 {
		workTokenRank = rankOf("work_tokens")
	}
	autopilotStrength := math.Max(float64(f.ToolCalls)/40, float64(f.WorkTokens())/400_000)
	autopilotStrength = math.Max(autopilotStrength, math.Max(toolRank, workTokenRank))
	add(f.ToolCalls >= 20 || f.WorkTokens() >= 200_000 || toolRank >= .8 || workTokenRank >= .8,
		"autopilot", autopilotStrength,
		Evidence{Metric: "tool_calls", Value: float64(f.ToolCalls), Unit: "calls", Comparison: comparison("tool_calls")},
		Evidence{Metric: "work_tokens", Value: float64(f.WorkTokens()), Unit: "tokens", Comparison: comparison("work_tokens")},
		Evidence{Metric: "generation_seconds", Value: float64(f.GenerationSeconds), Unit: "seconds"})

	deepStrength := math.Max(float64(f.TurnCount)/24, rankOf("turn_count"))
	add(f.TurnCount >= 8 || rankOf("turn_count") >= .8,
		"deep_collaboration", deepStrength,
		Evidence{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns", Comparison: comparison("turn_count")},
		Evidence{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"},
		Evidence{Metric: "generation_seconds", Value: float64(f.GenerationSeconds), Unit: "seconds"})

	// Two sources is the floor for "conducting" anything, but two is also the
	// most common case, so it must not start near full strength.
	add(f.SourceCount >= 2, "model_conductor", float64(f.SourceCount-1)/3,
		Evidence{Metric: "source_count", Value: float64(f.SourceCount), Unit: "agents"},
		Evidence{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"})

	visual := f.ModalityCounts["visual"]
	add(visual >= 3, "visual_director", float64(visual)/10,
		Evidence{Metric: "visual_events", Value: float64(visual), Unit: "events"},
		Evidence{Metric: "modality_share", Value: round(float64(visual) / turns * 100), Unit: "%"})

	code := f.ModalityCounts["code"]
	add(code >= 4, "code_architect", float64(code)/20,
		Evidence{Metric: "code_events", Value: float64(code), Unit: "events"},
		Evidence{Metric: "tool_calls", Value: float64(f.ToolCalls), Unit: "calls", Comparison: comparison("tool_calls")})

	loops := semantic("correction", "retry")
	add(loops >= 3, "quality_inspector", float64(loops)/8,
		Evidence{Metric: "quality_loops", Value: float64(loops), Unit: "turns"},
		Evidence{Metric: "loop_share", Value: round(float64(loops) / turns * 100), Unit: "%"})

	refine := semantic("refinement")
	add(refine >= 3, "follow_up", float64(refine)/8,
		Evidence{Metric: "refinements", Value: float64(refine), Unit: "turns"},
		Evidence{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns", Comparison: comparison("turn_count")})

	detail := semantic("question", "explanation")
	add(detail >= 6, "detail_microscope", float64(detail)/14,
		Evidence{Metric: "detail_turns", Value: float64(detail), Unit: "turns"},
		Evidence{Metric: "detail_share", Value: round(float64(detail) / turns * 100), Unit: "%"})

	add(modalities >= 3, "generalist", float64(modalities-2)/3,
		Evidence{Metric: "modality_count", Value: float64(modalities), Unit: "types"},
		Evidence{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns", Comparison: comparison("turn_count")})

	corrections := semantic("correction")
	add(corrections >= 3, "hard_to_fool", float64(corrections)/7,
		Evidence{Metric: "corrections", Value: float64(corrections), Unit: "turns"},
		Evidence{Metric: "correction_share", Value: round(float64(corrections) / turns * 100), Unit: "%"})

	// "First draft accepted" used to start at 0.8 strength for a single
	// acceptance, which made the thinnest possible evidence outrank a genuine
	// twelve-turn collaboration. It is now a ratio and needs a real day behind it.
	accept := semantic("acceptance")
	add(accept >= 2 && loops == 0 && f.TurnCount >= 4,
		"first_draft_accepted", float64(accept)/turns,
		Evidence{Metric: "acceptances", Value: float64(accept), Unit: "turns"},
		Evidence{Metric: "acceptance_share", Value: round(float64(accept) / turns * 100), Unit: "%"},
		Evidence{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns"})

	add(streakMilestones[streak], "streak", math.Min(.6+float64(streak)/200, 1),
		Evidence{Metric: "consecutive_days", Value: float64(streak), Unit: "days"},
		Evidence{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"})

	if len(raws) == 0 {
		raws = append(raws, raw{"deep_collaboration", .35, []Evidence{
			{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"},
			{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns"},
			{Metric: "work_tokens", Value: float64(f.WorkTokens()), Unit: "tokens"},
		}})
	}

	result := make([]scoredBadge, 0, len(raws))
	for _, raw := range raws {
		d, _ := definition(raw.id)
		rarity := badgeRarity(ctx, db, raw.id, f.Day)
		freshness, available := badgeFreshness(ctx, db, raw.id, f.Day, d.Kind)
		if !available {
			continue
		}
		// Novelty is 20% of the score, down from 40%. Confidence used to take
		// another 20% while being a constant for anyone with a month of history,
		// so the old effective split was half "what you did", half "what you
		// have not been given lately".
		score := .80*raw.strength + .12*rarity + .08*freshness
		result = append(result, scoredBadge{d, raw.strength, rarity, freshness, round(score), raw.evidence})
	}
	return result
}

func badgeFromScore(c scoredBadge) Badge {
	return Badge{ID: c.definition.ID, Name: c.definition.Name, Description: c.definition.Description, Family: c.definition.Family, Kind: c.definition.Kind, QualifiedDates: []string{}, Score: c.score, Evidence: c.evidence}
}
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// badgeRarity must only look at days strictly before the one being scored.
// Without the day filter it also counted rows left over from a previous rebuild
// for days *after* this one, so each run's rarity depended on the previous run's
// output: rebuilding the same range twice produced different history, and the
// sequence never converged.
func badgeRarity(ctx context.Context, db *sql.DB, id, day string) float64 {
	var total, hits int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT day),COUNT(DISTINCT CASE WHEN badge_id=? AND qualified=1 THEN day END) FROM ai_day_badge_days WHERE day<?`, id, day).Scan(&total, &hits)
	if total == 0 {
		return 1
	}
	return clamp(1 - float64(hits)/float64(total))
}
func badgeFreshness(ctx context.Context, db *sql.DB, id, day, kind string) (float64, bool) {
	var last string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(day),'') FROM ai_day_badge_days WHERE badge_id=? AND selected=1 AND day<?`, id, day).Scan(&last)
	if err != nil || last == "" {
		return 1, true
	}
	current, _ := time.Parse(time.DateOnly, day)
	prior, _ := time.Parse(time.DateOnly, last)
	days := int(current.Sub(prior).Hours() / 24)
	if kind == "instant" && days < 14 {
		return float64(days) / 14, false
	}
	return clamp(float64(days) / 30), true
}
func priorStreak(ctx context.Context, db *sql.DB, day string) int {
	current, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return 0
	}
	streak := 0
	for i := 1; i <= 365; i++ {
		d := current.AddDate(0, 0, -i).Format(time.DateOnly)
		var state string
		if db.QueryRowContext(ctx, `SELECT state FROM ai_day_results WHERE day=?`, d).Scan(&state) != nil || state != "ready" {
			break
		}
		streak++
	}
	return streak
}
func correctedBadge(ctx context.Context, db *sql.DB, day string) string {
	var id string
	_ = db.QueryRowContext(ctx, `SELECT corrected_badge_id FROM ai_day_feedback WHERE day=? AND verdict='corrected'`, day).Scan(&id)
	return id
}

func saveRewardState(ctx context.Context, db *sql.DB, result Result) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM ai_day_badge_days WHERE day=?`, result.Day); err != nil {
		return err
	}
	for _, b := range result.Candidates {
		evidence, _ := json.Marshal(b.Evidence)
		selected := result.Badge != nil && result.Badge.ID == b.ID
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_day_badge_days(day,badge_id,qualified,selected,level,score,evidence_json) VALUES (?,?,1,?,?,?,?)`, result.Day, b.ID, selected, 0, b.Score, string(evidence)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return rebuildBadgeProgress(ctx, db)
}

// CollectionStartKey marks the first day that counts toward Atlas progression.
// Baselines and percentiles still use the full retroactive window — they need
// history to be meaningful — but unlocking twelve badges from a backfill before
// the user has seen a single daily card spoils the whole collection.
const CollectionStartKey = "collection_start_day"

func collectionStart(ctx context.Context, db queryRower) string {
	var value string
	if db.QueryRowContext(ctx, `SELECT value FROM ai_day_settings WHERE key=?`, CollectionStartKey).Scan(&value) != nil {
		return ""
	}
	return value
}

// EnsureCollectionStart records the first day the user actually ran AI Day.
func EnsureCollectionStart(ctx context.Context, db *sql.DB, day string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO ai_day_settings(key,value,updated_at) VALUES (?,?,?) ON CONFLICT(key) DO NOTHING`, CollectionStartKey, day, time.Now().Unix())
	return err
}

func rebuildBadgeProgress(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	// Days before the collection start are excluded from progression but remain
	// available as baseline data.
	start := collectionStart(ctx, tx)
	for _, d := range badgeCatalog {
		var days int
		var last string
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(day),'') FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=?`, d.ID, start).Scan(&days, &last)
		var recent, highConfidence int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=? AND day>=?`, d.ID, time.Now().AddDate(0, 0, -59).Format(time.DateOnly), start).Scan(&recent)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_badge_days WHERE badge_id=? AND selected=1 AND score>=0.75 AND day>=?`, d.ID, start).Scan(&highConfidence)
		level := 0
		if days >= 3 || highConfidence > 0 {
			level = 1
		}
		if recent >= 7 {
			level = 2
		}
		if days >= 30 {
			level = 3
		}
		first := ""
		if level > 0 {
			if highConfidence == 0 {
				_ = tx.QueryRowContext(ctx, `SELECT day FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=? ORDER BY day LIMIT 1 OFFSET 2`, d.ID, start).Scan(&first)
			} else {
				_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(day),'') FROM ai_day_badge_days WHERE badge_id=? AND selected=1 AND score>=0.75 AND day>=?`, d.ID, start).Scan(&first)
			}
		}
		cooldown := ""
		var lastSelected string
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(day),'') FROM ai_day_badge_days WHERE badge_id=? AND selected=1`, d.ID).Scan(&lastSelected)
		if d.Kind == "instant" && lastSelected != "" {
			parsed, _ := time.Parse(time.DateOnly, lastSelected)
			cooldown = parsed.AddDate(0, 0, 14).Format(time.DateOnly)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO ai_day_badge_progress(badge_id,level,qualified_days,last_qualified,cooldown_until,first_unlocked,updated_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(badge_id) DO UPDATE SET level=excluded.level,qualified_days=excluded.qualified_days,last_qualified=excluded.last_qualified,cooldown_until=excluded.cooldown_until,first_unlocked=excluded.first_unlocked,updated_at=excluded.updated_at`, d.ID, level, days, last, cooldown, first, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadSelectedBadge(ctx context.Context, db *sql.DB, result *Result) error {
	if result.Concept == nil {
		return nil
	}
	d, ok := definition(result.Concept.ID)
	if !ok {
		return nil
	}
	b := Badge{ID: d.ID, Name: d.Name, Description: d.Description, Family: d.Family, Kind: d.Kind, Evidence: result.Concept.Evidence}
	var level, days int
	_ = db.QueryRowContext(ctx, `SELECT level,qualified_days,last_qualified,cooldown_until FROM ai_day_badge_progress WHERE badge_id=?`, d.ID).Scan(&level, &days, &b.LastQualified, &b.CooldownUntil)
	b.Level = level
	b.Unlocked = level > 0
	b.QualifiedDays = days
	b.NextLevelDays = nextLevel(level)
	b.Progress = levelProgress(level, days)
	rows, err := db.QueryContext(ctx, `SELECT day FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=? ORDER BY day DESC LIMIT 60`, d.ID, collectionStart(ctx, db))
	if err != nil {
		return err
	}
	b.QualifiedDates = []string{}
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			rows.Close()
			return err
		}
		b.QualifiedDates = append(b.QualifiedDates, day)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_ = db.QueryRowContext(ctx, `SELECT score FROM ai_day_badge_days WHERE day=? AND badge_id=?`, result.Day, d.ID).Scan(&b.Score)
	result.Badge = &b
	return nil
}
func nextLevel(level int) int {
	switch level {
	case 0:
		return 3
	case 1:
		return 7
	default:
		return 30
	}
}
func levelProgress(level, days int) float64 {
	switch level {
	case 0:
		return clamp(float64(days) / 3)
	case 1:
		return clamp(float64(days) / 7)
	case 2:
		return clamp(float64(days) / 30)
	default:
		return 1
	}
}
