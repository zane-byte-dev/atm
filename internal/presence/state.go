package presence

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
)

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// ResultKey converts potentially sensitive result text into a stable change
// token before it enters the presence projection.
func ResultKey(value string) string { return digest(value) }

func clipped(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func SourceForTool(tool string) string {
	key := strings.ToLower(strings.TrimSpace(tool))
	switch {
	case strings.HasPrefix(key, "claude"):
		return "claude"
	case strings.HasPrefix(key, "codex"):
		return "codex"
	case strings.HasPrefix(key, "grok"):
		return "grokbuild"
	case key == "qoder" || key == "qoder cli":
		return "qoder"
	default:
		return key
	}
}

func eventKey(event agentevent.Envelope) string {
	if event.SessionID != "" {
		return event.Source + "\x00id\x00" + event.SessionID
	}
	return event.Source + "\x00cwd\x00" + event.CWD
}

func sessionIDKeys(session Session) []string {
	var keys []string
	for _, id := range []string{session.ResumeID, session.SessionID, session.ID} {
		if id == "" {
			continue
		}
		key := session.Source + "\x00id\x00" + id
		if !containsKey(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func sessionCWDKey(session Session) string {
	if session.CWD != "" {
		return session.Source + "\x00cwd\x00" + session.CWD
	}
	return ""
}

func sessionKeys(session Session) []string {
	keys := sessionIDKeys(session)
	if key := sessionCWDKey(session); key != "" {
		keys = append(keys, key)
	}
	return keys
}

func containsKey(keys []string, wanted string) bool {
	for _, key := range keys {
		if key == wanted {
			return true
		}
	}
	return false
}

func sessionKey(session Session) string {
	keys := sessionKeys(session)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func sessionCWDCounts(sessions []Session) map[string]int {
	counts := make(map[string]int)
	for _, session := range sessions {
		if key := sessionCWDKey(session); key != "" {
			counts[key]++
		}
	}
	return counts
}

// Apply accepts the existing newline socket envelope. Session identity always
// includes its source. Directory fallback is used only by events with no ID,
// and can never mark another agent in the same repository as waiting.
func (r *Runtime) Apply(event agentevent.Envelope) error {
	event.Source = strings.ToLower(strings.TrimSpace(event.Source))
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.CWD = strings.TrimSpace(event.CWD)
	if err := event.Validate(); err != nil {
		return err
	}
	if len(event.Source) > 80 || len(event.SessionID) > 512 || len(event.CWD) > 4096 {
		return errors.New("Agent event identity is too large")
	}
	event.Text, event.Tool, event.Reason = clipped(event.Text, 400), clipped(event.Tool, 120), clipped(event.Reason, 100)
	now := r.opts.Now().UTC()
	eventAt := now
	if event.At != "" {
		parsed, err := time.Parse(time.RFC3339Nano, event.At)
		if err != nil {
			return errors.New("invalid Agent event timestamp")
		}
		eventAt = parsed.UTC()
	}
	if eventAt.After(now.Add(time.Minute)) || eventAt.Before(now.Add(-StateTTL)) {
		return errors.New("Agent event timestamp is stale or in the future")
	}
	key := eventKey(event)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	previous, exists := r.hooks[key]
	if exists && event.Event == agentevent.KindSessionStart {
		// Discovery carries no turn state. Even if it was delayed behind a tool
		// event, it still proves that lifecycle hooks report this session. Keep
		// the actionable event and its ordering timestamp intact.
		lastEvent := now
		r.lastEventAt = &lastEvent
		if !previous.turnReporting {
			previous.turnReporting = true
			r.hooks[key] = previous
		}
		// Session discovery can make the parser projection change even when an
		// earlier hook already established authority, so preserve its refresh.
		r.signalChange()
		return nil
	}
	if exists && eventAt.Before(previous.eventAt) {
		return nil
	}
	if exists && eventAt.Equal(previous.eventAt) && event.Event == previous.event.Event && event.Text == previous.event.Text && event.Reason == previous.event.Reason && event.Tool == previous.event.Tool {
		return nil
	}
	lastEvent := now
	r.lastEventAt = &lastEvent
	if !exists && len(r.hooks) >= MaxSessions {
		r.evictOldestHookLocked(now)
	}
	state := hookState{event: event, eventAt: eventAt, receivedAt: now, expiresAt: now.Add(StateTTL), state: "unknown", turnReporting: previous.turnReporting || event.Event != agentevent.KindResumed}
	var err error
	if event.Event.ClearsAttention() {
		// A session-ID clearing event may also retire a prior directory-only
		// signal from the same source; it must not clear a different source.
		cwdKey := event.Source + "\x00cwd\x00" + event.CWD
		if event.SessionID != "" && event.CWD != "" && r.canClearCWDWithIDLocked(event) {
			if fallback, ok := r.hooks[cwdKey]; ok && !eventAt.Before(fallback.eventAt) {
				if fallback.state == "attention" {
					err = r.withdrawLocked(cwdKey, fallback, now, "resumed")
				}
				delete(r.hooks, cwdKey)
			}
		}
		if previous.state == "attention" {
			err = r.withdrawLocked(key, previous, now, string(event.Event))
		}
	}
	switch event.Event {
	case agentevent.KindAttention:
		state.state = "attention"
		state.expiresAt = now.Add(AttentionTTL)
		if previous.state != "attention" {
			note := hookNotification(key, state, "attention", "post", now)
			note.DedupKey = key + ":attention:" + eventAt.Format(time.RFC3339Nano)
			_, err = r.publishLocked(note)
		}
	case agentevent.KindStarted, agentevent.KindResumed:
		state.state = "busy"
	case agentevent.KindCompleted:
		state.state = "completed"
		state.expiresAt = now.Add(CompletionTTL)
		// A freshly attached receiver must not replay a completed turn from
		// before its own baseline. A turn observed here can complete once.
		if exists && (previous.state == "busy" || previous.state == "attention") && !eventAt.Before(r.owner.StartedAt) {
			note := hookNotification(key, state, "completed", "post", now)
			note.DedupKey = key + ":completed:" + eventAt.Format(time.RFC3339Nano)
			_, err = r.publishLocked(note)
		}
	case agentevent.KindSessionEnd:
		state.state = "ended"
		state.expiresAt = now.Add(CompletionTTL)
	case agentevent.KindSessionStart:
	}
	r.hooks[key] = state
	r.signalChange()
	return err
}

// canClearCWDWithIDLocked prevents one of several sessions in a repository
// from retiring a directory-only signal that cannot be attributed to it. With
// no parsed candidate, or one candidate whose known IDs match, preserving the
// legacy ID-upgrade behavior is safe.
func (r *Runtime) canClearCWDWithIDLocked(event agentevent.Envelope) bool {
	cwdKey := event.Source + "\x00cwd\x00" + event.CWD
	idKey := event.Source + "\x00id\x00" + event.SessionID
	matches := 0
	idMatches := false
	for _, session := range r.base {
		if sessionCWDKey(session) != cwdKey {
			continue
		}
		matches++
		if containsKey(sessionIDKeys(session), idKey) {
			idMatches = true
		}
	}
	return matches == 0 || matches == 1 && idMatches
}

func hookNotification(key string, state hookState, kind, action string, now time.Time) Notification {
	objectID := state.event.SessionID
	if objectID == "" {
		objectID = "cwd-" + digest(key)[:16]
	}
	subtitle := "Agent 已完成"
	if kind == "attention" {
		subtitle = attentionReason(state.event.Reason)
	}
	return Notification{ID: "agent-" + digest(key)[:24], Kind: kind, Action: action, Title: "ATM · " + state.event.Source, Subtitle: subtitle, Body: state.event.Text, ObjectID: objectID, At: now}
}

func attentionReason(reason string) string {
	switch reason {
	case "permission_prompt", "permission_request":
		return "等待授权"
	case "idle_prompt":
		return "等待输入"
	case "agent_needs_input":
		return "需要补充信息"
	case "elicitation_dialog":
		return "等待填写"
	case "ask_user_question":
		return "等待选择"
	default:
		return "Agent 需要你"
	}
}

func (r *Runtime) withdrawLocked(key string, state hookState, now time.Time, reason string) error {
	note := hookNotification(key, state, "attention", "withdraw", now)
	note.DedupKey = key + ":withdraw:" + state.eventAt.Format(time.RFC3339Nano) + ":" + reason
	_, err := r.publishLocked(note)
	return err
}

func (r *Runtime) evictOldestHookLocked(now time.Time) {
	var oldest string
	for key, value := range r.hooks {
		if oldest == "" || value.receivedAt.Before(r.hooks[oldest].receivedAt) || value.receivedAt.Equal(r.hooks[oldest].receivedAt) && key < oldest {
			oldest = key
		}
	}
	if value, ok := r.hooks[oldest]; ok && value.state == "attention" {
		_ = r.withdrawLocked(oldest, value, now, "expired")
	}
	delete(r.hooks, oldest)
}

// Merge replaces the bounded parser projection. Missing sessions disappear;
// fresh hooks still survive until their own expiry. Returning sessions establish
// a fresh baseline, so reopening a transcript never replays its old completion.
func (r *Runtime) Merge(sessions []Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := r.opts.Now().UTC()
	previous := make(map[string]Session, len(r.base))
	for _, session := range r.base {
		previous[sessionKey(session)] = session
	}
	next := make([]Session, 0, min(len(sessions), MaxSessions))
	seen := make(map[string]bool)
	for _, session := range sessions {
		if len(next) == MaxSessions {
			break
		}
		session.Source = SourceForTool(session.Source)
		if session.Source == "" {
			session.Source = SourceForTool(session.Tool)
		}
		session.ID, session.SessionID, session.ResumeID = clipped(session.ID, 512), clipped(session.SessionID, 512), clipped(session.ResumeID, 512)
		session.CWD, session.Tool, session.Project = clipped(session.CWD, 4096), clipped(session.Tool, 120), clipped(session.Project, 160)
		session.Attention = nil
		session.UpdatedAt = now
		if session.ID == "" {
			session.ID = session.SessionID
		}
		key := sessionKey(session)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		next = append(next, session)
	}
	cwdCounts := sessionCWDCounts(next)
	for _, session := range next {
		key := sessionKey(session)
		old, existed := previous[key]
		_, hook, hasHook := r.hookForLocked(session, cwdCounts, now)
		hookBacked := hasHook && hook.turnReporting
		if r.primed && existed && !hookBacked && session.ResultKey != "" && old.ResultKey != session.ResultKey {
			note := Notification{ID: "agent-" + digest(key)[:24], Kind: "completed", Action: "post", Title: "ATM · " + session.Tool, Subtitle: "Agent 已完成", ObjectID: session.ID, At: now, DedupKey: key + ":result:" + session.ResultKey}
			_, _ = r.publishLocked(note)
		}
	}
	r.base, r.primed = next, true
	r.signalChange()
}

func (r *Runtime) hookForLocked(session Session, cwdCounts map[string]int, now time.Time) (string, hookState, bool) {
	for _, key := range sessionIDKeys(session) {
		if state, ok := r.hooks[key]; ok && now.Before(state.expiresAt) {
			return key, state, true
		}
	}
	if key := sessionCWDKey(session); key != "" && cwdCounts[key] == 1 {
		if state, ok := r.hooks[key]; ok && now.Before(state.expiresAt) {
			return key, state, true
		}
	}
	return "", hookState{}, false
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.opts.Now().UTC()
	result := Snapshot{GeneratedAt: now, Owner: "go", InstanceID: r.opts.InstanceID, Sessions: make([]Session, 0, MaxSessions)}
	if r.lastEventAt != nil {
		last := *r.lastEventAt
		result.LastEventAt = &last
	}
	joined := make(map[string]bool)
	cwdCounts := sessionCWDCounts(r.base)
	for _, base := range r.base {
		session := base
		if key, state, ok := r.hookForLocked(session, cwdCounts, now); ok {
			joined[key] = true
			session = applyOverlay(session, state)
		}
		result.Sessions = append(result.Sessions, session)
	}
	// Hooks can reach us before the transcript index. Keep their minimal row so
	// attention is not lost, without scanning or inventing transcript content.
	keys := make([]string, 0, len(r.hooks))
	for key := range r.hooks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		state := r.hooks[key]
		if len(result.Sessions) >= MaxSessions {
			break
		}
		if joined[key] || !now.Before(state.expiresAt) || state.state == "ended" {
			continue
		}
		id := state.event.SessionID
		if id == "" {
			id = "cwd-" + digest(key)[:16]
		}
		session := Session{ID: id, SessionID: state.event.SessionID, Source: state.event.Source, Tool: state.event.Source, CWD: state.event.CWD}
		result.Sessions = append(result.Sessions, applyOverlay(session, state))
	}
	for _, session := range result.Sessions {
		if session.State == "busy" || session.State == "active" {
			result.ActiveCount++
		}
		if session.Attention != nil {
			result.AttentionCount++
		}
	}
	return result
}

func applyOverlay(session Session, state hookState) Session {
	session.State, session.HookBacked, session.UpdatedAt = state.state, state.turnReporting, state.receivedAt
	if state.state == "attention" {
		session.Attention = &Attention{Reason: state.event.Reason, Tool: state.event.Tool, Text: state.event.Text, Source: state.event.Source, ReceivedAt: state.receivedAt}
	}
	return session
}

func (r *Runtime) expire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := r.opts.Now().UTC()
	changed := false
	for key, state := range r.hooks {
		if !now.Before(state.expiresAt) {
			if state.state == "attention" {
				_ = r.withdrawLocked(key, state, now, "expired")
			}
			delete(r.hooks, key)
			changed = true
		}
	}
	if r.companionID != "" && !now.Before(r.companionUntil) {
		r.companionID = ""
		for index := range r.entries {
			entry := &r.entries[index]
			if !entry.delivered {
				if r.systemNotificationsDisabled {
					r.suppressLocked(entry)
				} else {
					r.fallbackLocked(entry)
				}
				changed = true
			}
		}
	}
	if changed {
		r.signalChange()
	}
}
