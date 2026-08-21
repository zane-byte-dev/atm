package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// ForgetPlan is the session a human was shown before confirming. Message,
// request and spend counts are what the prompt quoted; the identity and the
// retained-source verdict are what Forget rechecks, because a sync between the
// prompt and the delete can re-adopt the transcript and make forgetting a no-op
// the next sync would undo.
type ForgetPlan struct {
	SessionID string  `json:"session_id"`
	ShortID   string  `json:"short_id"`
	Agent     string  `json:"agent"`
	Project   string  `json:"project"`
	CreatedAt string  `json:"created_at"`
	FilePath  string  `json:"file_path,omitempty"`
	Messages  int     `json:"messages"`
	Requests  int     `json:"requests"`
	CostUSD   float64 `json:"cost_usd"`
}

type PlanForgetInput struct {
	SessionID string `json:"session_id"`
}

type ForgetInput struct {
	Plan      ForgetPlan `json:"plan"`
	Confirmed bool       `json:"confirmed"`
}

// ForgetResult reports what left the index, so an adapter can name the losses
// without re-reading a session that no longer exists.
type ForgetResult struct {
	Session ForgetPlan `json:"session"`
}

// PlanForget resolves the target and refuses the cases where forgetting is
// pointless, so the confirmation prompt is only ever raised for a session that
// can actually be dropped.
func (service Service) PlanForget(ctx context.Context, input PlanForgetInput) (ForgetPlan, error) {
	if err := contextError(ctx); err != nil {
		return ForgetPlan{}, err
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.SessionID == "" {
		return ForgetPlan{}, invalidArgument("session id must not be empty", "session_id", input.SessionID)
	}
	db, _, err := service.openRead(ctx, false)
	if err != nil {
		return ForgetPlan{}, err
	}
	defer db.Close()
	plan, err := forgettable(db, input.SessionID)
	if err != nil {
		return ForgetPlan{}, err
	}
	if err := contextError(ctx); err != nil {
		return ForgetPlan{}, err
	}
	return plan, nil
}

// Forget permanently drops the confirmed session. Confirmed is a workflow
// guard, not proof of human identity: CLI flags and IPC payloads are
// replayable, so obtaining the confirmation stays with the adapter.
func (service Service) Forget(ctx context.Context, input ForgetInput) (ForgetResult, error) {
	if err := contextError(ctx); err != nil {
		return ForgetResult{}, err
	}
	if !input.Confirmed {
		return ForgetResult{}, application.NewError(
			application.CodeForbidden, "forgetting a session requires explicit confirmation",
		)
	}
	sessionID := strings.TrimSpace(input.Plan.SessionID)
	if sessionID == "" {
		return ForgetResult{}, invalidArgument("forget plan has no session id", "plan.session_id", input.Plan.SessionID)
	}
	db, err := service.openWrite(ctx)
	if err != nil {
		return ForgetResult{}, err
	}
	defer db.Close()
	current, err := forgettable(db, sessionID)
	if err != nil {
		return ForgetResult{}, err
	}
	if current.SessionID != sessionID {
		conflict := application.NewError(application.CodeConflict,
			"forget target changed after confirmation; confirm the current session again")
		conflict.Details = map[string]any{"confirmed_session_id": sessionID, "current_session_id": current.SessionID}
		return ForgetResult{}, conflict
	}
	if err := store.ForgetSession(db, current.SessionID); err != nil {
		return ForgetResult{}, unavailable("forget session", err)
	}
	return ForgetResult{Session: current}, nil
}

func forgettable(db *sql.DB, sessionID string) (ForgetPlan, error) {
	stored, err := store.FindForgettableSession(db, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return ForgetPlan{}, sessionNotFound(sessionID, err)
	}
	if err != nil {
		return ForgetPlan{}, unavailable("look up forgettable session", err)
	}
	if stored.SourceTracked {
		conflict := application.NewError(application.CodeConflict, fmt.Sprintf(
			"session %s is still backed by %s: delete the transcript and run `atm sync` first, or the next sync will index it again",
			stored.ShortID, stored.FilePath))
		conflict.Details = map[string]any{"session_id": stored.ID, "file_path": stored.FilePath}
		return ForgetPlan{}, conflict
	}
	return ForgetPlan{
		SessionID: stored.ID, ShortID: stored.ShortID, Agent: stored.Agent,
		Project: stored.Project, CreatedAt: stored.CreatedAt, FilePath: stored.FilePath,
		Messages: stored.Messages, Requests: stored.Requests, CostUSD: stored.CostUSD,
	}, nil
}
