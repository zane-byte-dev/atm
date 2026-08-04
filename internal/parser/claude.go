package parser

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// claudeContinuationMarker prefixes the summary message Claude Code writes when
// it compacts and replays a conversation. Its presence in a region signals that
// earlier messages are being replayed (and must be deduped by a full parse).
const claudeContinuationMarker = "This session is being continued from a previous conversation"

func isClaudeNoise(txt string) bool {
	t := strings.TrimSpace(txt)
	if len(t) < 3 {
		return true
	}
	for _, prefix := range []string{
		"<local-command-", "<bash-input>", "<bash-stdout>", "<bash-stderr>",
		"<command-name>", "<local-command-stdout>",
		"<ide_opened_file>", "<ide_selection>",
		"<system-reminder>", "<system_context>",
		"<task-notification>",
		"[Request interrupted",
		claudeContinuationMarker,
		"Base directory for this skill:",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// claudeUserText filters context-only blocks independently before joining the
// remaining prompt. Claude can store an IDE notice and the real user question
// as separate text blocks in one message.
func claudeUserText(message map[string]any) string {
	var meaningful []string
	for _, text := range config.ExtractTextBlocks(message) {
		text = strings.TrimSpace(text)
		if !isClaudeNoise(text) && len(text) > 2 {
			meaningful = append(meaningful, text)
		}
	}
	return strings.Join(meaningful, "\n\n")
}

// DiscoverClaude lists all Claude Code JSONL transcript files,
// including subagent transcripts under <session-id>/subagents/.
func DiscoverClaude() []string {
	var files []string
	entries, err := os.ReadDir(config.ClaudeProjects)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projDir := filepath.Join(config.ClaudeProjects, e.Name())
		matches, _ := filepath.Glob(filepath.Join(projDir, "*.jsonl"))
		files = append(files, matches...)
		subs, _ := filepath.Glob(filepath.Join(projDir, "*/subagents/*.jsonl"))
		files = append(files, subs...)
	}
	return files
}

func claudeProjectName(fp string) string {
	if projectDir := config.ProjectDirFromSessionPath(fp); projectDir != "" {
		return config.ProjectFromPath(projectDir)
	}
	dir := filepath.Dir(fp)
	name := filepath.Base(dir)
	if name == "subagents" {
		dir = filepath.Dir(filepath.Dir(dir))
		name = filepath.Base(dir)
	}
	return config.ProjectName(name)
}

func ClaudeParseFile(fp string) *ParsedFile {
	fullID := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	shortID := fullID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	projectName := claudeProjectName(fp)

	userMsgs, assistantMsgs, tools, skills, summary, createdAt, createdTS, lastTS, usage, usageEvents := claudeScan(fp, 0)
	if len(userMsgs) == 0 {
		return nil
	}

	// Self-dedup: if messages repeat within the same file (continuation
	// replays earlier messages after a summary), keep only the last occurrence
	// of each repeated prefix.
	userMsgs, assistantMsgs = claudeSelfDedup(userMsgs, assistantMsgs)

	// Dedup continuation: if this session shares leading messages with the
	// project's other sessions, trim the repeated prefix.
	userMsgs, assistantMsgs = claudeDedup(fp, userMsgs, assistantMsgs)
	if len(userMsgs) == 0 {
		return nil
	}

	return &ParsedFile{
		SessionID:   fullID,
		ShortID:     shortID,
		Agent:       "claude",
		Project:     projectName,
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     summary,
		Inputs:      userMsgs,
		Outputs:     assistantMsgs,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(fp),
	}
}

// ClaudeParseAppend parses only the bytes after offset, producing the new
// messages/tools/usage without dedup (the existing prefix is already stored).
func ClaudeParseAppend(fp string, offset int64) *ParsedFile {
	// If the appended region replays earlier messages (continuation/compaction),
	// the append path would store duplicates that a full parse dedups. Signal a
	// full re-parse by returning nil so the caller falls back to ClaudeParseFile.
	if claudeAppendHasReplay(fp, offset) {
		return nil
	}
	fullID := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	shortID := fullID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	userMsgs, assistantMsgs, tools, skills, summary, _, _, lastTS, usage, usageEvents := claudeScan(fp, offset)
	if len(userMsgs) == 0 && len(assistantMsgs) == 0 && len(tools) == 0 && len(usageEvents) == 0 && summary == "" {
		return nil
	}
	return &ParsedFile{
		SessionID:   fullID,
		ShortID:     shortID,
		Agent:       "claude",
		LastTS:      lastTS,
		Summary:     summary,
		Inputs:      userMsgs,
		Outputs:     assistantMsgs,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(fp),
		Append:      true,
	}
}

// claudeAppendHasReplay reports whether the region of fp after offset contains a
// continuation-summary marker, which precedes a replay of earlier messages.
func claudeAppendHasReplay(fp string, offset int64) bool {
	found := false
	scanJSONLFrom(fp, offset, func(r map[string]any) bool {
		if config.GetStr(r, "type") != "user" {
			return true
		}
		for _, text := range config.ExtractTextBlocks(config.GetMap(r, "message")) {
			if strings.HasPrefix(strings.TrimSpace(text), claudeContinuationMarker) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

func claudeScan(fp string, offset int64) (userMsgs, assistantMsgs []Message, tools map[string]int, skills []SkillEvent, summary, createdAt string, createdTS, lastTS int64, usage Usage, usageEvents []UsageEvent) {
	tools = map[string]int{}
	curAssistant := ""
	var curTS, curTSMS int64
	var prevUsageKey [4]int64
	seenUsageIDs := map[string]bool{}
	// Requests are timed from the last non-assistant record to the last record of
	// the assistant group that answers it. One response is written as several
	// records, so the event is emitted on the first of them and its duration
	// extended by the rest — hence the index of the event still being measured.
	var timing durationTracker
	timingEvent := -1
	scanJSONLFrom(fp, offset, func(r map[string]any) bool {
		tsStr := config.GetStr(r, "timestamp")
		if tsStr != "" {
			tsStr = strings.Replace(tsStr, "Z", "+00:00", 1)
			if dt, err := time.Parse(time.RFC3339, tsStr); err == nil {
				dt = dt.In(config.Loc)
				curTS = dt.Unix()
				curTSMS = dt.UnixMilli()
				if createdAt == "" {
					createdTS = curTS
					createdAt = dt.Format("01-02 15:04")
				}
				lastTS = curTS
			}
		}
		// Everything that is not the model talking is input to whatever it says
		// next: the human's message, a tool result, an attachment, a queued edit.
		if config.GetStr(r, "type") != "assistant" {
			timing.Input(curTSMS)
			timingEvent = -1
		}
		typ := config.GetStr(r, "type")
		if typ == "ai-title" {
			if t := config.GetStr(r, "aiTitle"); t != "" {
				summary = t
			}
			return true
		}
		if typ == "user" {
			msg := claudeUserText(config.GetMap(r, "message"))
			if msg == "" {
				return true
			}
			if len(userMsgs) > 0 && curAssistant != "" {
				assistantMsgs = append(assistantMsgs, Message{Content: curAssistant, TS: curTS})
				curAssistant = ""
			}
			if len(msg) > 2 {
				userMsgs = append(userMsgs, Message{Content: msg, TS: curTS})
			}
		} else if typ == "tool_use" {
			name := config.GetStr(r, "name")
			tools[name]++
			skills = appendSkillEvent(skills, skillFromToolCall(name, config.GetMap(r, "input")), curTS)
		} else if typ == "assistant" {
			timing.Output(curTSMS)
			msg := config.GetMap(r, "message")
			model := usage.Model
			if m := config.GetStr(msg, "model"); m != "" && !strings.HasPrefix(m, "<") {
				usage.Model = m
				model = m
			}
			if u := config.GetMap(msg, "usage"); u != nil {
				in, _ := config.GetFloat(u, "input_tokens")
				out, _ := config.GetFloat(u, "output_tokens")
				cc, _ := config.GetFloat(u, "cache_creation_input_tokens")
				cr, _ := config.GetFloat(u, "cache_read_input_tokens")
				key := [4]int64{int64(in), int64(out), int64(cc), int64(cr)}
				// One assistant response is written as several records — one per
				// content block — each repeating the same usage. message.id names
				// the API response, so it separates a repeated record from a second
				// request exactly, and it keeps doing so across files: continuing a
				// session replays the earlier transcript into a new one. Records
				// without an id fall back to suppressing an immediately repeated
				// usage tuple, which is all this had before.
				fingerprint := ""
				if id := config.GetStr(msg, "id"); id != "" {
					fingerprint = "claude:" + id
				}
				fresh := key != prevUsageKey
				if fingerprint != "" {
					fresh = !seenUsageIDs[fingerprint]
				}
				if fresh {
					usage.InputTokens += key[0]
					usage.OutputTokens += key[1]
					usage.CacheCreateTokens += key[2]
					usage.CacheReadTokens += key[3]
					usage.RequestCount++
					usageEvents = append(usageEvents, UsageEvent{Model: model, TS: curTS,
						InputTokens: key[0], OutputTokens: key[1], CacheCreateTokens: key[2],
						CacheReadTokens: key[3], Fingerprint: fingerprint,
						DurationMS: timing.Measure()})
					seenUsageIDs[fingerprint] = true
					prevUsageKey = key
					timingEvent = len(usageEvents) - 1
				} else if timingEvent >= 0 && fingerprint != "" &&
					usageEvents[timingEvent].Fingerprint == fingerprint {
					// A later record of the same response: the model was still
					// writing, so the window this event measures runs to here.
					usageEvents[timingEvent].DurationMS = timing.Measure()
				}
			}
			content := config.GetSlice(msg, "content")
			for _, item := range content {
				if m, ok := item.(map[string]any); ok {
					if config.GetStr(m, "type") == "tool_use" {
						name := config.GetStr(m, "name")
						tools[name]++
						skills = appendSkillEvent(skills, skillFromToolCall(name, config.GetMap(m, "input")), curTS)
					} else if config.GetStr(m, "type") == "text" {
						txt := strings.TrimSpace(config.GetStr(m, "text"))
						if len(txt) > 2 {
							curAssistant = txt
						}
					}
				}
			}
		}
		return true
	})
	if curAssistant != "" {
		assistantMsgs = append(assistantMsgs, Message{Content: curAssistant, TS: curTS})
	}
	return
}

// claudeDedup detects continuation sessions by checking if sibling JSONL files
// in the same project directory share leading user messages. If so, the shared
// prefix is stripped, keeping only the new messages from this continuation.
func claudeDedup(fp string, inputs, outputs []Message) ([]Message, []Message) {
	dir := filepath.Dir(fp)
	self := filepath.Base(fp)
	siblings, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(siblings) < 2 {
		return inputs, outputs
	}

	// Collect first-message content from all older siblings (by mtime)
	selfInfo, err := os.Stat(fp)
	if err != nil {
		return inputs, outputs
	}
	parentMsgs := map[string]bool{}
	for _, sib := range siblings {
		if filepath.Base(sib) == self {
			continue
		}
		info, err := os.Stat(sib)
		if err != nil || !info.ModTime().Before(selfInfo.ModTime()) {
			continue
		}
		for _, m := range cachedQuickReadUserMsgs(sib, info.ModTime().Unix()) {
			parentMsgs[m] = true
		}
	}
	if len(parentMsgs) == 0 {
		return inputs, outputs
	}

	// Find the longest prefix of inputs that all appear in parent sessions
	trimTo := 0
	for i, inp := range inputs {
		if !parentMsgs[inp.Content] {
			break
		}
		trimTo = i + 1
	}
	if trimTo == 0 || trimTo >= len(inputs) {
		return inputs, outputs
	}
	inputs = inputs[trimTo:]
	if trimTo <= len(outputs) {
		outputs = outputs[trimTo:]
	} else {
		outputs = nil
	}
	return inputs, outputs
}

// claudeSelfDedup removes repeated message prefixes within a single session.
// When Claude continues a conversation, it replays earlier messages after
// the continuation summary. This finds the last occurrence of the first
// message and trims everything before it.
func claudeSelfDedup(inputs, outputs []Message) ([]Message, []Message) {
	if len(inputs) < 4 {
		return inputs, outputs
	}
	first := inputs[0].Content
	lastIdx := 0
	for i := 1; i < len(inputs); i++ {
		if inputs[i].Content == first {
			lastIdx = i
		}
	}
	if lastIdx == 0 {
		return inputs, outputs
	}
	inputs = inputs[lastIdx:]
	if lastIdx <= len(outputs) {
		outputs = outputs[lastIdx:]
	} else {
		outputs = nil
	}
	return inputs, outputs
}

func claudeQuickReadUserMsgs(fp string) []string {
	var msgs []string
	scanJSONL(fp, func(r map[string]any) bool {
		if config.GetStr(r, "type") == "user" {
			msg := claudeUserText(config.GetMap(r, "message"))
			if msg != "" {
				msgs = append(msgs, msg)
			}
		}
		return true
	})
	return msgs
}

// quickReadCache memoizes per-file user messages keyed by path, invalidated by
// mtime. During a sync each session re-checks its siblings, so caching avoids
// re-reading the same files O(n) times within a single run.
type quickReadEntry struct {
	mtime int64
	msgs  []string
}

var quickReadCache = map[string]quickReadEntry{}

func cachedQuickReadUserMsgs(fp string, mtime int64) []string {
	if e, ok := quickReadCache[fp]; ok && e.mtime == mtime {
		return e.msgs
	}
	msgs := claudeQuickReadUserMsgs(fp)
	quickReadCache[fp] = quickReadEntry{mtime: mtime, msgs: msgs}
	return msgs
}

// ClaudeExtractThinking reads a JSONL file and returns thinking blocks
// paired with their corresponding assistant text output.
type ThinkingBlock struct {
	Thinking string
	Response string
}

func ClaudeExtractThinking(fp string) []ThinkingBlock {
	var blocks []ThinkingBlock
	scanJSONL(fp, func(r map[string]any) bool {
		if config.GetStr(r, "type") != "assistant" {
			return true
		}
		content := config.GetSlice(config.GetMap(r, "message"), "content")
		var thinking, response string
		for _, item := range content {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch config.GetStr(m, "type") {
			case "thinking":
				thinking = strings.TrimSpace(config.GetStr(m, "thinking"))
			case "text":
				txt := strings.TrimSpace(config.GetStr(m, "text"))
				if txt != "" {
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

func ClaudeLiveSessions(maxAge time.Duration) []Session {
	var sessions []Session
	now := time.Now().In(config.Loc)
	projDir := config.ClaudeProjects
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return sessions
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projectName := config.ProjectName(e.Name())
		projectDir := filepath.Join(projDir, e.Name())
		mainFiles, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		subFiles, _ := filepath.Glob(filepath.Join(projectDir, "*/subagents/*.jsonl"))
		files := append(mainFiles, subFiles...)
		for _, fp := range files {
			info, err := os.Stat(fp)
			if err != nil {
				continue
			}
			age := now.Sub(info.ModTime())
			if age > maxAge {
				continue
			}
			headRecords := config.HeadJSONL(fp, 20)
			var summary, firstQ, sessionCWD, client string
			var startedAt time.Time
			for _, r := range headRecords {
				if startedAt.IsZero() {
					startedAt, _ = time.Parse(time.RFC3339Nano, config.GetStr(r, "timestamp"))
				}
				if sessionCWD == "" {
					sessionCWD = config.GetStr(r, "cwd")
				}
				if client == "" {
					switch strings.ToLower(config.GetStr(r, "entrypoint")) {
					case "claude-vscode", "vscode":
						client = "Claude Code · VS Code"
					case "claude-code", "cli":
						client = "Claude Code"
					}
				}
				typ := config.GetStr(r, "type")
				if typ == "ai-title" {
					if t := config.GetStr(r, "aiTitle"); t != "" {
						summary = t
					}
				} else if typ == "user" && firstQ == "" {
					msg := claudeUserText(config.GetMap(r, "message"))
					if msg != "" {
						msg = truncateText(msg, 200)
						firstQ = msg
					}
				}
				if summary != "" && firstQ != "" {
					break
				}
			}

			records := config.TailJSONL(fp, 40)
			var lastUserMsg string
			var recentTools []string
			var lastAssistant string
			var model string
			for _, r := range records {
				typ := config.GetStr(r, "type")
				if typ == "user" {
					msg := claudeUserText(config.GetMap(r, "message"))
					if msg != "" {
						msg = truncateText(msg, 200)
						lastUserMsg = msg
					}
				} else if typ == "assistant" {
					if m := config.GetStr(config.GetMap(r, "message"), "model"); m != "" && !strings.HasPrefix(m, "<") {
						model = m
					}
					content := config.GetSlice(config.GetMap(r, "message"), "content")
					for _, item := range content {
						if m, ok := item.(map[string]any); ok {
							if config.GetStr(m, "type") == "tool_use" {
								recentTools = append(recentTools, config.GetStr(m, "name"))
							} else if config.GetStr(m, "type") == "text" {
								txt := strings.TrimSpace(config.GetStr(m, "text"))
								if txt != "" {
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

			// Sample user messages throughout the file to capture topics
			// discussed in long/reused sessions.
			var topics []string
			seen := make(map[string]bool)
			if firstQ != "" {
				seen[firstQ] = true
			}
			if lastUserMsg != "" {
				seen[lastUserMsg] = true
			}
			sampled := config.SampleJSONL(fp, 15, 8)
			for _, r := range sampled {
				if config.GetStr(r, "type") != "user" {
					continue
				}
				msg := claudeUserText(config.GetMap(r, "message"))
				if msg == "" {
					continue
				}
				msg = truncateText(msg, 100)
				if !seen[msg] {
					seen[msg] = true
					topics = append(topics, msg)
				}
			}
			if len(topics) > 10 {
				step := len(topics) / 10
				var trimmed []string
				for i := 0; i < len(topics) && len(trimmed) < 10; i += step {
					trimmed = append(trimmed, topics[i])
				}
				topics = trimmed
			}

			resumeID := strings.TrimSuffix(filepath.Base(fp), filepath.Ext(fp))
			if client == "" {
				client = "Claude Code"
			}
			sid := filepath.Base(fp)
			if len(sid) > 8 {
				sid = sid[:8]
			}
			sessions = append(sessions, Session{
				Tool:          "Claude Code",
				Project:       projectName,
				SessionID:     sid,
				ResumeID:      resumeID,
				Client:        client,
				CWD:           sessionCWD,
				StartedAt:     startedAt,
				Model:         model,
				Summary:       summary,
				FirstQ:        firstQ,
				AgeSeconds:    int(age.Seconds()),
				LastUserMsg:   lastUserMsg,
				RecentTools:   recentTools,
				LastAssistant: lastAssistant,
				Topics:        topics,
			})
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].AgeSeconds < sessions[j].AgeSeconds
	})
	return sessions
}
