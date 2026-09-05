package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/presence"
)

// AttachRuntime is safe during composition and shutdown. Requests take a pointer
// snapshot; lifecycle ownership and Close remain with the executable.
func (h *Host) AttachRuntime(jobs *background.Manager, live *presence.Runtime) {
	h.runtimeMu.Lock()
	h.jobs, h.presence = jobs, live
	h.runtimeMu.Unlock()
}

func (h *Host) attachedRuntime() (*background.Manager, *presence.Runtime) {
	h.runtimeMu.RLock()
	defer h.runtimeMu.RUnlock()
	return h.jobs, h.presence
}

func (h *Host) SetPresenceLoader(loader func(context.Context, string) (dashboard.LiveStatus, error)) {
	h.runtimeMu.Lock()
	h.presenceLoader = loader
	h.runtimeMu.Unlock()
}

// WithConfig pins the configuration used by an actual background execution,
// rather than only its enqueueing. Settings writers use TryLock so a slow job
// cannot put an exclusive waiter in front of unrelated Web reads.
func (h *Host) WithConfig(ctx context.Context, run func(context.Context) error) error {
	if ctx == nil || run == nil {
		return invalid("context and config operation are required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return run(ctx)
}

func (h *Host) RuntimeCapabilities() map[string]bool {
	jobs, live := h.attachedRuntime()
	return map[string]bool{"runtime_jobs": jobs != nil, "background_sync": jobs != nil, "collection_run": jobs != nil, "models": jobs != nil, "agent_hooks": live != nil, "quota_refresh": jobs != nil, "day_rebuild": jobs != nil, "notifications": live != nil}
}

func (h *Host) RefreshPresence(ctx context.Context) error {
	h.runtimeMu.RLock()
	live, loader := h.presence, h.presenceLoader
	h.runtimeMu.RUnlock()
	if live == nil || loader == nil {
		return nil
	}
	return h.WithConfig(ctx, func(ctx context.Context) error {
		status, err := loader(ctx, "")
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		sessions := make([]presence.Session, 0, min(len(status.Sessions), presence.MaxSessions))
		for _, row := range status.Sessions {
			if len(sessions) == presence.MaxSessions {
				break
			}
			var resultKey string
			if value := strings.TrimSpace(row.LatestResult); value != "" {
				resultKey = presence.ResultKey(value)
			}
			sessions = append(sessions, presence.Session{
				ID: row.SessionID, SessionID: row.SessionID, ResumeID: row.ResumeID,
				Source: presence.SourceForTool(row.Tool), Tool: row.Tool, Project: row.Project,
				CWD: row.CWD, State: row.ActivityState, ResultKey: resultKey,
			})
		}
		live.Merge(sessions)
		return nil
	})
}

// WebJobInput intentionally excludes DueOnly. It belongs to the trusted
// scheduler, and accepting it would silently alter a user's explicit run.
type WebJobInput struct {
	Kind         background.Kind `json:"kind"`
	Agent        string          `json:"agent,omitempty"`
	SourceID     string          `json:"source_id,omitempty"`
	ItemID       string          `json:"item_id,omitempty"`
	Day          string          `json:"day,omitempty"`
	From         string          `json:"from,omitempty"`
	To           string          `json:"to,omitempty"`
	TodoID       string          `json:"todo_id,omitempty"`
	ExpectedETag string          `json:"expected_etag,omitempty"`
	Hint         string          `json:"hint,omitempty"`
}

type RuntimeJobID struct {
	JobID string `json:"job_id"`
}
type RuntimeJobList struct {
	Jobs []background.Job `json:"jobs"`
}

func (h *Host) CallRuntime(ctx context.Context, call application.Call, method string, raw json.RawMessage, key string) (any, error) {
	if err := validate(ctx, call); err != nil {
		return nil, err
	}
	jobs, live := h.attachedRuntime()
	switch method {
	case "presence.snapshot":
		return invoke(raw, func(struct{}) (any, error) {
			if live == nil {
				return nil, runtimeUnavailable()
			}
			return live.Snapshot(), nil
		})
	case "jobs.run":
		return invoke(raw, func(input WebJobInput) (any, error) {
			if err := validateWrite(ctx, call); err != nil {
				return nil, err
			}
			if jobs == nil {
				return nil, runtimeUnavailable()
			}
			request := background.Request{Kind: input.Kind, Agent: input.Agent, SourceID: input.SourceID, ItemID: input.ItemID, Day: input.Day, From: input.From, To: input.To, TodoID: input.TodoID, ExpectedETag: input.ExpectedETag, Hint: input.Hint}
			// Manager.Run checks configured agents/dates while accepting work.
			// Keep that short read on the same config generation as the check;
			// the worker independently pins actual execution with WithConfig.
			var job background.Job
			err := h.WithConfig(ctx, func(ctx context.Context) error {
				var err error
				job, err = jobs.Run(ctx, call, request, key)
				return err
			})
			return job, err
		})
	case "jobs.list":
		return invoke(raw, func(input struct {
			Limit int `json:"limit,omitempty"`
		}) (any, error) {
			if input.Limit == 0 {
				input.Limit = 30
			}
			if input.Limit < 1 || input.Limit > 100 {
				return nil, invalid("limit must be between 1 and 100")
			}
			if jobs == nil {
				return nil, runtimeUnavailable()
			}
			listed, err := jobs.List(ctx, input.Limit)
			return RuntimeJobList{Jobs: listed}, err
		})
	case "jobs.show", "jobs.cancel":
		return invoke(raw, func(input RuntimeJobID) (any, error) {
			if !validRuntimeJobID(input.JobID) {
				return nil, invalid("a valid job_id is required")
			}
			if method == "jobs.cancel" {
				if err := validateWrite(ctx, call); err != nil {
					return nil, err
				}
			}
			if jobs == nil {
				return nil, runtimeUnavailable()
			}
			if method == "jobs.cancel" {
				return jobs.Cancel(ctx, input.JobID)
			}
			return jobs.Get(ctx, input.JobID)
		})
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown runtime API method")
	}
}

func validRuntimeJobID(id string) bool {
	if len(id) < 5 || len(id) > 100 || !strings.HasPrefix(id, "job-") {
		return false
	}
	for _, value := range id[4:] {
		if !(value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '-') {
			return false
		}
	}
	return true
}

func runtimeUnavailable() error {
	return application.NewError(application.CodeUnavailable, "Go background runtime is not enabled for this workspace")
}

type CompanionResult struct {
	Snapshot                 presence.Snapshot   `json:"snapshot"`
	Feed                     presence.Feed       `json:"feed"`
	AttentionNotificationIDs []string            `json:"attention_notification_ids"`
	Todos                    CompanionTodos      `json:"todos"`
	Quota                    CompanionQuota      `json:"quota"`
	TodayUsage               CompanionTodayUsage `json:"today_usage"`
}

// Companion is called only by the local control-token adapter, never a browser
// RPC. Merely reading status without notification permission does not claim the
// display channel or replay any historical system banners.
func (h *Host) Companion(ctx context.Context, raw json.RawMessage, ack bool) (any, error) {
	if ctx == nil {
		return nil, invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, live := h.attachedRuntime()
	if ack {
		return invoke(raw, func(input struct {
			ClientID string `json:"client_id"`
			Sequence uint64 `json:"sequence"`
		}) (any, error) {
			if !validCompanionID(input.ClientID) {
				return nil, invalid("a bounded client_id is required")
			}
			if live == nil {
				return nil, runtimeUnavailable()
			}
			if err := live.AckCompanion(input.ClientID, input.Sequence); err != nil {
				return nil, companionError(err)
			}
			return struct {
				Acknowledged bool `json:"acknowledged"`
			}{true}, nil
		})
	}
	return invoke(raw, func(input struct {
		ClientID             string `json:"client_id"`
		After                uint64 `json:"after"`
		NotificationsEnabled *bool  `json:"notifications_enabled"`
	}) (any, error) {
		if !validCompanionID(input.ClientID) || input.NotificationsEnabled == nil {
			return nil, invalid("client_id and notifications_enabled are required")
		}
		if live == nil {
			return nil, runtimeUnavailable()
		}
		var feed presence.Feed
		if !*input.NotificationsEnabled {
			// Persist the global display preference before any optional index read.
			// Notification records remain in this feed and in Web, while neither a
			// released lease nor a service restart may fall back to osascript.
			var err error
			feed, err = live.DisableSystemNotifications(input.ClientID, input.After)
			if err != nil {
				return nil, companionError(err)
			}
		} else {
			var err error
			feed, err = live.ClaimCompanion(input.ClientID, input.After)
			if err != nil {
				return nil, companionError(err)
			}
		}
		// Claim or renew the short notification lease before optional SQLite
		// projections. A briefly busy index must not hand the display channel back
		// to the server and create duplicate banners.
		snapshot := compactCompanionSnapshot(live.Snapshot())
		todos, quota, err := h.companionSummaries(ctx)
		if err != nil {
			return nil, err
		}
		todayUsage := h.companionTodayUsage(ctx, time.Now())
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return CompanionResult{Snapshot: snapshot, Feed: feed, AttentionNotificationIDs: live.AttentionNotificationIDs(), Todos: todos, Quota: quota, TodayUsage: todayUsage}, nil
	})
}

func validCompanionID(id string) bool { return strings.TrimSpace(id) != "" && len(id) <= 128 }
func companionError(err error) error {
	if errors.Is(err, presence.ErrLeaseHeld) {
		return application.WrapError(application.CodeBusy, "another companion owns notifications", err)
	}
	if errors.Is(err, presence.ErrClosed) {
		return runtimeUnavailable()
	}
	if errors.Is(err, presence.ErrNotificationState) {
		return application.WrapError(application.CodeUnavailable, "notification preference could not be saved", err)
	}
	return application.WrapError(application.CodeInvalidArgument, "invalid notification acknowledgement", err)
}

func configBusy() error {
	return application.NewError(application.CodeBusy, "background work is using the configuration; retry after it finishes")
}

func (h *Host) workspaceRuntime() WorkspaceRuntime {
	jobs, live := h.attachedRuntime()
	return WorkspaceRuntime{Mode: "go", Version: h.Version, BackgroundSync: jobs != nil, Collection: jobs != nil, Models: jobs != nil, AgentHooks: live != nil}
}

// RefreshConfig notices external CLI edits without waiting behind a running
// job. A busy tick is skipped, and repeated identical errors stay quiet until
// the file changes or becomes readable again. Failed JSON never replaces the
// last usable configuration; the next successful tick restores deleted keys to
// defaults and keeps this listener pinned to its original data directory.
func (h *Host) RefreshConfig(ctx context.Context) error {
	if ctx == nil {
		return invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !h.gate.TryLock() {
		return nil
	}
	defer h.gate.Unlock()
	revision, err := config.Default.ReloadRevision()
	h.restoreDataPaths()
	if err != nil {
		h.configRevisionErr = err
		if h.configRefreshError == err.Error() {
			return nil
		}
		h.configRefreshError = err.Error()
		return err
	}
	h.configRevision, h.configRevisionErr = revision, nil
	h.configRefreshError = ""
	return nil
}
