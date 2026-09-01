package work

import (
	"context"
	"errors"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// DefaultDedupMinQueryScore is the application default for deciding whether a
// goal is already represented by an active Todo. Adapters expose the knob but
// do not need to import persistence-level matching code.
const DefaultDedupMinQueryScore = store.TodoDedupMinQueryScore

type MatchInput struct {
	Project       string `json:"project,omitempty"`
	Query         string `json:"query,omitempty"`
	Limit         int    `json:"limit"`
	Deduplicate   bool   `json:"deduplicate,omitempty"`
	MinQueryScore int    `json:"min_query_score,omitempty"`
}

type MatchCandidate struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Project    string `json:"project,omitempty"`
	Status     string `json:"status"`
	Score      int    `json:"score"`
	QueryScore int    `json:"query_score"`
	Reason     string `json:"reason,omitempty"`
}

type MatchResult struct {
	Project       string                    `json:"project,omitempty"`
	Query         string                    `json:"query,omitempty"`
	MinQueryScore int                       `json:"min_query_score,omitempty"`
	Duplicate     bool                      `json:"duplicate,omitempty"`
	Bound         bool                      `json:"bound"`
	Binding       *store.TodoSessionBinding `json:"binding,omitempty"`
	Todo          *TodoSummary              `json:"todo,omitempty"`
	Candidates    []MatchCandidate          `json:"candidates"`
}

// Match returns either the current valid binding or ranked active Todo
// candidates. Deduplication deliberately ignores the current binding because it
// answers a different question: whether creating a new Todo would duplicate
// durable work anywhere in the repository.
func (service Service) Match(
	ctx context.Context,
	call application.Call,
	input MatchInput,
) (MatchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return MatchResult{}, err
	}
	if err := call.Validate(); err != nil {
		return MatchResult{}, err
	}
	if input.Limit < 1 || input.Limit > 10 {
		return MatchResult{}, bindingInvalidArgument("match limit must be between 1 and 10", "limit", input.Limit)
	}

	project := config.CanonicalProject(strings.TrimSpace(input.Project))
	query := strings.TrimSpace(input.Query)
	result := MatchResult{
		Project:       project,
		Query:         query,
		MinQueryScore: input.MinQueryScore,
	}
	if input.Deduplicate && query == "" {
		return MatchResult{}, bindingInvalidArgument("deduplication requires a goal", "query", input.Query)
	}

	if !input.Deduplicate && strings.TrimSpace(call.Actor.SessionID) != "" {
		current, err := service.Current(ctx, call, CurrentInput{})
		if err != nil {
			return MatchResult{}, err
		}
		if current.Bound && current.Context != nil && current.Context.Todo != nil {
			binding := current.Context.Binding
			todo := *current.Context.Todo
			result.Bound = true
			result.Binding = &binding
			result.Todo = &todo
			return result, nil
		}
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		if errors.Is(err, store.ErrDatabaseMissing) {
			result.Candidates = []MatchCandidate{}
			return result, nil
		}
		return MatchResult{}, bindingApplicationError("load todos for matching", err)
	}
	options := store.TodoMatchOptions{
		Project:       project,
		Query:         query,
		Limit:         input.Limit,
		MinQueryScore: input.MinQueryScore,
		AllProjects:   input.Deduplicate,
	}
	matches := store.MatchTodosWithOptions(todos, options)
	result.Candidates = make([]MatchCandidate, 0, len(matches))
	for _, match := range matches {
		result.Candidates = append(result.Candidates, MatchCandidate{
			ID: match.ID, Title: match.Title, Project: match.Project, Status: match.Status,
			Score: match.Score, QueryScore: match.QueryScore, Reason: match.Reason,
		})
	}
	result.Duplicate = input.Deduplicate && len(result.Candidates) > 0
	return result, nil
}
