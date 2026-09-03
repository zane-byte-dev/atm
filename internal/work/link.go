package work

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type TodoLink = store.TodoLink

type AddLinkInput struct {
	TodoID   string `json:"todo_id"`
	URL      string `json:"url"`
	Kind     string `json:"kind,omitempty"`
	Title    string `json:"title,omitempty"`
	Relation string `json:"relation,omitempty"`
}

type AddLinkResult struct {
	TodoID  string   `json:"todo_id"`
	Created bool     `json:"created"`
	Link    TodoLink `json:"link"`
}

type ListLinksInput struct {
	TodoID string `json:"todo_id"`
}

type ListLinksResult struct {
	TodoID string     `json:"todo_id"`
	Links  []TodoLink `json:"links"`
}

type RemoveLinkInput struct {
	TodoID string `json:"todo_id"`
	URL    string `json:"url"`
}

type RemoveLinkResult struct {
	TodoID  string   `json:"todo_id"`
	Removed TodoLink `json:"removed"`
	Todo    Todo     `json:"-"`
}

// SaveLinkInput is a full form submission. OriginalURL identifies an existing
// link; omitting it adds a new one. Empty title/relation explicitly clear them.
type SaveLinkInput struct {
	TodoID      string `json:"todo_id"`
	OriginalURL string `json:"original_url,omitempty"`
	URL         string `json:"url"`
	Kind        string `json:"kind,omitempty"`
	Title       string `json:"title,omitempty"`
	Relation    string `json:"relation,omitempty"`
}

// SaveLink replaces a link atomically, preserving order and rejecting collisions
// instead of removing the old URL before the replacement is safely committed.
func (service Service) SaveLink(ctx context.Context, call application.Call, input SaveLinkInput) (Todo, error) {
	if err := validateLinkCall(ctx, call); err != nil {
		return Todo{}, err
	}
	todoID, err := normalizeLinkTodoID(input.TodoID)
	if err != nil {
		return Todo{}, err
	}
	cleanURL, err := NormalizeTodoLinkURL(input.URL)
	if err != nil {
		return Todo{}, err
	}
	originalURL := ""
	if input.OriginalURL != "" {
		originalURL, err = NormalizeTodoLinkURL(input.OriginalURL)
		if err != nil {
			return Todo{}, err
		}
	}
	link := TodoLink{URL: cleanURL, Kind: strings.TrimSpace(input.Kind), Title: strings.TrimSpace(input.Title), Relation: strings.TrimSpace(input.Relation)}
	if link.Kind == "" {
		link.Kind = InferTodoLinkKind(cleanURL)
	}
	var result Todo
	err = service.Mutate(func(transaction *Transaction) error {
		todo, err := transaction.Todo(todoID)
		if err != nil {
			return linkTodoNotFound(todoID, err)
		}
		originalIndex := -1
		for index, existing := range todo.Links {
			if existing.URL == originalURL {
				originalIndex = index
			}
			if existing.URL == cleanURL && existing.URL != originalURL {
				return application.NewError(application.CodeConflict, "该地址已关联，请编辑已有条目")
			}
		}
		if originalURL != "" {
			if originalIndex < 0 {
				return application.NewError(application.CodeNotFound, "原关联已不存在，请刷新后重试")
			}
			todo.Links[originalIndex] = link
		} else {
			todo.Links = append(todo.Links, link)
		}
		result = *todo
		result.Links = append([]TodoLink(nil), todo.Links...)
		return nil
	})
	if err != nil {
		return Todo{}, linkApplicationError("save todo link", err)
	}
	return result, nil
}

// AddLink owns URL safety, kind inference and the idempotent update-or-insert
// rule. Transports should not pre-normalize URLs or edit Todo.Links directly.
func (service Service) AddLink(ctx context.Context, call application.Call, input AddLinkInput) (AddLinkResult, error) {
	if err := validateLinkCall(ctx, call); err != nil {
		return AddLinkResult{}, err
	}
	todoID, err := normalizeLinkTodoID(input.TodoID)
	if err != nil {
		return AddLinkResult{}, err
	}
	cleanURL, err := NormalizeTodoLinkURL(input.URL)
	if err != nil {
		return AddLinkResult{}, err
	}
	link := TodoLink{
		URL:      cleanURL,
		Kind:     strings.TrimSpace(input.Kind),
		Title:    strings.TrimSpace(input.Title),
		Relation: strings.TrimSpace(input.Relation),
	}
	if link.Kind == "" {
		link.Kind = InferTodoLinkKind(cleanURL)
	}

	result := AddLinkResult{TodoID: todoID, Created: true}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(todoID)
		if findErr != nil {
			return linkTodoNotFound(todoID, findErr)
		}
		for index := range todo.Links {
			if todo.Links[index].URL != cleanURL {
				continue
			}
			result.Created = false
			if link.Kind != "" {
				todo.Links[index].Kind = link.Kind
			}
			if link.Title != "" {
				todo.Links[index].Title = link.Title
			}
			if link.Relation != "" {
				todo.Links[index].Relation = link.Relation
			}
			result.Link = todo.Links[index]
			return nil
		}
		todo.Links = append(todo.Links, link)
		result.Link = link
		return nil
	})
	if err != nil {
		return AddLinkResult{}, linkApplicationError("add todo link", err)
	}
	return result, nil
}

func (service Service) ListLinks(ctx context.Context, call application.Call, input ListLinksInput) (ListLinksResult, error) {
	if err := validateLinkCall(ctx, call); err != nil {
		return ListLinksResult{}, err
	}
	todoID, err := normalizeLinkTodoID(input.TodoID)
	if err != nil {
		return ListLinksResult{}, err
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return ListLinksResult{}, linkUnavailable("load todo links", err)
	}
	todo := store.FindTodo(todos, todoID)
	if todo == nil {
		return ListLinksResult{}, linkTodoNotFound(todoID, store.TodoNotFoundError(todos, todoID))
	}
	links := append([]TodoLink(nil), todo.Links...)
	if links == nil {
		links = []TodoLink{}
	}
	return ListLinksResult{TodoID: todo.ID, Links: links}, nil
}

func (service Service) RemoveLink(ctx context.Context, call application.Call, input RemoveLinkInput) (RemoveLinkResult, error) {
	if err := validateLinkCall(ctx, call); err != nil {
		return RemoveLinkResult{}, err
	}
	todoID, err := normalizeLinkTodoID(input.TodoID)
	if err != nil {
		return RemoveLinkResult{}, err
	}
	cleanURL, err := NormalizeTodoLinkURL(input.URL)
	if err != nil {
		return RemoveLinkResult{}, err
	}

	result := RemoveLinkResult{TodoID: todoID}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(todoID)
		if findErr != nil {
			return linkTodoNotFound(todoID, findErr)
		}
		for index, link := range todo.Links {
			if link.URL != cleanURL {
				continue
			}
			result.Removed = link
			todo.Links = append(todo.Links[:index], todo.Links[index+1:]...)
			result.Todo = *todo
			result.Todo.Links = append([]TodoLink(nil), todo.Links...)
			return nil
		}
		appErr := application.NewError(application.CodeNotFound, fmt.Sprintf("link not found on todo %s", todo.ID))
		appErr.Details = map[string]any{"todo_id": todo.ID, "url": cleanURL}
		return appErr
	})
	if err != nil {
		return RemoveLinkResult{}, linkApplicationError("remove todo link", err)
	}
	return result, nil
}

// NormalizeTodoLinkURL is public to adapters only for preview and tests. Every
// mutation calls it again inside the application service, so it is never a
// transport-side security boundary.
func NormalizeTodoLinkURL(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", linkInvalidArgument("link must be a complete http/https URL", "url", raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", linkInvalidArgument(
			fmt.Sprintf("unsupported link scheme %q (use http or https)", parsed.Scheme), "url", raw,
		)
	}
	if parsed.User != nil {
		return "", linkInvalidArgument("link must not contain embedded credentials", "url", raw)
	}
	for key := range parsed.Query() {
		if sensitiveTodoLinkParameter(key) {
			return "", linkInvalidArgument(
				fmt.Sprintf("link contains sensitive query parameter %q", key), "url", raw,
			)
		}
	}
	fragment := strings.ToLower(parsed.Fragment)
	if index := strings.Index(raw, "#"); index >= 0 {
		fragment = strings.ToLower(raw[index+1:])
	}
	if strings.Contains(fragment, "token=") || strings.Contains(fragment, "password=") ||
		strings.Contains(fragment, "signature=") {
		return "", linkInvalidArgument("link fragment appears to contain credentials", "url", raw)
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = parsed.Query().Encode()
	return parsed.String(), nil
}

func InferTodoLinkKind(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.Contains(path, "merge_requests") || strings.Contains(path, "/pull/") || strings.Contains(path, "/codereview/"):
		return "mr"
	case strings.Contains(path, "/cr/") || strings.Contains(path, "change-request"):
		return "cr"
	case strings.Contains(path, "pipeline"):
		return "pipeline"
	case strings.Contains(path, "workitem") || strings.Contains(path, "/issues/"):
		return "workitem"
	case strings.Contains(path, "/release/") || strings.Contains(path, "/releases/") || strings.Contains(path, "/deploy/"):
		return "release"
	case host == "yuque.com" || strings.HasSuffix(host, ".yuque.com") ||
		host == "alidocs.dingtalk.com" || host == "docs.google.com" ||
		strings.HasSuffix(path, ".pdf") || strings.Contains(path, "/docs/"):
		return "document"
	default:
		return ""
	}
}

func validateLinkCall(ctx context.Context, call application.Call) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return linkUnavailable("todo link request canceled", err)
	}
	return call.Validate()
}

func normalizeLinkTodoID(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" || !store.LooksLikeTodoID(raw) {
		return "", linkInvalidArgument("valid todo ID is required", "todo_id", raw)
	}
	return store.NormalizeTodoID(raw), nil
}

func sensitiveTodoLinkParameter(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{
		"token", "secret", "password", "passwd", "signature", "credential", "authorization", "api_key", "apikey", "access_key",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return lower == "auth"
}

func linkTodoNotFound(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
	err.Details = map[string]any{"todo_id": store.NormalizeTodoID(id)}
	return err
}

func linkInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func linkUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}

func linkApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return linkUnavailable(operation, err)
}
