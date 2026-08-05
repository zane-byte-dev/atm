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

	rows, err := db.Query("SELECT session_id FROM chat_session")
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
	rows, err := db.Query(`SELECT s.session_id, s.session_title, s.project_name, s.mode, s.gmt_create
		FROM chat_session s
		WHERE s.last_user_query_at > ? OR s.gmt_modified > ?
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
		sessions = append(sessions, Session{
			Tool:       "Qoder",
			Project:    config.CanonicalProject(project),
			SessionID:  shortID,
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
func looksEncrypted(s string) bool {
	runes := []rune(s)
	if len(runes) < 20 {
		return false
	}
	head := runes[:min(len(runes), 100)]
	marks, inAlphabet := 0, 0
	for _, c := range head {
		switch {
		case c == '+' || c == '/' || c == '=':
			marks++
			inAlphabet++
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'):
			inAlphabet++
		}
	}
	// Every character in the alphabet, and enough padding//+ to be encoded data
	// rather than a long identifier.
	return marks > 5 && inAlphabet == len(head)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
