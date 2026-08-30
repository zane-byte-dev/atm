package work

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// MetadataEffectKind describes an after-commit signal produced by Todo metadata
// mutations. It deliberately says what happened without choosing a desktop,
// terminal, or JSON presentation; adapters decide whether and how to notify a
// human.
type MetadataEffectKind string

const (
	MetadataEffectCreated       MetadataEffectKind = "todo_created"
	MetadataEffectEnteredReview MetadataEffectKind = "todo_entered_review"
)

type MetadataEffect struct {
	Kind MetadataEffectKind `json:"kind"`
	Todo Todo               `json:"todo"`
}

type AddInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Project     string   `json:"project,omitempty"`
	Source      string   `json:"source,omitempty"`
	Creator     string   `json:"creator,omitempty"`
	OnDone      string   `json:"on_done,omitempty"`
	ImagePaths  []string `json:"image_paths,omitempty"`
}

type AddResult struct {
	Todo    Todo             `json:"todo"`
	Effects []MetadataEffect `json:"-"`
}

type BatchAddInput struct {
	Defaults BatchAddDefaults `json:"defaults"`
	Items    []BatchAddItem   `json:"items"`
}

type BatchAddDefaults struct {
	Priority string `json:"priority,omitempty"`
	Project  string `json:"project,omitempty"`
	Source   string `json:"source,omitempty"`
	Creator  string `json:"creator,omitempty"`
}

type BatchAddItem struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Project     string `json:"project,omitempty"`
	Source      string `json:"source,omitempty"`
	Creator     string `json:"creator,omitempty"`
}

type BatchAddResult struct {
	Todos   []Todo           `json:"todos"`
	Effects []MetadataEffect `json:"-"`
}

// EditPatch uses pointers so transports can distinguish an omitted field from
// deliberately clearing a string-valued field.
type EditPatch struct {
	Title            *string `json:"title,omitempty"`
	Description      *string `json:"description,omitempty"`
	Priority         *string `json:"priority,omitempty"`
	Project          *string `json:"project,omitempty"`
	Source           *string `json:"source,omitempty"`
	Creator          *string `json:"creator,omitempty"`
	Status           *string `json:"status,omitempty"`
	WakeCondition    *string `json:"wake_condition,omitempty"`
	ReviewAt         *string `json:"review_at,omitempty"`
	MaintenanceLimit *int    `json:"maintenance_limit,omitempty"`
}

type EditInput struct {
	TodoID string    `json:"todo_id"`
	Patch  EditPatch `json:"patch"`
}

type EditResult struct {
	Todo           Todo             `json:"todo"`
	PreviousStatus string           `json:"previous_status"`
	Effects        []MetadataEffect `json:"-"`
}

type MoveInput struct {
	TodoID  string `json:"todo_id"`
	Project string `json:"project"`
}

type MoveResult struct {
	Todo            Todo   `json:"todo"`
	PreviousProject string `json:"previous_project,omitempty"`
}

type normalizedAdd struct {
	title       string
	description string
	priority    string
	status      string
	project     string
	source      string
	creator     string
	onDone      string
	imagePaths  []string
}

// Add validates and creates one Todo, including managed image import, in the
// WorkState transaction that assigns its durable ID. If the database write
// fails after files were copied, those files are rolled back. The Todo document
// is materialized only after a successful commit.
func (service Service) Add(ctx context.Context, call application.Call, input AddInput) (AddResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return AddResult{}, err
	}
	normalized, err := normalizeAdd(call, input)
	if err != nil {
		return AddResult{}, err
	}

	result := AddResult{}
	var rollbackImageFiles func()
	err = service.Mutate(func(transaction *Transaction) error {
		todo := Todo{
			ID:          store.NextTodoID(transaction.Todos()),
			Title:       normalized.title,
			Description: normalized.description,
			Priority:    normalized.priority,
			Status:      normalized.status,
			Project:     normalized.project,
			Created:     store.Today(),
			Source:      normalized.source,
			Creator:     normalized.creator,
			OnDone:      normalized.onDone,
		}
		images, cleanup, importErr := store.ImportTodoImages(todo.ID, normalized.imagePaths)
		if importErr != nil {
			return metadataInvalidArgument(importErr.Error(), "image_paths", normalized.imagePaths)
		}
		rollbackImageFiles = cleanup
		todo.Images = images
		transaction.Todos().Items = append(transaction.Todos().Items, todo)
		result.Todo = cloneTodo(todo)
		return nil
	})
	if err != nil {
		if rollbackImageFiles != nil {
			rollbackImageFiles()
		}
		return AddResult{}, metadataApplicationError("add todo", err)
	}
	if _, err := store.EnsureTodoDoc(&result.Todo); err != nil {
		return AddResult{}, metadataUnavailable("materialize todo document after add", err)
	}
	result.Effects = metadataEffectsForCreate(result.Todo)
	return result, nil
}

// BatchAdd validates the complete batch before entering one WorkState
// transaction. One invalid item therefore cannot leave an earlier item applied.
func (service Service) BatchAdd(ctx context.Context, call application.Call, input BatchAddInput) (BatchAddResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return BatchAddResult{}, err
	}
	if len(input.Items) == 0 {
		return BatchAddResult{}, metadataInvalidArgument("no items in batch input", "items", input.Items)
	}

	defaultCreator, err := normalizeCreator(call, input.Defaults.Creator)
	if err != nil {
		return BatchAddResult{}, err
	}
	defaults := AddInput{
		Priority: valueOr(input.Defaults.Priority, "P2"),
		Project:  input.Defaults.Project,
		Source:   input.Defaults.Source,
		Creator:  defaultCreator,
	}
	normalized := make([]normalizedAdd, 0, len(input.Items))
	for index, item := range input.Items {
		if strings.TrimSpace(item.Title) == "" {
			continue
		}
		creator := defaults.Creator
		if strings.TrimSpace(item.Creator) != "" {
			creator = item.Creator
		}
		value, itemErr := normalizeAdd(call, AddInput{
			Title:       item.Title,
			Description: item.Description,
			Priority:    valueOr(item.Priority, defaults.Priority),
			Project:     valueOr(item.Project, defaults.Project),
			Source:      valueOr(item.Source, defaults.Source),
			Creator:     creator,
		})
		if itemErr != nil {
			return BatchAddResult{}, metadataBatchItemError(index, item.Title, itemErr)
		}
		normalized = append(normalized, value)
	}

	result := BatchAddResult{Todos: make([]Todo, 0, len(normalized))}
	err = service.Mutate(func(transaction *Transaction) error {
		for _, item := range normalized {
			todo := Todo{
				ID:          store.NextTodoID(transaction.Todos()),
				Title:       item.title,
				Description: item.description,
				Priority:    item.priority,
				Status:      item.status,
				Project:     item.project,
				Created:     store.Today(),
				Source:      item.source,
				Creator:     item.creator,
			}
			transaction.Todos().Items = append(transaction.Todos().Items, todo)
			result.Todos = append(result.Todos, cloneTodo(todo))
		}
		return nil
	})
	if err != nil {
		return BatchAddResult{}, metadataApplicationError("batch add todos", err)
	}
	for index := range result.Todos {
		if _, err := store.EnsureTodoDoc(&result.Todos[index]); err != nil {
			return BatchAddResult{}, metadataUnavailable(
				fmt.Sprintf("materialize todo document %s after batch add", result.Todos[index].ID), err,
			)
		}
		result.Effects = append(result.Effects, metadataEffectsForCreate(result.Todos[index])...)
	}
	return result, nil
}

// Edit applies a sparse metadata patch and owns the lifecycle rule that a Todo
// leaving in_progress cannot remain actively bound to an Agent session.
func (service Service) Edit(ctx context.Context, call application.Call, input EditInput) (EditResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return EditResult{}, err
	}
	if strings.TrimSpace(input.TodoID) == "" {
		return EditResult{}, metadataInvalidArgument("todo ID is required", "todo_id", input.TodoID)
	}
	patch, err := normalizeEditPatch(input.Patch)
	if err != nil {
		return EditResult{}, err
	}
	if !patch.changed() {
		return EditResult{}, metadataInvalidArgument(
			"nothing to update, use --title, --desc, --priority, --project, --source, --status, --wake, --review-at, or --maintenance-limit",
			"patch", input.Patch,
		)
	}

	result := EditResult{}
	err = service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(input.TodoID)
		if findErr != nil {
			return metadataTodoNotFound(input.TodoID, findErr)
		}
		result.PreviousStatus = todo.Status
		patch.apply(todo)
		if patch.MaintenanceLimit != nil {
			if *patch.MaintenanceLimit == 0 {
				todo.Tags = withoutTodoTag(todo.Tags, store.TodoTagMaintenance)
			} else {
				// maintenance is a scope tag on work that is still being done, so a
				// closed Todo cannot take one. Clearing it (limit 0) stays allowed,
				// because tidying a tag off finished work is not the same claim.
				// This rule came from the `todo maintain` use case, which this field
				// replaced; the merge is meant to keep its rules, not drop them.
				if !store.TodoIsActive(*todo) {
					return metadataConflict(
						fmt.Sprintf("cannot set a maintenance limit on todo %s with status %s", todo.ID, todo.Status),
						todo.ID, todo.Status,
					)
				}
				store.AddTodoTag(todo, store.TodoTagMaintenance)
			}
		}
		if todo.Status != store.TodoStatusInProgress &&
			((patch.WakeCondition != nil && *patch.WakeCondition != "") ||
				(patch.ReviewAt != nil && *patch.ReviewAt != "")) {
			return metadataInvalidArgument("wake metadata is only valid for in_progress todos", "status", todo.Status)
		}
		if todo.Status != store.TodoStatusInProgress {
			todo.WakeCondition = ""
			todo.ReviewAt = ""
		}
		shouldUnbind := patch.Status != nil && todo.Status != store.TodoStatusInProgress
		shouldUnbind = shouldUnbind || (todo.Status == store.TodoStatusInProgress &&
			(todo.WakeCondition != "" || todo.ReviewAt != "") &&
			(patch.WakeCondition != nil || patch.ReviewAt != nil))
		if shouldUnbind {
			if _, unbindErr := transaction.UnbindTodoSessions(todo.ID, "status-style:"+todo.Status); unbindErr != nil {
				return fmt.Errorf("unbind todo sessions before status change: %w", unbindErr)
			}
		}
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if err != nil {
		return EditResult{}, metadataApplicationError("edit todo", err)
	}
	if err := syncTodoDocumentIfPresent(&result.Todo); err != nil {
		return EditResult{}, err
	}
	if result.PreviousStatus != store.TodoStatusReview && result.Todo.Status == store.TodoStatusReview {
		result.Effects = []MetadataEffect{{Kind: MetadataEffectEnteredReview, Todo: cloneTodo(result.Todo)}}
	}
	return result, nil
}

// Move is a dedicated project mutation so transports do not have to construct
// an EditPatch merely to express a project move.
func (service Service) Move(ctx context.Context, call application.Call, input MoveInput) (MoveResult, error) {
	if err := validateMetadataCall(ctx, call); err != nil {
		return MoveResult{}, err
	}
	if strings.TrimSpace(input.TodoID) == "" {
		return MoveResult{}, metadataInvalidArgument("todo ID is required", "todo_id", input.TodoID)
	}
	project := config.CanonicalProject(input.Project)
	if project == "" {
		return MoveResult{}, metadataInvalidArgument("target project is required", "project", input.Project)
	}

	result := MoveResult{}
	err := service.Mutate(func(transaction *Transaction) error {
		todo, findErr := transaction.Todo(input.TodoID)
		if findErr != nil {
			return metadataTodoNotFound(input.TodoID, findErr)
		}
		result.PreviousProject = todo.Project
		todo.Project = project
		result.Todo = cloneTodo(*todo)
		return nil
	})
	if err != nil {
		return MoveResult{}, metadataApplicationError("move todo", err)
	}
	if err := syncTodoDocumentIfPresent(&result.Todo); err != nil {
		return MoveResult{}, err
	}
	return result, nil
}

type normalizedEditPatch struct {
	Title            *string
	Description      *string
	Priority         *string
	Project          *string
	Source           *string
	Creator          *string
	Status           *string
	WakeCondition    *string
	ReviewAt         *string
	MaintenanceLimit *int
}

func (patch normalizedEditPatch) changed() bool {
	return patch.Title != nil || patch.Description != nil || patch.Priority != nil || patch.Project != nil ||
		patch.Source != nil || patch.Creator != nil || patch.Status != nil || patch.WakeCondition != nil || patch.ReviewAt != nil || patch.MaintenanceLimit != nil
}

func (patch normalizedEditPatch) apply(todo *Todo) {
	if patch.Title != nil {
		todo.Title = *patch.Title
	}
	if patch.Description != nil {
		todo.Description = *patch.Description
	}
	if patch.Priority != nil {
		todo.Priority = *patch.Priority
	}
	if patch.Project != nil {
		todo.Project = *patch.Project
	}
	if patch.Source != nil {
		todo.Source = *patch.Source
	}
	if patch.Creator != nil {
		todo.Creator = *patch.Creator
	}
	if patch.Status != nil {
		todo.Status = *patch.Status
	}
	if patch.WakeCondition != nil {
		todo.WakeCondition = *patch.WakeCondition
	}
	if patch.ReviewAt != nil {
		todo.ReviewAt = *patch.ReviewAt
	}
	if patch.MaintenanceLimit != nil {
		todo.MaintenanceLimit = *patch.MaintenanceLimit
	}
}

func normalizeEditPatch(input EditPatch) (normalizedEditPatch, error) {
	patch := normalizedEditPatch{}
	if input.Title != nil {
		value := strings.TrimSpace(*input.Title)
		if value == "" {
			return patch, metadataInvalidArgument("todo title is required", "title", *input.Title)
		}
		patch.Title = &value
	}
	if input.Description != nil {
		if err := store.ValidateTodoDescription(*input.Description); err != nil {
			return patch, metadataInvalidArgument(err.Error(), "description", *input.Description)
		}
		value := *input.Description
		patch.Description = &value
	}
	if input.Priority != nil {
		value, err := normalizePriority(*input.Priority)
		if err != nil {
			return patch, err
		}
		patch.Priority = &value
	}
	if input.Project != nil {
		value := config.CanonicalProject(*input.Project)
		patch.Project = &value
	}
	if input.Source != nil {
		value := strings.TrimSpace(*input.Source)
		patch.Source = &value
	}
	if input.Creator != nil {
		value, err := store.NormalizeTodoCreator(*input.Creator)
		if err != nil {
			return patch, metadataInvalidArgument(err.Error(), "creator", *input.Creator)
		}
		patch.Creator = &value
	}
	if input.Status != nil {
		value, err := normalizeMetadataStatus(*input.Status)
		if err != nil {
			return patch, err
		}
		patch.Status = &value
	}
	if input.WakeCondition != nil {
		value := strings.TrimSpace(*input.WakeCondition)
		patch.WakeCondition = &value
	}
	if input.ReviewAt != nil {
		value := strings.TrimSpace(*input.ReviewAt)
		if err := ValidateReviewAt(value); err != nil {
			return patch, metadataInvalidArgument(err.Error(), "review_at", *input.ReviewAt)
		}
		patch.ReviewAt = &value
	}
	if input.MaintenanceLimit != nil {
		if *input.MaintenanceLimit < 0 {
			return patch, metadataInvalidArgument("maintenance limit cannot be negative", "maintenance_limit", *input.MaintenanceLimit)
		}
		value := *input.MaintenanceLimit
		patch.MaintenanceLimit = &value
	}
	return patch, nil
}

func withoutTodoTag(tags []string, target string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag != target {
			result = append(result, tag)
		}
	}
	return result
}

func normalizeAdd(call application.Call, input AddInput) (normalizedAdd, error) {
	normalized := normalizedAdd{
		title:       strings.TrimSpace(input.Title),
		description: input.Description,
		status:      store.TodoStatusOpen,
		project:     config.CanonicalProject(input.Project),
		source:      strings.TrimSpace(input.Source),
		onDone:      input.OnDone,
		imagePaths:  append([]string(nil), input.ImagePaths...),
	}
	if normalized.title == "" {
		return normalized, metadataInvalidArgument("todo title is required", "title", input.Title)
	}
	if err := store.ValidateTodoDescription(normalized.description); err != nil {
		return normalized, metadataInvalidArgument(err.Error(), "description", input.Description)
	}
	priority, err := normalizePriority(valueOr(input.Priority, "P2"))
	if err != nil {
		return normalized, err
	}
	normalized.priority = priority
	creator, err := normalizeCreator(call, input.Creator)
	if err != nil {
		return normalized, err
	}
	normalized.creator = creator
	return normalized, nil
}

func normalizeCreator(call application.Call, value string) (string, error) {
	creator := value
	if strings.TrimSpace(creator) == "" {
		creator = call.Actor.Agent
		if strings.TrimSpace(creator) == "" {
			creator = store.TodoCreatorMe
		}
	}
	normalized, err := store.NormalizeTodoCreator(creator)
	if err != nil {
		return "", metadataInvalidArgument(err.Error(), "creator", value)
	}
	return normalized, nil
}

func normalizePriority(value string) (string, error) {
	priority := strings.ToUpper(strings.TrimSpace(value))
	switch priority {
	case "P0", "P1", "P2":
		return priority, nil
	default:
		return "", metadataInvalidArgument("priority must be P0, P1, or P2", "priority", value)
	}
}

func normalizeMetadataStatus(value string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(value))
	if status == store.TodoStatusOpen {
		return status, nil
	}
	return "", metadataInvalidArgument(
		fmt.Sprintf("metadata edits may only return a todo to open; use start, submit, or done for status %q", value),
		"status", value,
	)
}

func validateMetadataCall(ctx context.Context, call application.Call) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return metadataUnavailable("todo metadata request canceled", err)
	}
	return call.Validate()
}

// metadataEffectsForCreate emits only creation. A new Todo is always open —
// reaching review is a transition a Todo has to be taken through, which is why
// this no longer also checks for a review status the caller cannot ask for.
func metadataEffectsForCreate(todo Todo) []MetadataEffect {
	return []MetadataEffect{{Kind: MetadataEffectCreated, Todo: cloneTodo(todo)}}
}

func syncTodoDocumentIfPresent(todo *Todo) error {
	if !store.TodoDocExists(todo.ID) {
		return nil
	}
	if err := store.SyncTodoDocMetadata(todo); err != nil {
		return metadataUnavailable("sync todo document "+todo.ID, err)
	}
	return nil
}

func cloneTodo(todo Todo) Todo {
	todo.Tags = append([]string(nil), todo.Tags...)
	todo.DependsOn = append([]string(nil), todo.DependsOn...)
	todo.Links = append([]store.TodoLink(nil), todo.Links...)
	todo.Images = append([]store.TodoImage(nil), todo.Images...)
	return todo
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func metadataBatchItemError(index int, title string, err error) error {
	var appErr *application.Error
	if !errors.As(err, &appErr) {
		return err
	}
	copy := *appErr
	copy.Message = fmt.Sprintf("item %q: %s", title, appErr.Message)
	copy.Details = make(map[string]any, len(appErr.Details)+2)
	for key, value := range appErr.Details {
		copy.Details[key] = value
	}
	copy.Details["item_index"] = index
	copy.Details["item_title"] = title
	return &copy
}

func metadataTodoNotFound(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, cause.Error(), cause)
	err.Details = map[string]any{"todo_id": store.NormalizeTodoID(id)}
	return err
}

func metadataConflict(message, todoID, currentStatus string) *application.Error {
	err := application.NewError(application.CodeConflict, message)
	err.Details = map[string]any{"todo_id": todoID, "current_status": currentStatus}
	return err
}

func metadataInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func metadataUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}

func metadataApplicationError(operation string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return metadataUnavailable(operation, err)
}
