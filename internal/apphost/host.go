// Package apphost composes typed local application operations without a CLI
// subprocess. Adapters supply identity and OS effects, never business rules.
package apphost

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	"github.com/zane-byte-dev/atm/internal/presence"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

type Host struct {
	Version            string
	gate               sync.RWMutex
	configRevision     string
	configRevisionErr  error
	effectsMu          sync.Mutex
	work               work.Service
	effects            work.EffectExecutor
	dataDir            string
	databasePath       string
	configPath         string
	runtimeMu          sync.RWMutex
	jobs               *background.Manager
	presence           *presence.Runtime
	presenceLoader     func(context.Context, string) (dashboard.LiveStatus, error)
	quickUsageMu       sync.Mutex
	quickUsageAt       time.Time
	quickUsageCache    CompanionTodayUsage
	configRefreshError string
}

func New(version string) *Host {
	store.SetStrictReadOnly(true)
	host := &Host{Version: version, work: work.Default,
		dataDir: config.AtmDir, databasePath: config.AtmDB, configPath: config.ConfigPath}
	host.configRevision, host.configRevisionErr = config.Default.ReloadRevision()
	host.restoreDataPaths()
	return host
}

// restoreDataPaths pins this resident host to its startup database. A config
// service write reloads config.json, whose data_dir must not redirect an already
// authenticated listener or invalidate its database-upgrade write gate. Callers
// hold gate exclusively while changing settings and before restoring these.
func (h *Host) restoreDataPaths() {
	config.AtmDir, config.AtmDB, config.ConfigPath = h.dataDir, h.databasePath, h.configPath
}

// SetWorkEffects is composition-time only, before the HTTP listener starts.
// The executable supplies the same controlled projection adapter used by CLI.
func (h *Host) SetWorkEffects(executor work.EffectExecutor) { h.effects = executor }

// ConfigureDataDir is startup-only. An explicit path takes precedence over a
// data_dir value in its config file and never changes HOME or Agent log paths.
func ConfigureDataDir(path string) error {
	if strings.TrimSpace(path) == "" {
		store.SetStrictReadOnly(true)
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	config.AtmDir, config.AtmDB, config.ConfigPath = absolute, filepath.Join(absolute, "atm.db"), filepath.Join(absolute, "config.json")
	// main may already have loaded the default account config. Do not inherit
	// registries or project aliases when selecting an isolated data directory.
	config.ResetBusinessDefaults()
	config.LoadConfig()
	config.AtmDir, config.AtmDB, config.ConfigPath = absolute, filepath.Join(absolute, "atm.db"), filepath.Join(absolute, "config.json")
	store.SetStrictReadOnly(true)
	return nil
}

type ListInput struct {
	Status  string `json:"status,omitempty"`
	Query   string `json:"query,omitempty"`
	Project string `json:"project,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type TodoInput struct {
	TodoID string `json:"todo_id"`
}

type TodoSummary struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	Project       string   `json:"project"`
	Created       string   `json:"created"`
	WakeCondition string   `json:"wake_condition,omitempty"`
	ReviewAt      string   `json:"review_at,omitempty"`
	DependsOn     []string `json:"depends_on"`
	Archived      bool     `json:"archived"`
}

type Image struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	URL       string `json:"url"`
}

type TodoView struct {
	TodoSummary
	Description  string           `json:"description"`
	Links        []store.TodoLink `json:"links"`
	Images       []Image          `json:"images"`
	Closed       *string          `json:"closed"`
	ClosedReason *string          `json:"closed_reason"`
	Creator      string           `json:"creator,omitempty"`
}

type ListResult struct {
	Items    []TodoSummary  `json:"items"`
	Total    int            `json:"total"`
	Limit    int            `json:"limit"`
	Offset   int            `json:"offset"`
	Projects []string       `json:"projects"`
	Counts   map[string]int `json:"counts"`
}

type ShowResult struct {
	Todo       TodoView                  `json:"todo"`
	ETag       string                    `json:"etag"`
	LatestPlan *work.PlanSnapshot        `json:"latest_plan"`
	Bindings   []work.TodoSessionBinding `json:"bindings"`
	Sessions   []work.TodoBoundSession   `json:"sessions"`
	Summary    *work.SessionSummary      `json:"summary"`
}

type DocResult struct {
	Exists  bool   `json:"exists"`
	Content string `json:"content"`
}

func (h *Host) ListTodos(ctx context.Context, call application.Call, input ListInput) (ListResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return ListResult{}, err
	}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 200 {
		return ListResult{}, invalid("limit must be 1–200 and offset non-negative")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	status := strings.TrimSpace(input.Status)
	switch status {
	case "", "all", "open", "in_progress", "review", "done", "archived":
	default:
		return ListResult{}, invalid("invalid todo status")
	}
	result := ListResult{Items: []TodoSummary{}, Limit: input.Limit, Offset: input.Offset, Projects: []string{}, Counts: map[string]int{"all": 0, "open": 0, "in_progress": 0, "review": 0, "done": 0, "archived": 0}}
	listed, err := h.work.List(ctx, call, work.ListInput{Status: "all"})
	if err != nil {
		if errors.Is(err, store.ErrDatabaseMissing) {
			return result, nil
		}
		return ListResult{}, err
	}
	projects := map[string]bool{}
	for _, todo := range listed.Todos {
		if todo.Project != "" {
			projects[todo.Project] = true
		}
	}
	filtered := make([]work.Todo, 0, len(listed.Todos))
	for _, todo := range listed.Todos {
		if !config.ProjectMatches(todo.Project, input.Project) || work.QueryRelevance(todo, input.Query) < 0 {
			continue
		}
		result.Counts[todo.Status]++
		result.Counts["all"]++
		if status == "archived" || status != "" && status != "all" && todo.Status != status {
			continue
		}
		filtered = append(filtered, todo)
	}
	// Counts describe the entire query/project scope, independent of the active
	// status tab and pagination. The archive count must also work from /tasks.
	archived, err := h.work.List(ctx, call, work.ListInput{Status: "archived"})
	if err != nil {
		return ListResult{}, err
	}
	for _, todo := range archived.Archived {
		if todo.Project != "" {
			projects[todo.Project] = true
		}
		if !config.ProjectMatches(todo.Project, input.Project) || work.QueryRelevance(todo.Todo, input.Query) < 0 {
			continue
		}
		result.Counts["archived"]++
		if status == "archived" {
			filtered = append(filtered, todo.Todo)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Created != filtered[j].Created {
			return filtered[i].Created > filtered[j].Created
		}
		left, _ := strconv.ParseInt(strings.TrimPrefix(filtered[i].ID, "t"), 10, 64)
		right, _ := strconv.ParseInt(strings.TrimPrefix(filtered[j].ID, "t"), 10, 64)
		if left != right {
			return left > right
		}
		return filtered[i].ID > filtered[j].ID
	})
	result.Total = len(filtered)
	start := min(input.Offset, len(filtered))
	end := min(start+input.Limit, len(filtered))
	for _, todo := range filtered[start:end] {
		item := summary(todo)
		item.Archived = status == "archived"
		result.Items = append(result.Items, item)
	}
	for project := range projects {
		result.Projects = append(result.Projects, project)
	}
	sort.Strings(result.Projects)
	return result, nil
}

func (h *Host) ShowTodo(ctx context.Context, call application.Call, input TodoInput) (ShowResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return ShowResult{}, err
	}
	result, err := h.work.ShowIncludingArchived(ctx, call, work.ShowInput{TodoID: input.TodoID})
	if err != nil {
		return ShowResult{}, err
	}
	if result.Bindings == nil {
		result.Bindings = []work.TodoSessionBinding{}
	}
	if result.Sessions == nil {
		result.Sessions = []work.TodoBoundSession{}
	}
	todo := view(result.Todo)
	todo.Archived = result.Archived
	return ShowResult{Todo: todo, ETag: work.TodoETag(result.Todo), LatestPlan: result.LatestPlan, Bindings: result.Bindings, Sessions: result.Sessions, Summary: result.Summary}, nil
}

func (h *Host) Doc(ctx context.Context, call application.Call, input TodoInput) (DocResult, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validateTodo(ctx, call, input.TodoID); err != nil {
		return DocResult{}, err
	}
	result, err := h.work.ReadDocument(ctx, call, work.ShowInput{TodoID: input.TodoID})
	return DocResult{Exists: result.Exists, Content: result.Content}, err
}

func summary(todo work.Todo) TodoSummary {
	depends := append([]string{}, todo.DependsOn...)
	return TodoSummary{ID: todo.ID, Title: todo.Title, Status: todo.Status, Priority: todo.Priority, Project: todo.Project, Created: todo.Created, WakeCondition: todo.WakeCondition, ReviewAt: todo.ReviewAt, DependsOn: depends}
}

func view(todo work.Todo) TodoView {
	result := TodoView{TodoSummary: summary(todo), Description: todo.Description, Links: append([]store.TodoLink{}, todo.Links...), Images: []Image{}, Closed: todo.Closed, ClosedReason: todo.ClosedReason, Creator: todo.Creator}
	for _, image := range todo.Images {
		id := base64.RawURLEncoding.EncodeToString([]byte(todo.ID + "/" + image.StoredName))
		result.Images = append(result.Images, Image{ID: id, Name: image.Name, MediaType: image.MediaType, SizeBytes: image.SizeBytes, URL: "/api/v1/attachments/" + id})
	}
	return result
}

func (h *Host) Attachment(ctx context.Context, call application.Call, id string) (string, string, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", invalid("invalid attachment ID")
	}
	parts := strings.Split(string(decoded), "/")
	if len(parts) != 2 || parts[1] == "" || filepath.Base(parts[1]) != parts[1] {
		return "", "", invalid("invalid attachment ID")
	}
	if err := validateTodo(ctx, call, parts[0]); err != nil {
		return "", "", err
	}
	result, err := h.work.ShowIncludingArchived(ctx, call, work.ShowInput{TodoID: parts[0]})
	if err != nil {
		return "", "", err
	}
	for _, image := range result.Todo.Images {
		if image.StoredName != parts[1] {
			continue
		}
		root, err := filepath.EvalSymlinks(filepath.Join(config.AtmDir, "todos", "assets", result.Todo.ID))
		if err != nil {
			return "", "", unavailable(err)
		}
		dataRoot, err := filepath.EvalSymlinks(config.AtmDir)
		if err != nil {
			return "", "", unavailable(err)
		}
		rootRel, err := filepath.Rel(dataRoot, root)
		if err != nil || rootRel == ".." || strings.HasPrefix(rootRel, ".."+string(filepath.Separator)) {
			return "", "", application.NewError(application.CodeForbidden, "attachment directory is outside the data directory")
		}
		path, err := filepath.EvalSymlinks(image.Path)
		if err != nil {
			return "", "", unavailable(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", "", application.NewError(application.CodeForbidden, "attachment is outside its managed directory")
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", "", unavailable(err)
		}
		if !info.Mode().IsRegular() {
			return "", "", invalid("attachment is not a regular file")
		}
		return path, image.MediaType, nil
	}
	return "", "", application.NewError(application.CodeNotFound, "attachment not found")
}

func validate(ctx context.Context, call application.Call) error {
	if ctx == nil {
		return invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return call.Validate()
}
func validateTodo(ctx context.Context, call application.Call, id string) error {
	if err := validate(ctx, call); err != nil {
		return err
	}
	if !store.LooksLikeTodoID(id) {
		return invalid("a valid todo_id is required")
	}
	return nil
}
func invalid(message string) error {
	return application.NewError(application.CodeInvalidArgument, message)
}
func unavailable(cause error) error {
	return application.WrapError(application.CodeUnavailable, fmt.Sprintf("local data unavailable: %v", cause), cause)
}
