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
	definition                                         badgeDefinition
	significance, rarity, confidence, freshness, score float64
	evidence                                           []Evidence
}

func selectReward(ctx context.Context, db *sql.DB, f Features, baseline []Features, generatedAt int64) Result {
	r := Result{SchemaVersion: ContractVersion, Day: f.Day, State: "ready", Timezone: f.Timezone, Features: f, BaselineDays: len(baseline), GeneratedAt: generatedAt, EngineVersion: EngineVersion}
	if f.Empty() {
		r.State = "empty"
		return r
	}
	p := percentiles(f, baseline)
	r.Percentiles = p
	candidates := qualifyBadges(ctx, db, f, p, len(baseline))
	if corrected := correctedBadge(ctx, db, f.Day); corrected != "" {
		if d, ok := definition(corrected); ok {
			candidates = append([]scoredBadge{{definition: d, significance: 1, rarity: 1, confidence: 1, freshness: 1, score: 1, evidence: []Evidence{{Metric: "user_correction", Value: 1}}}}, candidates...)
		}
	}
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
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	for _, c := range candidates {
		r.Candidates = append(r.Candidates, badgeFromScore(c))
	}
	if len(candidates) == 0 {
		return r
	}
	selected := candidates[0]
	r.Concept = &Concept{ID: selected.definition.ID, Title: selected.definition.Name, Explanation: selected.definition.Description, Tags: []string{selected.definition.Family, selected.definition.Kind}, Evidence: selected.evidence, Confidence: round(selected.confidence)}
	b := badgeFromScore(selected)
	r.Badge = &b
	return r
}

func qualifyBadges(ctx context.Context, db *sql.DB, f Features, p map[string]float64, baselineDays int) []scoredBadge {
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
	streak := priorStreak(ctx, db, f.Day)
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
	autopilotStrength := math.Max(float64(f.ToolCalls)/20, float64(f.TotalTokens())/1_000_000)
	if baselineDays >= 5 {
		autopilotStrength = math.Max(autopilotStrength, math.Max(p["tool_calls"], p["total_tokens"]))
	}
	add(f.ToolCalls >= 6 || f.TotalTokens() >= 500_000 || (baselineDays >= 5 && (p["tool_calls"] >= .8 || p["total_tokens"] >= .8)), "autopilot", autopilotStrength, Evidence{Metric: "tool_calls", Value: float64(f.ToolCalls), Unit: "calls", Comparison: percentileText(p["tool_calls"])}, Evidence{Metric: "total_tokens", Value: float64(f.TotalTokens()), Unit: "tokens", Comparison: percentileText(p["total_tokens"])})
	deepStrength := float64(f.TurnCount) / 24
	if baselineDays >= 5 {
		deepStrength = math.Max(deepStrength, p["turn_count"])
	}
	add(f.TurnCount >= 8 || (baselineDays >= 5 && p["turn_count"] >= .8), "deep_collaboration", deepStrength, Evidence{Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns", Comparison: percentileText(p["turn_count"])})
	add(f.SourceCount >= 2, "model_conductor", float64(f.SourceCount)/2, Evidence{Metric: "source_count", Value: float64(f.SourceCount), Unit: "agents"})
	add(f.ModalityCounts["visual"] >= 2, "visual_director", float64(f.ModalityCounts["visual"])/8, Evidence{Metric: "visual_events", Value: float64(f.ModalityCounts["visual"]), Unit: "events"})
	add(f.ModalityCounts["code"] >= 2, "code_architect", float64(f.ModalityCounts["code"])/10, Evidence{Metric: "code_events", Value: float64(f.ModalityCounts["code"]), Unit: "events"})
	add(semantic("correction", "retry") >= 2, "quality_inspector", float64(semantic("correction", "retry"))/6, Evidence{Metric: "quality_loops", Value: float64(semantic("correction", "retry")), Unit: "turns"})
	add(semantic("refinement") >= 2, "follow_up", float64(semantic("refinement"))/6, Evidence{Metric: "refinements", Value: float64(semantic("refinement")), Unit: "turns"})
	add(semantic("question", "explanation") >= 4, "detail_microscope", float64(semantic("question", "explanation"))/10, Evidence{Metric: "detail_turns", Value: float64(semantic("question", "explanation")), Unit: "turns"})
	add(modalities >= 3, "generalist", float64(modalities)/5, Evidence{Metric: "modality_count", Value: float64(modalities), Unit: "types"})
	add(semantic("correction") >= 2, "hard_to_fool", float64(semantic("correction"))/5, Evidence{Metric: "corrections", Value: float64(semantic("correction")), Unit: "turns"})
	add(semantic("acceptance") >= 1 && semantic("correction", "retry") == 0, "first_draft_accepted", math.Min(.7+float64(semantic("acceptance"))*.1, 1), Evidence{Metric: "acceptances", Value: float64(semantic("acceptance")), Unit: "turns"})
	add(streak >= 2, "streak", float64(streak+1)/14, Evidence{Metric: "consecutive_days", Value: float64(streak + 1), Unit: "days"})
	if len(raws) == 0 {
		raws = append(raws, raw{"deep_collaboration", .35, []Evidence{{Metric: "session_count", Value: float64(f.SessionCount), Unit: "sessions"}, {Metric: "turn_count", Value: float64(f.TurnCount), Unit: "turns"}}})
	}
	confidence := math.Min(.58+float64(baselineDays)*.35/30, .93)
	result := make([]scoredBadge, 0, len(raws))
	for _, raw := range raws {
		d, _ := definition(raw.id)
		rarity := badgeRarity(ctx, db, raw.id)
		freshness, available := badgeFreshness(ctx, db, raw.id, f.Day, d.Kind)
		if !available {
			continue
		}
		score := .40*raw.strength + .25*rarity + .20*confidence + .15*freshness
		result = append(result, scoredBadge{d, raw.strength, rarity, confidence, freshness, round(score), raw.evidence})
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

func badgeRarity(ctx context.Context, db *sql.DB, id string) float64 {
	var total, hits int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT day),COUNT(DISTINCT CASE WHEN badge_id=? AND qualified=1 THEN day END) FROM ai_day_badge_days`, id).Scan(&total, &hits)
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

func rebuildBadgeProgress(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, d := range badgeCatalog {
		var days int
		var first, last string
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MIN(day),''),COALESCE(MAX(day),'') FROM ai_day_badge_days WHERE badge_id=? AND qualified=1`, d.ID).Scan(&days, &first, &last)
		var recent, highConfidence int
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 AND day>=?`, d.ID, time.Now().AddDate(0, 0, -59).Format(time.DateOnly)).Scan(&recent)
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_badge_days WHERE badge_id=? AND selected=1 AND score>=0.75`, d.ID).Scan(&highConfidence)
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
		if level == 0 {
			first = ""
		} else if highConfidence == 0 {
			_ = tx.QueryRowContext(ctx, `SELECT day FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 ORDER BY day LIMIT 1 OFFSET 2`, d.ID).Scan(&first)
		} else {
			_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(day),'') FROM ai_day_badge_days WHERE badge_id=? AND selected=1 AND score>=0.75`, d.ID).Scan(&first)
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
	rows, err := db.QueryContext(ctx, `SELECT day FROM ai_day_badge_days WHERE badge_id=? AND qualified=1 ORDER BY day DESC LIMIT 60`, d.ID)
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
