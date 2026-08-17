package store

import (
	"math"
	"testing"
)

func TestRecordBuiltinUsageWritesOneSessionAndOneEventPerCall(t *testing.T) {
	db := openTempDB(t)
	calls := []BuiltinModelCall{
		{Task: "collection", Model: "deepseek-v4-flash", InputTokens: 1200, OutputTokens: 90,
			CacheHitTokens: 896, DurationMS: 1300, TS: 1000, OK: true},
		{Task: "collection", Model: "deepseek-v4-flash", InputTokens: 400, OutputTokens: 30,
			DurationMS: 800, TS: 1100, OK: true},
	}

	if err := RecordBuiltinUsage(db, "atm-1000-42", calls); err != nil {
		t.Fatalf("record: %v", err)
	}

	var agent, project, filePath, summary string
	var createdTS, lastTS int64
	if err := db.QueryRow(`SELECT agent, project, file_path, summary, created_ts, last_ts
		FROM sessions WHERE id = 'atm-1000-42'`).
		Scan(&agent, &project, &filePath, &summary, &createdTS, &lastTS); err != nil {
		t.Fatalf("session row: %v", err)
	}
	if agent != BuiltinAgent || project != "" || filePath != "" {
		t.Fatalf("agent=%q project=%q file_path=%q", agent, project, filePath)
	}
	if summary != "collection ×2" {
		t.Fatalf("summary = %q", summary)
	}
	if createdTS != 1000 || lastTS != 1100 {
		t.Fatalf("created_ts=%d last_ts=%d, want the first and last call", createdTS, lastTS)
	}

	var events, requests int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(request_count),0)
		FROM usage_events WHERE session_id = 'atm-1000-42'`).Scan(&events, &requests); err != nil {
		t.Fatal(err)
	}
	if events != 2 || requests != 2 {
		t.Fatalf("events=%d requests=%d, want 2 and 2", events, requests)
	}
}

// DeepSeek 的 prompt_tokens 含 cache 命中；这些列是 Anthropic 语义（input 与 cache_read
// 不重叠），CalcCost 也按两种费率分别计价。不减这一刀，命中的部分就被按全价重算一遍。
func TestRecordBuiltinUsageSplitsCacheHitsOutOfInput(t *testing.T) {
	db := openTempDB(t)
	if err := RecordBuiltinUsage(db, "atm-1", []BuiltinModelCall{{
		Task: "todo-refine", Model: "deepseek-v4-flash",
		InputTokens: 1000, OutputTokens: 100, CacheHitTokens: 800, TS: 10, OK: true,
	}}); err != nil {
		t.Fatal(err)
	}

	var input, cacheRead, cacheCreate int64
	var cost float64
	if err := db.QueryRow(`SELECT input_tokens, cache_read_tokens, cache_create_tokens, cost_usd
		FROM usage_events WHERE session_id = 'atm-1'`).
		Scan(&input, &cacheRead, &cacheCreate, &cost); err != nil {
		t.Fatal(err)
	}
	if input != 200 || cacheRead != 800 || cacheCreate != 0 {
		t.Fatalf("input=%d cache_read=%d cache_create=%d, want 200/800/0", input, cacheRead, cacheCreate)
	}
	// 分开计价的结果必须比「1000 全按 input 价」便宜。
	full := CalcCost("deepseek-v4-flash", 1000, 100, 0, 0)
	if !(cost < full) {
		t.Fatalf("cost = %f, want less than the %f a non-split reading would charge", cost, full)
	}
	if math.Abs(cost-CalcCost("deepseek-v4-flash", 200, 100, 0, 800)) > 1e-12 {
		t.Fatalf("cost = %f, want the split reading", cost)
	}
}

// 端点没报 usage（或调用失败）时 token 是零。凑进 usage_events 只会给请求数和吞吐
// 速度掺水，失败本身已经进日志了。全都没 token 时连 session 都不该有。
func TestRecordBuiltinUsageSkipsCallsWithNothingToAccount(t *testing.T) {
	db := openTempDB(t)
	if err := RecordBuiltinUsage(db, "atm-empty", []BuiltinModelCall{
		{Task: "collection", Model: "deepseek-v4-flash", DurationMS: 45000, TS: 5},
	}); err != nil {
		t.Fatal(err)
	}

	var sessions, events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = 'atm-empty'`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage_events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || events != 0 {
		t.Fatalf("sessions=%d events=%d, want nothing written", sessions, events)
	}
}

// 一次失败夹在两次成功之间：成功的两笔照记，失败那笔不占请求数。
func TestRecordBuiltinUsageKeepsBillableCallsAroundAFailure(t *testing.T) {
	db := openTempDB(t)
	if err := RecordBuiltinUsage(db, "atm-mixed", []BuiltinModelCall{
		{Task: "collection", Model: "m", InputTokens: 100, OutputTokens: 10, TS: 1, OK: true},
		{Task: "collection", Model: "m", DurationMS: 45000, TS: 2},
		{Task: "collection", Model: "m", InputTokens: 200, OutputTokens: 20, TS: 3, OK: true},
	}); err != nil {
		t.Fatal(err)
	}

	var events int64
	var input, output int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		FROM usage_events WHERE session_id = 'atm-mixed'`).Scan(&events, &input, &output); err != nil {
		t.Fatal(err)
	}
	if events != 2 || input != 300 || output != 30 {
		t.Fatalf("events=%d input=%d output=%d", events, input, output)
	}
}

// usage 是 usage_events 的 per-session 卷积，跟各 Agent 走同一个 rollup。
func TestRecordBuiltinUsageRollsUpTheSessionTotals(t *testing.T) {
	db := openTempDB(t)
	if err := RecordBuiltinUsage(db, "atm-roll", []BuiltinModelCall{
		{Task: "collection", Model: "deepseek-v4-flash", InputTokens: 500, OutputTokens: 50,
			CacheHitTokens: 100, TS: 1, OK: true},
		{Task: "todo-refine", Model: "deepseek-v4-flash", InputTokens: 300, OutputTokens: 30, TS: 2, OK: true},
	}); err != nil {
		t.Fatal(err)
	}

	var model string
	var input, output, cacheRead, requests int64
	if err := db.QueryRow(`SELECT model, input_tokens, output_tokens, cache_read_tokens, request_count
		FROM usage WHERE session_id = 'atm-roll'`).
		Scan(&model, &input, &output, &cacheRead, &requests); err != nil {
		t.Fatalf("usage rollup: %v", err)
	}
	if model != "deepseek-v4-flash" || input != 700 || output != 80 || cacheRead != 100 || requests != 2 {
		t.Fatalf("rollup: model=%q input=%d output=%d cache_read=%d requests=%d",
			model, input, output, cacheRead, requests)
	}
}

// 记两次同一个会话（同一条命令重跑 flush）不该把账翻倍：fingerprint 唯一索引挡住它。
func TestRecordBuiltinUsageIsIdempotentForTheSameSession(t *testing.T) {
	db := openTempDB(t)
	calls := []BuiltinModelCall{
		{Task: "collection", Model: "m", InputTokens: 100, OutputTokens: 10, TS: 1, OK: true},
	}
	for range 2 {
		if err := RecordBuiltinUsage(db, "atm-twice", calls); err != nil {
			t.Fatal(err)
		}
	}

	var events, requests int64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(request_count),0)
		FROM usage_events WHERE session_id = 'atm-twice'`).Scan(&events, &requests); err != nil {
		t.Fatal(err)
	}
	if events != 1 || requests != 1 {
		t.Fatalf("events=%d requests=%d, want the second write to be ignored", events, requests)
	}
}

// ATM 的用量进各项统计（那是重点），但会话列表和日报的单位是「一次对话」——那些行
// 没有问答可读，默认挡掉，点名 --agent atm 才给。
func TestBuiltinSessionsStayOutOfConversationViewsButCountInStats(t *testing.T) {
	db := openTempDB(t)
	if err := RecordBuiltinUsage(db, "atm-view", []BuiltinModelCall{
		{Task: "collection", Model: "deepseek-v4-flash", InputTokens: 100, OutputTokens: 10, TS: 100, OK: true},
	}); err != nil {
		t.Fatal(err)
	}

	listed, err := ListSessions(db, 0, 1000, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("session list = %+v, want the builtin session hidden by default", listed)
	}
	named, err := ListSessions(db, 0, 1000, BuiltinAgent, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(named) != 1 {
		t.Fatalf("--agent atm listed %d sessions, want 1", len(named))
	}

	reported, err := GetReport(db, 0, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range reported {
		if row.Agent == BuiltinAgent {
			t.Fatal("the daily report must not narrate ATM's own model calls")
		}
	}

	stats, err := GetStats(db, 0, 1000, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range stats {
		if row.Agent == BuiltinAgent {
			found = true
			if row.OutputTokens != 10 {
				t.Fatalf("stats row = %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("ATM's spend must appear in the usage stats: that is the whole point of one pipeline")
	}
}

// 上面那条测试撞出来的既有 bug：GetReport 的时间条件原本没整体括起来，SQL 里 AND 比
// OR 结合更紧，于是 `--agent X` 只作用在 OR 的第二个分支上，窗口内任何 Agent 新建的
// 会话都会漏进来。
func TestGetReportAgentFilterAppliesToBothTimeBranches(t *testing.T) {
	db := openTempDB(t)
	for _, row := range []struct {
		id, agent string
		created   int64
	}{
		{"s-claude", "claude", 100},
		{"s-codex", "codex", 110},
	} {
		if _, err := db.Exec(`INSERT INTO sessions (id, short_id, agent, project, file_path,
			created_at, created_ts, summary, last_ts) VALUES (?, ?, ?, '', '', '', ?, '', ?)`,
			row.id, row.id, row.agent, row.created, row.created); err != nil {
			t.Fatal(err)
		}
	}

	reported, err := GetReport(db, 0, 1000, "claude")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range reported {
		if row.Agent != "claude" {
			t.Fatalf("--agent claude returned a %s session: %+v", row.Agent, row)
		}
	}
	if len(reported) != 1 {
		t.Fatalf("reported %d sessions, want only the claude one", len(reported))
	}
}
