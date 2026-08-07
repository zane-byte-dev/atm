package parser

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// DiscoverQoderCLI finds Qoder CLI transcripts below ~/.qoder/projects.
// Qoder CLI has used both <project>/*.jsonl and
// <project>/transcript/*.jsonl layouts, so discovery is intentionally recursive.
func DiscoverQoderCLI() []string {
	var files []string
	_ = filepath.WalkDir(config.QoderCLIProjects, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func QoderCLIParseFile(path string) *ParsedFile {
	return parseQoderJSONL(path, "qodercli", 0)
}

// QoderCLIParseAppend parses records appended after offset. Qoder CLI transcripts
// are append-only JSONL and usage is accumulated per record (no cumulative
// snapshot), so incremental parsing yields the same stored result as a full scan.
func QoderCLIParseAppend(path string, offset int64) *ParsedFile {
	p := parseQoderJSONL(path, "qodercli", offset)
	if p != nil {
		p.Append = true
	}
	return p
}

func parseQoderJSONL(path, agent string, offset int64) *ParsedFile {
	rawID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	shortID := rawID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	var inputs, outputs []Message
	var messages []TranscriptMessage
	tools := map[string]int{}
	var skills []SkillEvent
	var usage Usage
	var usageEvents []UsageEvent
	var createdTS, lastTS int64
	var createdAt, cwd, summary string

	scanJSONLFrom(path, offset, func(record map[string]any) bool {
		var ts int64
		if parsed, ok := parseTimestampString(config.GetStr(record, "timestamp")); ok {
			ts = parsed.Unix()
			if createdTS == 0 {
				createdTS = ts
				createdAt = parsed.In(config.Loc).Format("01-02 15:04")
			}
			lastTS = ts
		}
		if value := config.GetStr(record, "cwd"); value != "" {
			cwd = value
		}

		switch config.GetStr(record, "type") {
		case "ai-title":
			summary = config.GetStr(record, "aiTitle")
		case "runtime-config":
			if model := config.GetStr(record, "model"); model != "" && !strings.HasPrefix(model, "<") {
				usage.Model = model
			}
		case "user", "assistant":
			message := config.GetMap(record, "message")
			if message == nil {
				return true
			}
			role := config.GetStr(message, "role")
			if role == "" {
				role = config.GetStr(record, "type")
			}
			text := strings.TrimSpace(config.ExtractText(message))
			if role == "user" {
				text = claudeUserText(message)
				if text != "" {
					inputs = append(inputs, Message{Content: text, TS: ts})
					messages = append(messages, TranscriptMessage{Role: "user", Content: text, TS: ts})
				}
				return true
			}
			if role != "assistant" {
				return true
			}
			if model := config.GetStr(message, "model"); model != "" && !strings.HasPrefix(model, "<") {
				usage.Model = model
			}
			for _, item := range config.GetSlice(message, "content") {
				block, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if config.GetStr(block, "type") == "tool_use" {
					if name := config.GetStr(block, "name"); name != "" {
						tools[name]++
						skills = appendSkillEvent(skills, skillFromToolCall(name, config.GetMap(block, "input")), ts)
					}
				}
			}
			if text != "" {
				outputs = append(outputs, Message{Content: text, TS: ts})
				messages = append(messages, TranscriptMessage{Role: "assistant", Content: text, TS: ts})
			}
			if rawUsage := config.GetMap(message, "usage"); rawUsage != nil {
				in, _ := config.GetFloat(rawUsage, "input_tokens")
				out, _ := config.GetFloat(rawUsage, "output_tokens")
				cacheCreate, _ := config.GetFloat(rawUsage, "cache_creation_input_tokens")
				cacheRead, _ := config.GetFloat(rawUsage, "cache_read_input_tokens")
				event := UsageEvent{Model: usage.Model, TS: ts, InputTokens: int64(in), OutputTokens: int64(out), CacheCreateTokens: int64(cacheCreate), CacheReadTokens: int64(cacheRead)}
				if event.InputTokens != 0 || event.OutputTokens != 0 || event.CacheCreateTokens != 0 || event.CacheReadTokens != 0 {
					usage.InputTokens += event.InputTokens
					usage.OutputTokens += event.OutputTokens
					usage.CacheCreateTokens += event.CacheCreateTokens
					usage.CacheReadTokens += event.CacheReadTokens
					usage.RequestCount++
					usageEvents = append(usageEvents, event)
				}
			}
		}
		return true
	})

	if len(messages) == 0 && len(tools) == 0 && len(usageEvents) == 0 {
		return nil
	}
	project := ""
	if cwd != "" {
		project = config.ProjectFromPath(cwd)
	}
	if project == "" {
		project = qoderProjectFromTranscriptPath(path, config.QoderCLIProjects)
	}
	return &ParsedFile{
		SessionID:   agent + ":" + rawID,
		ShortID:     shortID,
		Agent:       agent,
		Project:     project,
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     summary,
		Inputs:      inputs,
		Outputs:     outputs,
		Messages:    messages,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(path),
	}
}

func qoderProjectFromTranscriptPath(path, root string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return ""
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}
	return config.ProjectName(parts[0])
}

func QoderCLILiveSessions(maxAge time.Duration) []Session {
	return qoderJSONLLiveSessions(DiscoverQoderCLI(), maxAge, "Qoder CLI")
}

func qoderJSONLLiveSessions(files []string, maxAge time.Duration, toolName string) []Session {
	now := time.Now()
	var sessions []Session
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		if age > maxAge {
			continue
		}
		parsed := parseQoderJSONL(path, strings.ToLower(strings.ReplaceAll(toolName, " ", "")), 0)
		if parsed == nil {
			continue
		}
		firstQ, lastUser, lastAssistant := "", "", ""
		if len(parsed.Inputs) > 0 {
			firstQ = truncateText(parsed.Inputs[0].Content, 200)
			lastUser = truncateText(parsed.Inputs[len(parsed.Inputs)-1].Content, 200)
		}
		if len(parsed.Outputs) > 0 {
			lastAssistant = truncateText(parsed.Outputs[len(parsed.Outputs)-1].Content, 200)
		}
		// The transcript filename is the session id Qoder's hook reports, while
		// ShortID is truncated for display. ResumeID keeps the whole one so hook
		// events can be joined onto this row.
		resumeID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		sessions = append(sessions, Session{Tool: toolName, Project: parsed.Project, SessionID: parsed.ShortID, ResumeID: resumeID, Model: parsed.Usage.Model, Summary: parsed.Summary, FirstQ: firstQ, LastUserMsg: lastUser, LastAssistant: lastAssistant, AgeSeconds: int(age.Seconds())})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].AgeSeconds < sessions[j].AgeSeconds })
	return sessions
}
