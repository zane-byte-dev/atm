package parser

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

func isPiNoise(txt string) bool {
	t := strings.TrimSpace(txt)
	if len(t) < 3 {
		return true
	}
	for _, prefix := range []string{
		"<system-reminder>", "<system_context>",
		"<ide_opened_file>", "<ide_selection>",
		"[Request interrupted",
		"Base directory for this skill:",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// DiscoverPi lists all pi session JSONL transcript files under
// ~/.pi/agent/sessions/--<path>--/<timestamp>_<uuid>.jsonl.
func DiscoverPi() []string {
	var files []string
	entries, err := os.ReadDir(config.PiSessions)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(config.PiSessions, e.Name(), "*.jsonl"))
		files = append(files, matches...)
	}
	return files
}

// piMessageText extracts assistant/user visible text from a pi message content
// field (string or array of content blocks), skipping thinking/toolCall blocks.
func piMessageText(msg map[string]any) string {
	if s := config.GetStr(msg, "content"); s != "" {
		return s
	}
	var parts []string
	for _, item := range config.GetSlice(msg, "content") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if config.GetStr(m, "type") == "text" {
			if txt := strings.TrimSpace(config.GetStr(m, "text")); txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func PiParseFile(fp string) *ParsedFile {
	return piParse(fp, 0)
}

// PiParseAppend parses records appended after offset. Pi session files are
// append-only, so this avoids repeatedly decoding long-running sessions.
func PiParseAppend(fp string, offset int64) *ParsedFile {
	p := piParse(fp, offset)
	if p != nil {
		p.Append = true
	}
	return p
}

func piParse(fp string, offset int64) *ParsedFile {
	fullID := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	// stem is <timestamp>_<uuid>; use the uuid tail for a stable short id.
	shortID := fullID
	if idx := strings.LastIndex(fullID, "_"); idx >= 0 {
		shortID = fullID[idx+1:]
	}
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	var userMsgs, assistantMsgs []Message
	var messages []TranscriptMessage
	tools := map[string]int{}
	var skills []SkillEvent
	cwd := ""
	summary := ""
	curAssistant := ""
	var curAssistantTS int64
	var createdTS, lastTS int64
	createdAt := ""
	var usage Usage
	var usageEvents []UsageEvent
	currentModel := ""
	var timing durationTracker

	scanJSONLFrom(fp, offset, func(r map[string]any) bool {
		typ := config.GetStr(r, "type")
		var ts, tsMS int64
		if dt, ok := parseTimestampString(config.GetStr(r, "timestamp")); ok {
			ts = dt.Unix()
			tsMS = dt.UnixMilli()
			if createdTS == 0 {
				createdTS = ts
				createdAt = dt.Format("01-02 15:04")
			}
			lastTS = ts
		}
		// pi writes one record per assistant response, so that record is where the
		// model stopped; everything else in the log — the human's turn, a tool
		// result, a model switch — is what it read before starting.
		if typ == "message" && config.GetStr(config.GetMap(r, "message"), "role") == "assistant" {
			timing.Output(tsMS)
		} else {
			timing.Input(tsMS)
		}
		switch typ {
		case "session":
			cwd = config.GetStr(r, "cwd")
		case "session_info":
			if n := config.GetStr(r, "name"); n != "" {
				summary = n
			}
		case "model_change":
			if m := config.GetStr(r, "modelId"); m != "" {
				currentModel = m
				usage.Model = m
			}
		case "message":
			msg := config.GetMap(r, "message")
			if msg == nil {
				return true
			}
			switch config.GetStr(msg, "role") {
			case "user":
				txt := piMessageText(msg)
				if isPiNoise(txt) {
					return true
				}
				skills = appendSkillEvent(skills, skillFromCommand(txt), ts)
				if len(userMsgs) > 0 && curAssistant != "" {
					assistantMsgs = append(assistantMsgs, Message{Content: curAssistant, TS: curAssistantTS})
					curAssistant = ""
				}
				if len(txt) > 2 {
					userMsgs = append(userMsgs, Message{Content: txt, TS: ts})
					messages = append(messages, TranscriptMessage{Role: "user", Content: txt, TS: ts})
				}
			case "assistant":
				if m := config.GetStr(msg, "model"); m != "" {
					currentModel = m
					usage.Model = m
				}
				var response string
				for _, item := range config.GetSlice(msg, "content") {
					if m, ok := item.(map[string]any); ok {
						switch config.GetStr(m, "type") {
						case "toolCall":
							name := config.GetStr(m, "name")
							tools[name]++
							skills = appendSkillEvent(skills, skillFromToolCall(name, config.GetMap(m, "arguments")), ts)
						case "text":
							if txt := strings.TrimSpace(config.GetStr(m, "text")); len(txt) > 2 {
								curAssistant = appendTextBlock(curAssistant, txt)
								response = appendTextBlock(response, txt)
								curAssistantTS = ts
							}
						}
					}
				}
				if response != "" {
					messages = append(messages, TranscriptMessage{Role: "assistant", Content: response, TS: ts})
				}
				if u := config.GetMap(msg, "usage"); u != nil {
					in, _ := config.GetFloat(u, "input")
					out, _ := config.GetFloat(u, "output")
					cr, _ := config.GetFloat(u, "cacheRead")
					cw, _ := config.GetFloat(u, "cacheWrite")
					usage.InputTokens += int64(in)
					usage.OutputTokens += int64(out)
					usage.CacheReadTokens += int64(cr)
					usage.CacheCreateTokens += int64(cw)
					usage.RequestCount++
					usageEvents = append(usageEvents, UsageEvent{Model: currentModel, TS: ts,
						InputTokens: int64(in), OutputTokens: int64(out),
						CacheReadTokens: int64(cr), CacheCreateTokens: int64(cw),
						DurationMS: timing.Measure()})
				}
			}
		}
		return true
	})
	if curAssistant != "" {
		assistantMsgs = append(assistantMsgs, Message{Content: curAssistant, TS: curAssistantTS})
	}
	if len(userMsgs) == 0 && len(assistantMsgs) == 0 && len(tools) == 0 && len(usageEvents) == 0 {
		return nil
	}

	project := ""
	if cwd != "" {
		project = config.ProjectFromPath(cwd)
	}

	return &ParsedFile{
		SessionID:   fullID,
		ShortID:     shortID,
		Agent:       "pi",
		Project:     project,
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     summary,
		Inputs:      userMsgs,
		Outputs:     assistantMsgs,
		Messages:    messages,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(fp),
	}
}

// PiExtractThinking reads a pi session file and returns thinking blocks paired
// with their corresponding assistant text output.
func PiExtractThinking(fp string) []ThinkingBlock {
	var blocks []ThinkingBlock
	scanJSONL(fp, func(r map[string]any) bool {
		if config.GetStr(r, "type") != "message" {
			return true
		}
		msg := config.GetMap(r, "message")
		if config.GetStr(msg, "role") != "assistant" {
			return true
		}
		var thinking, response string
		for _, item := range config.GetSlice(msg, "content") {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch config.GetStr(m, "type") {
			case "thinking":
				thinking = strings.TrimSpace(config.GetStr(m, "thinking"))
			case "text":
				if txt := strings.TrimSpace(config.GetStr(m, "text")); txt != "" {
					response = txt
				}
			}
		}
		if thinking != "" {
			blocks = append(blocks, ThinkingBlock{Thinking: thinking, Response: response})
		}
		return true
	})
	return blocks
}

func PiLiveSessions(maxAge time.Duration) []Session {
	var sessions []Session
	now := time.Now().In(config.Loc)
	entries, err := os.ReadDir(config.PiSessions)
	if err != nil {
		return sessions
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(config.PiSessions, e.Name(), "*.jsonl"))
		for _, fp := range files {
			info, err := os.Stat(fp)
			if err != nil {
				continue
			}
			age := now.Sub(info.ModTime())
			if age > maxAge {
				continue
			}

			var cwd, summary, firstQ, model string
			for _, r := range config.HeadJSONL(fp, 30) {
				switch config.GetStr(r, "type") {
				case "session":
					cwd = config.GetStr(r, "cwd")
				case "session_info":
					if n := config.GetStr(r, "name"); n != "" {
						summary = n
					}
				case "model_change":
					if m := config.GetStr(r, "modelId"); m != "" {
						model = m
					}
				case "message":
					msg := config.GetMap(r, "message")
					if firstQ == "" && config.GetStr(msg, "role") == "user" {
						txt := piMessageText(msg)
						if !isPiNoise(txt) && len(txt) > 2 {
							txt = truncateText(txt, 200)
							firstQ = txt
						}
					}
				}
			}

			var lastUserMsg, lastAssistant string
			var recentTools []string
			for _, r := range config.TailJSONL(fp, 40) {
				if config.GetStr(r, "type") != "message" {
					continue
				}
				msg := config.GetMap(r, "message")
				switch config.GetStr(msg, "role") {
				case "user":
					txt := piMessageText(msg)
					if !isPiNoise(txt) && len(txt) > 2 {
						txt = truncateText(txt, 200)
						lastUserMsg = txt
					}
				case "assistant":
					if m := config.GetStr(msg, "model"); m != "" {
						model = m
					}
					for _, item := range config.GetSlice(msg, "content") {
						if m, ok := item.(map[string]any); ok {
							switch config.GetStr(m, "type") {
							case "toolCall":
								recentTools = append(recentTools, config.GetStr(m, "name"))
							case "text":
								if txt := strings.TrimSpace(config.GetStr(m, "text")); txt != "" {
									txt = truncateText(txt, 200)
									lastAssistant = txt
								}
							}
						}
					}
				}
			}
			if len(recentTools) > 5 {
				recentTools = recentTools[len(recentTools)-5:]
			}

			project := ""
			if cwd != "" {
				project = config.ProjectFromPath(cwd)
			}
			stem := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
			sid := stem
			if idx := strings.LastIndex(stem, "_"); idx >= 0 {
				sid = stem[idx+1:]
			}
			if len(sid) > 8 {
				sid = sid[:8]
			}
			if project == "" {
				project = sid
			}

			sessions = append(sessions, Session{
				Tool:          "Pi",
				Project:       project,
				SessionID:     sid,
				Model:         model,
				Summary:       summary,
				FirstQ:        firstQ,
				AgeSeconds:    int(age.Seconds()),
				LastUserMsg:   lastUserMsg,
				RecentTools:   recentTools,
				LastAssistant: lastAssistant,
			})
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].AgeSeconds < sessions[j].AgeSeconds
	})
	return sessions
}
