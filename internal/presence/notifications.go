package presence

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/agentevent"
)

const notificationType = "notification"

type notificationEnvelope struct {
	Version      int          `json:"v"`
	Type         string       `json:"type"`
	DedupKey     string       `json:"dedup_key"`
	Notification Notification `json:"notification"`
}

type cursorState struct {
	Version                     int               `json:"v"`
	Sequence                    uint64            `json:"sequence"`
	Seen                        map[string]uint64 `json:"seen"`
	SystemNotificationsDisabled bool              `json:"system_notifications_disabled,omitempty"`
}

func (r *Runtime) loadCursor() error {
	file, err := os.Open(filepath.Join(r.opts.DataDir, "runtime", NotificationFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	var state cursorState
	if err = json.NewDecoder(io.LimitReader(file, 512<<10)).Decode(&state); err != nil {
		return err
	}
	if state.Version != 1 || len(state.Seen) > maxDedup {
		return errors.New("invalid notification cursor")
	}
	for key, sequence := range state.Seen {
		if len(key) != 64 || sequence > state.Sequence {
			return errors.New("invalid notification deduplication cursor")
		}
	}
	r.sequence = state.Sequence
	if state.Seen != nil {
		r.seen = state.Seen
	}
	r.systemNotificationsDisabled = state.SystemNotificationsDisabled
	return nil
}

func (r *Runtime) saveCursorLocked() error {
	return writeJSON(filepath.Join(r.opts.DataDir, "runtime", NotificationFile), cursorState{
		Version:                     1,
		Sequence:                    r.sequence,
		Seen:                        r.seen,
		SystemNotificationsDisabled: r.systemNotificationsDisabled,
	})
}

// Publish accepts one live business transition. The deduplication receipt is
// flushed before any display callback, so a CLI retry or restart cannot replay
// a previously accepted completion. It intentionally provides at-most-once
// display after an ambiguous crash; business acceptance remains in its database.
func (r *Runtime) Publish(notification Notification) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false, ErrClosed
	}
	return r.publishLocked(notification)
}

func (r *Runtime) publishLocked(notification Notification) (bool, error) {
	if notification.ID == "" || notification.DedupKey == "" || len(notification.ID) > 256 || len(notification.DedupKey) > 4096 {
		return false, errors.New("notification requires a bounded ID and transition key")
	}
	if notification.Action != "post" && notification.Action != "withdraw" {
		return false, errors.New("unsupported notification action")
	}
	key := digest(notification.DedupKey)
	if _, exists := r.seen[key]; exists {
		return false, nil
	}
	notification.Title, notification.Subtitle, notification.Body = clipped(notification.Title, 160), clipped(notification.Subtitle, 160), clipped(notification.Body, 400)
	notification.ObjectID, notification.Kind = clipped(notification.ObjectID, 512), clipped(notification.Kind, 80)
	notification.At = r.opts.Now().UTC()
	r.sequence++
	notification.Sequence = r.sequence
	r.seen[key] = r.sequence
	var evicted string
	var evictedSequence uint64
	if len(r.seen) > maxDedup {
		for candidate, sequence := range r.seen {
			if evicted == "" || sequence < evictedSequence {
				evicted, evictedSequence = candidate, sequence
			}
		}
		delete(r.seen, evicted)
	}
	if err := r.saveCursorLocked(); err != nil {
		delete(r.seen, key)
		if evicted != "" {
			r.seen[evicted] = evictedSequence
		}
		r.sequence--
		return false, err
	}
	if len(r.entries) >= MaxNotifications {
		if !r.entries[0].delivered {
			r.fallbackLocked(&r.entries[0])
		}
		copy(r.entries, r.entries[1:])
		r.entries = r.entries[:len(r.entries)-1]
	}
	entry := notificationEntry{notification: notification}
	if r.systemNotificationsDisabled {
		r.suppressLocked(&entry)
	} else if r.companionID == "" || !notification.At.Before(r.companionUntil) {
		r.fallbackLocked(&entry)
	}
	r.entries = append(r.entries, entry)
	r.signalChange()
	return true, nil
}

// suppressLocked resolves the system-display side of an entry without hiding
// it from Notifications. Re-enabling notifications therefore starts at the
// companion's current cursor instead of replaying banners created while the
// user had native notifications turned off.
func (r *Runtime) suppressLocked(entry *notificationEntry) {
	entry.delivered = true
}

func (r *Runtime) fallbackLocked(entry *notificationEntry) {
	// Mark receipt before enqueuing. A companion connecting during a slow native
	// fallback must not also receive that same banner. The bounded queue can
	// shed excess system banners; the current state remains visible in Web.
	entry.delivered = true
	// Once handed to a native client, a lost acknowledgement is ambiguous.
	// Changing channels then could display it twice. Keep the Web state, but
	// only fall back for events no companion has ever been offered.
	if !entry.offered && r.opts.Notify != nil {
		select {
		case r.deliver <- entry.notification:
		default:
		}
	}
}

func (r *Runtime) feedLocked(after uint64, pendingOnly bool) Feed {
	feed := Feed{Notifications: make([]Notification, 0), Cursor: r.sequence}
	if after > r.sequence {
		feed.Truncated = true
		after = 0
	}
	if len(r.entries) == 0 {
		feed.Truncated = after < r.sequence
	} else if after < r.entries[0].notification.Sequence-1 {
		feed.Truncated = true
	}
	for _, entry := range r.entries {
		if entry.notification.Sequence > after && (!pendingOnly || !entry.delivered) {
			feed.Notifications = append(feed.Notifications, entry.notification)
		}
	}
	return feed
}

// Notifications is a read-only feed for Web. It never claims the system display
// channel; multiple browser tabs can safely read the same station notifications.
func (r *Runtime) Notifications(after uint64) Feed {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.feedLocked(after, false)
}

// ClaimCompanion renews a short display lease. The first claim gets no historical
// fallback banners, and an acknowledged native banner is never replayed. The
// authenticated HTTP adapter must call this only after notification permission
// was granted; a read-only observer should use Notifications instead.
func (r *Runtime) ClaimCompanion(clientID string, after uint64) (Feed, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || len(clientID) > 128 {
		return Feed{}, errors.New("invalid companion client ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Feed{}, ErrClosed
	}
	now := r.opts.Now().UTC()
	if r.companionID != "" && r.companionID != clientID && now.Before(r.companionUntil) {
		return Feed{}, ErrLeaseHeld
	}
	if r.systemNotificationsDisabled {
		r.systemNotificationsDisabled = false
		if err := r.saveCursorLocked(); err != nil {
			r.systemNotificationsDisabled = true
			return Feed{}, errors.Join(ErrNotificationState, err)
		}
	}
	r.companionID, r.companionUntil = clientID, now.Add(CompanionLease)
	feed := r.feedLocked(after, true)
	offeredAfter := after
	if offeredAfter > r.sequence {
		offeredAfter = 0
	}
	for index := range r.entries {
		entry := &r.entries[index]
		if entry.notification.Sequence > offeredAfter && !entry.delivered {
			entry.offered = true
		}
	}
	until := r.companionUntil
	feed.LeaseUntil = &until
	return feed, nil
}

// DisableSystemNotifications records the Menu user's global display choice,
// releases any native display lease without invoking the Go fallback, and
// returns the ordinary notification feed for in-app use. The choice is stored
// beside the notification cursor so a service restart cannot briefly resume
// osascript banners before Menu reconnects.
func (r *Runtime) DisableSystemNotifications(clientID string, after uint64) (Feed, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || len(clientID) > 128 {
		return Feed{}, errors.New("invalid companion client ID")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Feed{}, ErrClosed
	}
	if !r.systemNotificationsDisabled {
		r.systemNotificationsDisabled = true
		if err := r.saveCursorLocked(); err != nil {
			r.systemNotificationsDisabled = false
			return Feed{}, errors.Join(ErrNotificationState, err)
		}
	}
	if r.sequence > r.suppressedFallbackThrough {
		r.suppressedFallbackThrough = r.sequence
	}
	r.companionID = ""
	r.companionUntil = time.Time{}
	for index := range r.entries {
		if !r.entries[index].delivered {
			r.suppressLocked(&r.entries[index])
		}
	}
	feed := r.feedLocked(after, false)
	return feed, nil
}

// SystemNotificationsEnabled lets the executable discard a banner already
// queued at the application boundary when the user disables notifications.
func (r *Runtime) SystemNotificationsEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && !r.systemNotificationsDisabled
}

// ShouldDisplayFallback is the final executable-side gate for an asynchronous
// banner. The sequence check prevents a banner queued before disable from
// leaking out if the user re-enables notifications before the worker reaches it.
func (r *Runtime) ShouldDisplayFallback(notification Notification) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && !r.systemNotificationsDisabled && notification.Sequence > r.suppressedFallbackThrough
}

func (r *Runtime) AckCompanion(clientID string, sequence uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	if clientID != r.companionID || clientID == "" {
		return ErrLeaseHeld
	}
	if sequence > r.sequence {
		return errors.New("notification acknowledgement is ahead of the cursor")
	}
	for index := range r.entries {
		if r.entries[index].notification.Sequence <= sequence {
			r.entries[index].delivered = true
		}
	}
	return nil
}

func (r *Runtime) ReleaseCompanion(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if clientID != r.companionID || clientID == "" {
		return ErrLeaseHeld
	}
	r.companionID = ""
	r.companionUntil = time.Time{}
	for index := range r.entries {
		if !r.entries[index].delivered {
			r.fallbackLocked(&r.entries[index])
		}
	}
	return nil
}

func (r *Runtime) applyGuard(guard agentevent.GuardEnvelope) error {
	if guard.Version != 1 || guard.Type != agentevent.TypeGuardRequest || guard.ID == "" || len(guard.ID) > 128 || guard.ExpiresAt <= r.opts.Now().Unix() {
		return errors.New("invalid or expired guard request")
	}
	_, err := r.Publish(Notification{ID: "guard-" + guard.ID, Kind: "guard", Action: "post", Title: "ATM · 等待批准", Subtitle: guard.Label, Body: guard.Body, ObjectID: guard.ID, DedupKey: "guard:" + guard.ID})
	return err
}

var todoIDPattern = regexp.MustCompile(`^t[1-9][0-9]*$`)

func validForwardedNotification(notification Notification) bool {
	if !todoIDPattern.MatchString(notification.ObjectID) || notification.Action != "post" || notification.ID != "todo-"+notification.ObjectID {
		return false
	}
	switch notification.Kind {
	case "todo_created", "todo_review", "todo_done", "todo_archived":
	default:
		return false
	}
	return notification.DedupKey != "" && len(notification.DedupKey) <= 4096
}

// Forward is a typed, acknowledged courtesy channel for CLI lifecycle banners.
// owned remains true on any ambiguous delivery failure while the permanent Go
// owner marker exists. The caller then suppresses its own fallback: the server
// may have accepted the notification even if its acknowledgement was lost.
func Forward(dataDir, socketPath string, notification Notification) (owned bool, err error) {
	_, ownerErr := ReadOwner(dataDir)
	if errors.Is(ownerErr, os.ErrNotExist) {
		return false, nil
	}
	if ownerErr != nil {
		return true, ownerErr
	}
	if !validForwardedNotification(notification) {
		return true, errors.New("unsupported forwarded notification")
	}
	if socketPath == "" {
		socketPath = os.Getenv(agentevent.SocketEnvVar)
	}
	if socketPath == "" {
		socketPath = filepath.Join(dataDir, "notch.sock")
	}
	if err = agentevent.CheckSocketPath(socketPath); err != nil {
		return true, err
	}
	deadline := time.Now().Add(agentevent.DeliverTimeout)
	conn, err := net.DialTimeout("unix", socketPath, agentevent.DeliverTimeout)
	if err != nil {
		return true, err
	}
	defer conn.Close()
	if err = conn.SetDeadline(deadline); err != nil {
		return true, err
	}
	envelope := notificationEnvelope{Version: agentevent.Version, Type: notificationType, Notification: notification, DedupKey: notification.DedupKey}
	if err = json.NewEncoder(conn).Encode(envelope); err != nil {
		return true, err
	}
	var reply struct {
		OK bool `json:"ok"`
	}
	if err = json.NewDecoder(io.LimitReader(conn, 1024)).Decode(&reply); err != nil {
		return true, err
	}
	if !reply.OK {
		return true, errors.New("runtime did not accept notification")
	}
	return true, nil
}

// Sorted notification IDs are useful to reconcile stable native banners against
// current attention after a stream reconnect. This does not replay completion.
func (r *Runtime) AttentionNotificationIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.opts.Now()
	ids := make([]string, 0)
	for key, state := range r.hooks {
		if state.state == "attention" && now.Before(state.expiresAt) {
			ids = append(ids, "agent-"+digest(key)[:24])
		}
	}
	sort.Strings(ids)
	return ids
}
