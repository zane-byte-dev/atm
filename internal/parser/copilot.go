package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

type CopilotTranscript struct {
	Path      string
	Project   string
	SessionID string
}

func FindCopilotTranscripts() []CopilotTranscript {
	pattern := filepath.Join(config.CopilotWorkspaces, "*", "GitHub.copilot-chat", "transcripts", "*.jsonl")
	files, _ := filepath.Glob(pattern)
	var result []CopilotTranscript
	for _, fp := range files {
		sid := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
		wsDir := filepath.Dir(filepath.Dir(filepath.Dir(fp)))
		project := copilotProjectName(wsDir)
		result = append(result, CopilotTranscript{Path: fp, Project: project, SessionID: sid})
	}
	return result
}

func copilotProjectName(wsDir string) string {
	wsFile := filepath.Join(wsDir, "workspace.json")
	data, err := os.ReadFile(wsFile)
	if err != nil {
		return ""
	}
	var ws map[string]any
	if json.Unmarshal(data, &ws) != nil {
		return ""
	}
	folder := config.GetStr(ws, "folder")
	folder = strings.TrimPrefix(folder, "file://")
	if folder != "" {
		return config.ProjectFromPath(folder)
	}
	return ""
}

func copilotParseTranscript(fp string, filterLen bool) (startTime time.Time, inputs, outputs []Message, tools map[string]int, skills []SkillEvent, files []string) {
	tools = map[string]int{}
	fileSet := map[string]bool{}
	curAssistant := ""
	var curAssistantTS int64
	scanJSONL(fp, func(r map[string]any) bool {
		typ := config.GetStr(r, "type")
		data := config.GetMap(r, "data")
		if data == nil {
			return true
		}
		var eventTS int64
		if !startTime.IsZero() {
			eventTS = startTime.Unix()
		}
		if dt, ok := firstTimestamp([]string{"timestamp", "time", "created_at", "createdAt", "ts"}, r, data); ok {
			eventTS = dt.Unix()
		}
		switch typ {
		case "session.start":
			if parsed, ok := firstTimestamp([]string{"startTime", "started_at", "startedAt", "timestamp", "time", "created_at", "createdAt", "ts"}, data, r); ok {
				startTime = parsed.In(config.Loc)
			}
		case "user.message":
			if len(inputs) > 0 && curAssistant != "" {
				outputs = append(outputs, Message{Content: curAssistant, TS: curAssistantTS})
				curAssistant = ""
				curAssistantTS = 0
			}
			content := config.GetStr(data, "content")
			if isCopilotNoise(content) {
				return true
			}
			if filterLen && len(content) >= 500 {
				return true
			}
			inputs = append(inputs, Message{Content: content, TS: eventTS})
		case "assistant.message":
			txt := strings.TrimSpace(config.GetStr(data, "content"))
			if len(txt) > 2 {
				curAssistant = appendTextBlock(curAssistant, txt)
				curAssistantTS = eventTS
			}
		case "tool.execution_start":
			toolName := config.GetStr(data, "tool_name")
			if toolName == "" {
				toolName = config.GetStr(data, "toolName")
			}
			if toolName != "" {
				tools[toolName]++
			}
			argsStr := config.GetStr(data, "arguments")
			if argsStr != "" {
				var args map[string]any
				if json.Unmarshal([]byte(argsStr), &args) == nil {
					skills = appendSkillEvent(skills, skillFromToolCall(toolName, args), eventTS)
					fp := config.GetStr(args, "filePath")
					if fp == "" {
						fp = config.GetStr(args, "file_path")
					}
					if fp != "" {
						fileSet[filepath.Base(fp)] = true
					}
				}
			}
		}
		return true
	})
	if curAssistant != "" {
		outputs = append(outputs, Message{Content: curAssistant, TS: curAssistantTS})
	}
	for name := range fileSet {
		files = append(files, name)
	}
	sort.Strings(files)
	skills = compactSkillEvents(skills)
	return
}

func isCopilotNoise(txt string) bool {
	t := strings.TrimSpace(txt)
	if len(t) < 3 {
		return true
	}
	if strings.HasPrefix(t, "[Terminal ") {
		return true
	}
	return false
}

func CopilotParseFile(fp, project string) *ParsedFile {
	sid := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	fullID := copilotFullSessionID(fp, sid)
	shortID := sid
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	startTime, inputs, outputs, tools, skills, _ := copilotParseTranscript(fp, false)
	if len(inputs) == 0 && len(tools) == 0 {
		return nil
	}
	var createdTS int64
	createdAt := ""
	if !startTime.IsZero() {
		createdTS = startTime.Unix()
		createdAt = startTime.Format("01-02 15:04")
	}
	lastTS := createdTS
	for _, msg := range inputs {
		if msg.TS > lastTS {
			lastTS = msg.TS
		}
	}
	for _, msg := range outputs {
		if msg.TS > lastTS {
			lastTS = msg.TS
		}
	}
	return &ParsedFile{
		SessionID: fullID,
		ShortID:   shortID,
		Agent:     "copilot",
		Project:   project,
		CreatedAt: createdAt,
		CreatedTS: createdTS,
		LastTS:    lastTS,
		Inputs:    inputs,
		Outputs:   outputs,
		Tools:     tools,
		Skills:    skills,
		EndOffset: fileSize(fp),
	}
}

func copilotFullSessionID(fp, sid string) string {
	transcriptsDir := filepath.Dir(fp)
	if filepath.Base(transcriptsDir) != "transcripts" {
		return sid
	}
	chatDir := filepath.Dir(transcriptsDir)
	if filepath.Base(chatDir) != "GitHub.copilot-chat" {
		return sid
	}
	workspaceDir := filepath.Dir(chatDir)
	workspaceID := filepath.Base(workspaceDir)
	if workspaceID == "." || workspaceID == "" {
		return sid
	}
	return "copilot:" + workspaceID + ":" + sid
}

func CopilotLiveSessions(maxAge time.Duration) []Session {
	var sessions []Session
	now := time.Now().In(config.Loc)
	for _, t := range FindCopilotTranscripts() {
		info, err := os.Stat(t.Path)
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		if age > maxAge {
			continue
		}

		_, inputs, _, tools, _, files := copilotParseTranscript(t.Path, true)

		var recentTools []string
		for k := range tools {
			recentTools = append(recentTools, k)
		}
		sort.Strings(recentTools)
		if len(recentTools) > 5 {
			recentTools = recentTools[:5]
		}

		var lastUserMsg string
		if len(inputs) > 0 {
			lastUserMsg = inputs[len(inputs)-1].Content
			lastUserMsg = truncateText(lastUserMsg, 200)
		}

		var lastAsst string
		if len(files) > 5 {
			files = files[:5]
		}
		if len(files) > 0 {
			lastAsst = "files: " + strings.Join(files, ", ")
		}

		shortSid := t.SessionID
		if len(shortSid) > 8 {
			shortSid = shortSid[:8]
		}

		var firstQ string
		if len(inputs) > 0 {
			firstQ = inputs[0].Content
			firstQ = truncateText(firstQ, 200)
		}

		sessions = append(sessions, Session{
			Tool:          "Copilot",
			Project:       t.Project,
			SessionID:     shortSid,
			AgeSeconds:    int(age.Seconds()),
			FirstQ:        firstQ,
			LastUserMsg:   lastUserMsg,
			RecentTools:   recentTools,
			LastAssistant: lastAsst,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].AgeSeconds < sessions[j].AgeSeconds
	})
	return sessions
}
