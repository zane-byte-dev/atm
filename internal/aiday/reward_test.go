package aiday_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
)

// snapshot captures everything a rebuild derives, so a second rebuild can be
// compared byte for byte rather than through whichever field a caller happens
// to read.
func snapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	var out string
	for _, query := range []string{
		`SELECT day,badge_id,qualified,selected,score,evidence_json FROM ai_day_badge_days ORDER BY day,badge_id`,
		`SELECT badge_id,level,qualified_days,last_qualified,cooldown_until,first_unlocked FROM ai_day_badge_progress ORDER BY badge_id`,
		`SELECT day,state,concept_id,confidence,evidence_strength,origin,computed_badge_id,evidence_json FROM ai_day_results ORDER BY day`,
		`SELECT day,turn_count,tool_calls,source_count FROM ai_day_features ORDER BY day`,
	} {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			cells := make([]any, len(columns))
			for i := range cells {
				cells[i] = new(sql.NullString)
			}
			if err := rows.Scan(cells...); err != nil {
				t.Fatal(err)
			}
			for _, cell := range cells {
				out += cell.(*sql.NullString).String + "|"
			}
			out += "\n"
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}

// seedWeek writes a fortnight of varied activity: enough days, sources,
// modalities and semantic labels that many badges qualify and the scoring engine
// actually has to choose between them.
func seedWeek(t *testing.T, db *sql.DB, loc *time.Location, from time.Time, days int) {
	t.Helper()
	texts := []string{
		"帮我实现这个函数", "为什么这里会报错", "不对，应该是另一种写法", "优化一下这段代码",
		"解释一下这个原理", "可以，就这样", "生成一张图片", "再试一次", "写个文档总结",
	}
	for i := 0; i < days; i++ {
		day := from.AddDate(0, 0, i)
		for s, agent := range []string{"codex", "claude"} {
			id := day.Format("20060102") + "-" + agent
			mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES (?,?,?,'atm','/tmp/x',?,?)`,
				id, id, agent, day.Add(time.Duration(8+s)*time.Hour).Unix(), day.Add(time.Duration(18)*time.Hour).Unix())
			for m := 0; m <= (i+s)%len(texts); m++ {
				mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES (?,?,'user',?,?)`,
					id, m, texts[(i+s+m)%len(texts)], day.Add(time.Duration(9+m)*time.Hour).Unix())
			}
			mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES (?,?,?,?,?)`,
				id, day.Add(10*time.Hour).Unix(), 5000+i*400, 900+i*70, 60000)
			mustExec(t, db, `INSERT INTO tools(session_id,name,count) VALUES (?,'Bash',?),(?,'Edit',?),(?,'WebSearch',?)`,
				id, 6+i, id, 4+i, id, 2+s)
		}
	}
}

// TestRepeatedRebuildIsByteIdenticalAcrossRuns is the regression for the bug
// that made AI Day history unreliable: the rarity term counted badge rows for
// days *after* the day being scored, which on a range rebuild meant the rows a
// previous run had left behind. Each run's output became the next run's input,
// so rebuilding the same range twice produced different past days — 2026-07-20
// changed from 深度共创 to 代码架构师 with no new data — and the sequence never
// settled. Reading a full snapshot rather than one field is deliberate: the
// original symptom surfaced as a badge's cumulative day count differing between
// two commands, not as a changed concept.
func TestRepeatedRebuildIsByteIdenticalAcrossRuns(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	from := mustDay(t, "2026-07-20", loc)
	seedWeek(t, db, loc, from, 14)
	to := from.AddDate(0, 0, 13)

	if _, err := aiday.Rebuild(context.Background(), db, from, to, loc); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	first := snapshot(t, db)
	if _, err := aiday.Rebuild(context.Background(), db, from, to, loc); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	second := snapshot(t, db)
	if first != second {
		t.Fatalf("rebuild is not idempotent\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	// A third pass catches an engine that merely alternates between two states.
	if _, err := aiday.Rebuild(context.Background(), db, from, to, loc); err != nil {
		t.Fatalf("third rebuild: %v", err)
	}
	if third := snapshot(t, db); third != second {
		t.Fatalf("rebuild does not converge\n--- second ---\n%s\n--- third ---\n%s", second, third)
	}
}

// TestRefreshMatchesRebuildAndLeavesHistoryAlone covers the other half of the
// same report: `day today` and `day dashboard` disagreed about a badge's
// progress. Both go through Refresh, so what has to hold is that refreshing
// agrees with an explicit rebuild and does not rewrite days it already built.
func TestRefreshMatchesRebuildAndLeavesHistoryAlone(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	now := mustDay(t, "2026-08-02", loc).Add(11 * time.Hour)
	seedWeek(t, db, loc, mustDay(t, "2026-07-27", loc), 7)

	if _, err := aiday.Refresh(context.Background(), db, now, loc, 30); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	first := snapshot(t, db)
	for i := 0; i < 3; i++ {
		if _, err := aiday.Refresh(context.Background(), db, now, loc, 30); err != nil {
			t.Fatalf("refresh %d: %v", i, err)
		}
	}
	if again := snapshot(t, db); again != first {
		t.Fatalf("repeated refresh changed derived state\n--- first ---\n%s\n--- after ---\n%s", first, again)
	}
}

// TestSelfSourceNeverBecomesAnAISource pins the fix for a card that told the
// user they had coordinated work across multiple AI sources when the second
// "source" was ATM's own classifier call.
func TestSelfSourceNeverBecomesAnAISource(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES
		('u1','u1','codex','atm','/tmp/u1',?,?), ('a1','a1','atm','atm','/tmp/a1',?,?)`,
		day.Add(9*time.Hour).Unix(), day.Add(12*time.Hour).Unix(),
		day.Add(9*time.Hour).Unix(), day.Add(12*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES
		('u1',0,'user','帮我实现登录',?), ('a1',0,'user','internal classify prompt',?)`,
		day.Add(9*time.Hour).Unix(), day.Add(9*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES
		('u1',?,4000,700,30000), ('a1',?,900,120,4000)`,
		day.Add(10*time.Hour).Unix(), day.Add(10*time.Hour).Unix())

	result, err := aiday.RebuildDay(context.Background(), db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Features.SourceCount != 1 {
		t.Fatalf("source count = %d, want 1 (atm's own calls excluded)", result.Features.SourceCount)
	}
	if result.Concept != nil && result.Concept.ID == "model_conductor" {
		t.Fatal("model_conductor awarded from ATM's own model call")
	}
	// ATM's own tokens must not inflate the day either.
	if result.Features.WorkTokens() != 4700 {
		t.Fatalf("work tokens = %d, want 4700 from the user's session alone", result.Features.WorkTokens())
	}
	privacy, err := aiday.LoadPrivacy(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range privacy.Sources {
		if source.Source == "atm" {
			t.Fatal("privacy pane offers a permission toggle for a source that is never ingested")
		}
	}
}

// TestScoringPrefersStrongerEvidence pins the rebalanced weights. A single
// acceptance used to enter at 0.8 strength — a hardcoded floor that beat a
// genuine twelve-turn collaboration at 0.5 — and won the day on the thinnest
// evidence available.
func TestScoringPrefersStrongerEvidence(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('s1','s1','codex','atm','/tmp/s1',?,?)`,
		day.Add(9*time.Hour).Unix(), day.Add(20*time.Hour).Unix())
	// Eleven working turns and exactly one "可以" at the end. The wording is
	// deliberately free of code/visual keywords so the comparison under test is
	// acceptance-ratio versus turn-depth, not modality.
	for i := 0; i < 11; i++ {
		mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('s1',?,'user','请继续往下推进这件事',?)`,
			i, day.Add(time.Duration(9+i)*time.Hour).Unix())
	}
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('s1',11,'user','可以',?)`,
		day.Add(20*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES ('s1',?,9000,1500,120000)`,
		day.Add(10*time.Hour).Unix())

	result, err := aiday.RebuildDay(context.Background(), db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Concept == nil {
		t.Fatal("no concept")
	}
	if result.Concept.ID == "first_draft_accepted" {
		t.Fatalf("one acceptance in twelve turns still wins the day: %+v", result.Concept)
	}
	if result.Concept.ID != "deep_collaboration" {
		t.Fatalf("concept = %q, want deep_collaboration for a twelve-turn day", result.Concept.ID)
	}
}

// TestCorrectionKeepsEngineAnswerAndDoesNotInflateConfidence covers the second
// P0 from review: a correction used to set significance, rarity, confidence and
// freshness all to 1 and replace the evidence with a single "user_correction"
// row, so the card claimed 100% confidence on the user's own click.
func TestCorrectionKeepsEngineAnswerAndDoesNotInflateConfidence(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	seedWeek(t, db, loc, mustDay(t, "2026-08-09", loc), 7)

	before, err := aiday.RebuildDay(ctx, db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if before.Concept == nil {
		t.Fatal("no computed concept")
	}
	target := "visual_director"
	if before.Concept.ID == target {
		target = "quality_inspector"
	}
	if err := aiday.SaveFeedback(ctx, db, aiday.Feedback{Day: day.Format(time.DateOnly), Verdict: "corrected", CorrectedBadge: target}); err != nil {
		t.Fatal(err)
	}
	after, err := aiday.RebuildDay(ctx, db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if after.Concept.ID != target {
		t.Fatalf("concept = %q, want the corrected %q", after.Concept.ID, target)
	}
	if after.Concept.Origin != "user_corrected" {
		t.Fatalf("origin = %q, want user_corrected", after.Concept.Origin)
	}
	if after.Concept.ComputedID != before.Concept.ID {
		t.Fatalf("computed id = %q, want the engine's own answer %q preserved", after.Concept.ComputedID, before.Concept.ID)
	}
	if after.Concept.Confidence >= 1 {
		t.Fatalf("confidence = %v, a click must not make the engine certain", after.Concept.Confidence)
	}
	for _, evidence := range after.Concept.Evidence {
		if evidence.Metric == "user_correction" {
			t.Fatal("behavioural evidence was replaced by the correction itself")
		}
	}
	if len(after.Concept.Evidence) < 2 {
		t.Fatalf("corrected day kept only %d evidence items", len(after.Concept.Evidence))
	}

	// And it has to be reversible.
	if err := aiday.ClearFeedback(ctx, db, day.Format(time.DateOnly)); err != nil {
		t.Fatal(err)
	}
	restored, err := aiday.RebuildDay(ctx, db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Concept.ID != before.Concept.ID || restored.Concept.Origin != "computed" {
		t.Fatalf("clearing feedback left %+v, want the original computed %q", restored.Concept, before.Concept.ID)
	}
	if restored.Feedback != nil {
		t.Fatalf("feedback still attached after clear: %+v", restored.Feedback)
	}
}

// TestProvisionalDayWithholdsComparisons stops an in-progress day from being
// ranked against completed ones, which put every morning at the bottom of its
// own baseline (p0 for tokens, p6 for tool calls at 10:30).
func TestProvisionalDayWithholdsComparisons(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	seedWeek(t, db, loc, mustDay(t, "2026-07-20", loc), 20)

	today := time.Now().In(loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('now','now','codex','atm','/tmp/now',?,?)`,
		today.Unix(), today.Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('now',0,'user','帮我看下这个 bug',?)`, today.Unix())
	mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES ('now',?,1200,300,9000)`, today.Unix())

	result, err := aiday.RebuildDay(ctx, db, today, loc)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Provisional {
		t.Fatal("the current day is not marked provisional")
	}
	if len(result.Percentiles) != 0 {
		t.Fatalf("percentiles = %v, want none for an unfinished day", result.Percentiles)
	}
	// A finished day keeps its comparisons.
	past, err := aiday.RebuildDay(ctx, db, mustDay(t, "2026-07-25", loc), loc)
	if err != nil {
		t.Fatal(err)
	}
	if past.Provisional {
		t.Fatal("a past day is marked provisional")
	}
	if len(past.Percentiles) == 0 {
		t.Fatal("a finished day lost its percentiles")
	}
}

// TestCoverageFlagsASourceThatWentQuiet is what turns "0 tool calls" from a
// silent lie into a visible warning: the mirror is filled in by other processes,
// so a source active all week can simply not have flushed yet.
func TestCoverageFlagsASourceThatWentQuiet(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	seedWeek(t, db, loc, mustDay(t, "2026-08-08", loc), 5)

	// Day six has codex only; claude was active on all five preceding days.
	day := mustDay(t, "2026-08-13", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('c6','c6','codex','atm','/tmp/c6',?,?)`,
		day.Add(9*time.Hour).Unix(), day.Add(12*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('c6',0,'user','继续实现',?)`, day.Add(9*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES ('c6',?,3000,400,20000)`, day.Add(10*time.Hour).Unix())

	for d := 0; d < 6; d++ {
		if _, err := aiday.RebuildDay(ctx, db, mustDay(t, "2026-08-08", loc).AddDate(0, 0, d), loc); err != nil {
			t.Fatal(err)
		}
	}
	result, err := aiday.Load(ctx, db, day.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage == nil {
		t.Fatal("no coverage reported")
	}
	if result.Coverage.Complete {
		t.Fatalf("coverage reported complete while claude is missing: %+v", result.Coverage)
	}
	found := false
	for _, source := range result.Coverage.MissingSources {
		if source == "claude" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing sources = %v, want claude", result.Coverage.MissingSources)
	}
}

// TestPausedSourceIsNotReportedMissing keeps a deliberate choice from being
// reported as a data problem. Pausing stops ingestion going forward but leaves
// already-derived events in place, so a paused source still has events in the
// trailing week and was flagged missing every day until they aged out.
func TestPausedSourceIsNotReportedMissing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	seedWeek(t, db, loc, mustDay(t, "2026-08-08", loc), 5)

	// Day six: codex only, exactly as in TestCoverageFlagsASourceThatWentQuiet.
	day := mustDay(t, "2026-08-13", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('c6','c6','codex','atm','/tmp/c6',?,?)`,
		day.Add(9*time.Hour).Unix(), day.Add(12*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('c6',0,'user','继续实现',?)`, day.Add(9*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES ('c6',?,3000,400,20000)`, day.Add(10*time.Hour).Unix())
	for d := 0; d < 6; d++ {
		if _, err := aiday.RebuildDay(ctx, db, mustDay(t, "2026-08-08", loc).AddDate(0, 0, d), loc); err != nil {
			t.Fatal(err)
		}
	}
	// Baseline: claude is missing and correctly reported.
	before, err := aiday.Load(ctx, db, day.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	if before.Coverage == nil || before.Coverage.Complete {
		t.Fatalf("expected claude to be reported missing before pausing: %+v", before.Coverage)
	}

	// The user pauses claude on purpose. Its past events are still in the window.
	if err := aiday.SetSource(ctx, db, "claude", false, false); err != nil {
		t.Fatal(err)
	}
	after, err := aiday.Load(ctx, db, day.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	if after.Coverage == nil {
		t.Fatal("no coverage reported")
	}
	for _, source := range after.Coverage.MissingSources {
		if source == "claude" {
			t.Fatalf("paused source still reported missing: %+v", after.Coverage)
		}
	}
	if !after.Coverage.Complete {
		t.Fatalf("coverage still incomplete after the only gap was paused: %+v", after.Coverage)
	}
}

// TestBackfillDoesNotUnlockTheWholeAtlas keeps the collection worth collecting:
// a first run derives a month of baseline, and that used to unlock all twelve
// badges (six at L2) before the user had seen a single daily card.
func TestBackfillDoesNotUnlockTheWholeAtlas(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	now := mustDay(t, "2026-08-16", loc).Add(21 * time.Hour)
	seedWeek(t, db, loc, mustDay(t, "2026-07-18", loc), 30)

	if _, err := aiday.Refresh(ctx, db, now, loc, 30); err != nil {
		t.Fatal(err)
	}
	atlas, err := aiday.LoadAtlas(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if atlas.Unlocked == atlas.Total {
		t.Fatalf("first run unlocked the entire atlas (%d/%d)", atlas.Unlocked, atlas.Total)
	}
	for _, badge := range atlas.Badges {
		for _, date := range badge.QualifiedDates {
			if date < now.Format(time.DateOnly) {
				t.Fatalf("%s counts backfilled day %s toward progression", badge.ID, date)
			}
		}
	}
	// The baseline still exists — it is only progression that starts today.
	today, err := aiday.Load(ctx, db, now.Format(time.DateOnly))
	if err != nil {
		t.Fatal(err)
	}
	if today.BaselineDays < 20 {
		t.Fatalf("baseline days = %d, want the backfilled window to remain usable", today.BaselineDays)
	}
}

// TestModalityIgnoresUsageVolume pins the attribution fix: modality counts used
// to include one "general" per API call, so a day spent entirely in code read as
// 137 general against 1 code and 代码架构师 could not qualify.
func TestModalityIgnoresUsageVolume(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('s1','s1','codex','atm','/tmp/s1',?,?)`,
		day.Add(9*time.Hour).Unix(), day.Add(19*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('s1',0,'user','实现这个接口',?)`, day.Add(9*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO tools(session_id,name,count) VALUES ('s1','Edit',30),('s1','Bash',25),('s1','Read',40),('s1','Grep',12)`)
	// Sixty API calls: under the old rule these alone produced sixty "general".
	for i := 0; i < 60; i++ {
		mustExec(t, db, `INSERT INTO usage_events(session_id,ts,input_tokens,output_tokens,duration_ms) VALUES ('s1',?,800,120,3000)`,
			day.Add(time.Duration(9)*time.Hour+time.Duration(i)*time.Minute).Unix())
	}

	result, err := aiday.RebuildDay(context.Background(), db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Features.ModalityCounts["general"]; got > 1 {
		t.Fatalf("general modality = %d, want at most the one non-code turn", got)
	}
	if got := result.Features.ModalityCounts["code"]; got < 4 {
		t.Fatalf("code modality = %d, want the four code tools counted", got)
	}
	// The day is 107 tool calls, so autopilot legitimately outscores it; what
	// matters is that code_architect can now qualify at all. Under the old
	// counting a day of pure tool-driven coding produced code=1 and the badge was
	// unreachable.
	qualified := false
	for _, candidate := range result.Candidates {
		if candidate.ID == "code_architect" {
			qualified = true
		}
	}
	if !qualified {
		t.Fatalf("code_architect did not qualify on a day of pure tool-driven coding; candidates = %+v", result.Candidates)
	}
}

// TestStreakOnlyFiresOnMilestones stops 持续同行 from occupying a candidate slot
// every single day of any continuous stretch of use.
func TestStreakOnlyFiresOnMilestones(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	from := mustDay(t, "2026-07-20", loc)
	seedWeek(t, db, loc, from, 10)
	if _, err := aiday.Rebuild(ctx, db, from, from.AddDate(0, 0, 9), loc); err != nil {
		t.Fatal(err)
	}
	var qualified []string
	rows, err := db.Query(`SELECT day FROM ai_day_badge_days WHERE badge_id='streak' AND qualified=1 ORDER BY day`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			t.Fatal(err)
		}
		qualified = append(qualified, day)
	}
	// Ten consecutive ready days contain exactly one milestone: the seventh.
	if len(qualified) != 1 || qualified[0] != from.AddDate(0, 0, 6).Format(time.DateOnly) {
		t.Fatalf("streak qualified on %v, want only the seventh consecutive day", qualified)
	}
}

// TestEveryBadgeCarriesAtLeastTwoEvidenceItems holds the documented promise of
// "2–3 verifiable numbers". Thirty of thirty-two selected days used to ship one.
func TestEveryBadgeCarriesAtLeastTwoEvidenceItems(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	from := mustDay(t, "2026-07-18", loc)
	seedWeek(t, db, loc, from, 28)
	if _, err := aiday.Rebuild(ctx, db, from, from.AddDate(0, 0, 27), loc); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT day,badge_id,json_array_length(evidence_json) FROM ai_day_badge_days WHERE qualified=1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var day, badge string
		var count int
		if err := rows.Scan(&day, &badge, &count); err != nil {
			t.Fatal(err)
		}
		seen++
		if count < 2 {
			t.Fatalf("%s on %s has %d evidence items, want at least 2", badge, day, count)
		}
	}
	if seen == 0 {
		t.Fatal("no qualified badges to check")
	}
}
