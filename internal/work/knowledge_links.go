package work

import (
	"context"
	"net/url"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Knowledge links use a stable document identity, independent of host, port,
// title or collection. Only this exact relative route is accepted as a link.
func KnowledgeLinkURL(id string) string {
	return "/knowledge?" + url.Values{"document": {id}}.Encode()
}

func KnowledgeLinkID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != "/knowledge" || u.RawPath != "" || u.Fragment != "" {
		return ""
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["document"]) != 1 {
		return ""
	}
	id := q.Get("document")
	if strings.TrimSpace(id) == "" || len(id) > 500 || strings.ContainsAny(id, "/\\\x00\r\n") || id == "." || id == ".." {
		return ""
	}
	return id
}

func (service Service) KnowledgeTasks(ctx context.Context, call application.Call, documentID string, offset, limit int) ([]store.LinkedKnowledgeTask, error) {
	if err := validateLinkCall(ctx, call); err != nil {
		return nil, err
	}
	return store.ReadKnowledgeTasks(ctx, KnowledgeLinkURL(documentID), offset, limit)
}
