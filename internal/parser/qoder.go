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
			if len(c) > 0 && len(c) < 500 && !looksEncrypted(c) {
				inputs = append(inputs, Message{Content: c, TS: ts})
			}
		} else if role == "assistant" {
			if content.Valid && len(content.String) > 0 && !looksEncrypted(content.String) {
				outputs = append(outputs, Message{Content: truncateText(content.String, 2000), TS: ts})
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

func looksEncrypted(s string) bool {
	if len(s) < 20 {
		return false
	}
	nonAlnum := 0
	for _, c := range s[:min(len(s), 100)] {
		if c == '+' || c == '/' || c == '=' {
			nonAlnum++
		}
	}
	return nonAlnum > 5
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
