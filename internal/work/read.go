package work

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

// Read-model aliases keep the application contract explicit without forcing
// adapters to import the persistence package. They intentionally preserve the
// established JSON shapes while Work takes ownership of how they are loaded.
type Todo = store.Todo
type ArchivedTodo = store.ArchivedTodo
type TodoSessionBinding = store.TodoSessionBinding
type TodoBoundSession = store.TodoBoundSession

type PlanItem struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

// PlanSnapshot reserves the typed read-model slot used by Show and Context.
// Plan persistence is not part of this slice, so LatestPlan remains nil until
// the Plan use case exists; omitempty keeps today's wire contracts unchanged.
type PlanSnapshot struct {
	TodoID      string                `json:"todo_id"`
	Revision    int64                 `json:"revision"`
	Explanation string                `json:"explanation,omitempty"`
	Items       []PlanItem            `json:"items"`
	CreatedAt   int64                 `json:"created_at"`
	ActorKind   application.ActorKind `json:"actor_kind"`
	Origin      application.Origin    `json:"origin"`
	SessionID   string                `json:"session_id,omitempty"`
	BindingID   int64                 `json:"binding_id,omitempty"`
	Agent       string                `json:"agent,omitempty"`
}

type ShowInput struct {
	TodoID string `json:"todo_id,omitempty"`
}

type DocumentSnapshot struct {
	Path           string   `json:"path"`
	Exists         bool     `json:"exists"`
	Content        string   `json:"-"`
	RecentProgress []string `json:"recent_progress,omitempty"`
}

type SessionSummary struct {
	Sessions  int     `json:"sessions"`
	Queries   int     `json:"queries"`
	ToolCalls int     `json:"tool_calls"`
	CostUSD   float64 `json:"cost_usd"`
}

type ShowResult struct {
	Todo       Todo                 `json:"todo"`
	Document   DocumentSnapshot     `json:"document"`
	Bindings   []TodoSessionBinding `json:"bindings,omitempty"`
	Sessions   []TodoBoundSession   `json:"sessions,omitempty"`
	Summary    *SessionSummary      `json:"summary,omitempty"`
	LatestPlan *PlanSnapshot        `json:"latest_plan,omitempty"`
}

type ListInput struct {
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Project  string `json:"project,omitempty"`
	Query    string `json:"query,omitempty"`
	Creator  string `json:"creator,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

type ListKind string

const (
	ListKindWorking  ListKind = "working"
	ListKindArchived ListKind = "archived"
)

type ListResult struct {
	Kind           ListKind
	Todos          []Todo
	Archived       []ArchivedTodo
	DocumentExists map[string]bool
}

type DocInput struct {
	TodoID     string `json:"todo_id,omitempty"`
	Initialize bool   `json:"initialize,omitempty"`
}

type DocResult struct {
	TodoID  string `json:"todo_id"`
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Created bool   `json:"created,omitempty"`
	Content string `json:"content,omitempty"`
}

func (service Service) Show(ctx context.Context, call application.Call, input ShowInput) (ShowResult, error) {
	_, todo, err := loadTodoForRead(ctx, call, input.TodoID)
	if err != nil {
		return ShowResult{}, err
	}

	bindings, err := store.ListTodoSessionBindings(todo.ID)
	if err != nil {
		return ShowResult{}, readApplicationError("load todo session bindings", err)
	}
	db, err := store.OpenReadOnly()
	if err != nil {
		return ShowResult{}, readApplicationError("open todo read model", err)
	}
	defer db.Close()
	sessions, err := store.FindSessionsForTodo(db, todo.ID)
	if err != nil {
		return ShowResult{}, readApplicationError("load sessions for todo", err)
	}
	if err := nameTodoSessions(db, sessions); err != nil {
		return ShowResult{}, readApplicationError("name sessions for todo", err)
	}

	result := ShowResult{
		Todo:     *todo,
		Document: inspectTodoDocument(todo.ID, 5),
		Bindings: bindings,
		Sessions: sessions,
	}
	result.LatestPlan, err = latestPlanSnapshot(todo.ID)
	if err != nil {
		return ShowResult{}, readApplicationError("load latest todo plan", err)
	}
	if len(sessions) > 0 {
		summary := SessionSummary{Sessions: len(sessions)}
		for _, session := range sessions {
			summary.Queries += session.Queries
			summary.ToolCalls += session.ToolCalls
			summary.CostUSD += session.CostUSD
		}
		result.Summary = &summary
	}
	return result, nil
}

func (service Service) List(ctx context.Context, call application.Call, input ListInput) (ListResult, error) {
	if _, err := validateReadCall(ctx, call); err != nil {
		return ListResult{}, err
	}

	status, activeOnly, archived, err := normalizeListStatus(input.Status)
	if err != nil {
		return ListResult{}, err
	}
	priority := strings.TrimSpace(strings.ToUpper(input.Priority))
	if priority != "" && priority != "P0" && priority != "P1" && priority != "P2" {
		return ListResult{}, readInvalidArgument("priority must be P0, P1, or P2", "priority", input.Priority)
	}
	creator, err := store.NormalizeTodoCreator(input.Creator)
	if err != nil {
		return ListResult{}, readInvalidArgument(err.Error(), "creator", input.Creator)
	}
	if input.Offset < 0 {
		return ListResult{}, readInvalidArgument("offset must not be negative", "offset", input.Offset)
	}
	if input.Limit < 0 {
		return ListResult{}, readInvalidArgument("limit must not be negative", "limit", input.Limit)
	}

	if archived {
		values, err := store.LoadArchivedTodos()
		if err != nil {
			return ListResult{}, readApplicationError("load archived todos", err)
		}
		filtered := make([]ArchivedTodo, 0, len(values))
		for _, todo := range values {
			if priority != "" && todo.Priority != priority {
				continue
			}
			if !config.ProjectMatches(todo.Project, input.Project) || creator != "" && todo.Creator != creator {
				continue
			}
			if QueryRelevance(todo.Todo, input.Query) < 0 {
				continue
			}
			filtered = append(filtered, todo)
		}
		filtered = paginateReadResult(filtered, input.Offset, input.Limit)
		return ListResult{Kind: ListKindArchived, Archived: filtered}, nil
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return ListResult{}, readApplicationError("load todos", err)
	}
	filtered := make([]Todo, 0, len(todos.Items))
	for _, todo := range todos.Items {
		if activeOnly && !store.TodoIsActive(todo) || status != "" && todo.Status != status {
			continue
		}
		if priority != "" && todo.Priority != priority {
			continue
		}
		if !config.ProjectMatches(todo.Project, input.Project) || creator != "" && todo.Creator != creator {
			continue
		}
		if QueryRelevance(todo, input.Query) < 0 {
			continue
		}
		filtered = append(filtered, todo)
	}
	if strings.TrimSpace(input.Query) != "" {
		sort.SliceStable(filtered, func(left, right int) bool {
			return QueryRelevance(filtered[left], input.Query) > QueryRelevance(filtered[right], input.Query)
		})
	}
	filtered = paginateReadResult(filtered, input.Offset, input.Limit)
	documents := make(map[string]bool, len(filtered))
	for _, todo := range filtered {
		documents[todo.ID] = todoDocumentExists(todo.ID)
	}
	return ListResult{Kind: ListKindWorking, Todos: filtered, DocumentExists: documents}, nil
}

func (service Service) Doc(ctx context.Context, call application.Call, input DocInput) (DocResult, error) {
	_, todo, err := loadTodoForRead(ctx, call, input.TodoID)
	if err != nil {
		return DocResult{}, err
	}
	if input.Initialize {
		if todoDocumentExists(todo.ID) {
			err := application.NewError(application.CodeConflict, "todo document already exists")
			err.Details = map[string]any{"todo_id": todo.ID, "path": store.TodoDocPath(todo.ID)}
			return DocResult{}, err
		}
		path, err := store.InitTodoDoc(todo)
		if err != nil {
			return DocResult{}, readApplicationError("initialize todo document", err)
		}
		return DocResult{TodoID: todo.ID, Path: path, Exists: true, Created: true}, nil
	}
	path, err := syncTodoDocumentWithLatestPlan(todo)
	if err != nil {
		return DocResult{}, readApplicationError("materialize todo document", err)
	}
	content, err := store.ReadTodoDoc(todo.ID)
	if err != nil {
		return DocResult{}, readApplicationError("read todo document", err)
	}
	return DocResult{TodoID: todo.ID, Path: path, Exists: true, Content: content}, nil
}

// QueryRelevance preserves the list query's AND matching and field weighting.
// Document lookup lives here because document content is part of the Work read
// model, not something a Cobra adapter should discover on its own.
func QueryRelevance(todo Todo, rawQuery string) int {
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	if query == "" {
		return 0
	}
	document := ""
	if loaded, err := store.ReadTodoDoc(todo.ID); err == nil {
		document = strings.ToLower(loaded)
	}
	id := strings.ToLower(todo.ID)
	title := strings.ToLower(todo.Title)
	description := strings.ToLower(todo.Description)
	project := strings.ToLower(todo.Project)
	source := strings.ToLower(todo.Source)
	haystack := strings.Join([]string{id, title, description, project, source, document}, "\n")
	terms := strings.Fields(query)
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return -1
		}
	}

	score := 0
	if id == query {
		score += 1000
	} else if strings.Contains(id, query) {
		score += 300
	}
	if strings.Contains(title, query) {
		score += 500
		titleRunes, queryRunes := len([]rune(title)), len([]rune(query))
		if titleRunes > 0 {
			score += 1000 * queryRunes / titleRunes
		}
		if position := strings.Index(title, query); position >= 0 {
			score += 100 / (1 + len([]rune(title[:position])))
		}
	}
	if strings.Contains(description, query) {
		score += 180
	}
	if project == query {
		score += 100
	}
	if source == query {
		score += 60
	}
	if strings.Contains(document, query) {
		score += 30
	}
	for _, term := range terms {
		if strings.Contains(title, term) {
			score += 120
		}
		if strings.Contains(description, term) {
			score += 40
		}
		if strings.Contains(document, term) {
			score += 5
		}
	}
	return score
}

func loadTodoForRead(ctx context.Context, call application.Call, rawID string) (*store.TodoFile, *Todo, error) {
	if _, err := validateReadCall(ctx, call); err != nil {
		return nil, nil, err
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return nil, nil, readApplicationError("load todos", err)
	}
	id := strings.TrimSpace(rawID)
	if id == "" || strings.EqualFold(id, "current") {
		sessionID := strings.TrimSpace(call.Actor.SessionID)
		if sessionID == "" {
			return nil, nil, readInvalidArgument("session ID is required to resolve the current todo", "actor.session_id", call.Actor.SessionID)
		}
		binding, err := store.CurrentTodoBinding(sessionID)
		if err != nil {
			return nil, nil, readApplicationError("resolve current todo binding", err)
		}
		if binding == nil {
			err := application.NewError(application.CodeNotFound,
				"no todo bound to current session; run `atm todo match --prompt` then `atm session bind <id>`")
			err.Details = map[string]any{"session_id": sessionID}
			return nil, nil, err
		}
		id = binding.TodoID
	}
	canonical := store.NormalizeTodoID(id)
	todo := store.FindTodo(todos, canonical)
	if todo == nil {
		cause := store.TodoNotFoundError(todos, id)
		err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
		err.Details = map[string]any{"todo_id": canonical}
		return nil, nil, err
	}
	return todos, todo, nil
}

func validateReadCall(ctx context.Context, call application.Call) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if err := call.Validate(); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func normalizeListStatus(value string) (status string, activeOnly, archived bool, err error) {
	status = strings.TrimSpace(strings.ToLower(value))
	activeOnly = status == ""
	switch status {
	case "":
		return status, activeOnly, false, nil
	case "all":
		return "", false, false, nil
	case "archived", "trashed":
		return status, false, true, nil
	case store.TodoStatusOpen, store.TodoStatusInProgress, store.TodoStatusReview, store.TodoStatusDone:
		return status, false, false, nil
	default:
		return "", false, false, readInvalidArgument(
			"invalid todo status (use open, in_progress, review, done, archived, or all)",
			"status", value,
		)
	}
}

func paginateReadResult[T any](values []T, offset, limit int) []T {
	if offset >= len(values) {
		return []T{}
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return values[offset:end]
}

func todoDocumentExists(id string) bool {
	_, err := os.Stat(store.TodoDocPath(id))
	return err == nil
}

func inspectTodoDocument(id string, recentLimit int) DocumentSnapshot {
	document := DocumentSnapshot{Path: store.TodoDocPath(id)}
	if !todoDocumentExists(id) {
		return document
	}
	document.Exists = true
	content, err := store.ReadTodoDoc(id)
	if err != nil {
		return document
	}
	document.Content = content
	document.RecentProgress = extractTodoProgress(content, recentLimit)
	return document
}

func extractTodoProgress(content string, limit int) []string {
	inProgress := false
	logs := []string{}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "## 进展") {
			inProgress = true
			continue
		}
		if inProgress && strings.HasPrefix(line, "## ") {
			break
		}
		if inProgress && strings.HasPrefix(line, "- [") {
			logs = append(logs, line)
		}
	}
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}
	return logs
}

func nameTodoSessions(db *sql.DB, sessions []TodoBoundSession) error {
	var codexTitles map[string]string
	pending := []string{}
	for index := range sessions {
		session := &sessions[index]
		if session.Summary != "" {
			continue
		}
		if strings.EqualFold(session.Agent, "codex") {
			if codexTitles == nil {
				codexTitles = parser.CodexThreadTitles()
			}
			if title := strings.TrimSpace(codexTitles[session.SessionID]); title != "" {
				session.Summary = title
				continue
			}
		}
		if session.IndexedID != "" {
			pending = append(pending, session.IndexedID)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	messages, err := store.EarliestUserMessages(db, pending, 8)
	if err != nil {
		return err
	}
	for index := range sessions {
		session := &sessions[index]
		if session.Summary != "" {
			continue
		}
		for _, message := range messages[session.IndexedID] {
			visible := parser.FirstLine(parser.VisibleUserText(message))
			runes := []rune(visible)
			if len(runes) > 120 {
				visible = string(runes[:120])
			}
			if visible != "" {
				session.Summary = visible
				break
			}
		}
	}
	return nil
}

func readApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	wrapped := application.WrapError(application.CodeUnavailable, operation, err)
	wrapped.Retryable = true
	return wrapped
}

func readInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}
