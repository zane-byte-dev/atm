package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// QuotaLimit is one rate-limit / credit window shared by agent quota parsers.
// UsedPercent is 0–100; ResetsAt is a unix epoch when the window refills.
type QuotaLimit struct {
	UsedPercent   float64
	WindowMinutes int
	ResetsAt      int64
}

// QuotaProduct is one product's share of a shared credit pool (Grok bills
// GrokBuild / GrokChat / GrokImagine against the same weekly window).
type QuotaProduct struct {
	Name        string
	UsedPercent float64
}

// QuotaInfo is the latest known quota for one agent (Codex rate limits, Grok
// credits, …). Primary is the main window; Secondary is optional (e.g. Codex
// short window, Grok on-demand spend).
type QuotaInfo struct {
	Primary   *QuotaLimit
	Secondary *QuotaLimit
	Plan      string
	Timestamp time.Time
	// Source says where the snapshot came from: "log" (local transcript /
	// shell log), "live" (billing API), or "cache" (recent live result).
	// Empty for agents that only have one source.
	Source string
	// Products is only populated by live/cached Grok billing data.
	Products []QuotaProduct
}

// codexQuotaCacheFresh mirrors the Grok live-quota cache policy: a reading
// this young is served straight from disk, skipping the sessions-tree scan.
const codexQuotaCacheFresh = 2 * time.Minute

type codexQuotaCacheEntry struct {
	ScannedAt   time.Time  `json:"scanned_at"`
	SessionsDir string     `json:"sessions_dir"`
	Quota       *QuotaInfo `json:"quota"`
}

func codexQuotaCachePath() string {
	return filepath.Join(config.AtmDir, "codex_quota_cache.json")
}

func readCodexQuotaCache(now time.Time, maxAge time.Duration) *QuotaInfo {
	data, err := os.ReadFile(codexQuotaCachePath())
	if err != nil {
		return nil
	}
	var entry codexQuotaCacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return nil
	}
	if entry.Quota == nil || entry.SessionsDir != config.CodexSessions {
		return nil
	}
	if entry.ScannedAt.IsZero() || now.Sub(entry.ScannedAt) > maxAge {
		return nil
	}
	return entry.Quota
}

func writeCodexQuotaCache(entry codexQuotaCacheEntry) {
	// Best-effort: the cache only saves a directory scan, so a failed write
	// must never fail the quota read itself.
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(config.AtmDir, 0755); err != nil {
		return
	}
	_ = os.WriteFile(codexQuotaCachePath(), data, 0600)
}

func CodexQuota() *QuotaInfo {
	now := time.Now()
	if cached := readCodexQuotaCache(now, codexQuotaCacheFresh); cached != nil {
		return cached
	}
	info := codexQuotaScan()
	if info != nil {
		writeCodexQuotaCache(codexQuotaCacheEntry{
			ScannedAt:   now,
			SessionsDir: config.CodexSessions,
			Quota:       info,
		})
	}
	return info
}

func codexQuotaScan() *QuotaInfo {
	if _, err := os.Stat(config.CodexSessions); err != nil {
		return nil
	}
	yearDirs, err := os.ReadDir(config.CodexSessions)
	if err != nil {
		return nil
	}
	// reverse order: newest year first
	sort.Slice(yearDirs, func(i, j int) bool {
		return yearDirs[i].Name() > yearDirs[j].Name()
	})

	var best *QuotaInfo
	var bestTS string

	for _, yd := range yearDirs {
		if !yd.IsDir() {
			continue
		}
		monthDirs, _ := os.ReadDir(filepath.Join(config.CodexSessions, yd.Name()))
		sort.Slice(monthDirs, func(i, j int) bool {
			return monthDirs[i].Name() > monthDirs[j].Name()
		})
		for _, md := range monthDirs {
			if !md.IsDir() {
				continue
			}
			monthPath := filepath.Join(config.CodexSessions, yd.Name(), md.Name())
			dayDirs, _ := os.ReadDir(monthPath)
			sort.Slice(dayDirs, func(i, j int) bool {
				return dayDirs[i].Name() > dayDirs[j].Name()
			})
			for _, dd := range dayDirs {
				if !dd.IsDir() {
					continue
				}
				files, _ := filepath.Glob(filepath.Join(monthPath, dd.Name(), "rollout-*.jsonl"))
				for _, fp := range files {
					q := codexQuotaFromFile(fp)
					if q != nil && q.ts > bestTS {
						bestTS = q.ts
						best = q.info
					}
				}
				if best != nil {
					return best
				}
			}
		}
	}
	return best
}

type codexQuotaRaw struct {
	info *QuotaInfo
	ts   string
}

func codexQuotaFromFile(fp string) *codexQuotaRaw {
	var bestTS string
	var bestRL map[string]any

	scanJSONL(fp, func(r map[string]any) bool {
		if config.GetStr(r, "type") != "event_msg" {
			return true
		}
		p := config.GetMap(r, "payload")
		if p == nil || config.GetStr(p, "type") != "token_count" {
			return true
		}
		rl := config.GetMap(p, "rate_limits")
		if rl == nil {
			return true
		}
		if config.GetMap(rl, "primary") == nil {
			return true
		}
		ts := config.GetStr(r, "timestamp")
		if ts > bestTS {
			bestTS = ts
			bestRL = rl
		}
		return true
	})

	if bestRL == nil {
		return nil
	}

	info := &QuotaInfo{
		Plan: config.GetStr(bestRL, "plan_type"),
	}
	if dt, ok := parseTimestampString(bestTS); ok {
		info.Timestamp = dt
	}
	if pm := config.GetMap(bestRL, "primary"); pm != nil {
		pct, _ := config.GetFloat(pm, "used_percent")
		win, _ := config.GetFloat(pm, "window_minutes")
		rst, _ := config.GetFloat(pm, "resets_at")
		info.Primary = &QuotaLimit{
			UsedPercent:   pct,
			WindowMinutes: int(win),
			ResetsAt:      int64(rst),
		}
	}
	if sm := config.GetMap(bestRL, "secondary"); sm != nil {
		pct, _ := config.GetFloat(sm, "used_percent")
		win, _ := config.GetFloat(sm, "window_minutes")
		rst, _ := config.GetFloat(sm, "resets_at")
		info.Secondary = &QuotaLimit{
			UsedPercent:   pct,
			WindowMinutes: int(win),
			ResetsAt:      int64(rst),
		}
	}
	return &codexQuotaRaw{info: info, ts: bestTS}
}

// DiscoverCodex lists all Codex session JSONL files under YYYY/MM/DD dirs.
func DiscoverCodex() []string {
	var files []string
	yearDirs, err := os.ReadDir(config.CodexSessions)
	if err != nil {
		return nil
	}
	for _, yd := range yearDirs {
		if !yd.IsDir() {
			continue
		}
		yearPath := filepath.Join(config.CodexSessions, yd.Name())
		monthDirs, err := os.ReadDir(yearPath)
		if err != nil {
			continue
		}
		for _, md := range monthDirs {
			if !md.IsDir() {
				continue
			}
			monthPath := filepath.Join(yearPath, md.Name())
			dayDirs, err := os.ReadDir(monthPath)
			if err != nil {
				continue
			}
			for _, dd := range dayDirs {
				if !dd.IsDir() {
					continue
				}
				matches, _ := filepath.Glob(filepath.Join(monthPath, dd.Name(), "*.jsonl"))
				files = append(files, matches...)
			}
		}
	}
	return files
}

func CodexParseFile(fp string) *ParsedFile {
	metadata := codexLiveMetadata(fp)
	stem := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
	fullID := stem
	shortID := stem
	if idx := strings.LastIndex(stem, "-"); idx > 0 {
		shortID = stem[idx+1:]
	}
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	// derive date from directory path: .../YYYY/MM/DD/file.jsonl
	var createdTS int64
	createdAt := ""
	dir := filepath.Dir(fp)
	day := filepath.Base(dir)
	month := filepath.Base(filepath.Dir(dir))
	year := filepath.Base(filepath.Dir(filepath.Dir(dir)))
	if dt, err := time.ParseInLocation("2006-01-02", year+"-"+month+"-"+day, config.Loc); err == nil {
		createdTS = dt.Unix()
		createdAt = dt.Format("01-02")
	}
	lastTS := createdTS
	sawPreciseTS := false

	var userInputs, assistantOutputs []Message
	var transcriptMessages []TranscriptMessage
	tools := map[string]int{}
	var skills []SkillEvent
	cwd := metadata.CWD
	threadID := metadata.ResumeID
	curAssistant := ""
	var curAssistantTS int64
	var usage Usage
	var usageEvents []UsageEvent
	var previousTotal [3]int64
	var sawTotal bool
	var timing durationTracker
	seenStructuredMessages := map[string]int64{}
	legacyUnboundedSubagent := metadata.IsSubagent && !metadata.HasHistoryStartOrdinal

	scanJSONL(fp, func(r map[string]any) bool {
		p := config.GetMap(r, "payload")
		if p == nil {
			p = map[string]any{}
		}
		recordTS := createdTS
		var recordTSMS int64
		if dt, ok := firstTimestamp([]string{"timestamp", "time", "created_at", "createdAt", "ts"}, r, p); ok {
			recordTS = dt.Unix()
			recordTSMS = dt.UnixMilli()
			lastTS = recordTS
			if !sawPreciseTS {
				createdTS = recordTS
				createdAt = dt.Format("01-02 15:04")
				sawPreciseTS = true
			}
		}
		typ := config.GetStr(r, "type")
		ownRecord := !metadata.IsSubagent || !metadata.HasHistoryStartOrdinal || codexRecordBelongsToLiveSession(r, metadata)
		if !ownRecord {
			// Keep only the copied lineage's last cumulative total as the baseline
			// for old logs whose first own token_count lacks last_token_usage. The
			// copied request itself must not become usage owned by this child.
			if typ == "event_msg" && config.GetStr(p, "type") == "token_count" {
				if info := config.GetMap(p, "info"); info != nil {
					if totalUsage := config.GetMap(info, "total_token_usage"); totalUsage != nil {
						inTok, _ := config.GetFloat(totalUsage, "input_tokens")
						outTok, _ := config.GetFloat(totalUsage, "output_tokens")
						cached, _ := config.GetFloat(totalUsage, "cached_input_tokens")
						previousTotal = [3]int64{int64(inTok), int64(outTok), int64(cached)}
						sawTotal = true
					}
				}
			}
			return true
		}
		// Old spawned rollouts copied their entire parent transcript but carried no
		// ordinal boundary.  There is no honest way to tell the child's rows from
		// the inherited ones, so quarantine the ambiguous payload rather than
		// presenting the parent's prompts, tools, and spend as the child's.  The
		// lineage row itself is still indexed and can be grouped under its root.
		if (legacyUnboundedSubagent || metadata.IsInternalSubagent) && typ != "session_meta" {
			return true
		}
		codexTrackTiming(&timing, typ, p, recordTSMS)
		if typ == "session_meta" {
			// codexLiveMetadata owns the canonical first session_meta. Retain the
			// old fallback only for malformed files whose head had no metadata.
			if !metadata.HasSessionMeta {
				cwd = config.GetStr(p, "cwd")
				threadID = strings.TrimSpace(config.GetStr(p, "id"))
				if threadID == "" {
					threadID = strings.TrimSpace(config.GetStr(p, "session_id"))
				}
				metadata.HasSessionMeta = true
			}
		} else if typ == "turn_context" {
			if m := config.GetStr(p, "model"); m != "" {
				usage.Model = m
			}
		} else if typ == "event_msg" && config.GetStr(p, "type") == "token_count" {
			if info := config.GetMap(p, "info"); info != nil {
				var total [3]int64
				hasTotal := false
				if tu := config.GetMap(info, "total_token_usage"); tu != nil {
					inTok, _ := config.GetFloat(tu, "input_tokens")
					outTok, _ := config.GetFloat(tu, "output_tokens")
					cached, _ := config.GetFloat(tu, "cached_input_tokens")
					total = [3]int64{int64(inTok), int64(outTok), int64(cached)}
					hasTotal = true
				}

				// Codex exposes the exact usage for the latest model request. AI Trace
				// uses this snapshot as its source of truth; doing the same avoids
				// over-counting when cumulative totals reset during compaction.
				if last := config.GetMap(info, "last_token_usage"); last != nil && !(hasTotal && sawTotal && total == previousTotal) {
					inTok, _ := config.GetFloat(last, "input_tokens")
					outTok, _ := config.GetFloat(last, "output_tokens")
					cached, _ := config.GetFloat(last, "cached_input_tokens")
					input, cacheRead := int64(inTok), int64(cached)
					if cacheRead > input {
						cacheRead = input
					}
					input -= cacheRead
					event := UsageEvent{Model: usage.Model, TS: recordTS,
						InputTokens: input, OutputTokens: int64(outTok), CacheReadTokens: cacheRead,
						Fingerprint: codexUsageFingerprint(hasTotal, total, input, int64(outTok), cacheRead),
						DurationMS:  timing.Measure()}
					usageEvents = append(usageEvents, event)
					usage.InputTokens += event.InputTokens
					usage.OutputTokens += event.OutputTokens
					usage.CacheReadTokens += event.CacheReadTokens
					usage.RequestCount++
				} else if hasTotal && !(sawTotal && total == previousTotal) {
					// Older Codex logs may not contain last_token_usage. Preserve the
					// cumulative-delta fallback for those files.
					delta := [3]int64{total[0] - previousTotal[0], total[1] - previousTotal[1], total[2] - previousTotal[2]}
					if delta[0] < 0 || delta[1] < 0 || delta[2] < 0 {
						delta = total
					}
					input := delta[0] - delta[2]
					if input < 0 {
						input = 0
					}
					event := UsageEvent{Model: usage.Model, TS: recordTS,
						InputTokens: input, OutputTokens: delta[1], CacheReadTokens: delta[2],
						Fingerprint: codexUsageFingerprint(hasTotal, total, input, delta[1], delta[2]),
						DurationMS:  timing.Measure()}
					usageEvents = append(usageEvents, event)
					usage.InputTokens += event.InputTokens
					usage.OutputTokens += event.OutputTokens
					usage.CacheReadTokens += event.CacheReadTokens
					usage.RequestCount++
				}
				if hasTotal {
					previousTotal = total
					sawTotal = true
				}
			}
		} else if typ == "event_msg" && config.GetStr(p, "type") == "agent_message" {
			message := strings.TrimSpace(config.GetStr(p, "message"))
			kind, phase := codexAssistantMessageKind(config.GetStr(p, "phase"))
			key := kind + "\x00" + message
			if previousTS, duplicate := seenStructuredMessages[key]; message != "" && (!duplicate || previousTS != recordTS) {
				seenStructuredMessages[key] = recordTS
				transcriptMessages = append(transcriptMessages, TranscriptMessage{
					Role: "assistant", Content: message, TS: recordTS,
					Scope: MessageScopeLocal, Kind: kind, Phase: phase,
				})
				if kind == MessageKindFinal {
					metadata.FinalResult = message
				} else if kind == MessageKindProgress {
					metadata.LatestProgress = message
				}
			}
		} else if typ == "response_item" {
			role := config.GetStr(p, "role")
			if role == "user" {
				if len(userInputs) > 0 && curAssistant != "" {
					assistantOutputs = append(assistantOutputs, Message{Content: curAssistant, TS: curAssistantTS})
					curAssistant = ""
					curAssistantTS = 0
				}
				var userText string
				for _, c := range config.GetSlice(p, "content") {
					if m, ok := c.(map[string]any); ok {
						if config.GetStr(m, "type") == "input_text" {
							t := VisibleUserText(config.GetStr(m, "text"))
							if len(strings.TrimSpace(t)) > 2 && !isCodexNoise(t) {
								userText = appendTextBlock(userText, t)
							}
						}
					}
				}
				if userText != "" {
					userInputs = append(userInputs, Message{Content: userText, TS: recordTS})
					scope, kind := MessageScopeLocal, MessageKindConversation
					if metadata.IsSubagent {
						scope, kind = MessageScopeControl, MessageKindControl
					}
					transcriptMessages = append(transcriptMessages, TranscriptMessage{
						Role: "user", Content: userText, TS: recordTS, Scope: scope, Kind: kind,
					})
				}
			} else if role == "assistant" {
				var responseText string
				for _, c := range config.GetSlice(p, "content") {
					if m, ok := c.(map[string]any); ok {
						if config.GetStr(m, "type") == "output_text" {
							txt := strings.TrimSpace(config.GetStr(m, "text"))
							if len(txt) > 2 && !strings.HasPrefix(txt, "{") {
								curAssistant = appendTextBlock(curAssistant, txt)
								responseText = appendTextBlock(responseText, txt)
								curAssistantTS = recordTS
							}
						}
					}
				}
				if responseText != "" {
					kind, phase := codexAssistantMessageKind(config.GetStr(p, "phase"))
					key := kind + "\x00" + responseText
					if previousTS, duplicate := seenStructuredMessages[key]; !duplicate || previousTS != recordTS {
						seenStructuredMessages[key] = recordTS
						transcriptMessages = append(transcriptMessages, TranscriptMessage{
							Role: "assistant", Content: responseText, TS: recordTS,
							Scope: MessageScopeLocal, Kind: kind, Phase: phase,
						})
					}
					if kind == MessageKindFinal {
						metadata.FinalResult = responseText
					} else if kind == MessageKindProgress {
						metadata.LatestProgress = responseText
					}
				}
			} else if config.GetStr(p, "type") == "agent_message" {
				if len(userInputs) > 0 && curAssistant != "" {
					assistantOutputs = append(assistantOutputs, Message{Content: curAssistant, TS: curAssistantTS})
					curAssistant = ""
					curAssistantTS = 0
				}
				var delegatedText string
				for _, c := range config.GetSlice(p, "content") {
					if m, ok := c.(map[string]any); ok && config.GetStr(m, "type") == "input_text" {
						text := strings.TrimSpace(config.GetStr(m, "text"))
						if len(text) > 2 && !isCodexNoise(text) {
							delegatedText = appendTextBlock(delegatedText, text)
						}
					}
				}
				if delegatedText != "" {
					// Preserve it as a title fallback for a child card, but mark it as
					// control input so it is not a human query and is not searchable.
					userInputs = append(userInputs, Message{Content: delegatedText, TS: recordTS})
					transcriptMessages = append(transcriptMessages, TranscriptMessage{
						Role: "user", Content: delegatedText, TS: recordTS,
						Scope: MessageScopeControl, Kind: MessageKindControl,
					})
				}
			} else if payloadType := config.GetStr(p, "type"); payloadType == "function_call" || payloadType == "custom_tool_call" || payloadType == "tool_search_call" {
				name := strings.TrimSpace(config.GetStr(p, "name"))
				if name != "" {
					tools[name]++
				}
				skill := skillFromToolCall(name, config.GetMap(p, "input"))
				if skill == "" {
					skill = skillFromJSONArguments(config.GetStr(p, "arguments"))
				}
				skills = appendSkillEvent(skills, skill, recordTS)
			}
		}
		return true
	})
	if curAssistant != "" {
		assistantOutputs = append(assistantOutputs, Message{Content: curAssistant, TS: curAssistantTS})
	}
	if len(userInputs) == 0 && len(tools) == 0 && len(transcriptMessages) == 0 && !metadata.HasSessionMeta {
		return nil
	}
	project := ""
	if cwd != "" {
		project = config.ProjectFromPath(cwd)
	}
	summary := codexSummary(threadID, userInputs)
	contentState := ContentStateEmpty
	for _, message := range transcriptMessages {
		if message.Scope == MessageScopeLocal && message.Kind != MessageKindControl {
			contentState = ContentStateAvailable
			break
		}
		contentState = ContentStateControlOnly
	}
	if legacyUnboundedSubagent || metadata.IsInternalSubagent {
		contentState = ContentStateControlOnly
	} else if contentState == ContentStateEmpty && summary != "" {
		contentState = ContentStateEphemeral
	}
	resultStatus := SessionResultUnknown
	if metadata.FinalResult != "" {
		resultStatus = SessionResultCompleted
	} else if metadata.LatestProgress != "" {
		resultStatus = SessionResultInProgress
	}
	return &ParsedFile{
		SessionID:       fullID,
		ShortID:         shortID,
		Agent:           "codex",
		Project:         project,
		CreatedAt:       createdAt,
		CreatedTS:       createdTS,
		LastTS:          lastTS,
		Summary:         summary,
		ResumeID:        metadata.ResumeID,
		RootSessionID:   metadata.RootSessionID,
		ParentSessionID: metadata.ParentSessionID,
		AgentPath:       metadata.AgentPath,
		AgentNickname:   metadata.AgentNickname,
		SubagentDepth:   metadata.SubagentDepth,
		IsSubagent:      metadata.IsSubagent,
		IsInternal:      metadata.IsInternalSubagent,
		ParserVersion:   CurrentSessionParserVersion,
		ContentState:    contentState,
		ResultStatus:    resultStatus,
		LatestProgress:  truncateText(metadata.LatestProgress, 1_000),
		FinalResult:     truncateText(metadata.FinalResult, 4_000),
		Inputs:          userInputs,
		Outputs:         assistantOutputs,
		Messages:        transcriptMessages,
		Tools:           tools,
		Skills:          compactSkillEvents(skills),
		Usage:           usage,
		UsageEvents:     usageEvents,
		EndOffset:       fileSize(fp),
	}
}

func codexAssistantMessageKind(rawPhase string) (kind, phase string) {
	phase = strings.ToLower(strings.TrimSpace(rawPhase))
	switch phase {
	case "commentary":
		return MessageKindProgress, phase
	case "final_answer":
		return MessageKindFinal, phase
	default:
		return MessageKindConversation, phase
	}
}

// codexTrackTiming tells the duration tracker which side of a request each
// record belongs to.
//
// Codex writes the model's own work as response items (reasoning, assistant
// text, the tool call it decided on) and everything the model reads as either a
// message from the user, a tool's output, or a turn-level event. The distinction
// matters because `token_count` — the record that carries the usage — is written
// at the same millisecond as the tool output that follows the response, so
// timing to the usage record itself would measure the tool, not the model.
//
// Records that are neither, such as `session_meta`, leave the window untouched.
func codexTrackTiming(timing *durationTracker, typ string, payload map[string]any, ms int64) {
	payloadType := config.GetStr(payload, "type")
	switch typ {
	case "response_item":
		// Role decides for anything that carries one — the rest of this parser reads
		// role rather than payload type, and older logs leave the type off entirely.
		switch config.GetStr(payload, "role") {
		case "assistant":
			timing.Output(ms)
			return
		case "user", "developer", "system":
			// Context the model reads, whoever authored it.
			timing.Input(ms)
			return
		}
		switch payloadType {
		case "agent_message":
			// Current collaboration messages carry direction explicitly and are
			// context delivered to this rollout. Legacy role-less agent_message
			// records have no direction and represented model output.
			if strings.TrimSpace(config.GetStr(payload, "author")) != "" ||
				strings.TrimSpace(config.GetStr(payload, "recipient")) != "" {
				timing.Input(ms)
			} else {
				timing.Output(ms)
			}
		case "reasoning", "custom_tool_call", "function_call", "tool_search_call":
			timing.Output(ms)
		case "function_call_output", "custom_tool_call_output", "tool_search_output":
			timing.Input(ms)
		}
	case "event_msg":
		switch payloadType {
		case "agent_message", "agent_reasoning":
			timing.Output(ms)
		case "user_message", "task_started", "mcp_tool_call_end", "patch_apply_end",
			"image_generation_end", "context_compacted":
			timing.Input(ms)
		}
	case "turn_context", "compacted":
		timing.Input(ms)
	}
}

// codexUsageFingerprint identifies a model request by its own token counts plus
// the conversation's running totals at that point.
//
// Forking a Codex thread — which is also how a spawned subagent starts — copies
// the parent's entire history into the new rollout file, token_count events
// included, restamped with the fork's own timestamp. Both files then report the
// same requests. The running totals are what make the two copies recognisable as
// one request: they are cumulative over the thread, so they differ between any
// two real requests in a lineage while matching exactly between an event and its
// replayed copy.
//
// Older logs without total_token_usage have nothing stable to key on, so their
// events stay unfingerprinted and are always counted.
func codexUsageFingerprint(hasTotal bool, total [3]int64, input, output, cacheRead int64) string {
	if !hasTotal {
		return ""
	}
	return fmt.Sprintf("codex:%d:%d:%d:%d:%d:%d",
		total[0], total[1], total[2], input, output, cacheRead)
}

func appendTextBlock(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "\n\n" + next
}

func codexReadCwdFromHead(fp string) string {
	cwd := ""
	n := 0
	scanJSONL(fp, func(r map[string]any) bool {
		if config.GetStr(r, "type") == "session_meta" {
			if c := config.GetStr(config.GetMap(r, "payload"), "cwd"); c != "" {
				cwd = filepath.Base(c)
				return false
			}
		}
		n++
		return n < 10
	})
	return cwd
}

func isCodexNoise(txt string) bool {
	t := strings.TrimSpace(txt)
	if len(t) < 3 {
		return true
	}
	for _, prefix := range []string{">>>", "The following", "Reviewed Codex", "Assess the", "Planned action", "{", "<environment_context", "[1] user:", "TRANSCRIPT", "The Codex agent", "Chunk ID:", "[", "tool exec_command"} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// codexLiveSessionMetadata is the identity carried by the rollout's own first
// session_meta record. A spawned subagent copies parent history into its JSONL,
// including older session_meta rows, so later metadata must never overwrite it.
type codexLiveSessionMetadata struct {
	ResumeID               string
	RootSessionID          string
	ParentSessionID        string
	AgentPath              string
	AgentNickname          string
	SubagentDepth          int
	Client                 string
	CWD                    string
	StartedAt              time.Time
	Model                  string
	FirstQ                 string
	LatestProgress         string
	FinalResult            string
	HistoryStartOrdinal    int
	HasHistoryStartOrdinal bool
	IsSubagent             bool
	IsInternalSubagent     bool
	HasSessionMeta         bool
}

func codexLiveMetadata(path string) codexLiveSessionMetadata {
	var metadata codexLiveSessionMetadata
	sawSessionMeta := false
	for _, record := range config.HeadJSONL(path, 30) {
		if metadata.StartedAt.IsZero() {
			metadata.StartedAt, _ = time.Parse(time.RFC3339Nano, config.GetStr(record, "timestamp"))
		}
		typ := config.GetStr(record, "type")
		payload := config.GetMap(record, "payload")
		if payload == nil {
			payload = map[string]any{}
		}
		if typ == "session_meta" && !sawSessionMeta {
			sawSessionMeta = true
			metadata.HasSessionMeta = true
			metadata.ResumeID = strings.TrimSpace(config.GetStr(payload, "id"))
			metadata.RootSessionID = strings.TrimSpace(config.GetStr(payload, "session_id"))
			metadata.AgentPath = strings.TrimSpace(config.GetStr(payload, "agent_path"))
			metadata.AgentNickname = strings.TrimSpace(config.GetStr(payload, "agent_nickname"))
			metadata.Client = config.GetStr(payload, "originator")
			metadata.CWD = config.GetStr(payload, "cwd")

			source := config.GetMap(payload, "source")
			_, sourceHasSubagent := source["subagent"]
			subagent := config.GetMap(source, "subagent")
			spawn := config.GetMap(subagent, "thread_spawn")
			if metadata.AgentPath == "" {
				metadata.AgentPath = strings.TrimSpace(config.GetStr(spawn, "agent_path"))
			}
			if metadata.AgentNickname == "" {
				metadata.AgentNickname = strings.TrimSpace(config.GetStr(spawn, "agent_nickname"))
			}
			if depth, ok := config.GetFloat(payload, "subagent_depth"); ok {
				metadata.SubagentDepth = int(depth)
			} else if depth, ok := config.GetFloat(spawn, "depth"); ok {
				metadata.SubagentDepth = int(depth)
			}
			if ordinal, ok := config.GetFloat(payload, "subagent_history_start_ordinal"); ok {
				metadata.HistoryStartOrdinal = int(ordinal)
				metadata.HasHistoryStartOrdinal = true
			}
			threadSource := strings.ToLower(strings.TrimSpace(config.GetStr(payload, "thread_source")))
			subagentKind := strings.ToLower(strings.TrimSpace(config.GetStr(source, "subagent")))
			subagentOther := strings.ToLower(strings.TrimSpace(config.GetStr(subagent, "other")))
			metadata.IsInternalSubagent = threadSource == "guardian_review" || threadSource == "review" ||
				subagentKind == "guardian" || subagentKind == "review" ||
				subagentOther == "guardian" || subagentOther == "review"
			metadata.IsSubagent = threadSource == "subagent" || metadata.IsInternalSubagent ||
				strings.TrimSpace(config.GetStr(payload, "source")) == "subagent" || sourceHasSubagent ||
				spawn != nil || metadata.AgentPath != "" || metadata.SubagentDepth > 0 || metadata.HasHistoryStartOrdinal
			if metadata.ResumeID == "" && metadata.IsSubagent {
				metadata.ResumeID = codexRolloutThreadID(path)
			}
			if metadata.ResumeID == "" {
				metadata.ResumeID = metadata.RootSessionID
			}
			if metadata.IsSubagent {
				metadata.ParentSessionID = strings.TrimSpace(config.GetStr(payload, "parent_thread_id"))
				if metadata.ParentSessionID == "" {
					metadata.ParentSessionID = strings.TrimSpace(config.GetStr(spawn, "parent_thread_id"))
				}
				if metadata.ParentSessionID == "" {
					metadata.ParentSessionID = strings.TrimSpace(config.GetStr(payload, "forked_from_id"))
				}
			} else {
				metadata.RootSessionID = ""
			}
			continue
		}
		if metadata.Model == "" && typ == "turn_context" && codexRecordBelongsToLiveSession(record, metadata) {
			if model := config.GetStr(payload, "model"); model != "" {
				metadata.Model = model
			}
		}
		// Spawned Agent tasks arrive through the collaboration channel, not as a
		// human user turn. Treating a copied user row as FirstQ gives every child
		// its parent's title and exposes the parent's prompt on the child card.
		if !metadata.IsSubagent && typ == "response_item" && metadata.FirstQ == "" && config.GetStr(payload, "role") == "user" {
			for _, content := range config.GetSlice(payload, "content") {
				item, ok := content.(map[string]any)
				if !ok || config.GetStr(item, "type") != "input_text" {
					continue
				}
				text := VisibleUserText(config.GetStr(item, "text"))
				if len(text) > 2 && !isCodexNoise(text) {
					metadata.FirstQ = truncateText(text, 200)
					break
				}
			}
		}
	}
	if metadata.Model == "" && metadata.IsSubagent && metadata.HasHistoryStartOrdinal {
		metadata.Model = codexFirstOwnModel(path, metadata)
	}
	return metadata
}

func codexRolloutThreadID(path string) string {
	stem := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	const uuidLength = 36
	if len(stem) < uuidLength {
		return ""
	}
	candidate := stem[len(stem)-uuidLength:]
	if candidate[8] != '-' || candidate[13] != '-' || candidate[18] != '-' || candidate[23] != '-' {
		return ""
	}
	return candidate
}

// codexFirstOwnModel is the bounded fallback for deeply nested subagents whose
// copied history pushes their first turn_context past the cheap 30-record head
// read. It streams from the start and stops on the first eligible record, so a
// live poll pays only through the subagent boundary rather than parsing the
// rest of a long, still-growing rollout.
func codexFirstOwnModel(path string, metadata codexLiveSessionMetadata) string {
	model := ""
	scanJSONL(path, func(record map[string]any) bool {
		if !codexRecordBelongsToLiveSession(record, metadata) || config.GetStr(record, "type") != "turn_context" {
			return true
		}
		model = strings.TrimSpace(config.GetStr(config.GetMap(record, "payload"), "model"))
		return model == ""
	})
	return model
}

func codexRecordBelongsToLiveSession(record map[string]any, metadata codexLiveSessionMetadata) bool {
	if !metadata.HasHistoryStartOrdinal {
		return true
	}
	ordinal, ok := config.GetFloat(record, "ordinal")
	return ok && int(ordinal) >= metadata.HistoryStartOrdinal
}

func CodexLiveSessions(maxAge time.Duration) []Session {
	var sessions []Session
	now := time.Now().In(config.Loc)
	titles := codexSessionTitles()
	for _, dayDir := range codexSessionDayDirs(config.CodexSessions, now, maxAge) {
		files, _ := filepath.Glob(filepath.Join(dayDir, "*.jsonl"))
		for _, fp := range files {
			info, err := os.Stat(fp)
			if err != nil {
				continue
			}
			age := now.Sub(info.ModTime())
			if age > maxAge {
				continue
			}
			metadata := codexLiveMetadata(fp)
			// Guardian/security-review rollouts are Codex implementation machinery,
			// not user-returnable sessions. Source metadata is available before the
			// review process writes its model, so filter it before reading activity.
			if metadata.IsInternalSubagent || metadata.Model == "codex-auto-review" {
				continue
			}
			records := config.TailJSONL(fp, 40)
			cwd := metadata.CWD
			var lastUserMsg string
			var recentTools []string
			var lastAssistant string
			for _, r := range records {
				if !codexRecordBelongsToLiveSession(r, metadata) {
					continue
				}
				typ := config.GetStr(r, "type")
				p := config.GetMap(r, "payload")
				if p == nil {
					p = map[string]any{}
				}
				if typ == "response_item" {
					role := config.GetStr(p, "role")
					if role == "user" && !metadata.IsSubagent {
						content := config.GetSlice(p, "content")
						for _, c := range content {
							if m, ok := c.(map[string]any); ok {
								if config.GetStr(m, "type") == "input_text" {
									txt := VisibleUserText(config.GetStr(m, "text"))
									if len(txt) > 2 && !isCodexNoise(txt) {
										txt = truncateText(txt, 200)
										lastUserMsg = txt
									}
								}
							}
						}
					} else if role == "assistant" {
						content := config.GetSlice(p, "content")
						for _, c := range content {
							if m, ok := c.(map[string]any); ok {
								if config.GetStr(m, "type") == "output_text" {
									txt := strings.TrimSpace(config.GetStr(m, "text"))
									if txt != "" && !strings.HasPrefix(txt, "{") {
										txt = truncateText(txt, 200)
										lastAssistant = txt
									}
								}
							}
						}
					} else if payloadType := config.GetStr(p, "type"); payloadType == "function_call" || payloadType == "custom_tool_call" || payloadType == "tool_search_call" {
						if name := strings.TrimSpace(config.GetStr(p, "name")); name != "" {
							recentTools = append(recentTools, name)
						}
					}
				}
			}
			if message := codexLastEventMessage(fp, "user_message", metadata); message != "" && !metadata.IsSubagent {
				lastUserMsg = codexVisibleUserMessage(message)
			}
			// Keep enough agent_message rows to still fill 10 commentary updates
			// after final_answer phases are filtered out.
			agentEvents := codexRecentAgentEvents(fp, 24, metadata)
			var latestResult string
			var recentUpdates []string
			if len(agentEvents) > 0 {
				lastAssistant = truncateText(agentEvents[len(agentEvents)-1].Message, 400)
			}
			for _, event := range agentEvents {
				switch event.Phase {
				case "final_answer":
					latestResult = truncateText(event.Message, 1_200)
				case "commentary":
					recentUpdates = append(recentUpdates, truncateText(event.Message, 400))
				}
			}
			if len(recentUpdates) > 10 {
				recentUpdates = recentUpdates[len(recentUpdates)-10:]
			}
			project := ""
			if cwd != "" {
				project = config.ProjectFromPath(cwd)
			}
			if project == "" {
				if headCwd := codexReadCwdFromHead(fp); headCwd != "" {
					project = config.ProjectFromPath(headCwd)
				}
			}
			stem := strings.TrimSuffix(filepath.Base(fp), ".jsonl")
			sid := stem
			if idx := strings.LastIndex(stem, "-"); idx > 0 {
				sid = stem[idx+1:]
			}
			if len(sid) > 8 {
				sid = sid[:8]
			}
			if project == "" {
				project = sid
			}
			if len(recentTools) > 5 {
				recentTools = recentTools[len(recentTools)-5:]
			}
			sessions = append(sessions, Session{
				Tool:            "Codex",
				Project:         project,
				SessionID:       sid,
				ResumeID:        metadata.ResumeID,
				RootSessionID:   metadata.RootSessionID,
				ParentSessionID: metadata.ParentSessionID,
				AgentPath:       metadata.AgentPath,
				AgentNickname:   metadata.AgentNickname,
				SubagentDepth:   metadata.SubagentDepth,
				Client:          metadata.Client,
				CWD:             metadata.CWD,
				StartedAt:       metadata.StartedAt,
				Model:           metadata.Model,
				AgeSeconds:      int(age.Seconds()),
				Summary:         titles[metadata.ResumeID],
				FirstQ:          metadata.FirstQ,
				LastUserMsg:     lastUserMsg,
				RecentTools:     recentTools,
				LastAssistant:   lastAssistant,
				LatestResult:    latestResult,
				RecentUpdates:   recentUpdates,
			})
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].AgeSeconds < sessions[j].AgeSeconds
	})
	return sessions
}

// Codex Desktop stores the user-visible, generated thread title separately
// from the rollout transcript. Reading the adjacent append-only index keeps ATM
// cards aligned with the title shown in Codex instead of using the latest turn
// as a moving pseudo-title.
func codexSessionTitles() map[string]string {
	path := codexSessionIndexPath()
	var stamp int64
	if info, err := os.Stat(path); err == nil {
		stamp = info.ModTime().UnixNano()
	}

	codexTitleCache.mu.Lock()
	defer codexTitleCache.mu.Unlock()
	if codexTitleCache.titles != nil && codexTitleCache.path == path && codexTitleCache.stamp == stamp {
		return codexTitleCache.titles
	}
	titles := codexSessionTitlesFromFile(path)
	codexTitleCache.path = path
	codexTitleCache.stamp = stamp
	codexTitleCache.titles = titles
	return titles
}

// CodexThreadTitles exposes the same titles to callers that hold a codex thread
// id but no transcript — the todo binding ledger stores that id, so a bound
// session indexed before this title lookup existed can still be named without
// re-parsing its rollout.
func CodexThreadTitles() map[string]string { return codexSessionTitles() }

func codexSessionIndexPath() string {
	return filepath.Join(filepath.Dir(config.CodexSessions), "session_index.jsonl")
}

// The index is one small file re-read for every rollout during a full sync, so
// it is cached until its mtime moves. Titles are returned read-only.
var codexTitleCache struct {
	mu     sync.Mutex
	path   string
	stamp  int64
	titles map[string]string
}

// codexSummary names a session the way Codex itself does. Codex never writes a
// title into the transcript, so without this every codex session reached ATM
// with an empty summary and read as a bare id. The generated thread name is
// preferred; the first substantive prompt is the fallback for threads the index
// has not (or no longer) covered.
func codexSummary(threadID string, inputs []Message) string {
	if threadID != "" {
		if title := strings.TrimSpace(codexSessionTitles()[threadID]); title != "" {
			return truncateText(title, 200)
		}
	}
	for _, input := range inputs {
		text := VisibleUserText(input.Content)
		if text == "" || isCodexNoise(text) {
			continue
		}
		return truncateText(FirstLine(text), 200)
	}
	return ""
}

func codexSessionTitlesFromFile(path string) map[string]string {
	titles := make(map[string]string)
	scanJSONL(path, func(record map[string]any) bool {
		id := strings.TrimSpace(config.GetStr(record, "id"))
		title := strings.TrimSpace(config.GetStr(record, "thread_name"))
		if id != "" && title != "" {
			titles[id] = title
		}
		return true
	})
	return titles
}

func codexLastEventMessage(path, eventType string, metadata codexLiveSessionMetadata) string {
	marker := []byte(`"type":"` + eventType + `"`)
	record := lastJSONLRecordContaining(path, marker)
	if !codexRecordBelongsToLiveSession(record, metadata) {
		return ""
	}
	payload := config.GetMap(record, "payload")
	if config.GetStr(record, "type") != "event_msg" || config.GetStr(payload, "type") != eventType {
		return ""
	}
	return config.GetStr(payload, "message")
}

type codexAgentEvent struct {
	Message string
	Phase   string
}

func codexRecentAgentEvents(path string, limit int, metadata codexLiveSessionMetadata) []codexAgentEvent {
	// Both the legacy event_msg and current response_item encodings carry a
	// phase. Current rollouts also emit an item_completed row for each assistant
	// item, so oversample the reverse scan and count only records we can decode.
	records := lastJSONLRecordsContaining(path, []byte(`"phase":"`), limit*3)
	newestFirst := make([]codexAgentEvent, 0, limit)
	for _, record := range records {
		if !codexRecordBelongsToLiveSession(record, metadata) {
			continue
		}
		event, ok := codexAgentEventFromRecord(record)
		if !ok {
			continue
		}
		newestFirst = append(newestFirst, event)
		if len(newestFirst) == limit {
			break
		}
	}
	events := make([]codexAgentEvent, len(newestFirst))
	for index, event := range newestFirst {
		events[len(newestFirst)-1-index] = event
	}
	return events
}

func codexAgentEventFromRecord(record map[string]any) (codexAgentEvent, bool) {
	payload := config.GetMap(record, "payload")
	phase := strings.TrimSpace(config.GetStr(payload, "phase"))
	var message string
	switch {
	case config.GetStr(record, "type") == "event_msg" && config.GetStr(payload, "type") == "agent_message":
		message = strings.TrimSpace(config.GetStr(payload, "message"))
	case config.GetStr(record, "type") == "response_item" && config.GetStr(payload, "role") == "assistant":
		for _, content := range config.GetSlice(payload, "content") {
			item, ok := content.(map[string]any)
			if !ok || config.GetStr(item, "type") != "output_text" {
				continue
			}
			text := strings.TrimSpace(config.GetStr(item, "text"))
			if text != "" {
				message = appendTextBlock(message, text)
			}
		}
	default:
		return codexAgentEvent{}, false
	}
	if message == "" || phase == "" {
		return codexAgentEvent{}, false
	}
	return codexAgentEvent{Message: message, Phase: phase}, true
}

func codexVisibleUserMessage(message string) string {
	return truncateText(VisibleUserText(message), 200)
}

func codexSessionDayDirs(root string, now time.Time, maxAge time.Duration) []string {
	start := now.Add(-maxAge)
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	endDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var dirs []string
	for day := startDay; !day.After(endDay); day = day.AddDate(0, 0, 1) {
		dirs = append(dirs, filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02")))
	}
	return dirs
}
