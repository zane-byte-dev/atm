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

// CodexQuotaInfo is a compatibility alias for QuotaInfo.
type CodexQuotaInfo = QuotaInfo

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
	tools := map[string]int{}
	var skills []SkillEvent
	cwd := ""
	threadID := ""
	curAssistant := ""
	var curAssistantTS int64
	var usage Usage
	var usageEvents []UsageEvent
	var previousTotal [3]int64
	var sawTotal bool
	var timing durationTracker

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
		codexTrackTiming(&timing, typ, p, recordTSMS)
		if typ == "session_meta" {
			cwd = config.GetStr(p, "cwd")
			// The thread id is what Codex keys its own generated titles by; the
			// rollout filename only carries it as a suffix.
			if threadID == "" {
				threadID = strings.TrimSpace(config.GetStr(p, "id"))
				if threadID == "" {
					threadID = strings.TrimSpace(config.GetStr(p, "session_id"))
				}
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
		} else if typ == "response_item" {
			role := config.GetStr(p, "role")
			if role == "user" {
				if len(userInputs) > 0 && curAssistant != "" {
					assistantOutputs = append(assistantOutputs, Message{Content: curAssistant, TS: curAssistantTS})
					curAssistant = ""
					curAssistantTS = 0
				}
				content := config.GetSlice(p, "content")
				for _, c := range content {
					if m, ok := c.(map[string]any); ok {
						if config.GetStr(m, "type") == "input_text" {
							t := config.GetStr(m, "text")
							if len(strings.TrimSpace(t)) > 2 && !isCodexNoise(t) {
								userInputs = append(userInputs, Message{Content: t, TS: recordTS})
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
							if len(txt) > 2 && !strings.HasPrefix(txt, "{") {
								curAssistant = appendTextBlock(curAssistant, txt)
								curAssistantTS = recordTS
							}
						}
					}
				}
			} else if config.GetStr(p, "type") == "function_call" {
				name := config.GetStr(p, "name")
				tools[name]++
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
	if len(userInputs) == 0 && len(tools) == 0 {
		return nil
	}
	project := ""
	if cwd != "" {
		project = config.ProjectFromPath(cwd)
	}
	return &ParsedFile{
		SessionID:   fullID,
		ShortID:     shortID,
		Agent:       "codex",
		Project:     project,
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     codexSummary(threadID, userInputs),
		Inputs:      userInputs,
		Outputs:     assistantOutputs,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(fp),
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
		case "reasoning", "agent_message", "custom_tool_call", "function_call", "tool_search_call":
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
			records := config.TailJSONL(fp, 40)
			cwd := ""
			var lastUserMsg string
			var recentTools []string
			var lastAssistant string
			for _, r := range records {
				typ := config.GetStr(r, "type")
				p := config.GetMap(r, "payload")
				if p == nil {
					p = map[string]any{}
				}
				if typ == "session_meta" {
					cwd = config.GetStr(p, "cwd")
				} else if typ == "response_item" {
					role := config.GetStr(p, "role")
					if role == "user" {
						content := config.GetSlice(p, "content")
						for _, c := range content {
							if m, ok := c.(map[string]any); ok {
								if config.GetStr(m, "type") == "input_text" {
									txt := config.GetStr(m, "text")
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
					} else if config.GetStr(p, "type") == "function_call" {
						recentTools = append(recentTools, config.GetStr(p, "name"))
					}
				}
			}
			if message := codexLastEventMessage(fp, "user_message"); message != "" {
				lastUserMsg = codexVisibleUserMessage(message)
			}
			// Keep enough agent_message rows to still fill 10 commentary updates
			// after final_answer phases are filtered out.
			agentEvents := codexRecentAgentEvents(fp, 24)
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
			var firstQ, model string
			var resumeID, client, sessionCWD string
			var startedAt time.Time
			for _, r := range config.HeadJSONL(fp, 30) {
				typ := config.GetStr(r, "type")
				if startedAt.IsZero() {
					startedAt, _ = time.Parse(time.RFC3339Nano, config.GetStr(r, "timestamp"))
				}
				if typ == "session_meta" {
					payload := config.GetMap(r, "payload")
					resumeID = config.GetStr(payload, "id")
					if resumeID == "" {
						resumeID = config.GetStr(payload, "session_id")
					}
					client = config.GetStr(payload, "originator")
					sessionCWD = config.GetStr(payload, "cwd")
				} else if typ == "turn_context" {
					if m := config.GetStr(config.GetMap(r, "payload"), "model"); m != "" {
						model = m
					}
				} else if typ == "response_item" && firstQ == "" {
					p := config.GetMap(r, "payload")
					if p != nil && config.GetStr(p, "role") == "user" {
						for _, c := range config.GetSlice(p, "content") {
							if m, ok := c.(map[string]any); ok && config.GetStr(m, "type") == "input_text" {
								txt := config.GetStr(m, "text")
								if len(txt) > 2 && !isCodexNoise(txt) {
									txt = truncateText(txt, 200)
									firstQ = txt
								}
							}
						}
					}
				}
				if firstQ != "" && model != "" {
					break
				}
			}
			// Codex Desktop writes short-lived guardian/security-review rollouts
			// beside user-owned threads. They are implementation machinery rather
			// than a session the user can return to, so keep them out of presence.
			if model == "codex-auto-review" {
				continue
			}

			sessions = append(sessions, Session{
				Tool:          "Codex",
				Project:       project,
				SessionID:     sid,
				ResumeID:      resumeID,
				Client:        client,
				CWD:           sessionCWD,
				StartedAt:     startedAt,
				Model:         model,
				AgeSeconds:    int(age.Seconds()),
				Summary:       titles[resumeID],
				FirstQ:        firstQ,
				LastUserMsg:   lastUserMsg,
				RecentTools:   recentTools,
				LastAssistant: lastAssistant,
				LatestResult:  latestResult,
				RecentUpdates: recentUpdates,
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

func codexLastEventMessage(path, eventType string) string {
	marker := []byte(`"type":"` + eventType + `"`)
	record := lastJSONLRecordContaining(path, marker)
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

func codexRecentAgentEvents(path string, limit int) []codexAgentEvent {
	records := lastJSONLRecordsContaining(path, []byte(`"type":"agent_message"`), limit)
	events := make([]codexAgentEvent, 0, len(records))
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		payload := config.GetMap(record, "payload")
		if config.GetStr(record, "type") != "event_msg" || config.GetStr(payload, "type") != "agent_message" {
			continue
		}
		message := strings.TrimSpace(config.GetStr(payload, "message"))
		if message == "" {
			continue
		}
		events = append(events, codexAgentEvent{
			Message: message,
			Phase:   config.GetStr(payload, "phase"),
		})
	}
	return events
}

func codexVisibleUserMessage(message string) string {
	message = strings.TrimSpace(message)
	const requestMarker = "## My request for Codex:"
	if index := strings.LastIndex(message, requestMarker); index >= 0 {
		message = strings.TrimSpace(message[index+len(requestMarker):])
	}
	return truncateText(message, 200)
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
