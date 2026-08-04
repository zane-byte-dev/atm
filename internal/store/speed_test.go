package store

import (
	"database/sql"
	"testing"
	"time"
)

// seedSpeedSession installs one session with its requests. Requests are given as
// (ts, output tokens, duration ms) so a fixture reads as a timeline.
func seedSpeedSession(t *testing.T, db *sql.DB, id, agent, model string, requests [][3]int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path, created_ts, last_ts)
		VALUES (?, ?, ?, 'proj', ?, 1000, 100000)`, id, id, agent, "/tmp/"+id); err != nil {
		t.Fatal(err)
	}
	for i, r := range requests {
		if _, err := db.Exec(`INSERT INTO usage_events (session_id, model, ts, input_tokens, output_tokens,
			cost_usd, fingerprint, request_count, duration_ms) VALUES (?, ?, ?, 0, ?, 0, ?, 1, ?)`,
			id, model, r[0], r[1], id+":"+model+":"+string(rune('a'+i)), r[2]); err != nil {
			t.Fatal(err)
		}
	}
}

func seedUserMessage(t *testing.T, db *sql.DB, sessionID string, seq int, ts int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO messages (session_id, seq, role, content, ts)
		VALUES (?, ?, 'user', 'ask', ?)`, sessionID, seq, ts); err != nil {
		t.Fatal(err)
	}
}

// The sampling window is what separates a slow model from a broken measurement,
// and what it rejects has to be reported rather than dropped: a table that says
// "50 tok/s" over two of a thousand requests is not the same claim as one that
// says it over all of them.
func TestSpeedStatsSamplesOnlyMeasurableRequests(t *testing.T) {
	db := openTempDB(t)
	seedSpeedSession(t, db, "s1", "claude", "claude-opus-5", [][3]int64{
		{2000, 500, 10_000}, // 50 tok/s
		{2100, 300, 10_000}, // 30 tok/s
		{2200, 400, 10_000}, // 40 tok/s
		{2300, 900, 10_000}, // 90 tok/s
		{2400, 200, 0},      // no window at all
		{2500, 200, 50},     // faster than the log's own resolution
		{2600, 200, 900_000},
		{2700, 5, 5_000}, // too small to say anything about generation rate
	})

	report, err := GetSpeedStats(db, 0, 100000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("models = %#v", report.Models)
	}
	got := report.Models[0]
	if got.Requests != 8 || got.Sampled != 4 {
		t.Fatalf("requests = %d, sampled = %d; want 8 and 4", got.Requests, got.Sampled)
	}
	// Nearest-rank on [30, 40, 50, 90]: p50 is the 2nd sample, p90 the 4th.
	if got.TokensPerSecondP50 != 40 || got.TokensPerSecondP90 != 90 {
		t.Fatalf("p50 = %.1f, p90 = %.1f; want 40 and 90", got.TokensPerSecondP50, got.TokensPerSecondP90)
	}
	// 2100 tokens over 40s, not the mean of the four rates.
	if got.TokensPerSecondWeighted != 52.5 {
		t.Fatalf("weighted = %.2f, want 52.5", got.TokensPerSecondWeighted)
	}
	if got.OutputTokens != 2100 || got.SampledSeconds != 40 {
		t.Fatalf("sums = %d tokens / %.1fs; want 2100 / 40", got.OutputTokens, got.SampledSeconds)
	}
	if report.Untimed != 1 {
		t.Fatalf("untimed = %d, want 1", report.Untimed)
	}
	if report.OutOfWindow != 3 {
		t.Fatalf("out of window = %d, want 3", report.OutOfWindow)
	}
}

// Grok reports one turn's API time across every model call in it, so the sampling
// window has to be judged per call: a five-call turn taking two minutes is
// ordinary, and rejecting it would drop the only agent that measures itself.
func TestSpeedStatsJudgesAggregatedTurnsPerModelCall(t *testing.T) {
	db := openTempDB(t)
	if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path, created_ts, last_ts)
		VALUES ('g1','g1','grokbuild','proj','/tmp/g1',1000,100000)`); err != nil {
		t.Fatal(err)
	}
	// Five calls, 114s of API time, 7328 output tokens: 22.8s and 1466 tokens per
	// call, and 64.3 tok/s over the turn.
	if _, err := db.Exec(`INSERT INTO usage_events (session_id, model, ts, output_tokens,
		fingerprint, request_count, duration_ms)
		VALUES ('g1','grok-4.5-build',2000,7328,'g:1',5,114049)`); err != nil {
		t.Fatal(err)
	}
	// Ten calls but only 40 tokens each: too small to say anything about a rate.
	if _, err := db.Exec(`INSERT INTO usage_events (session_id, model, ts, output_tokens,
		fingerprint, request_count, duration_ms)
		VALUES ('g1','grok-4.5-build',2100,190,'g:2',10,60000)`); err != nil {
		t.Fatal(err)
	}

	// One human message, so both rows land in the same turn.
	seedUserMessage(t, db, "g1", 0, 1_900)

	report, err := GetSpeedStats(db, 0, 100_000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("models = %#v", report.Models)
	}
	got := report.Models[0]
	if got.Requests != 15 {
		t.Fatalf("requests = %d, want 15 (both turns' calls)", got.Requests)
	}
	// Calls per turn, not rows per turn: a row here covers a whole turn's calls.
	if len(report.Turns) != 1 || report.Turns[0].RequestsPerTurn != 15 {
		t.Fatalf("turns = %#v; want one turn of 15 calls", report.Turns)
	}
	// Sampled counts calls, not rows, so it can be read against Requests.
	if got.Sampled != 5 {
		t.Fatalf("sampled = %d, want 5", got.Sampled)
	}
	if report.OutOfWindow != 10 {
		t.Fatalf("out of window = %d, want 10 (the small-token turn's calls)", report.OutOfWindow)
	}
	if diff := got.TokensPerSecondP50 - 64.25; diff > 0.1 || diff < -0.1 {
		t.Fatalf("p50 = %.2f, want ≈64.25 (the turn's aggregate rate)", got.TokensPerSecondP50)
	}
	// The duration column stays per call, so it is comparable across agents.
	if diff := got.DurationP50Seconds - 22.81; diff > 0.1 || diff < -0.1 {
		t.Fatalf("duration p50 = %.2f, want ≈22.81 (114.049s over 5 calls)", got.DurationP50Seconds)
	}
}

// A model nobody can measure still belongs in the table — as a row with no rate,
// so it reads as unmeasured rather than absent.
func TestSpeedStatsKeepsModelsWithNoSamples(t *testing.T) {
	db := openTempDB(t)
	seedSpeedSession(t, db, "s1", "qoder", "qoder-model", [][3]int64{
		{2000, 400, 0},
		{2100, 500, 0},
	})
	report, err := GetSpeedStats(db, 0, 100000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Models) != 1 {
		t.Fatalf("models = %#v", report.Models)
	}
	if report.Models[0].Requests != 2 || report.Models[0].Sampled != 0 {
		t.Fatalf("row = %#v", report.Models[0])
	}
	if report.Models[0].TokensPerSecondP50 != 0 {
		t.Fatalf("unmeasured model reported a rate: %#v", report.Models[0])
	}
}

// A turn runs from a human message to the end of the last reply before the next
// one, so tool time and every internal request count towards the wait.
func TestTurnWaitSpansEveryRequestInTheTurn(t *testing.T) {
	db := openTempDB(t)
	// A usage event's ts is where its response finished, so the last one in a turn
	// is where the wait ends.
	seedSpeedSession(t, db, "s1", "claude", "claude-opus-5", [][3]int64{
		{1_010, 300, 4_000}, // first turn: two replies, the later at 1050
		{1_050, 300, 5_000}, // → wait 50s
		{2_010, 300, 2_000}, // second turn → wait 10s
	})
	seedUserMessage(t, db, "s1", 0, 1_000)
	seedUserMessage(t, db, "s1", 1, 2_000)
	// A third message the model never answered: no requests, so no wait to report.
	seedUserMessage(t, db, "s1", 2, 3_000)

	report, err := GetSpeedStats(db, 0, 100_000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Turns) != 1 {
		t.Fatalf("turns = %#v", report.Turns)
	}
	got := report.Turns[0]
	if got.Agent != "claude" || got.Turns != 2 {
		t.Fatalf("turn row = %#v", got)
	}
	// Nearest-rank p50 over [10, 50] is the lower of the two; no interpolation.
	if got.WaitP50Seconds != 10 || got.WaitP90Seconds != 50 || got.WaitMaxSeconds != 50 {
		t.Fatalf("waits p50 = %.1f, p90 = %.1f, max = %.1f; want 10, 50, 50",
			got.WaitP50Seconds, got.WaitP90Seconds, got.WaitMaxSeconds)
	}
	if got.RequestsPerTurn != 1.5 {
		t.Fatalf("requests per turn = %.2f, want 1.5", got.RequestsPerTurn)
	}
}

// Turns belong to the session that holds them: two agents' messages must not be
// interleaved into each other's waits.
func TestTurnWaitKeepsSessionsApart(t *testing.T) {
	db := openTempDB(t)
	seedSpeedSession(t, db, "s1", "claude", "claude-opus-5", [][3]int64{{1_020, 300, 1_000}})
	seedSpeedSession(t, db, "s2", "codex", "gpt-5", [][3]int64{{1_500, 300, 1_000}})
	seedUserMessage(t, db, "s1", 0, 1_000)
	seedUserMessage(t, db, "s2", 0, 1_400)

	report, err := GetSpeedStats(db, 0, 100_000, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Turns) != 2 {
		t.Fatalf("turns = %#v", report.Turns)
	}
	waits := map[string]float64{}
	for _, row := range report.Turns {
		waits[row.Agent] = row.WaitP50Seconds
	}
	if waits["claude"] != 20 {
		t.Fatalf("claude wait = %.1f, want 20", waits["claude"])
	}
	if waits["codex"] != 100 {
		t.Fatalf("codex wait = %.1f, want 100", waits["codex"])
	}
}

// The per-bucket speed the desktop chart draws comes from two sums rather than a
// stored rate, so that merging models, clients or hours divides totals instead of
// averaging averages. Only sampled requests may contribute to them.
func TestModelDayStatsCarrySampledSpeedComponents(t *testing.T) {
	db := openTempDB(t)
	// Same day, one model: one sampled request and one below the token floor.
	seedSpeedSession(t, db, "s1", "claude", "claude-opus-5", [][3]int64{
		{1_800_000_000, 600, 12_000},
		{1_800_000_100, 5, 9_000},
	})
	stats, err := GetModelDayStats(db, 1_700_000_000, 1_900_000_000, "", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if stats[0].OutputTokens != 605 {
		t.Fatalf("output tokens = %d, want 605: totals still count every request", stats[0].OutputTokens)
	}
	if stats[0].MeasuredOutputTokens != 600 || stats[0].MeasuredDurationMS != 12_000 {
		t.Fatalf("measured = %d tokens / %d ms; want 600 / 12000",
			stats[0].MeasuredOutputTokens, stats[0].MeasuredDurationMS)
	}
}
