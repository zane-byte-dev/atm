// Package presence owns the local Agent hook receiver and notification routing.
// It stores only bounded, expiring presence overlays; transcripts and work state
// remain in their application services.
package presence

import (
	"errors"
	"time"
)

var (
	ErrAlreadyRunning    = errors.New("Agent hook receiver is already running")
	ErrSocketOwned       = errors.New("Agent hook socket is owned by another listener")
	ErrLeaseHeld         = errors.New("another companion owns notification delivery")
	ErrNotificationState = errors.New("notification state is unavailable")
	ErrClosed            = errors.New("Agent hook receiver is closed")
)

const (
	OwnerFile        = "presence-owner.json"
	NotificationFile = "notification.json"
	OwnerLease       = 30 * time.Second
	CompanionLease   = 30 * time.Second
	AttentionTTL     = 10 * time.Minute
	StateTTL         = 10 * time.Minute
	CompletionTTL    = time.Minute
	MaxSessions      = 512
	MaxNotifications = 256
	maxDedup         = 2048
)

// Options keeps OS and application edges injectable. Notify and OnChange must
// return promptly; neither runs while presence state is locked. Notify displays
// a notification, never executes a command supplied by an event or browser.
type Options struct {
	DataDir    string
	SocketPath string
	InstanceID string
	Notify     func(Notification)
	OnChange   func()
	Now        func() time.Time
}

type Owner struct {
	Version    int       `json:"v"`
	Owner      string    `json:"owner"`
	InstanceID string    `json:"instance_id"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Running    bool      `json:"running"`
}

type Attention struct {
	Reason     string    `json:"reason"`
	Tool       string    `json:"tool,omitempty"`
	Text       string    `json:"text,omitempty"`
	Source     string    `json:"source"`
	ReceivedAt time.Time `json:"received_at"`
}

// Session is the deliberately small projection accepted by Merge. ID may be the
// parser's shortened identifier; ResumeID lets full hook IDs join Codex rows.
// ResultKey is a digest or stable revision, not the entire assistant response.
// The first Merge is a baseline and never generates completion notifications.
type Session struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id,omitempty"`
	ResumeID   string     `json:"resume_id,omitempty"`
	Source     string     `json:"source"`
	Tool       string     `json:"tool,omitempty"`
	Project    string     `json:"project,omitempty"`
	CWD        string     `json:"cwd,omitempty"`
	State      string     `json:"state"`
	HookBacked bool       `json:"hook_backed"`
	Attention  *Attention `json:"attention,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ResultKey  string     `json:"-"`
}

type Snapshot struct {
	GeneratedAt    time.Time  `json:"generated_at"`
	Owner          string     `json:"owner"`
	InstanceID     string     `json:"instance_id"`
	Sessions       []Session  `json:"sessions"`
	ActiveCount    int        `json:"active_count"`
	AttentionCount int        `json:"attention_count"`
	LastEventAt    *time.Time `json:"last_event_at,omitempty"`
}

// ID is stable for a displayed object, while DedupKey identifies one transition.
// A withdraw reuses the post ID. Sequence is assigned solely by this runtime.
type Notification struct {
	Sequence uint64    `json:"sequence"`
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Action   string    `json:"action"`
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle,omitempty"`
	Body     string    `json:"body,omitempty"`
	ObjectID string    `json:"object_id,omitempty"`
	At       time.Time `json:"at"`
	DedupKey string    `json:"-"`
}

type Feed struct {
	Notifications []Notification `json:"notifications"`
	Cursor        uint64         `json:"cursor"`
	LeaseUntil    *time.Time     `json:"lease_until,omitempty"`
	Truncated     bool           `json:"truncated"`
}
