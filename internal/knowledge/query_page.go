package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
)

// QueryPageInput is document-level browsing, without retrieval side effects.
// CLI Search and Query retain their existing chunk/retrieval contracts.
type QueryPageInput struct {
	Text            string `json:"text,omitempty"`
	Collection      string `json:"collection,omitempty"`
	Status          string `json:"status,omitempty"`
	ExcludeArchived bool   `json:"exclude_archived,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
}

type QueryPageResult struct {
	Documents  []DocumentView `json:"documents"`
	Total      int            `json:"total"`
	HasMore    bool           `json:"has_more"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type documentPageCursor struct {
	Version  int    `json:"v"`
	Offset   int    `json:"offset"`
	Snapshot string `json:"snapshot"`
}

// QueryPage returns a bounded page after document deduplication. The cursor is
// tied to both the scope and ordered result: a concurrent move, edit or archive
// forces a fresh first page instead of silently skipping/repeating documents.
// The Markdown corpus remains the source of truth; no persistent page cache is
// introduced and reads never create a corpus directory or lock file.
func (service Service) QueryPage(ctx context.Context, input QueryPageInput) (QueryPageResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return QueryPageResult{}, err
	}
	input.Text, input.Collection, input.Status = strings.TrimSpace(input.Text), strings.TrimSpace(input.Collection), strings.TrimSpace(input.Status)
	if len(input.Text) > 2000 || input.Limit < 0 || input.Limit > 200 || len(input.Cursor) > 1024 {
		return QueryPageResult{}, invalidKnowledgeArgument("knowledge page requires a bounded query, cursor and limit of 1–200", "limit", input.Limit)
	}
	if input.Limit == 0 {
		input.Limit = 100
	}
	switch input.Status {
	case "", "active", "draft", "archived":
	default:
		return QueryPageResult{}, invalidKnowledgeArgument("invalid knowledge status", "status", input.Status)
	}
	cursor := documentPageCursor{}
	if input.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(input.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Offset < 1 || len(cursor.Snapshot) != 64 {
			return QueryPageResult{}, invalidKnowledgeArgument("invalid knowledge page cursor", "cursor", input.Cursor)
		}
	}
	documents, err := Discover(service.dataDir)
	if err != nil {
		return QueryPageResult{}, unavailableKnowledge("read knowledge page", err)
	}
	var views []DocumentView
	if input.Text != "" {
		options := SearchOptions{}
		if input.Collection != "" {
			options.Collections = []string{input.Collection}
		}
		if input.Status != "" {
			options.Statuses = []string{input.Status}
		}
		tallies, _ := service.feedback.Totals()
		hits := rankSearchDocuments(documents, input.Text, options, qualityIndexFromTallies(documents, tallies))
		seen := make(map[string]bool)
		for _, hit := range hits {
			if seen[hit.DocumentID] || input.ExcludeArchived && hit.Status == "archived" {
				continue
			}
			seen[hit.DocumentID] = true
			score := hit.Score
			views = append(views, DocumentView{DocumentID: hit.DocumentID, Title: hit.Title, Collection: hit.Collection,
				Status: hit.Status, Domains: hit.Domains, Tags: hit.Tags, Projects: hit.Projects, Snippet: hit.Snippet, Score: &score})
		}
	} else {
		for _, document := range documents {
			metadata := document.Metadata
			if input.Collection != "" && document.Collection != input.Collection || input.Status != "" && metadata.Status != input.Status ||
				input.ExcludeArchived && metadata.Status == "archived" {
				continue
			}
			createdAt, updatedAt := metadata.CreatedAt, metadata.UpdatedAt
			views = append(views, DocumentView{DocumentID: metadata.ID, Title: metadata.Title, Collection: document.Collection,
				Status: metadata.Status, Domains: metadata.Domains, Tags: metadata.Tags, Projects: metadata.Projects, Producer: metadata.Producer,
				CreatedAt: &createdAt, UpdatedAt: &updatedAt, Snippet: compactSnippet(document.Content, 240)})
		}
		sort.SliceStable(views, func(i, j int) bool {
			if !views[i].UpdatedAt.Equal(*views[j].UpdatedAt) {
				return views[i].UpdatedAt.After(*views[j].UpdatedAt)
			}
			if views[i].Title != views[j].Title {
				return views[i].Title < views[j].Title
			}
			return views[i].DocumentID < views[j].DocumentID
		})
		seen := make(map[string]bool)
		unique := views[:0]
		for _, view := range views {
			if !seen[view.DocumentID] {
				seen[view.DocumentID] = true
				unique = append(unique, view)
			}
		}
		views = unique
	}
	if err := knowledgeContextError(ctx); err != nil {
		return QueryPageResult{}, err
	}
	// Hash the scope separately from pagination, so changing page size is safe
	// but replaying a cursor for another collection or search cannot be accepted.
	scope := input
	scope.Cursor, scope.Limit = "", 0
	data, err := json.Marshal(struct {
		Scope QueryPageInput
		Rows  []DocumentView
	}{scope, views})
	if err != nil {
		return QueryPageResult{}, unavailableKnowledge("identify knowledge page", err)
	}
	hash := sha256.Sum256(data)
	snapshot := hex.EncodeToString(hash[:])
	if input.Cursor != "" && cursor.Snapshot != snapshot {
		err := application.NewError(application.CodeConflict, "文档列表已变化，请刷新后继续浏览。")
		err.Details = map[string]any{"reason": "knowledge_cursor_stale"}
		return QueryPageResult{}, err
	}
	if cursor.Offset > len(views) {
		return QueryPageResult{}, invalidKnowledgeArgument("knowledge page cursor is outside the result", "cursor", input.Cursor)
	}
	end := min(cursor.Offset+input.Limit, len(views))
	result := QueryPageResult{Documents: append([]DocumentView{}, views[cursor.Offset:end]...), Total: len(views), HasMore: end < len(views)}
	if result.HasMore {
		data, _ := json.Marshal(documentPageCursor{Version: 1, Offset: end, Snapshot: snapshot})
		result.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return result, nil
}
