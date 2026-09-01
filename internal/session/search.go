package session

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

type SearchInput struct {
	Keyword        string `json:"keyword"`
	Agent          string `json:"agent,omitempty"`
	Project        string `json:"project,omitempty"`
	Since          string `json:"since,omitempty"`
	Days           int    `json:"days,omitempty"`
	Role           string `json:"role,omitempty"`
	Limit          int    `json:"limit"`
	Snippet        int    `json:"snippet"`
	SyncBeforeRead bool   `json:"sync_before_read,omitempty"`
}

type SearchHit struct {
	ID               string `json:"id"`
	ShortID          string `json:"short_id"`
	Agent            string `json:"agent"`
	Project          string `json:"project"`
	CreatedAt        string `json:"created_at"`
	IndexedCreated   string `json:"-"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	SnippetTruncated bool   `json:"snippet_truncated,omitempty"`
}

type SearchResult struct {
	Keyword   string      `json:"keyword"`
	Total     int         `json:"total"`
	Returned  int         `json:"returned"`
	Truncated bool        `json:"truncated"`
	Limit     int         `json:"limit"`
	Matches   []SearchHit `json:"matches"`
	Meta      ReadMeta    `json:"meta"`
}

func (service Service) Search(ctx context.Context, input SearchInput) (SearchResult, error) {
	if err := contextError(ctx); err != nil {
		return SearchResult{}, err
	}
	input.Keyword = strings.TrimSpace(input.Keyword)
	input.Agent = strings.TrimSpace(input.Agent)
	input.Project = strings.TrimSpace(input.Project)
	input.Since = strings.TrimSpace(input.Since)
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if input.Keyword == "" {
		return SearchResult{}, invalidArgument("search keyword must not be empty", "keyword", input.Keyword)
	}
	if input.Limit < 1 {
		return SearchResult{}, invalidArgument("search limit must be at least 1", "limit", input.Limit)
	}
	if input.Snippet < 1 {
		return SearchResult{}, invalidArgument("search snippet must be at least 1", "snippet", input.Snippet)
	}
	if input.Since != "" && input.Days != 0 {
		return SearchResult{}, invalidArgument("since and days are mutually exclusive", "days", input.Days)
	}
	if input.Days < 0 {
		return SearchResult{}, invalidArgument("search days must be at least 1", "days", input.Days)
	}
	if input.Role != "" && input.Role != "user" && input.Role != "assistant" {
		return SearchResult{}, invalidArgument(
			"invalid role: use user or assistant", "role", input.Role,
		)
	}

	var sinceTS int64
	if input.Since != "" {
		since, err := service.parseSince(input.Since)
		if err != nil {
			return SearchResult{}, err
		}
		sinceTS = since.Unix()
	} else if input.Days != 0 {
		if input.Days < 1 {
			return SearchResult{}, invalidArgument("search days must be at least 1", "days", input.Days)
		}
		sinceTS = service.startOfDayWindow(service.now(), input.Days).Unix()
	}

	db, meta, err := service.openRead(ctx, input.SyncBeforeRead)
	if err != nil {
		return SearchResult{}, err
	}
	defer db.Close()
	matches, err := store.SearchMessagesWithOptions(db, input.Keyword, store.SearchOptions{
		Agent: input.Agent, Project: input.Project, Role: input.Role,
		SinceTS: sinceTS, Limit: input.Limit,
		Keep: func(content string) bool { return cleanMessage(content) != "" },
	})
	if err != nil {
		return SearchResult{}, unavailable("failed to search sessions", err)
	}

	hits := make([]SearchHit, 0, len(matches.Hits))
	for _, row := range matches.Hits {
		content, truncated := matchSnippet(cleanMessage(row.Content), input.Keyword, input.Snippet)
		hits = append(hits, SearchHit{
			ID: row.FullID, ShortID: row.ShortID, Agent: row.Agent, Project: row.Project,
			CreatedAt: searchCreatedAt(row, service.location), IndexedCreated: row.CreatedAt,
			Role: row.Role, Content: content, SnippetTruncated: truncated,
		})
	}
	if err := contextError(ctx); err != nil {
		return SearchResult{}, err
	}
	return SearchResult{
		Keyword: input.Keyword, Total: matches.Total, Returned: len(hits),
		Truncated: matches.Truncated, Limit: input.Limit, Matches: hits, Meta: meta,
	}, nil
}

func searchCreatedAt(hit store.SearchHit, location *time.Location) string {
	if hit.CreatedTS > 0 {
		return time.Unix(hit.CreatedTS, 0).In(location).Format(time.RFC3339)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, hit.CreatedAt); err == nil {
			return parsed.Format(time.RFC3339)
		}
	}
	return ""
}

func cleanMessage(value string) string {
	return parser.VisibleUserText(value)
}

func matchSnippet(content, keyword string, maxChars int) (string, bool) {
	contentRunes := []rune(content)
	if len(contentRunes) <= maxChars {
		return content, false
	}
	if maxChars == 1 {
		return "…", true
	}

	lowerContent := strings.ToLower(content)
	lowerKeyword := strings.ToLower(keyword)
	matchAt := runeIndex(lowerContent, lowerKeyword)
	if matchAt < 0 {
		matchAt = 0
	}
	center := matchAt + utf8.RuneCountInString(lowerKeyword)/2
	start := center - maxChars/2
	if start < 0 {
		start = 0
	}
	end := start + maxChars
	if end > len(contentRunes) {
		end = len(contentRunes)
		start = end - maxChars
	}

	for {
		indicators := 0
		if start > 0 {
			indicators++
		}
		if end < len(contentRunes) {
			indicators++
		}
		allowed := maxChars - indicators
		if end-start <= allowed {
			break
		}
		start = center - allowed/2
		if start < 0 {
			start = 0
		}
		end = start + allowed
		if end > len(contentRunes) {
			end = len(contentRunes)
			start = end - allowed
		}
	}

	var snippet strings.Builder
	if start > 0 {
		snippet.WriteRune('…')
	}
	snippet.WriteString(string(contentRunes[start:end]))
	if end < len(contentRunes) {
		snippet.WriteRune('…')
	}
	return snippet.String(), true
}

func runeIndex(haystack, needle string) int {
	byteAt := strings.Index(haystack, needle)
	if byteAt < 0 {
		return -1
	}
	return utf8.RuneCountInString(haystack[:byteAt])
}
