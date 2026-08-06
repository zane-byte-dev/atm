package parser

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func DiscoverQoder() []string {
	if _, err := os.Stat(config.QoderDB); err != nil {
		return nil
	}
	db, err := openReadOnlySQLite(config.QoderDB)
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query("SELECT session_id FROM chat_session" + qoderRootOnly(db))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var sid string
		if rows.Scan(&sid) == nil {
			paths = append(paths, "qoder://"+sid)
		}
	}
	return paths
}

// qoderRootOnly is the WHERE clause that keeps subagent sessions out of the
// session list, or the empty string on a schema that has no such concept.
//
// A Qoder agent run that delegates spawns a child row in chat_session with its
// own session_id, `agent_sub` mode and the spawning session in
// parent_session_id. Discovering those alongside the real ones indexes work the
// user never started as a session of its own: five of sixteen rows on a live
// database, each titled with the verbatim delegation prompt because Qoder puts
// the task text in session_title, so they surface in `session status` and
// `session list` as multi-hundred-character summaries and inflate the session
// and query counts in `stats`. Their token spend is real and is rolled into the
// parent instead — see qoderSubagentUsage.
//
// parent_session_id was added by an upstream migration, so its absence has to
// degrade to the unfiltered list rather than to no sessions at all.
func qoderRootOnly(db *sql.DB) string {
	if !qoderHasColumn(db, "chat_session", "parent_session_id") {
		return ""
	}
	return " WHERE COALESCE(parent_session_id,'') = ''"
}

func qoderHasColumn(db *sql.DB, table, column string) bool {
	rows, err := db.Query("SELECT 1 FROM pragma_table_info(?) WHERE name = ?", table, column)
	if err != nil {
		return false
	}
	defer rows.Close()
	return rows.Next()
}

func qoderIsSubagent(db *sql.DB, sid string) bool {
	if !qoderHasColumn(db, "chat_session", "parent_session_id") {
		return false
	}
	var parent string
	if db.QueryRow("SELECT COALESCE(parent_session_id,'') FROM chat_session WHERE session_id = ?", sid).Scan(&parent) != nil {
		return false
	}
	return parent != ""
}

// qoderAddSubagentUsage folds the spend of every session descended from sid into
// usage. Nesting is walked rather than assumed to be one level deep, since a
// delegated run may delegate again.
//
// Only tokens are rolled up. The transcript stays the parent's own: mixing a
// subagent's prompts into Inputs and Outputs would report a conversation the user
// did not have. Tool calls stay put for the same reason — the parent already
// counts the one call that did the delegating, and the child's calls are that
// call's internals, not additional work at this level.
func qoderAddSubagentUsage(db *sql.DB, sid string, usage *Usage) {
	if !qoderHasColumn(db, "chat_session", "parent_session_id") {
		return
	}
	rows, err := db.Query(`WITH RECURSIVE descendants(id) AS (
			SELECT session_id FROM chat_session WHERE parent_session_id = ?
			UNION
			SELECT c.session_id FROM chat_session c
				JOIN descendants d ON c.parent_session_id = d.id
		)
		SELECT m.token_info, m.model_info FROM chat_message m
			WHERE m.session_id IN (SELECT id FROM descendants)
				AND m.token_info IS NOT NULL AND m.token_info != ''`, sid)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var tokenInfo, modelInfo sql.NullString
		if rows.Scan(&tokenInfo, &modelInfo) != nil {
			continue
		}
		var ti struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			CachedTokens     int64 `json:"cached_tokens"`
		}
		if json.Unmarshal([]byte(tokenInfo.String), &ti) != nil {
			continue
		}
		usage.InputTokens += ti.PromptTokens - ti.CachedTokens
		usage.OutputTokens += ti.CompletionTokens
		usage.CacheReadTokens += ti.CachedTokens
		usage.RequestCount++
		if modelInfo.Valid && modelInfo.String != "" && usage.Model == "" {
			var mi struct {
				ModelKey string `json:"model_key"`
			}
			if json.Unmarshal([]byte(modelInfo.String), &mi) == nil && mi.ModelKey != "" {
				usage.Model = "qoder-" + mi.ModelKey
			}
		}
	}
}

func QoderParseFile(virtualPath string) *ParsedFile {
	sid := strings.TrimPrefix(virtualPath, "qoder://")
	if sid == virtualPath {
		return nil
	}
	if _, err := os.Stat(config.QoderDB); err != nil {
		return nil
	}
	db, err := openReadOnlySQLite(config.QoderDB)
	if err != nil {
		return nil
	}
	defer db.Close()

	var title, project, mode string
	var gmtCreate int64
	err = db.QueryRow("SELECT session_title, COALESCE(project_name,''), COALESCE(mode,''), gmt_create FROM chat_session WHERE session_id = ?", sid).
		Scan(&title, &project, &mode, &gmtCreate)
	if err != nil {
		return nil
	}
	// Discovery already filters these out; this also catches a virtual path held
	// over from an earlier scan, so a subagent row cannot come back as a session
	// through the incremental path.
	if qoderIsSubagent(db, sid) {
		return nil
	}

	createdTS := gmtCreate / 1000
	createdAt := time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
	shortID := sid
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	rows, err := db.Query(`SELECT role, content, token_info, model_info, gmt_create
		FROM chat_message WHERE session_id = ? ORDER BY gmt_create`, sid)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var inputs, outputs []Message
	// Messages carries the turns in order. Inputs/Outputs are kept alongside it
	// because the session views read them, but they cannot be the storage path
	// here: that fallback pairs one output to each input by index, and a Qoder
	// agent run answers a single prompt with many assistant messages — 27 prompts
	// against 154 replies on a real database, of which the pairing would keep 27.
	var messages []TranscriptMessage
	var usage Usage
	var lastTS int64
	tools := map[string]int{}

	for rows.Next() {
		var role string
		var content, tokenInfo, modelInfo sql.NullString
		var msgTS int64
		if rows.Scan(&role, &content, &tokenInfo, &modelInfo, &msgTS) != nil {
			continue
		}
		ts := msgTS / 1000
		if ts > lastTS {
			lastTS = ts
		}

		if role == "user" && content.Valid {
			c := content.String
			// Truncate rather than drop. This used to skip anything over 500 bytes,
			// presumably to keep IDE context out of the index — but Qoder inlines
			// that context into the prompt, so on a real database every single user
			// message exceeded it (shortest observed: 1048 bytes). The result was an
			// empty Inputs slice, and because the storage layer pairs outputs to
			// inputs by index, every assistant reply was discarded with them: five
			// sessions, 331 messages upstream, zero indexed. No other parser drops on
			// length; they all truncate.
			if len(c) > 0 && !looksEncrypted(c) {
				text := truncateText(c, 2000)
				inputs = append(inputs, Message{Content: text, TS: ts})
				messages = append(messages, TranscriptMessage{Role: "user", Content: text, TS: ts})
			}
		} else if role == "assistant" {
			if content.Valid && len(content.String) > 0 && !looksEncrypted(content.String) {
				text := truncateText(content.String, 2000)
				outputs = append(outputs, Message{Content: text, TS: ts})
				messages = append(messages, TranscriptMessage{Role: "assistant", Content: text, TS: ts})
			}
		} else if role == "tool" {
			tools["tool_call"]++
		}

		if tokenInfo.Valid && tokenInfo.String != "" {
			var ti struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
				CachedTokens     int64 `json:"cached_tokens"`
			}
			if json.Unmarshal([]byte(tokenInfo.String), &ti) == nil {
				usage.InputTokens += ti.PromptTokens - ti.CachedTokens
				usage.OutputTokens += ti.CompletionTokens
				usage.CacheReadTokens += ti.CachedTokens
				usage.RequestCount++
			}
		}
		if modelInfo.Valid && modelInfo.String != "" && usage.Model == "" {
			var mi struct {
				ModelKey string `json:"model_key"`
			}
			if json.Unmarshal([]byte(modelInfo.String), &mi) == nil && mi.ModelKey != "" {
				usage.Model = "qoder-" + mi.ModelKey
			}
		}
	}

	// The delegated work is this session's spend even though upstream books it
	// against a row of its own, and that row is no longer indexed, so without the
	// rollup those tokens would go unreported by every caller: 1.8M prompt tokens
	// across five subagent rows on a live database.
	qoderAddSubagentUsage(db, sid, &usage)

	if usage.InputTokens == 0 && usage.OutputTokens == 0 && len(inputs) == 0 {
		return nil
	}

	info, _ := os.Stat(config.QoderDB)
	var endOffset int64
	if info != nil {
		endOffset = info.Size()
	}

	return &ParsedFile{
		SessionID: sid,
		ShortID:   shortID,
		Agent:     "qoder",
		Project:   config.CanonicalProject(project),
		CreatedAt: createdAt,
		CreatedTS: createdTS,
		LastTS:    lastTS,
		Summary:   title,
		Inputs:    inputs,
		Outputs:   outputs,
		Messages:  messages,
		Tools:     tools,
		Usage:     usage,
		EndOffset: endOffset,
	}
}

func QoderLiveSessions(maxAge time.Duration) []Session {
	if _, err := os.Stat(config.QoderDB); err != nil {
		return nil
	}
	db, err := openReadOnlySQLite(config.QoderDB)
	if err != nil {
		return nil
	}
	defer db.Close()

	cutoff := time.Now().Add(-maxAge).UnixMilli()
	// Same exclusion as discovery: a delegated run is activity within its parent,
	// not a session of the user's own sitting in the live list beside it.
	subagentFilter := ""
	if qoderHasColumn(db, "chat_session", "parent_session_id") {
		subagentFilter = " AND COALESCE(s.parent_session_id,'') = ''"
	}
	rows, err := db.Query(`SELECT s.session_id, s.session_title, s.project_name, s.mode, s.gmt_create
		FROM chat_session s
		WHERE (s.last_user_query_at > ? OR s.gmt_modified > ?)`+subagentFilter+`
		ORDER BY s.gmt_create DESC`, cutoff, cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()

	now := time.Now()
	var sessions []Session
	for rows.Next() {
		var sid, title, project, mode string
		var gmtCreate int64
		if rows.Scan(&sid, &title, &project, &mode, &gmtCreate) != nil {
			continue
		}
		age := now.Sub(time.UnixMilli(gmtCreate))
		shortID := sid
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		// Qoder's hook payload carries the full session_id while SessionID is
		// truncated for display, so the untruncated one goes in ResumeID — the
		// field the notch joins hook events on.
		sessions = append(sessions, Session{
			Tool:       "Qoder",
			Project:    config.CanonicalProject(project),
			SessionID:  shortID,
			ResumeID:   sid,
			Summary:    title,
			AgeSeconds: int(age.Seconds()),
		})
	}
	return sessions
}

// looksEncrypted reports whether the text is a stored blob rather than prose —
// Qoder puts base64 payloads in the same column as conversation.
//
// The test is composition, not occurrence. Counting `+ / =` and rejecting at six
// meant any prose containing a couple of paths and a few plus signs — "go test
// ./... + go vet ./..." clears the threshold on its own — was discarded as a
// blob. What actually distinguishes base64 is that it is *only* base64: an
// unbroken run of the alphabet with no spaces, punctuation or non-Latin script.
// Prose and code always have some.
//
// The mark count cannot carry the decision either, in the other direction. Only
// three of the 64 code points are `+ / =`, so a 100-character window of real
// base64 holds two or three of them on average and a threshold of six almost
// never trips: on a live database every one of 528 messages was an unbroken
// base64 run and the old test recognised 3, leaving 525 ciphertext blobs indexed
// as conversation. What separates encoded bytes from the other things that run
// without spaces — hex digests, snake_case and camelCase identifiers, decimal
// IDs — is that base64 either reaches outside [0-9A-Za-z] or spends all three
// alphanumeric classes at once.
func looksEncrypted(s string) bool {
	runes := []rune(s)
	if len(runes) < 20 {
		return false
	}
	head := runes[:min(len(runes), 100)]
	var marks, upper, lower, digit, inAlphabet int
	for _, c := range head {
		switch {
		case c == '+' || c == '/' || c == '=':
			marks++
			inAlphabet++
		case c >= 'A' && c <= 'Z':
			upper++
			inAlphabet++
		case c >= 'a' && c <= 'z':
			lower++
			inAlphabet++
		case c >= '0' && c <= '9':
			digit++
			inAlphabet++
		}
	}
	if inAlphabet != len(head) {
		return false
	}
	return marks > 0 || (upper > 0 && lower > 0 && digit > 0)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
