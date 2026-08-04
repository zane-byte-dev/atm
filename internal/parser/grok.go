package parser

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

var grokUserQueryPattern = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)

func isGrokNoise(txt string) bool {
	t := strings.TrimSpace(txt)
	if len(t) < 3 {
		return true
	}
	for _, prefix := range []string{
		"<system-reminder>",
		"<user_info>",
		"<git_status>",
		"<action_safety>",
		"<open_and_recently_viewed_files>",
		"<agent_skills>",
		"<mcp_file_system>",
		"<offline_docs>",
		"[grok-build-vscode primer",
		"## HIDDEN PRIMER",
		"You are Grok",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

// DiscoverGrok lists Grok Build chat_history.jsonl transcripts under
// ~/.grok/sessions/<url-encoded-cwd>/<session-id>/chat_history.jsonl.
func DiscoverGrok() []string {
	var files []string
	cwdEntries, err := os.ReadDir(config.GrokSessions)
	if err != nil {
		return nil
	}
	for _, cwdEntry := range cwdEntries {
		if !cwdEntry.IsDir() {
			continue
		}
		cwdPath := filepath.Join(config.GrokSessions, cwdEntry.Name())
		sessionEntries, err := os.ReadDir(cwdPath)
		if err != nil {
			continue
		}
		for _, sessionEntry := range sessionEntries {
			if !sessionEntry.IsDir() {
				continue
			}
			chatPath := filepath.Join(cwdPath, sessionEntry.Name(), "chat_history.jsonl")
			if _, err := os.Stat(chatPath); err == nil {
				files = append(files, chatPath)
			}
		}
	}
	sort.Strings(files)
	return files
}

// GrokSourceVersion returns max mtime and total size across the session files
// that feed a parse: chat_history (messages), updates (usage), summary (title).
// Sync compares these instead of chat alone so a late turn_completed write is
// not skipped forever after chat has stabilized.
func GrokSourceVersion(chatPath string) (mtime, size int64, ok bool) {
	dir := filepath.Dir(chatPath)
	for _, name := range []string{"chat_history.jsonl", "updates.jsonl", "summary.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		ok = true
		if mt := info.ModTime().Unix(); mt > mtime {
			mtime = mt
		}
		size += info.Size()
	}
	return mtime, size, ok
}

func GrokParseFile(fp string) *ParsedFile {
	sessionDir := filepath.Dir(fp)
	sessionID := filepath.Base(sessionDir)
	if sessionID == "" || sessionID == "." {
		return nil
	}
	shortID := sessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	summary := readGrokSummary(filepath.Join(sessionDir, "summary.json"))
	cwd := summary.cwd
	if cwd == "" {
		cwd = grokCWDFromSessionPath(sessionDir)
	}
	project := ""
	if cwd != "" {
		project = config.ProjectFromPath(cwd)
	}
	if project == "" {
		project = config.CanonicalProject(filepath.Base(strings.TrimRight(cwd, string(filepath.Separator))))
	}

	var inputs, outputs []Message
	var messages []TranscriptMessage
	tools := map[string]int{}
	var skillDrafts []grokSkillDraft
	assistantOrd := 0
	usage := Usage{Model: summary.model}
	var createdTS, lastTS int64
	createdAt := ""

	if summary.createdTS > 0 {
		createdTS = summary.createdTS
		createdAt = time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
	}
	if summary.updatedTS > 0 {
		lastTS = summary.updatedTS
	}

	// chat_history has no per-line timestamps; spread message order across the
	// session window so relative ordering is preserved in timeline views.
	scanJSONL(fp, func(r map[string]any) bool {
		typ := config.GetStr(r, "type")
		switch typ {
		case "user":
			if config.GetStr(r, "synthetic_reason") != "" {
				return true
			}
			txt := grokMessageText(r)
			txt = grokExtractUserQuery(txt)
			if isGrokNoise(txt) {
				return true
			}
			if len([]rune(txt)) <= 2 {
				return true
			}
			inputs = append(inputs, Message{Content: txt})
			messages = append(messages, TranscriptMessage{Role: "user", Content: txt})
		case "assistant":
			ord := assistantOrd
			assistantOrd++
			if model := config.GetStr(r, "model_id"); model != "" {
				usage.Model = model
			}
			for _, call := range config.GetSlice(r, "tool_calls") {
				m, ok := call.(map[string]any)
				if !ok {
					continue
				}
				name := config.GetStr(m, "name")
				if name == "" {
					continue
				}
				tools[name]++
				args := grokToolArgs(m)
				if skill := skillFromToolCall(name, args); skill != "" {
					skillDrafts = append(skillDrafts, grokSkillDraft{name: skill, assistantOrd: ord})
				}
			}
			txt := strings.TrimSpace(grokMessageText(r))
			if txt != "" {
				outputs = append(outputs, Message{Content: txt})
				messages = append(messages, TranscriptMessage{Role: "assistant", Content: txt})
			}
		}
		return true
	})

	usageEvents := grokUsageFromUpdates(filepath.Join(sessionDir, "updates.jsonl"), usage.Model)
	for _, event := range usageEvents {
		if usage.Model == "" && event.Model != "" {
			usage.Model = event.Model
		}
		usage.InputTokens += event.InputTokens
		usage.OutputTokens += event.OutputTokens
		usage.CacheCreateTokens += event.CacheCreateTokens
		usage.CacheReadTokens += event.CacheReadTokens
		usage.RequestCount += EventRequestCount(event)
		if event.TS > lastTS {
			lastTS = event.TS
		}
		if createdTS == 0 || (event.TS > 0 && event.TS < createdTS) {
			createdTS = event.TS
			createdAt = time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
		}
	}

	if lastTS == 0 {
		if info, err := os.Stat(fp); err == nil {
			lastTS = info.ModTime().Unix()
		}
	}
	if createdTS == 0 {
		createdTS = lastTS
		if createdTS > 0 {
			createdAt = time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
		}
	}

	// Assign timestamps after usage may have tightened the session window.
	assignGrokMessageTimestamps(inputs, outputs, messages, createdTS, lastTS)
	skills := grokBuildSkillEvents(skillDrafts, assistantOrd, createdTS, lastTS)

	if len(messages) == 0 && len(tools) == 0 && len(usageEvents) == 0 {
		return nil
	}

	title := summary.title
	if title == "" && len(inputs) > 0 {
		title = truncateText(inputs[0].Content, 120)
	}

	return &ParsedFile{
		SessionID:   sessionID,
		ShortID:     shortID,
		Agent:       "grokbuild",
		Project:     project,
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     title,
		Inputs:      inputs,
		Outputs:     outputs,
		Messages:    messages,
		Tools:       tools,
		Skills:      compactSkillEvents(skills),
		Usage:       usage,
		UsageEvents: usageEvents,
		EndOffset:   fileSize(fp),
	}
}

type grokSummaryInfo struct {
	cwd       string
	title     string
	model     string
	createdTS int64
	updatedTS int64
}

func readGrokSummary(path string) grokSummaryInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return grokSummaryInfo{}
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		return grokSummaryInfo{}
	}
	info := grokSummaryInfo{
		title: firstNonEmpty(
			config.GetStr(raw, "generated_title"),
			config.GetStr(raw, "session_summary"),
		),
		model: config.GetStr(raw, "current_model_id"),
	}
	if nested := config.GetMap(raw, "info"); nested != nil {
		info.cwd = config.GetStr(nested, "cwd")
	}
	if info.cwd == "" {
		info.cwd = config.GetStr(raw, "cwd")
	}
	if ts, ok := parseTimestampString(config.GetStr(raw, "created_at")); ok {
		info.createdTS = ts.Unix()
	}
	if ts, ok := parseTimestampString(firstNonEmpty(
		config.GetStr(raw, "last_active_at"),
		config.GetStr(raw, "updated_at"),
	)); ok {
		info.updatedTS = ts.Unix()
	}
	return info
}

func grokCWDFromSessionPath(sessionDir string) string {
	// .../sessions/<url-encoded-cwd>/<session-id>
	encoded := filepath.Base(filepath.Dir(sessionDir))
	if encoded == "" || encoded == "." || encoded == "sessions" {
		return ""
	}
	decoded, err := url.PathUnescape(encoded)
	if err != nil || decoded == "" {
		decoded, err = url.QueryUnescape(encoded)
		if err != nil {
			return ""
		}
	}
	return decoded
}

func grokMessageText(r map[string]any) string {
	if s := config.GetStr(r, "content"); s != "" {
		return s
	}
	var parts []string
	for _, item := range config.GetSlice(r, "content") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch config.GetStr(m, "type") {
		case "text", "":
			if txt := strings.TrimSpace(config.GetStr(m, "text")); txt != "" {
				parts = append(parts, txt)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func grokExtractUserQuery(txt string) string {
	match := grokUserQueryPattern.FindStringSubmatch(txt)
	if len(match) == 2 && strings.TrimSpace(match[1]) != "" {
		return strings.TrimSpace(match[1])
	}
	return strings.TrimSpace(txt)
}

func grokToolArgs(call map[string]any) map[string]any {
	if args := config.GetMap(call, "arguments"); args != nil {
		return args
	}
	raw := config.GetStr(call, "arguments")
	if raw == "" {
		return nil
	}
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) != nil {
		return map[string]any{"command": raw}
	}
	return args
}

func assignGrokMessageTimestamps(inputs, outputs []Message, messages []TranscriptMessage, createdTS, lastTS int64) {
	if createdTS == 0 && lastTS == 0 {
		return
	}
	if lastTS == 0 {
		lastTS = createdTS
	}
	if createdTS == 0 {
		createdTS = lastTS
	}
	n := len(messages)
	if n == 0 {
		return
	}
	span := lastTS - createdTS
	if span < 0 {
		span = 0
	}
	for i := range messages {
		var ts int64
		if n == 1 {
			ts = createdTS
		} else {
			ts = createdTS + span*int64(i)/int64(n-1)
		}
		messages[i].TS = ts
	}
	// Mirror timestamps onto the parallel input/output slices by role order.
	inIdx, outIdx := 0, 0
	for _, m := range messages {
		switch m.Role {
		case "user":
			if inIdx < len(inputs) {
				inputs[inIdx].TS = m.TS
				inIdx++
			}
		case "assistant":
			if outIdx < len(outputs) {
				outputs[outIdx].TS = m.TS
				outIdx++
			}
		}
	}
}

func grokInterpolateTS(createdTS, lastTS int64, index, count int) int64 {
	if count <= 0 {
		return createdTS
	}
	if lastTS == 0 {
		lastTS = createdTS
	}
	if createdTS == 0 {
		createdTS = lastTS
	}
	if count == 1 {
		return createdTS
	}
	span := lastTS - createdTS
	if span < 0 {
		span = 0
	}
	if index < 0 {
		index = 0
	}
	if index >= count {
		index = count - 1
	}
	return createdTS + span*int64(index)/int64(count-1)
}

type grokSkillDraft struct {
	name         string
	assistantOrd int
}

func grokBuildSkillEvents(drafts []grokSkillDraft, assistantCount int, createdTS, lastTS int64) []SkillEvent {
	var skills []SkillEvent
	for _, d := range drafts {
		ts := grokInterpolateTS(createdTS, lastTS, d.assistantOrd, assistantCount)
		skills = appendSkillEvent(skills, d.name, ts)
	}
	return skills
}

// grokUsageFromUpdates extracts per-turn token usage from updates.jsonl
// turn_completed records. Grok reports aggregated turn usage (not always
// per model call); each turn becomes one usage_event whose RequestCount is
// modelCalls so rollup can SUM call counts.
func grokUsageFromUpdates(path, fallbackModel string) []UsageEvent {
	var events []UsageEvent
	scanJSONL(path, func(r map[string]any) bool {
		params := config.GetMap(r, "params")
		if params == nil {
			return true
		}
		update := config.GetMap(params, "update")
		if update == nil {
			return true
		}
		if config.GetStr(update, "sessionUpdate") != "turn_completed" {
			return true
		}
		usage := config.GetMap(update, "usage")
		if usage == nil {
			return true
		}

		ts := grokUpdateTimestamp(r, update)
		promptID := firstNonEmpty(
			config.GetStr(update, "prompt_id"),
			config.GetStr(update, "promptId"),
		)

		modelUsage := config.GetMap(usage, "modelUsage")
		if len(modelUsage) > 0 {
			// Stable order for tests and deterministic fingerprints.
			models := make([]string, 0, len(modelUsage))
			for model := range modelUsage {
				models = append(models, model)
			}
			sort.Strings(models)
			for _, model := range models {
				raw, ok := modelUsage[model].(map[string]any)
				if !ok {
					continue
				}
				if event, ok := grokUsageEvent(raw, model, ts, promptID); ok {
					events = append(events, event)
				}
			}
			return true
		}

		model := firstNonEmpty(fallbackModel, "grok")
		if event, ok := grokUsageEvent(usage, model, ts, promptID); ok {
			events = append(events, event)
		}
		return true
	})
	return events
}

func grokUsageEvent(raw map[string]any, model string, ts int64, promptID string) (UsageEvent, bool) {
	in, _ := config.GetFloat(raw, "inputTokens")
	out, _ := config.GetFloat(raw, "outputTokens")
	cacheRead, _ := config.GetFloat(raw, "cachedReadTokens")
	if in == 0 && out == 0 && cacheRead == 0 {
		// Also accept snake_case in case the wire format changes.
		in, _ = config.GetFloat(raw, "input_tokens")
		out, _ = config.GetFloat(raw, "output_tokens")
		cacheRead, _ = config.GetFloat(raw, "cached_read_tokens")
	}
	input := int64(in)
	cache := int64(cacheRead)
	if cache > input {
		cache = input
	}
	input -= cache
	if input == 0 && int64(out) == 0 && cache == 0 {
		return UsageEvent{}, false
	}
	calls := 1
	if n, ok := config.GetFloat(raw, "modelCalls"); ok && n > 0 {
		calls = int(n)
	} else if n, ok := config.GetFloat(raw, "model_calls"); ok && n > 0 {
		calls = int(n)
	} else if n, ok := config.GetFloat(raw, "numTurns"); ok && n > 0 {
		// Older blobs used numTurns for the same agent-loop count.
		calls = int(n)
	}
	// Grok is the one agent that reports how long the model took, so nothing here
	// is derived from record timestamps. apiDurationMs covers the API calls only —
	// tool execution between them is excluded, which is the same window the other
	// parsers approximate — and it covers all `calls` of them together.
	var durationMS int64
	if ms, ok := config.GetFloat(raw, "apiDurationMs"); ok && ms > 0 {
		durationMS = int64(ms)
	} else if ms, ok := config.GetFloat(raw, "api_duration_ms"); ok && ms > 0 {
		durationMS = int64(ms)
	}
	fp := ""
	if promptID != "" {
		fp = "grokbuild:" + promptID
		if model != "" {
			fp += ":" + model
		}
	}
	return UsageEvent{
		Model:           model,
		TS:              ts,
		InputTokens:     input,
		OutputTokens:    int64(out),
		CacheReadTokens: cache,
		RequestCount:    calls,
		Fingerprint:     fp,
		DurationMS:      durationMS,
	}, true
}

func grokUpdateTimestamp(r, update map[string]any) int64 {
	// _meta may sit on the update itself (stream chunks) or as a sibling under
	// params (turn_completed).
	if meta := config.GetMap(update, "_meta"); meta != nil {
		if ms, ok := config.GetFloat(meta, "agentTimestampMs"); ok && ms > 0 {
			return int64(ms / 1000)
		}
	}
	if params := config.GetMap(r, "params"); params != nil {
		if meta := config.GetMap(params, "_meta"); meta != nil {
			if ms, ok := config.GetFloat(meta, "agentTimestampMs"); ok && ms > 0 {
				return int64(ms / 1000)
			}
		}
	}
	if ts, ok := firstTimestamp([]string{"timestamp", "ts"}, r); ok {
		return ts.Unix()
	}
	return 0
}

func GrokLiveSessions(maxAge time.Duration) []Session {
	var sessions []Session
	now := time.Now().In(config.Loc)
	for _, fp := range DiscoverGrok() {
		// Prefer the freshest sibling so an in-flight turn still looks live
		// after chat_history has stopped growing.
		age := maxAge + time.Second
		if _, sizeTotal, ok := GrokSourceVersion(fp); ok && sizeTotal >= 0 {
			for _, name := range []string{"updates.jsonl", "chat_history.jsonl", "summary.json"} {
				if info, err := os.Stat(filepath.Join(filepath.Dir(fp), name)); err == nil {
					if a := now.Sub(info.ModTime()); a < age {
						age = a
					}
				}
			}
		} else {
			info, err := os.Stat(fp)
			if err != nil {
				continue
			}
			age = now.Sub(info.ModTime())
		}
		if age > maxAge {
			continue
		}

		summary := readGrokSummary(filepath.Join(filepath.Dir(fp), "summary.json"))
		cwd := summary.cwd
		if cwd == "" {
			cwd = grokCWDFromSessionPath(filepath.Dir(fp))
		}
		project := ""
		if cwd != "" {
			project = config.ProjectFromPath(cwd)
		}
		sessionID := filepath.Base(filepath.Dir(fp))
		firstQ, lastUser, lastAssistant, recentTools, model := grokLiveScan(fp)
		if model == "" {
			model = summary.model
		}
		title := summary.title
		if title == "" {
			title = firstQ
		}
		sessions = append(sessions, Session{
			Tool:          "Grok Build",
			Project:       project,
			SessionID:     sessionID,
			Model:         model,
			AgeSeconds:    int(age.Seconds()),
			Summary:       truncateText(title, 120),
			FirstQ:        firstQ,
			LastUserMsg:   lastUser,
			RecentTools:   recentTools,
			LastAssistant: lastAssistant,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].AgeSeconds < sessions[j].AgeSeconds
	})
	return sessions
}

// GrokLogPath is the unified shell log that records billing: fetched credits
// config lines. Derived from GrokSessions so a custom sessions root still works.
func GrokLogPath() string {
	// ~/.grok/sessions → ~/.grok/logs/unified.jsonl
	return filepath.Join(filepath.Dir(config.GrokSessions), "logs", "unified.jsonl")
}

// GrokQuota reads the latest Grok credit-usage snapshot from the shell log.
// Grok does not store rate limits next to sessions the way Codex does; instead
// each interactive shell logs `billing: fetched credits config` with
// creditUsagePercent and the billing period end (the refresh time).
//
// Returns nil when the log is missing or never recorded a successful fetch.
func GrokQuota() *QuotaInfo {
	var best *QuotaInfo
	var bestTS string
	scanJSONL(GrokLogPath(), func(r map[string]any) bool {
		if config.GetStr(r, "msg") != "billing: fetched credits config" {
			return true
		}
		ts := config.GetStr(r, "ts")
		// ISO-8601 timestamps with a fixed Z offset sort chronologically as text.
		if ts != "" && bestTS != "" && ts < bestTS {
			return true
		}
		info := grokQuotaFromLogEntry(r)
		if info == nil {
			return true
		}
		best = info
		if ts != "" {
			bestTS = ts
		}
		return true
	})
	if best != nil {
		best.Source = "log"
	}
	return best
}

func grokQuotaFromLogEntry(r map[string]any) *QuotaInfo {
	ctx := config.GetMap(r, "ctx")
	if ctx == nil {
		return nil
	}
	cfg := config.GetMap(ctx, "config")
	if cfg == nil {
		return nil
	}
	info := grokQuotaFromConfig(cfg, config.GetStr(ctx, "subscriptionTier"))
	if info == nil {
		return nil
	}
	if ts, ok := parseTimestampString(config.GetStr(r, "ts")); ok {
		info.Timestamp = ts
	}
	return info
}

// grokQuotaFromConfig turns one Grok credits config blob into QuotaInfo. The
// same shape appears in the unified shell log (under ctx.config), in the live
// billing API response (under config), and in the local live cache.
func grokQuotaFromConfig(cfg map[string]any, plan string) *QuotaInfo {
	if cfg == nil {
		return nil
	}
	periodStart, periodEnd, periodType := grokBillingPeriod(cfg)
	// A usable snapshot needs either a used% or a period end (refresh time).
	pct, hasPct := config.GetFloat(cfg, "creditUsagePercent")
	if !hasPct && periodEnd == 0 {
		return nil
	}
	if !hasPct {
		pct = 0
	}

	info := &QuotaInfo{
		Plan: firstNonEmpty(plan, config.GetStr(cfg, "subscription_tier")),
		Primary: &QuotaLimit{
			UsedPercent:   pct,
			WindowMinutes: grokWindowMinutes(periodType, periodStart, periodEnd),
			ResetsAt:      periodEnd,
		},
	}

	// On-demand (pay-as-you-go) is a second window when a cap is configured.
	if capVal, ok := grokMoneyVal(cfg, "onDemandCap"); ok && capVal > 0 {
		used, _ := grokMoneyVal(cfg, "onDemandUsed")
		secondaryPct := used / capVal * 100
		if secondaryPct < 0 {
			secondaryPct = 0
		}
		info.Secondary = &QuotaLimit{
			UsedPercent:   secondaryPct,
			WindowMinutes: info.Primary.WindowMinutes,
			ResetsAt:      periodEnd,
		}
	}

	// Per-product split of the same pool; the live API reports it, the shell
	// log historically does not, so this usually stays empty for source=log.
	for _, item := range config.GetSlice(cfg, "productUsage") {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := config.GetStr(m, "product")
		used, hasUsed := config.GetFloat(m, "usagePercent")
		if name == "" || !hasUsed {
			continue
		}
		info.Products = append(info.Products, QuotaProduct{Name: name, UsedPercent: used})
	}
	return info
}

func grokBillingPeriod(cfg map[string]any) (startTS, endTS int64, periodType string) {
	if start, ok := parseTimestampString(config.GetStr(cfg, "billingPeriodStart")); ok {
		startTS = start.Unix()
	}
	if end, ok := parseTimestampString(config.GetStr(cfg, "billingPeriodEnd")); ok {
		endTS = end.Unix()
	}
	if period := config.GetMap(cfg, "currentPeriod"); period != nil {
		periodType = config.GetStr(period, "type")
		if startTS == 0 {
			if start, ok := parseTimestampString(config.GetStr(period, "start")); ok {
				startTS = start.Unix()
			}
		}
		if endTS == 0 {
			if end, ok := parseTimestampString(config.GetStr(period, "end")); ok {
				endTS = end.Unix()
			}
		}
	}
	return startTS, endTS, periodType
}

func grokWindowMinutes(periodType string, startTS, endTS int64) int {
	if startTS > 0 && endTS > startTS {
		mins := int((endTS - startTS) / 60)
		if mins > 0 {
			return mins
		}
	}
	switch periodType {
	case "USAGE_PERIOD_TYPE_WEEKLY":
		return 7 * 24 * 60
	case "USAGE_PERIOD_TYPE_MONTHLY":
		return 30 * 24 * 60
	case "USAGE_PERIOD_TYPE_DAILY":
		return 24 * 60
	default:
		return 0
	}
}

// grokMoneyVal unwraps {"val": N} money/credit wrappers Grok logs use.
func grokMoneyVal(cfg map[string]any, key string) (float64, bool) {
	if v, ok := config.GetFloat(cfg, key); ok {
		return v, true
	}
	if m := config.GetMap(cfg, key); m != nil {
		return config.GetFloat(m, "val")
	}
	return 0, false
}

func grokLiveScan(fp string) (firstQ, lastUser, lastAssistant string, recentTools []string, model string) {
	toolSet := map[string]bool{}
	scanJSONL(fp, func(r map[string]any) bool {
		switch config.GetStr(r, "type") {
		case "user":
			if config.GetStr(r, "synthetic_reason") != "" {
				return true
			}
			txt := grokExtractUserQuery(grokMessageText(r))
			if isGrokNoise(txt) || len([]rune(txt)) <= 2 {
				return true
			}
			txt = truncateText(txt, 200)
			if firstQ == "" {
				firstQ = txt
			}
			lastUser = txt
		case "assistant":
			if m := config.GetStr(r, "model_id"); m != "" {
				model = m
			}
			for _, call := range config.GetSlice(r, "tool_calls") {
				if m, ok := call.(map[string]any); ok {
					if name := config.GetStr(m, "name"); name != "" {
						toolSet[name] = true
					}
				}
			}
			if txt := strings.TrimSpace(grokMessageText(r)); txt != "" {
				lastAssistant = truncateText(txt, 200)
			}
		}
		return true
	})
	for name := range toolSet {
		recentTools = append(recentTools, name)
	}
	sort.Strings(recentTools)
	if len(recentTools) > 8 {
		recentTools = recentTools[:8]
	}
	return
}
