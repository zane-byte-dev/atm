package session

import (
	"context"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

const SessionReviewSchemaVersion = 1

type Review struct {
	SchemaVersion int       `json:"schemaVersion"`
	SessionID     string    `json:"sessionId"`
	Outcome       string    `json:"outcome"`
	Note          string    `json:"note,omitempty"`
	ReviewedAt    time.Time `json:"reviewedAt"`
}

type ListInput struct {
	Agent          string `json:"agent,omitempty"`
	Project        string `json:"project,omitempty"`
	Days           int    `json:"days,omitempty"`
	Since          string `json:"since,omitempty"`
	Review         string `json:"review,omitempty"`
	All            bool   `json:"all,omitempty"`
	Order          string `json:"order,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Offset         int    `json:"offset,omitempty"`
	SyncBeforeRead bool   `json:"sync_before_read,omitempty"`
}

type Summary struct {
	ID             string  `json:"id"`
	ShortID        string  `json:"short_id"`
	Agent          string  `json:"agent"`
	Project        string  `json:"project"`
	CreatedAt      string  `json:"created_at"`
	IndexedCreated string  `json:"-"`
	LastAt         string  `json:"last_at,omitempty"`
	QuestionCount  int     `json:"q_count"`
	Summary        string  `json:"summary,omitempty"`
	FirstQuestion  string  `json:"first_q,omitempty"`
	Review         *Review `json:"memory_review,omitempty"`
}

type ListResult struct {
	Sessions []Summary `json:"sessions"`
	Total    int       `json:"total"`
	Days     int       `json:"days"`
	All      bool      `json:"all"`
	Offset   int       `json:"offset"`
	Limit    int       `json:"limit"`
	Meta     ReadMeta  `json:"meta"`
}

func (service Service) List(ctx context.Context, input ListInput) (ListResult, error) {
	if err := contextError(ctx); err != nil {
		return ListResult{}, err
	}
	input.Agent = strings.TrimSpace(input.Agent)
	input.Project = strings.TrimSpace(input.Project)
	input.Since = strings.TrimSpace(input.Since)
	input.Review = strings.ToLower(strings.TrimSpace(input.Review))
	input.Order = strings.ToLower(strings.TrimSpace(input.Order))
	if input.Days < 1 {
		input.Days = 1
	}
	if input.Review == "" {
		input.Review = "all"
	}
	if input.Review != "all" && input.Review != "pending" && input.Review != "reviewed" {
		return ListResult{}, invalidArgument(
			"invalid review state: use all, pending, or reviewed", "review", input.Review,
		)
	}
	if input.Order == "" {
		input.Order = "asc"
	}
	if input.Order != "asc" && input.Order != "desc" {
		return ListResult{}, invalidArgument(
			"invalid order: use asc or desc", "order", input.Order,
		)
	}
	if input.Offset < 0 {
		return ListResult{}, invalidArgument("offset must not be negative", "offset", input.Offset)
	}
	if input.Limit < 0 {
		return ListResult{}, invalidArgument("limit must not be negative", "limit", input.Limit)
	}

	now := service.now().In(service.location)
	start := service.startOfDayWindow(now, input.Days)
	if input.Since != "" {
		parsed, err := service.parseSince(input.Since)
		if err != nil {
			return ListResult{}, err
		}
		start = parsed
	}
	if input.All {
		start = time.Unix(0, 0).In(service.location)
	}

	db, meta, err := service.openRead(ctx, input.SyncBeforeRead)
	if err != nil {
		return ListResult{}, err
	}
	defer db.Close()
	rows, err := store.ListSessions(db, start.Unix(), now.Unix(), input.Agent, input.Project)
	if err != nil {
		return ListResult{}, unavailable("failed to list sessions", err)
	}
	reviewRows, err := store.SessionReviews()
	if err != nil {
		return ListResult{}, unavailable("failed to read session review state", err)
	}

	filtered := rows[:0]
	for _, row := range rows {
		_, reviewed := reviewRows[row.FullID]
		if input.Review == "pending" && reviewed {
			continue
		}
		if input.Review == "reviewed" && !reviewed {
			continue
		}
		filtered = append(filtered, row)
	}
	if input.Order == "desc" {
		for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
			filtered[left], filtered[right] = filtered[right], filtered[left]
		}
	}
	total := len(filtered)
	filtered, err = page(filtered, input.Offset, input.Limit)
	if err != nil {
		return ListResult{}, err
	}

	sessions := make([]Summary, 0, len(filtered))
	for _, row := range filtered {
		createdAt := row.CreatedAt
		if row.CreatedTS > 0 {
			createdAt = time.Unix(row.CreatedTS, 0).In(service.location).Format(time.RFC3339)
		}
		lastAt := ""
		if row.LastTS > 0 {
			lastAt = time.Unix(row.LastTS, 0).In(service.location).Format(time.RFC3339)
		}
		var review *Review
		if stored, ok := reviewRows[row.FullID]; ok {
			value := reviewFromStore(stored)
			review = &value
		}
		sessions = append(sessions, Summary{
			ID: row.FullID, ShortID: row.ShortID, Agent: row.Agent, Project: row.Project,
			CreatedAt: createdAt, IndexedCreated: row.CreatedAt, LastAt: lastAt,
			QuestionCount: row.QCount, Summary: row.Summary,
			FirstQuestion: cleanMessage(row.FirstQ), Review: review,
		})
	}
	if err := contextError(ctx); err != nil {
		return ListResult{}, err
	}
	return ListResult{
		Sessions: sessions, Total: total, Days: input.Days, All: input.All,
		Offset: input.Offset, Limit: input.Limit, Meta: meta,
	}, nil
}

func reviewFromStore(row store.SessionReview) Review {
	reviewedAt, _ := time.Parse(time.RFC3339Nano, row.ReviewedAt)
	return Review{
		SchemaVersion: SessionReviewSchemaVersion,
		SessionID:     row.SessionID, Outcome: row.Outcome, Note: row.Note,
		ReviewedAt: reviewedAt.UTC(),
	}
}
