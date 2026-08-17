package aiday

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type normalizedEvent struct {
	id, source, sessionHash, eventType, modality, executionMode           string
	occurredAt, quantity, input, output, cacheCreate, cacheRead, duration int64
	labels                                                                []string
	confidence                                                            float64
}

func normalizeDay(ctx context.Context, db *sql.DB, start, end time.Time) error {
	startTS, endTS := start.Unix(), end.Unix()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM ai_day_events WHERE occurred_at >= ? AND occurred_at < ?`, startTS, endTS); err != nil {
		return err
	}

	globalSemantic := settingBool(ctx, tx, "semantic_enabled", true)
	sourceState, err := loadSourceState(ctx, tx)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	insert := func(event normalizedEvent) error {
		// ATM's own model calls are recorded like any agent's. They are ATM
		// working for the user, not the user working with another AI, so letting
		// them through would manufacture a second "AI source" and hand out
		// 模型指挥家 for a background classifier call.
		if IsSelfSource(event.source) {
			return nil
		}
		state, ok := sourceState[event.source]
		if ok && !state.enabled {
			return nil
		}
		labels := event.labels
		confidence := event.confidence
		if !globalSemantic || (ok && !state.semantic) {
			labels = []string{}
			confidence = 0
		}
		labelsJSON, _ := json.Marshal(labels)
		if event.quantity == 0 {
			event.quantity = 1
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO ai_day_events
			(event_id, occurred_at, source, session_hash, event_type, quantity, modality, execution_mode,
			 input_tokens, output_tokens, cache_create_tokens, cache_read_tokens, duration_ms,
			 semantic_labels_json, semantic_confidence, raw_content_retained, schema_version, ingested_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
			ON CONFLICT(event_id) DO UPDATE SET occurred_at=excluded.occurred_at,
			 source=excluded.source, session_hash=excluded.session_hash, event_type=excluded.event_type, quantity=excluded.quantity,
			 modality=excluded.modality, execution_mode=excluded.execution_mode,
			 input_tokens=excluded.input_tokens, output_tokens=excluded.output_tokens,
			 cache_create_tokens=excluded.cache_create_tokens, cache_read_tokens=excluded.cache_read_tokens,
			 duration_ms=excluded.duration_ms, semantic_labels_json=excluded.semantic_labels_json,
			 semantic_confidence=excluded.semantic_confidence, raw_content_retained=0,
			 schema_version=excluded.schema_version, ingested_at=excluded.ingested_at`,
			event.id, event.occurredAt, event.source, event.sessionHash, event.eventType, event.quantity,
			event.modality, event.executionMode, event.input, event.output, event.cacheCreate,
			event.cacheRead, event.duration, string(labelsJSON), confidence, EventVersion, now)
		return err
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, agent, created_ts FROM sessions WHERE created_ts >= ? AND created_ts < ?`, startTS, endTS)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, source string
		var ts int64
		if err := rows.Scan(&id, &source, &ts); err != nil {
			rows.Close()
			return err
		}
		if source == "" {
			source = "unknown"
		}
		if IsSelfSource(source) {
			continue
		}
		if err := ensureSource(ctx, tx, source, now); err != nil {
			rows.Close()
			return err
		}
		if err := insert(normalizedEvent{id: eventID("session", id), source: source, sessionHash: sessionHash(id), eventType: "session", modality: "general", executionMode: "interactive", occurredAt: ts}); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	rows, err = tx.QueryContext(ctx, `SELECT m.session_id, m.seq, m.content, m.ts, COALESCE(NULLIF(s.agent,''),'unknown') FROM messages m JOIN sessions s ON s.id=m.session_id WHERE m.role='user' AND m.ts >= ? AND m.ts < ?`, startTS, endTS)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sessionID, content, source string
		var seq, ts int64
		if err := rows.Scan(&sessionID, &seq, &content, &ts, &source); err != nil {
			rows.Close()
			return err
		}
		if IsSelfSource(source) {
			continue
		}
		classification := Classify(content)
		if err := ensureSource(ctx, tx, source, now); err != nil {
			rows.Close()
			return err
		}
		if err := insert(normalizedEvent{id: eventID("message", sessionID, fmt.Sprint(seq)), source: source, sessionHash: sessionHash(sessionID), eventType: "turn", modality: modalityFromText(content), executionMode: "interactive", occurredAt: ts, labels: classification.Labels, confidence: classification.Confidence}); err != nil {
			rows.Close()
			return err
		}
		content = "" // make the transient lifetime explicit
	}
	if err := rows.Close(); err != nil {
		return err
	}

	rows, err = tx.QueryContext(ctx, `SELECT u.id, u.session_id, u.ts, u.input_tokens, u.output_tokens, u.cache_create_tokens, u.cache_read_tokens, u.duration_ms, COALESCE(NULLIF(s.agent,''),'unknown') FROM usage_events u JOIN sessions s ON s.id=u.session_id WHERE u.ts >= ? AND u.ts < ?`, startTS, endTS)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, ts int64
		var sessionID, source string
		var e normalizedEvent
		if err := rows.Scan(&id, &sessionID, &ts, &e.input, &e.output, &e.cacheCreate, &e.cacheRead, &e.duration, &source); err != nil {
			rows.Close()
			return err
		}
		if IsSelfSource(source) {
			continue
		}
		e.id, e.source, e.sessionHash, e.eventType, e.modality, e.executionMode, e.occurredAt = eventID("usage", fmt.Sprint(id)), source, sessionHash(sessionID), "usage", "general", "interactive", ts
		if err := ensureSource(ctx, tx, source, now); err != nil {
			rows.Close()
			return err
		}
		if err := insert(e); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}

	// `tools` is a per-session lifetime aggregate with no event timestamp
	// (PRIMARY KEY (session_id, name)), so a day has to be inferred. Attributing
	// every row to the session's creation day — the previous behaviour — put a
	// long session's entire tool history on the day it started, while turns and
	// usage for the same session were attributed by event time. Instead, split
	// each row across the days the session was measurably active, in proportion
	// to that day's share of the session's usage events. Sessions with no usage
	// rows to weight by fall back to the creation day.
	rows, err = tx.QueryContext(ctx, `
		SELECT t.session_id, t.name, t.count, s.created_ts,
		       COALESCE(NULLIF(s.agent,''),'unknown'),
		       (SELECT COUNT(*) FROM usage_events u WHERE u.session_id=t.session_id AND u.ts>=? AND u.ts<?) AS day_usage,
		       (SELECT COUNT(*) FROM usage_events u WHERE u.session_id=t.session_id) AS all_usage,
		       (SELECT COALESCE(MIN(u.ts),0) FROM usage_events u WHERE u.session_id=t.session_id AND u.ts>=? AND u.ts<?) AS first_day_usage
		FROM tools t JOIN sessions s ON s.id=t.session_id
		WHERE (s.created_ts >= ? AND s.created_ts < ?)
		   OR EXISTS (SELECT 1 FROM usage_events u WHERE u.session_id=t.session_id AND u.ts>=? AND u.ts<?)`,
		startTS, endTS, startTS, endTS, startTS, endTS, startTS, endTS)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sessionID, name, source string
		var count, createdTS, dayUsage, allUsage, firstDayUsage int64
		if err := rows.Scan(&sessionID, &name, &count, &createdTS, &source, &dayUsage, &allUsage, &firstDayUsage); err != nil {
			rows.Close()
			return err
		}
		quantity, occurredAt := count, createdTS
		switch {
		case allUsage > 0 && dayUsage > 0:
			// Round to nearest so the per-day shares sum back to the session
			// total rather than systematically undercounting.
			quantity = (count*dayUsage + allUsage/2) / allUsage
			occurredAt = firstDayUsage
		case allUsage > 0:
			quantity = 0 // active elsewhere, not on this day
		case createdTS < startTS || createdTS >= endTS:
			quantity = 0
		}
		if quantity <= 0 {
			continue
		}
		// The day is part of the id: one tool row can now yield an event on each
		// day the session was active, and a day-independent id would let the
		// second day's insert overwrite the first.
		if err := insert(normalizedEvent{id: eventID("tool", sessionID, name, start.Format(time.DateOnly)), source: source, sessionHash: sessionHash(sessionID), eventType: "tool", quantity: quantity, modality: modalityFromTool(name), executionMode: "agentic", occurredAt: occurredAt}); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return tx.Commit()
}

type sourceStateValue struct{ enabled, semantic bool }

func loadSourceState(ctx context.Context, tx *sql.Tx) (map[string]sourceStateValue, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source, enabled, semantic_enabled FROM ai_day_sources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]sourceStateValue{}
	for rows.Next() {
		var s string
		var e, semantic bool
		if err := rows.Scan(&s, &e, &semantic); err != nil {
			return nil, err
		}
		result[s] = sourceStateValue{e, semantic}
	}
	return result, rows.Err()
}

func ensureSource(ctx context.Context, tx *sql.Tx, source string, now int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ai_day_sources(source, enabled, semantic_enabled, updated_at) VALUES (?,1,1,?) ON CONFLICT(source) DO NOTHING`, source, now)
	return err
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func settingBool(ctx context.Context, db queryRower, key string, fallback bool) bool {
	var value string
	if err := db.QueryRowContext(ctx, `SELECT value FROM ai_day_settings WHERE key=?`, key).Scan(&value); err != nil {
		return fallback
	}
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "on")
}

func eventID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}
func sessionHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:12])
}

func modalityFromText(text string) string {
	lower := strings.ToLower(text)
	for _, term := range []string{"image", "图片", "图像", "视觉", "video", "视频", "海报"} {
		if strings.Contains(lower, term) {
			return "visual"
		}
	}
	for _, term := range []string{"code", "代码", "bug", "测试", "compile", "api", "函数"} {
		if strings.Contains(lower, term) {
			return "code"
		}
	}
	for _, term := range []string{"文章", "文案", "写作", "document", "文档", "总结"} {
		if strings.Contains(lower, term) {
			return "writing"
		}
	}
	return "general"
}

// modalityFromTool is the reliable half of modality detection: a tool name is
// chosen by the agent for a purpose, unlike the wording of a user's message.
func modalityFromTool(name string) string {
	lower := strings.ToLower(name)
	for _, term := range []string{"image", "video", "render", "screenshot", "diagram", "figma"} {
		if strings.Contains(lower, term) {
			return "visual"
		}
	}
	for _, term := range []string{"websearch", "webfetch", "search", "browser", "fetch", "curl", "crawl"} {
		if strings.Contains(lower, term) {
			return "research"
		}
	}
	for _, term := range []string{
		"edit", "write", "read", "bash", "shell", "exec", "grep", "glob",
		"notebook", "patch", "diff", "code", "test", "build", "compile", "lint", "git",
	} {
		if strings.Contains(lower, term) {
			return "code"
		}
	}
	return "general"
}
