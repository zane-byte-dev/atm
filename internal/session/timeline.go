package session

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/store"
)

type TimelineInput struct {
	SessionID      string `json:"session_id"`
	SyncBeforeRead bool   `json:"sync_before_read,omitempty"`
}

type TimelineEvent struct {
	Kind         string  `json:"kind"`
	Role         string  `json:"role,omitempty"`
	Content      string  `json:"content,omitempty"`
	Scope        string  `json:"scope,omitempty"`
	MessageKind  string  `json:"message_kind,omitempty"`
	Phase        string  `json:"phase,omitempty"`
	Model        string  `json:"model,omitempty"`
	TS           int64   `json:"ts"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	CacheTokens  int64   `json:"cache_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

type TimelineResult struct {
	Events []TimelineEvent `json:"events"`
	Meta   ReadMeta        `json:"meta"`
}

func (service Service) Timeline(ctx context.Context, input TimelineInput) (TimelineResult, error) {
	if err := contextError(ctx); err != nil {
		return TimelineResult{}, err
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.SessionID == "" {
		return TimelineResult{}, invalidArgument("session id must not be empty", "session_id", input.SessionID)
	}
	db, meta, err := service.openRead(ctx, input.SyncBeforeRead)
	if err != nil {
		return TimelineResult{}, err
	}
	defer db.Close()
	stored, err := store.GetSessionTimeline(db, input.SessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TimelineResult{}, sessionNotFound(input.SessionID, err)
		}
		return TimelineResult{}, unavailable("read session timeline", err)
	}
	events := make([]TimelineEvent, 0, len(stored))
	for _, event := range stored {
		events = append(events, TimelineEvent{
			Kind: event.Kind, Role: event.Role, Content: event.Content, Scope: event.Scope,
			MessageKind: event.MessageKind, Phase: event.Phase, Model: event.Model,
			TS: event.TS, InputTokens: event.InputTokens, OutputTokens: event.OutputTokens,
			CacheTokens: event.CacheTokens, CostUSD: event.CostUSD,
		})
	}
	if err := contextError(ctx); err != nil {
		return TimelineResult{}, err
	}
	return TimelineResult{Events: events, Meta: meta}, nil
}
