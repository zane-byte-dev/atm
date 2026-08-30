package parser

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func DiscoverQoderWork() []string {
	db, ok := openQoderWorkDB()
	if !ok {
		return nil
	}
	defer db.Close()
	rows, err := db.Query(`SELECT sc.id
		FROM sub_chats sc
		JOIN chats c ON c.id = sc.chat_id
		WHERE c.deleted_at IS NULL
		ORDER BY COALESCE(sc.created_at, c.created_at), sc.id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			paths = append(paths, "qoderwork://"+id)
		}
	}
	return paths
}

func openQoderWorkDB() (*sql.DB, bool) {
	if _, err := os.Stat(config.QoderWorkDB); err != nil {
		return nil, false
	}
	db, err := openReadOnlySQLite(config.QoderWorkDB)
	if err != nil {
		return nil, false
	}
	return db, true
}

func QoderWorkParseFile(virtualPath string) *ParsedFile {
	id := strings.TrimPrefix(virtualPath, "qoderwork://")
	if id == virtualPath || id == "" {
		return nil
	}
	db, ok := openQoderWorkDB()
	if !ok {
		return nil
	}
	defer db.Close()

	var subChatName, mode, modelLevel, chatName, projectName, projectPath string
	var subCreated, subUpdated, chatCreated, chatUpdated int64
	err := db.QueryRow(`SELECT COALESCE(sc.name,''), COALESCE(sc.mode,''), COALESCE(sc.model_level,''),
		COALESCE(sc.created_at,0), COALESCE(sc.updated_at,0), COALESCE(c.name,''),
		COALESCE(c.created_at,0), COALESCE(c.updated_at,0), COALESCE(p.name,''), COALESCE(p.path,'')
		FROM sub_chats sc
		JOIN chats c ON c.id = sc.chat_id
		JOIN projects p ON p.id = c.project_id
		WHERE sc.id = ? AND c.deleted_at IS NULL`, id).
		Scan(&subChatName, &mode, &modelLevel, &subCreated, &subUpdated, &chatName, &chatCreated, &chatUpdated, &projectName, &projectPath)
	if err != nil {
		return nil
	}

	rows, err := db.Query(`SELECT role, COALESCE(searchable_text,''), parts, metadata,
		COALESCE(created_at,0), COALESCE(updated_at,0)
		FROM messages WHERE sub_chat_id = ? ORDER BY sequence`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var inputs, outputs []Message
	var messages []TranscriptMessage
	tools := map[string]int{}
	var skills []SkillEvent
	seenTools := map[string]bool{}
	usage := Usage{Model: modelLevel}
	var usageEvents []UsageEvent
	createdTS := firstNonZero(subCreated, chatCreated)
	lastTS := firstNonZero(subUpdated, chatUpdated, createdTS)
	for rows.Next() {
		var role, searchableText, partsJSON, metadataJSON string
		var messageCreated, messageUpdated int64
		if rows.Scan(&role, &searchableText, &partsJSON, &metadataJSON, &messageCreated, &messageUpdated) != nil {
			continue
		}
		ts := firstNonZero(messageCreated, messageUpdated)
		if ts > lastTS {
			lastTS = ts
		}
		var parts []map[string]any
		_ = json.Unmarshal([]byte(partsJSON), &parts)
		text := strings.TrimSpace(searchableText)
		if text == "" {
			text = qoderWorkText(parts)
		}
		if text != "" && (role == "user" || role == "assistant") {
			messages = append(messages, TranscriptMessage{Role: role, Content: text, TS: ts})
			if role == "user" {
				inputs = append(inputs, Message{Content: text, TS: ts})
			} else {
				outputs = append(outputs, Message{Content: text, TS: ts})
			}
		}
		for index, part := range parts {
			typeName := config.GetStr(part, "type")
			if !strings.HasPrefix(typeName, "tool-") || typeName == "tool-Thinking" || typeName == "tool-BashOutput" {
				continue
			}
			name := strings.TrimPrefix(typeName, "tool-")
			key := firstNonEmpty(config.GetStr(part, "toolUseId"), config.GetStr(part, "toolCallId"), config.GetStr(part, "id"))
			if key == "" {
				key = role + ":" + strconv.FormatInt(ts, 10) + ":" + name + ":" + strconv.Itoa(index)
			}
			if !seenTools[key] {
				seenTools[key] = true
				tools[name]++
				skills = appendSkillEvent(skills, skillFromToolCall(name, config.GetMap(part, "input")), ts)
			}
		}
		if role == "assistant" {
			var metadata map[string]any
			if json.Unmarshal([]byte(metadataJSON), &metadata) == nil {
				in, _ := config.GetFloat(metadata, "inputTokens")
				out, _ := config.GetFloat(metadata, "outputTokens")
				if in != 0 || out != 0 {
					event := UsageEvent{Model: modelLevel, TS: ts, InputTokens: int64(in), OutputTokens: int64(out)}
					usage.InputTokens += event.InputTokens
					usage.OutputTokens += event.OutputTokens
					usage.RequestCount++
					usageEvents = append(usageEvents, event)
				}
			}
		}
	}
	if len(messages) == 0 && len(tools) == 0 {
		return nil
	}
	project := config.ProjectFromPath(projectPath)
	if project == "" {
		project = config.CanonicalProject(projectName)
	}
	summary := qoderWorkSummary(firstNonEmpty(subChatName, chatName), inputs)
	createdAt := ""
	if createdTS > 0 {
		createdAt = time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
	}
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return &ParsedFile{SessionID: "qoderwork:" + id, ShortID: shortID, Agent: "qoderwork", Project: project,
		CreatedAt: createdAt, CreatedTS: createdTS, LastTS: lastTS, Summary: summary,
		Inputs: inputs, Outputs: outputs, Messages: messages, Tools: tools, Skills: compactSkillEvents(skills), Usage: usage, UsageEvents: usageEvents}
}

func qoderWorkSummary(title string, inputs []Message) string {
	title = strings.TrimSpace(title)
	for _, input := range inputs {
		path := qoderWorkAttachedPath(input.Content)
		if path == "" {
			continue
		}
		base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		if base == "" || strings.Contains(title, base) {
			break
		}
		if title == "" {
			return truncateText(base, 200)
		}
		return truncateText(title+" · "+base, 200)
	}
	return truncateText(title, 200)
}

func qoderWorkAttachedPath(content string) string {
	const marker = "@[file:local:"
	start := strings.Index(content, marker)
	if start < 0 {
		return ""
	}
	value := content[start+len(marker):]
	if end := strings.IndexByte(value, ']'); end >= 0 {
		value = value[:end]
	}
	return strings.TrimSpace(value)
}

func qoderWorkText(parts []map[string]any) string {
	var blocks []string
	for _, part := range parts {
		if config.GetStr(part, "type") != "text" {
			continue
		}
		if text := strings.TrimSpace(config.GetStr(part, "text")); text != "" {
			blocks = append(blocks, text)
		}
	}
	return strings.Join(blocks, "\n\n")
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func QoderWorkLiveSessions(maxAge time.Duration) []Session {
	db, ok := openQoderWorkDB()
	if !ok {
		return nil
	}
	defer db.Close()
	cutoff := time.Now().Add(-maxAge).Unix()
	rows, err := db.Query(`SELECT sc.id, COALESCE(sc.name,''), COALESCE(sc.model_level,''),
		COALESCE(sc.updated_at, c.updated_at, sc.created_at, c.created_at, 0), COALESCE(p.name,''), COALESCE(p.path,''),
		COALESCE((SELECT m.searchable_text FROM messages m WHERE m.sub_chat_id = sc.id
			AND m.role = 'user' ORDER BY m.sequence LIMIT 1), '')
		FROM sub_chats sc JOIN chats c ON c.id = sc.chat_id JOIN projects p ON p.id = c.project_id
		WHERE c.deleted_at IS NULL AND COALESCE(sc.updated_at, c.updated_at, sc.created_at, c.created_at, 0) >= ?
		ORDER BY COALESCE(sc.updated_at, c.updated_at, sc.created_at, c.created_at, 0) DESC`, cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var sessions []Session
	now := time.Now()
	for rows.Next() {
		var id, summary, model, projectName, projectPath, firstQ string
		var updated int64
		if rows.Scan(&id, &summary, &model, &updated, &projectName, &projectPath, &firstQ) != nil {
			continue
		}
		summary = qoderWorkSummary(summary, []Message{{Content: firstQ}})
		project := config.ProjectFromPath(projectPath)
		if project == "" {
			project = config.CanonicalProject(projectName)
		}
		shortID := id
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		sessions = append(sessions, Session{Tool: "QoderWork", Project: project, SessionID: shortID, Model: model, Summary: summary, AgeSeconds: int(now.Sub(time.Unix(updated, 0)).Seconds())})
	}
	return sessions
}
