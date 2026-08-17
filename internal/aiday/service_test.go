package aiday_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestRebuildAggregatesByEventDayAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	dayOne := mustDay(t, "2026-08-13", loc)
	dayTwo := mustDay(t, "2026-08-14", loc)

	mustExec(t, db, `INSERT INTO sessions
		(id, short_id, agent, project, file_path, created_ts, last_ts)
		VALUES ('s1','s1','codex','atm','/tmp/s1',?,?),
		       ('s2','s2','claude','atm','/tmp/s2',?,?)`,
		dayOne.Add(9*time.Hour).Unix(), dayTwo.Add(10*time.Hour).Unix(),
		dayTwo.Add(8*time.Hour).Unix(), dayTwo.Add(11*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO messages(session_id, seq, role, content, ts) VALUES
		('s1',0,'user','day one private content',?),
		('s1',1,'user','continued private content',?),
		('s2',0,'user','other private content',?)`,
		dayOne.Add(9*time.Hour).Unix(), dayTwo.Add(10*time.Hour).Unix(), dayTwo.Add(8*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO tools(session_id, name, count) VALUES
		('s1','exec',4), ('s2','search',2)`)
	mustExec(t, db, `INSERT INTO usage_events
		(session_id, ts, input_tokens, output_tokens, duration_ms) VALUES
		('s1',?,1000,500,2500), ('s1',?,100,50,1000), ('s2',?,200,50,1000)`,
		dayOne.Add(9*time.Hour).Unix(), dayTwo.Add(10*time.Hour).Unix(), dayTwo.Add(8*time.Hour).Unix())

	if _, err := aiday.RebuildDay(context.Background(), db, dayOne, loc); err != nil {
		t.Fatalf("rebuild first day: %v", err)
	}
	first, err := aiday.RebuildDay(context.Background(), db, dayTwo, loc)
	if err != nil {
		t.Fatalf("rebuild second day: %v", err)
	}
	if first.Features.SessionCount != 2 || first.Features.SourceCount != 2 || first.Features.TurnCount != 2 {
		t.Fatalf("cross-day features = %+v", first.Features)
	}
	if first.Features.ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2 from sessions created on day two", first.Features.ToolCalls)
	}
	if first.Features.TotalTokens() != 400 {
		t.Fatalf("total tokens = %d, want 400", first.Features.TotalTokens())
	}
	if first.Concept == nil || first.Concept.ID != "model_conductor" {
		t.Fatalf("concept = %+v, want model conductor", first.Concept)
	}

	second, err := aiday.RebuildDay(context.Background(), db, dayTwo, loc)
	if err != nil {
		t.Fatalf("repeat rebuild: %v", err)
	}
	if second.Concept == nil || second.Concept.ID != first.Concept.ID {
		t.Fatalf("repeat concept = %+v, first = %+v", second.Concept, first.Concept)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_day_results WHERE day = '2026-08-14'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("result rows = %d, want 1", count)
	}
}

func TestRebuildUsesLatestThirtyNonEmptyBaselineDays(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	current := mustDay(t, "2026-08-15", loc)

	for i := 35; i >= 1; i-- {
		day := current.AddDate(0, 0, -i).Format(time.DateOnly)
		mustExec(t, db, `INSERT INTO ai_day_features
			(day, timezone, session_count, turn_count, tool_calls, source_count,
			 input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
			 generation_seconds, built_at, feature_version)
			VALUES (?, 'CST', 1, 1, 1, 1, ?, 0, 0, 0, 1, 1, 1)`, day, 100+i)
	}
	mustExec(t, db, `INSERT INTO sessions
		(id, short_id, agent, project, file_path, created_ts, last_ts)
		VALUES ('current','current','codex','atm','/tmp/current',?,?)`,
		current.Add(12*time.Hour).Unix(), current.Add(12*time.Hour).Unix())
	mustExec(t, db, `INSERT INTO usage_events
		(session_id, ts, input_tokens, output_tokens)
		VALUES ('current',?,2000000,1000000)`, current.Add(12*time.Hour).Unix())

	result, err := aiday.RebuildDay(context.Background(), db, current, loc)
	if err != nil {
		t.Fatal(err)
	}
	if result.BaselineDays != 30 {
		t.Fatalf("baseline days = %d, want 30", result.BaselineDays)
	}
	if result.Percentiles["total_tokens"] != 1 {
		t.Fatalf("token percentile = %v, want 1", result.Percentiles["total_tokens"])
	}
	if result.Concept == nil || result.Concept.ID != "autopilot" {
		t.Fatalf("concept = %+v, want autopilot", result.Concept)
	}

	loaded, err := aiday.Load(context.Background(), db, "2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != aiday.ContractVersion || loaded.Concept == nil || loaded.Concept.ID != result.Concept.ID {
		t.Fatalf("loaded contract = %+v", loaded)
	}
}

func TestEmptyDayHasNoInventedConcept(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	result, err := aiday.RebuildDay(context.Background(), db, mustDay(t, "2026-08-15", loc), loc)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "empty" || result.Concept != nil {
		t.Fatalf("empty result = %+v", result)
	}
}

func TestNormalizedEventsNeverRetainRawContent(t *testing.T) {
	db := openTestDB(t)
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('private-session','p','codex','atm','/tmp/p',?,?)`, day.Unix(), day.Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('private-session',0,'user','不对，请优化并解释为什么',?)`, day.Add(time.Hour).Unix())
	if _, err := aiday.RebuildDay(context.Background(), db, day, loc); err != nil {
		t.Fatal(err)
	}
	var rawRetained int
	var sessionHash, labels string
	if err := db.QueryRow(`SELECT raw_content_retained,session_hash,semantic_labels_json FROM ai_day_events WHERE event_type='turn'`).Scan(&rawRetained, &sessionHash, &labels); err != nil {
		t.Fatal(err)
	}
	if rawRetained != 0 || sessionHash == "private-session" {
		t.Fatalf("privacy contract retained=%d hash=%q", rawRetained, sessionHash)
	}
	for _, label := range []string{"correction", "refinement", "question", "directive", "explanation"} {
		if !strings.Contains(labels, label) {
			t.Fatalf("labels %s missing %s", labels, label)
		}
	}
}

func TestClassifierCoversEightDocumentedIntents(t *testing.T) {
	cases := map[string]string{"correction": "不对，应该是另一个值", "retry": "请重试一次", "refinement": "继续优化细节", "question": "为什么会这样？", "directive": "帮我实现功能", "acceptance": "很好，就这样通过", "brainstorm": "头脑风暴几个方案", "explanation": "解释一下原理"}
	for want, text := range cases {
		got := aiday.Classify(text)
		found := false
		for _, label := range got.Labels {
			if label == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Classify(%q)=%v, want %s", text, got.Labels, want)
		}
	}
}

func TestAtlasFeedbackAndPrivacyControls(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	day := mustDay(t, "2026-08-15", loc)
	mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES ('s','s','codex','atm','/tmp/s',?,?)`, day.Unix(), day.Unix())
	mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES ('s',0,'user','帮我实现功能',?)`, day.Unix())
	if _, err := aiday.RebuildDay(ctx, db, day, loc); err != nil {
		t.Fatal(err)
	}
	if err := aiday.SaveFeedback(ctx, db, aiday.Feedback{Day: "2026-08-15", Verdict: "corrected", CorrectedBadge: "code_architect"}); err != nil {
		t.Fatal(err)
	}
	result, err := aiday.RebuildDay(ctx, db, day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Badge == nil || result.Badge.ID != "code_architect" {
		t.Fatalf("corrected result=%+v", result.Badge)
	}
	atlas, err := aiday.LoadAtlas(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if atlas.Total != 12 || len(atlas.Badges) != 12 {
		t.Fatalf("atlas=%+v", atlas)
	}
	semantic := false
	retention := 30
	if err := aiday.SetPrivacy(ctx, db, &semantic, &retention); err != nil {
		t.Fatal(err)
	}
	privacy, err := aiday.LoadPrivacy(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if privacy.SemanticEnabled || privacy.RetentionDays != 30 || privacy.RawRetained {
		t.Fatalf("privacy=%+v", privacy)
	}
}

func TestBadgeLevelsAndInstantCooldown(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	loc := time.FixedZone("CST", 8*60*60)
	base := mustDay(t, "2026-08-09", loc)
	for offset := 0; offset < 7; offset++ {
		day := base.AddDate(0, 0, offset)
		session := fmt.Sprintf("deep-%d", offset)
		mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES (?,?,?,?,?,?,?)`, session, session, "codex", "atm", "/tmp/"+session, day.Unix(), day.Unix())
		for seq := 0; seq < 8; seq++ {
			mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES (?,?,'user','继续推进',?)`, session, seq, day.Add(time.Duration(seq)*time.Minute).Unix())
		}
		if _, err := aiday.RebuildDay(ctx, db, day, loc); err != nil {
			t.Fatal(err)
		}
	}
	atlas, err := aiday.LoadAtlas(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var deep aiday.Badge
	for _, badge := range atlas.Badges {
		if badge.ID == "deep_collaboration" {
			deep = badge
		}
	}
	if deep.Level != 2 || deep.QualifiedDays != 7 {
		t.Fatalf("deep badge=%+v, want L2 after 7/60 days", deep)
	}

	first := mustDay(t, "2026-08-20", loc)
	for dayOffset := 0; dayOffset < 2; dayOffset++ {
		day := first.AddDate(0, 0, dayOffset)
		for sourceIndex, source := range []string{"codex", "claude"} {
			session := fmt.Sprintf("instant-%d-%d", dayOffset, sourceIndex)
			mustExec(t, db, `INSERT INTO sessions(id,short_id,agent,project,file_path,created_ts,last_ts) VALUES (?,?,?,?,?,?,?)`, session, session, source, "atm", "/tmp/"+session, day.Unix(), day.Unix())
			mustExec(t, db, `INSERT INTO messages(session_id,seq,role,content,ts) VALUES (?,0,'user','推进',?)`, session, day.Unix())
		}
		result, err := aiday.RebuildDay(ctx, db, day, loc)
		if err != nil {
			t.Fatal(err)
		}
		if dayOffset == 0 && (result.Badge == nil || result.Badge.ID != "model_conductor") {
			t.Fatalf("first instant=%+v", result.Badge)
		}
		if dayOffset == 1 && result.Badge != nil && result.Badge.ID == "model_conductor" {
			t.Fatalf("instant badge ignored 14-day cooldown: %+v", result.Badge)
		}
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	oldDir, oldDB := config.AtmDir, config.AtmDB
	dir := t.TempDir()
	config.AtmDir = dir
	config.AtmDB = filepath.Join(dir, "atm.db")
	t.Cleanup(func() {
		config.AtmDir = oldDir
		config.AtmDB = oldDB
	})
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustDay(t *testing.T, value string, loc *time.Location) time.Time {
	t.Helper()
	day, err := time.ParseInLocation(time.DateOnly, value, loc)
	if err != nil {
		t.Fatal(err)
	}
	return day
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func mustExec(t *testing.T, db execer, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}
