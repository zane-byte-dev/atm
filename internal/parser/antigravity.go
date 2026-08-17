package parser

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

// Antigravity keeps one SQLite database per conversation, and inside it a
// gen_metadata row per model call whose single column is a serialized protobuf.
// The field numbers below were established by decoding a real install; the .proto
// is not published, so they are read positionally through protowire.go.
//
// Only accounting is extracted. The conversation itself lives in the steps table
// as a different protobuf per step kind, which is a separate reverse-engineering
// job — see the antigravity entry in capabilities.go.
const (
	// antigravityGenRoot is the field holding the whole per-call record; every
	// path below is relative to it.
	antigravityGenRoot = 1
	// antigravityGenModelName is the model's display name, e.g.
	// "Gemini 3.1 Pro (High)".
	antigravityGenModelName = 21
	// antigravityGenTokens is the token submessage. Field 2 is the input that
	// missed cache and field 5 is the input that hit it — two disjoint counts,
	// not a total and a subset. Field 3 is total output, and equals field 9
	// (answer) plus field 10 (thinking) on every call observed; ATM stores the
	// total and lets the split go, having no column for reasoning tokens.
	antigravityGenTokens      = 4
	antigravityTokensInput    = 2
	antigravityTokensOutput   = 3
	antigravityTokensCacheHit = 5
	// antigravityTokensResponseID identifies the request. It is stable enough to
	// deduplicate: forking a conversation copies earlier calls into the new
	// database, and without this they would be counted twice.
	antigravityTokensResponseID = 11
	// antigravityGenTiming holds when the call ran (field 4, a Timestamp) and how
	// long generation took (field 8, a Duration).
	antigravityGenTiming    = 9
	antigravityTimingStart  = 4
	antigravityTimingLength = 8
	// antigravityGenError is set on a call that failed. Those rows carry no
	// tokens and no response id, so the zero-token skip below covers them; this
	// is named only to record that the field is accounted for.
	antigravityGenError = 5
)

// Summary index field numbers. The conversation databases hold neither a title
// nor a workspace, so both come from agyhub_summaries_proto.pb.
const (
	antigravitySummaryEntry     = 1
	antigravityEntryID          = 1
	antigravityEntryBody        = 2
	antigravityBodyTitle        = 1
	antigravityBodyUpdated      = 3
	antigravityBodyCreated      = 7
	antigravityBodyWorkspace    = 9
	antigravityWorkspaceFolder  = 1
	antigravityWorkspaceBranch  = 4
	antigravityTimestampSeconds = 1
)

// antigravitySummary is what the index knows about one conversation.
type antigravitySummary struct {
	Title      string
	CreatedTS  int64
	LastTS     int64
	FolderPath string
	Branch     string
}

func DiscoverAntigravity() []string {
	paths, err := filepath.Glob(filepath.Join(config.AntigravityConversations(), "*.db"))
	if err != nil {
		return nil
	}
	sort.Strings(paths)
	return paths
}

// AntigravitySourceVersion folds the write-ahead log into the change fingerprint
// sync compares.
//
// These databases declare WAL in their header, so a commit can land in the
// sibling -wal while the main file's mtime and size stay exactly as they were.
// The reader below does see that content — but sync decides whether to call the
// reader at all by comparing this fingerprint, so watching only the main file
// would skip a conversation whose newest calls have not been checkpointed yet,
// and keep skipping it.
//
// -shm is deliberately excluded even though it sits beside the other two. It is
// runtime scratch, holds no committed data, and its mtime advances every time any
// process attaches — including ATM's own read a moment ago. Including it made
// every sync look like a change and re-parsed all sixteen databases forever.
func AntigravitySourceVersion(path string) (mtime, size int64, ok bool) {
	for _, candidate := range []string{path, path + "-wal"} {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		ok = true
		if modified := info.ModTime().Unix(); modified > mtime {
			mtime = modified
		}
		size += info.Size()
	}
	return mtime, size, ok
}

// antigravitySummaries reads the whole index once. It is a single small file
// shared by every conversation, so parsing it per database would re-read it
// eighteen times a sync; callers that need it for one conversation still pay one
// read, which is the same cost as the targeted lookup would have been.
func antigravitySummaries() map[string]antigravitySummary {
	data, err := os.ReadFile(config.AntigravitySummaries())
	if err != nil {
		return nil
	}
	out := map[string]antigravitySummary{}
	for _, entry := range protoRepeated(data, antigravitySummaryEntry) {
		id, ok := protoString(entry, antigravityEntryID)
		if !ok || id == "" {
			continue
		}
		body, ok := protoSub(entry, antigravityEntryBody)
		if !ok {
			continue
		}
		summary := antigravitySummary{}
		summary.Title, _ = protoString(body, antigravityBodyTitle)
		summary.CreatedTS, _ = protoInt64(body, antigravityBodyCreated, antigravityTimestampSeconds)
		summary.LastTS, _ = protoInt64(body, antigravityBodyUpdated, antigravityTimestampSeconds)
		if folder, ok := protoString(body, antigravityBodyWorkspace, antigravityWorkspaceFolder); ok {
			summary.FolderPath = antigravityFolderPath(folder)
		}
		summary.Branch, _ = protoString(body, antigravityBodyWorkspace, antigravityWorkspaceBranch)
		out[id] = summary
	}
	return out
}

// antigravityFolderPath turns the index's file:// workspace URI into a path
// ProjectFromPath can resolve. A conversation opened outside any folder has no
// workspace at all, which stays empty rather than becoming "/".
func antigravityFolderPath(uri string) string {
	trimmed := strings.TrimSpace(uri)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "file://") {
		return trimmed
	}
	return strings.TrimPrefix(trimmed, "file://")
}

// antigravityModelSlug turns a display name into the hyphenated form the rest of
// ATM records models in: "Gemini 3.1 Pro (High)" becomes "gemini-3.1-pro".
//
// The trailing parenthetical is the reasoning effort, and it is dropped on
// purpose. Rates are per model, not per effort level, so keeping it would split
// one model's spend across three rows in `atm usage` and match no pricing entry
// that a "(High)" suffix was never going to match anyway.
func antigravityModelSlug(display string) string {
	name := display
	if open := strings.LastIndex(name, "("); open >= 0 {
		name = name[:open]
	}
	name = strings.ToLower(strings.TrimSpace(name))
	var builder strings.Builder
	lastWasDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.':
			builder.WriteRune(r)
			lastWasDash = false
		default:
			if !lastWasDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastWasDash = true
			}
		}
	}
	return strings.TrimRight(builder.String(), "-")
}

func openAntigravityDB(path string) (*sql.DB, bool) {
	if _, err := os.Stat(path); err != nil {
		return nil, false
	}
	db, err := openReadOnlySQLite(path)
	if err != nil {
		return nil, false
	}
	return db, true
}

// antigravityConversationID is the conversation uuid, which is the database's
// file name. The uuid is also inside the blobs, but the file name is what sync
// discovered and what identity has to agree with.
func antigravityConversationID(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".db")
}

func AntigravityParseFile(path string) *ParsedFile {
	conversationID := antigravityConversationID(path)
	if conversationID == "" || conversationID == "." {
		return nil
	}
	db, ok := openAntigravityDB(path)
	if !ok {
		return nil
	}
	defer db.Close()

	rows, err := db.Query(`SELECT data FROM gen_metadata ORDER BY idx`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []UsageEvent
	usage := Usage{}
	perModel := map[string]int{}
	var firstTS, lastTS int64
	for rows.Next() {
		var blob []byte
		if rows.Scan(&blob) != nil || len(blob) == 0 {
			continue
		}
		event, ok := antigravityUsageEvent(blob)
		if !ok {
			continue
		}
		events = append(events, event)
		usage.InputTokens += event.InputTokens
		usage.OutputTokens += event.OutputTokens
		usage.CacheReadTokens += event.CacheReadTokens
		usage.RequestCount++
		perModel[event.Model]++
		if event.TS > 0 && (firstTS == 0 || event.TS < firstTS) {
			firstTS = event.TS
		}
		if event.TS > lastTS {
			lastTS = event.TS
		}
	}
	// Same guard as the other read-only-database parsers: a conversation that
	// billed nothing is not worth a session row. Without message text there would
	// be nothing else in it.
	if len(events) == 0 {
		return nil
	}
	usage.Model = antigravityDominantModel(perModel)

	summary := antigravitySummaries()[conversationID]
	createdTS := firstNonZero(summary.CreatedTS, firstTS)
	if summary.LastTS > lastTS {
		lastTS = summary.LastTS
	}
	createdAt := ""
	if createdTS > 0 {
		createdAt = time.Unix(createdTS, 0).In(config.Loc).Format("01-02 15:04")
	}
	shortID := conversationID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return &ParsedFile{
		SessionID:   "antigravity:" + conversationID,
		ShortID:     shortID,
		Agent:       "antigravity",
		Project:     config.ProjectFromPath(summary.FolderPath),
		CreatedAt:   createdAt,
		CreatedTS:   createdTS,
		LastTS:      lastTS,
		Summary:     strings.TrimSpace(summary.Title),
		Usage:       usage,
		UsageEvents: events,
	}
}

// antigravityUsageEvent decodes one gen_metadata blob. It reports false for a
// call that billed nothing, which is how failed calls are excluded: an error row
// carries zero tokens and no response id, so counting it would add a request to
// the throughput statistics with no work behind it and no identity to
// deduplicate it by.
func antigravityUsageEvent(blob []byte) (UsageEvent, bool) {
	record, ok := protoSub(blob, antigravityGenRoot)
	if !ok {
		return UsageEvent{}, false
	}
	tokens, ok := protoSub(record, antigravityGenTokens)
	if !ok {
		return UsageEvent{}, false
	}
	input, _ := protoInt64(tokens, antigravityTokensInput)
	output, _ := protoInt64(tokens, antigravityTokensOutput)
	cacheRead, _ := protoInt64(tokens, antigravityTokensCacheHit)
	if input == 0 && output == 0 && cacheRead == 0 {
		return UsageEvent{}, false
	}
	display, _ := protoString(record, antigravityGenModelName)
	event := UsageEvent{
		// input and cacheRead are disjoint here — the upstream already excludes
		// cache hits from the input count — so neither is subtracted from the
		// other. This differs from endpoints whose prompt total includes hits.
		Model:           antigravityModelSlug(display),
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cacheRead,
		RequestCount:    1,
	}
	event.TS, _ = protoInt64(record, antigravityGenTiming, antigravityTimingStart, antigravityTimestampSeconds)
	event.DurationMS, _ = protoDurationMS(record, antigravityGenTiming, antigravityTimingLength)
	if responseID, ok := protoString(tokens, antigravityTokensResponseID); ok && responseID != "" {
		event.Fingerprint = "antigravity:" + responseID
	}
	return event, true
}

// antigravityDominantModel picks the model a session is reported under. The
// rolled-up usage row holds one model name for a conversation that may have
// switched models mid-way, and the one used most often describes it better than
// whichever happened to be last.
func antigravityDominantModel(counts map[string]int) string {
	best, bestCount := "", 0
	for model, count := range counts {
		if count > bestCount || (count == bestCount && model < best) {
			best, bestCount = model, count
		}
	}
	return best
}

// AntigravityLiveSessions reports conversations touched recently. It filters on
// file modification time before opening anything: this runs on the menu bar's
// refresh path, and opening every conversation database to find the one that
// changed would make a glance cost eighteen SQLite connections.
func AntigravityLiveSessions(maxAge time.Duration) []Session {
	paths := DiscoverAntigravity()
	if len(paths) == 0 {
		return nil
	}
	now := time.Now()
	cutoff := now.Add(-maxAge)
	type candidate struct {
		path     string
		modified time.Time
	}
	var recent []candidate
	for _, path := range paths {
		mtime, _, ok := AntigravitySourceVersion(path)
		if !ok || mtime == 0 {
			continue
		}
		modified := time.Unix(mtime, 0)
		if modified.Before(cutoff) {
			continue
		}
		recent = append(recent, candidate{path: path, modified: modified})
	}
	if len(recent) == 0 {
		return nil
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].modified.After(recent[j].modified) })

	summaries := antigravitySummaries()
	var sessions []Session
	for _, item := range recent {
		conversationID := antigravityConversationID(item.path)
		summary := summaries[conversationID]
		session := Session{
			Tool:       "antigravity",
			Client:     "Antigravity",
			SessionID:  conversationID,
			ResumeID:   conversationID,
			Project:    config.ProjectFromPath(summary.FolderPath),
			CWD:        summary.FolderPath,
			StartedAt:  item.modified,
			AgeSeconds: int(now.Sub(item.modified).Seconds()),
			Summary:    strings.TrimSpace(summary.Title),
			Model:      antigravityLastModel(item.path),
		}
		sessions = append(sessions, session)
	}
	return sessions
}

// antigravityLastModel reads only the newest accounting row, which is all a live
// session needs to say which model it is on.
func antigravityLastModel(path string) string {
	db, ok := openAntigravityDB(path)
	if !ok {
		return ""
	}
	defer db.Close()
	var blob []byte
	if db.QueryRow(`SELECT data FROM gen_metadata ORDER BY idx DESC LIMIT 1`).Scan(&blob) != nil {
		return ""
	}
	record, ok := protoSub(blob, antigravityGenRoot)
	if !ok {
		return ""
	}
	display, _ := protoString(record, antigravityGenModelName)
	return antigravityModelSlug(display)
}
