package session

import (
	"context"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

type ToolsInput struct {
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Since     string `json:"since,omitempty"`
	Days      int    `json:"days,omitempty"`
	Failed    bool   `json:"failed,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

type ToolInvocation struct {
	ID          int64  `json:"id"`
	OccurredAt  string `json:"occurred_at"`
	SessionID   string `json:"session_id,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Version     string `json:"version,omitempty"`
	CommandPath string `json:"command_path"`
	ExitCode    int    `json:"exit_code"`
	ErrorCode   string `json:"error_code,omitempty"`
	CauseClass  string `json:"cause_class,omitempty"`
	Retryable   bool   `json:"retryable"`
	DurationMS  int64  `json:"duration_ms"`
	Success     bool   `json:"success"`
}

type ToolsResult struct {
	Invocations []ToolInvocation `json:"invocations"`
	Total       int              `json:"total"`
	Matched     int              `json:"matched"`
	Succeeded   int              `json:"succeeded"`
	Failed      int              `json:"failed"`
	Offset      int              `json:"offset"`
	Limit       int              `json:"limit"`
}

// Tools reads ATM's own content-free CLI invocation telemetry. Unlike session
// transcripts, this ledger is already local and is never synchronized from an
// agent source, so the use case intentionally has no SyncBeforeRead switch.
func (service Service) Tools(ctx context.Context, input ToolsInput) (ToolsResult, error) {
	if err := contextError(ctx); err != nil {
		return ToolsResult{}, err
	}
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Agent = strings.TrimSpace(input.Agent)
	input.Since = strings.TrimSpace(input.Since)
	if input.Days < 0 {
		return ToolsResult{}, invalidArgument("tools days must be at least 1", "days", input.Days)
	}
	if input.Since != "" && input.Days != 0 {
		return ToolsResult{}, invalidArgument("since and days are mutually exclusive", "days", input.Days)
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return ToolsResult{}, invalidArgument("tools limit must be between 1 and 1000", "limit", input.Limit)
	}
	if input.Offset < 0 {
		return ToolsResult{}, invalidArgument("offset must not be negative", "offset", input.Offset)
	}

	now := service.now().In(service.location)
	var sinceTS int64
	if input.Since != "" {
		parsed, err := service.parseSince(input.Since)
		if err != nil {
			return ToolsResult{}, err
		}
		sinceTS = parsed.Unix()
	} else {
		days := input.Days
		if days == 0 {
			days = 7
		}
		sinceTS = service.startOfDayWindow(now, days).Unix()
	}

	db, _, err := service.openRead(ctx, false)
	if err != nil {
		return ToolsResult{}, err
	}
	defer db.Close()
	stored, err := store.QueryCLIInvocations(db, store.CLIInvocationQuery{
		SessionID: input.SessionID,
		Agent:     input.Agent,
		SinceTS:   sinceTS,
		UntilTS:   now.Unix() + 1,
		Failed:    input.Failed,
		Limit:     input.Limit,
		Offset:    input.Offset,
	})
	if err != nil {
		return ToolsResult{}, unavailable("failed to read CLI invocation telemetry", err)
	}

	result := ToolsResult{
		Invocations: make([]ToolInvocation, 0, len(stored.Invocations)),
		Total:       stored.Total, Matched: stored.Matched,
		Succeeded: stored.Succeeded, Failed: stored.Failed,
		Offset: stored.Offset, Limit: stored.Limit,
	}
	for _, invocation := range stored.Invocations {
		result.Invocations = append(result.Invocations, ToolInvocation{
			ID:          invocation.ID,
			OccurredAt:  time.Unix(invocation.OccurredAt, 0).In(service.location).Format(time.RFC3339),
			SessionID:   invocation.SessionID,
			Agent:       invocation.Agent,
			Version:     invocation.Version,
			CommandPath: invocation.CommandPath,
			ExitCode:    invocation.ExitCode,
			ErrorCode:   invocation.ErrorCode,
			CauseClass:  invocation.CauseClass,
			Retryable:   invocation.Retryable,
			DurationMS:  invocation.DurationMS,
			Success:     invocation.Success,
		})
	}
	if err := contextError(ctx); err != nil {
		return ToolsResult{}, err
	}
	return result, nil
}
