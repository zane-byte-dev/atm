package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// BuiltinAgent is the agent name ATM's own text-model spend is recorded under.
//
// ATM 是 DeepSeek 的一个 client，跟 claude / codex 平级——「哪个 client 烧的 token」
// 本来就是 `sessions.agent` 这个维度在回答的问题，所以这些调用直接进 usage /
// usage_events，而不是另起一张表配一套查询。
//
// 它**不在** `parser.All()` 的注册表里，所以 `atm sync` 不会摄入它、活跃面板和菜单栏
// 也看不见它（那两处直接扫 transcript 文件，不查库）。会话列表和日报按会话叙事，
// 默认把它挡掉（见 ListSessions / GetReport），点名 `--agent atm` 才给。
const BuiltinAgent = "atm"

// BuiltinModelCall is one call ATM itself made to its built-in text model, with
// the token counts exactly as the endpoint reported them.
type BuiltinModelCall struct {
	// Task is textmodel's task name: collection / todo-refine / check.
	Task  string
	Model string
	// InputTokens is the endpoint's prompt total, **cache 命中包含在内**（DeepSeek 的
	// prompt_tokens 就是这个语义）。拆分留给这里做，见 RecordBuiltinUsage。
	InputTokens    int64
	OutputTokens   int64
	CacheHitTokens int64
	DurationMS     int64
	TS             int64
	OK             bool
}

// hasTokens reports whether the endpoint told us what this call cost.
func (c BuiltinModelCall) hasTokens() bool {
	return c.InputTokens > 0 || c.OutputTokens > 0 || c.CacheHitTokens > 0
}

// RecordBuiltinUsage writes one ATM command's model calls as a single session
// plus one usage event per call, through the same tables and the same pricing
// every agent goes through.
//
// 只有报了 token 的调用会成为 usage_event：一次超时的判定 token 是零，凑进去只会
// 给「请求数」和吞吐速度掺水，而它作为失败记录已经落在日志里了。全都没 token 时
// 连 session 都不建——宁可没有这一行，也不要一行空壳会话。
func RecordBuiltinUsage(db *sql.DB, sessionID string, calls []BuiltinModelCall) error {
	billable := make([]BuiltinModelCall, 0, len(calls))
	for _, call := range calls {
		if call.hasTokens() {
			billable = append(billable, call)
		}
	}
	if len(billable) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	firstTS, lastTS := billable[0].TS, billable[0].TS
	for _, call := range billable {
		if call.TS < firstTS {
			firstTS = call.TS
		}
		if call.TS > lastTS {
			lastTS = call.TS
		}
	}

	// file_path 留空：这个会话没有 transcript，空串就是「没有源文件」的意思。
	// 它也不在 sync_state 里，所以 forgetRemovedSources 永远不会来收它。
	if err := execTx(tx, `INSERT OR REPLACE INTO sessions
		(id, short_id, agent, project, file_path, created_at, created_ts, summary, last_ts)
		VALUES (?, ?, ?, '', '', ?, ?, ?, ?)`,
		sessionID, builtinShortID(sessionID), BuiltinAgent,
		// created_at 是给人看的那一列，跟各 parser 一样用 "01-02 15:04"。
		time.Unix(firstTS, 0).In(config.Loc).Format("01-02 15:04"),
		firstTS, builtinSummary(billable), lastTS); err != nil {
		return err
	}

	model := ""
	for index, call := range billable {
		// DeepSeek 的 prompt_tokens 含 cache 命中，而这些列是 Anthropic 语义：
		// input 与 cache_read 互不重叠，CalcCost 也按两种费率分别计价。不减这一刀
		// 就是把命中的部分按全价重复算一遍。
		input := call.InputTokens - call.CacheHitTokens
		if input < 0 {
			input = 0
		}
		cost := CalcCost(call.Model, input, call.OutputTokens, 0, call.CacheHitTokens)
		if err := execTx(tx, `INSERT OR IGNORE INTO usage_events (session_id, model, ts,
			input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, cost_usd,
			fingerprint, request_count, duration_ms)
			VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, 1, ?)`,
			sessionID, call.Model, call.TS, input, call.OutputTokens,
			call.CacheHitTokens, cost, builtinFingerprint(sessionID, index),
			call.DurationMS); err != nil {
			return err
		}
		if model == "" {
			model = call.Model
		}
	}

	if err := rollupUsageFromEvents(tx, sessionID, model); err != nil {
		return err
	}
	return tx.Commit()
}

// builtinShortID gives the session the same kind of handle `atm session show`
// takes for an agent session.
func builtinShortID(sessionID string) string {
	if len(sessionID) <= 8 {
		return sessionID
	}
	return sessionID[len(sessionID)-8:]
}

// builtinSummary names what the command spent its calls on, so the row reads as
// something when someone does ask for `--agent atm`.
func builtinSummary(calls []BuiltinModelCall) string {
	counts := map[string]int{}
	for _, call := range calls {
		task := strings.TrimSpace(call.Task)
		if task == "" {
			task = "unknown"
		}
		counts[task]++
	}
	tasks := make([]string, 0, len(counts))
	for task := range counts {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	parts := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if counts[task] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", task, counts[task]))
		} else {
			parts = append(parts, task)
		}
	}
	return strings.Join(parts, ", ")
}

// builtinFingerprint keeps the unique index happy without inventing identity:
// the session id is already unique to one process, so its index is enough.
func builtinFingerprint(sessionID string, index int) string {
	return fmt.Sprintf("%s#%d", sessionID, index)
}
