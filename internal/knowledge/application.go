package knowledge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

// DocumentView is the stable read model shared by collection browsing and
// full-text search. A browser row has timestamps and a search row has a snippet
// and score; optional fields keep that distinction explicit without forcing the
// browser adapter to decode two representations of the same document identity.
type DocumentView struct {
	DocumentID string     `json:"document_id"`
	Title      string     `json:"title"`
	Collection string     `json:"collection"`
	Status     string     `json:"status"`
	Domains    []string   `json:"domains,omitempty"`
	Tags       []string   `json:"tags,omitempty"`
	Projects   []string   `json:"projects,omitempty"`
	Producer   string     `json:"producer,omitempty"`
	CreatedAt  *time.Time `json:"created_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	Snippet    string     `json:"snippet,omitempty"`
	Score      *float64   `json:"score,omitempty"`
}

// QueryInput describes the browser library's one read operation. Text is
// optional because collection browsing is the empty-query form of the same
// operation, not a separate mutation or an argv mode switch.
type QueryInput struct {
	Text       string `json:"text,omitempty"`
	Collection string `json:"collection,omitempty"`
	Status     string `json:"status,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type QueryResult struct {
	Documents []DocumentView `json:"documents"`
}

// GovernanceInput and GovernanceResult are one coherent read snapshot. Audit
// issues and quality evidence are displayed together and must describe the same
// corpus read, rather than two independently timed CLI calls.
type GovernanceInput struct {
	StaleDays int `json:"stale_days,omitempty"`
}

type GovernanceResult struct {
	Audit   AuditReport   `json:"audit"`
	Quality QualityResult `json:"quality"`
}

type CreateDocumentInput struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Collection string   `json:"collection,omitempty"`
	Domains    []string `json:"domains,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Projects   []string `json:"projects,omitempty"`
	Producer   string   `json:"producer,omitempty"`
}

type SetDocumentContentInput struct {
	DocumentID string `json:"document_id"`
	Content    string `json:"content"`
}

type SetDocumentMetadataInput struct {
	DocumentID string    `json:"document_id"`
	Title      *string   `json:"title,omitempty"`
	Collection *string   `json:"collection,omitempty"`
	Status     *string   `json:"status,omitempty"`
	Domains    *[]string `json:"domains,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
	Projects   *[]string `json:"projects,omitempty"`
}

// SaveDocumentInput is a closed, structural sum type. Exactly one member must
// be present. This keeps one stable "save" use case without introducing an
// action string and a universal bag of optional Cobra-like arguments.
type SaveDocumentInput struct {
	Create   *CreateDocumentInput      `json:"create,omitempty"`
	Content  *SetDocumentContentInput  `json:"content,omitempty"`
	Metadata *SetDocumentMetadataInput `json:"metadata,omitempty"`
}

type DeleteDocumentInput struct {
	DocumentID string `json:"document_id"`
	Confirmed  bool   `json:"confirmed"`
}

type ImportDocumentInput struct {
	Path       string   `json:"path"`
	Collection string   `json:"collection,omitempty"`
	Domains    []string `json:"domains,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Projects   []string `json:"projects,omitempty"`
	Producer   string   `json:"producer,omitempty"`
}

type CreateCollectionInput struct {
	ID           string    `json:"id"`
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Role         *string   `json:"role,omitempty"`
	Topics       *[]string `json:"topics,omitempty"`
	UseWhen      *[]string `json:"use_when,omitempty"`
	AvoidWhen    *[]string `json:"avoid_when,omitempty"`
	Instructions *[]string `json:"instructions,omitempty"`
}

type UpdateCollectionInput struct {
	ID           string    `json:"id"`
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Role         *string   `json:"role,omitempty"`
	Topics       *[]string `json:"topics,omitempty"`
	UseWhen      *[]string `json:"use_when,omitempty"`
	AvoidWhen    *[]string `json:"avoid_when,omitempty"`
	Instructions *[]string `json:"instructions,omitempty"`
}

// SaveCollectionInput follows the same closed one-of rule as document save.
type SaveCollectionInput struct {
	Create *CreateCollectionInput `json:"create,omitempty"`
	Update *UpdateCollectionInput `json:"update,omitempty"`
}

type RenameCollectionInput struct {
	ID    string `json:"id"`
	NewID string `json:"new_id"`
}

type DeleteCollectionInput struct {
	ID        string `json:"id"`
	Force     bool   `json:"force,omitempty"`
	MoveTo    string `json:"move_to,omitempty"`
	Confirmed bool   `json:"confirmed"`
}

func (service Service) Query(ctx context.Context, input QueryInput) (QueryResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return QueryResult{}, err
	}
	input.Text = strings.TrimSpace(input.Text)
	input.Collection = strings.TrimSpace(input.Collection)
	input.Status = strings.TrimSpace(input.Status)
	input.SessionID = strings.TrimSpace(input.SessionID)
	if input.Limit < 0 {
		return QueryResult{}, invalidKnowledgeArgument("knowledge query limit must not be negative", "limit", input.Limit)
	}

	if input.Text != "" {
		options := SearchOptions{Limit: input.Limit}
		if input.Collection != "" {
			options.Collections = []string{input.Collection}
		}
		if input.Status != "" {
			options.Statuses = []string{input.Status}
		}
		result, err := service.Search(ctx, SearchInput{
			Query: input.Text, SessionID: input.SessionID, Options: options,
		})
		if err != nil {
			return QueryResult{}, err
		}
		views := make([]DocumentView, 0, len(result.Hits))
		for _, hit := range result.Hits {
			score := hit.Score
			views = append(views, DocumentView{
				DocumentID: hit.DocumentID, Title: hit.Title, Collection: hit.Collection,
				Status: hit.Status, Domains: hit.Domains, Tags: hit.Tags, Projects: hit.Projects,
				Snippet: hit.Snippet, Score: &score,
			})
		}
		return QueryResult{Documents: views}, nil
	}

	collections := []string(nil)
	if input.Collection != "" {
		collections = []string{input.Collection}
	}
	documents, err := service.List(ctx, ListInput{Collections: collections})
	if err != nil {
		return QueryResult{}, err
	}
	views := make([]DocumentView, 0, len(documents))
	for _, document := range documents {
		if input.Status != "" && document.Status != input.Status {
			continue
		}
		createdAt, updatedAt := document.CreatedAt, document.UpdatedAt
		views = append(views, DocumentView{
			DocumentID: document.DocumentID, Title: document.Title, Collection: document.Collection,
			Status: document.Status, Domains: document.Domains, Tags: document.Tags,
			Projects: document.Projects, Producer: document.Producer,
			CreatedAt: &createdAt, UpdatedAt: &updatedAt,
		})
		if input.Limit > 0 && len(views) == input.Limit {
			break
		}
	}
	return QueryResult{Documents: views}, nil
}

func (service Service) Governance(ctx context.Context, input GovernanceInput) (GovernanceResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return GovernanceResult{}, err
	}
	audit, err := Audit(service.dataDir, AuditOptions{
		StaleDays: input.StaleDays,
		Now:       service.now().UTC(),
	})
	if err != nil {
		return GovernanceResult{}, unavailableKnowledge("audit knowledge", err)
	}
	quality, err := service.Quality(ctx, QualityInput{})
	if err != nil {
		return GovernanceResult{}, err
	}
	return GovernanceResult{Audit: *audit, Quality: quality}, nil
}

func (service Service) SaveDocument(ctx context.Context, input SaveDocumentInput) (Document, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return Document{}, err
	}
	if oneOf(input.Create != nil, input.Content != nil, input.Metadata != nil) != 1 {
		return Document{}, invalidKnowledgeArgument(
			"knowledge document save requires exactly one of create, content, or metadata",
			"save", nil,
		)
	}
	var (
		document *Document
		err      error
	)
	switch {
	case input.Create != nil:
		value := input.Create
		document, err = Add(service.dataDir, AddDocumentInput{
			Title: value.Title, Content: value.Content, Collection: value.Collection,
			Domains: value.Domains, Tags: value.Tags, Projects: value.Projects, Producer: value.Producer,
		})
	case input.Content != nil:
		value := input.Content
		document, err = Update(service.dataDir, value.DocumentID, value.Content)
	case input.Metadata != nil:
		value := input.Metadata
		if value.Title == nil && value.Collection == nil && value.Status == nil &&
			value.Domains == nil && value.Tags == nil && value.Projects == nil {
			return Document{}, invalidKnowledgeArgument("knowledge metadata save has no changes", "metadata", nil)
		}
		document, err = Edit(service.dataDir, value.DocumentID, EditDocumentInput{
			Title: value.Title, Collection: value.Collection, Status: value.Status,
			Domains: value.Domains, Tags: value.Tags, Projects: value.Projects,
		})
	}
	if err != nil {
		return Document{}, knowledgeMutationError("save knowledge document", err)
	}
	return *document, nil
}

func (service Service) DeleteDocument(ctx context.Context, input DeleteDocumentInput) (Document, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return Document{}, err
	}
	if !input.Confirmed {
		return Document{}, invalidKnowledgeArgument(
			"permanent knowledge document deletion requires confirmed=true", "confirmed", false,
		)
	}
	document, err := Delete(service.dataDir, strings.TrimSpace(input.DocumentID))
	if err != nil {
		return Document{}, knowledgeMutationError("delete knowledge document", err)
	}
	return *document, nil
}

func (service Service) ImportDocument(ctx context.Context, input ImportDocumentInput) ([]Document, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return nil, err
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return nil, invalidKnowledgeArgument("knowledge import path must not be empty", "path", input.Path)
	}
	documents, err := Import(service.dataDir, input.Path, AddDocumentInput{
		Collection: input.Collection,
		Domains:    input.Domains,
		Tags:       input.Tags,
		Projects:   input.Projects,
		Producer:   input.Producer,
	})
	if err != nil {
		return nil, knowledgeMutationError("import knowledge document", err)
	}
	return documents, nil
}

func (service Service) SaveCollection(ctx context.Context, input SaveCollectionInput) (CollectionInfo, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return CollectionInfo{}, err
	}
	if oneOf(input.Create != nil, input.Update != nil) != 1 {
		return CollectionInfo{}, invalidKnowledgeArgument(
			"knowledge collection save requires exactly one of create or update", "save", nil,
		)
	}
	var (
		collection *CollectionInfo
		err        error
	)
	if input.Create != nil {
		collection, err = CreateCollection(
			service.dataDir,
			input.Create.ID,
			collectionCreateEdit(*input.Create),
		)
	} else {
		edit := collectionUpdateEdit(*input.Update)
		if edit.Name == nil && edit.Description == nil && edit.Role == nil && edit.Topics == nil &&
			edit.UseWhen == nil && edit.AvoidWhen == nil && edit.Instructions == nil {
			return CollectionInfo{}, invalidKnowledgeArgument("knowledge collection save has no changes", "update", nil)
		}
		collection, err = EditCollection(service.dataDir, input.Update.ID, edit)
	}
	if err != nil {
		return CollectionInfo{}, knowledgeMutationError("save knowledge collection", err)
	}
	return *collection, nil
}

func collectionCreateEdit(input CreateCollectionInput) EditCollectionInput {
	return EditCollectionInput{
		Name: input.Name, Description: input.Description, Role: input.Role,
		Topics: input.Topics, UseWhen: input.UseWhen, AvoidWhen: input.AvoidWhen,
		Instructions: input.Instructions,
	}
}

func collectionUpdateEdit(input UpdateCollectionInput) EditCollectionInput {
	return EditCollectionInput{
		Name: input.Name, Description: input.Description, Role: input.Role,
		Topics: input.Topics, UseWhen: input.UseWhen, AvoidWhen: input.AvoidWhen,
		Instructions: input.Instructions,
	}
}

func (service Service) RenameCollection(ctx context.Context, input RenameCollectionInput) (CollectionInfo, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return CollectionInfo{}, err
	}
	collection, err := RenameCollection(service.dataDir, input.ID, input.NewID)
	if err != nil {
		return CollectionInfo{}, knowledgeMutationError("rename knowledge collection", err)
	}
	return *collection, nil
}

func (service Service) DeleteCollection(ctx context.Context, input DeleteCollectionInput) (DeleteCollectionResult, error) {
	if err := knowledgeContextError(ctx); err != nil {
		return DeleteCollectionResult{}, err
	}
	if !input.Confirmed {
		return DeleteCollectionResult{}, invalidKnowledgeArgument(
			"permanent knowledge collection deletion requires confirmed=true", "confirmed", false,
		)
	}
	if input.Force && strings.TrimSpace(input.MoveTo) != "" {
		return DeleteCollectionResult{}, invalidKnowledgeArgument(
			"use either force or move_to, not both", "move_to", input.MoveTo,
		)
	}
	result, err := DeleteCollection(service.dataDir, input.ID, DeleteCollectionOptions{
		Force: input.Force, MoveTo: input.MoveTo,
	})
	if err != nil {
		return DeleteCollectionResult{}, knowledgeMutationError("delete knowledge collection", err)
	}
	return *result, nil
}

func oneOf(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func knowledgeMutationError(operation string, cause error) error {
	var notFound documentNotFoundError
	message := cause.Error()
	switch {
	case errors.As(cause, &notFound), strings.Contains(message, " not found:"):
		return application.WrapError(application.CodeNotFound, message, cause)
	case strings.Contains(message, "already exists"):
		return application.WrapError(application.CodeConflict, message, cause)
	case strings.Contains(message, " is not empty "):
		return application.WrapError(application.CodeConflict, message, cause)
	case strings.Contains(message, "must "), strings.Contains(message, "invalid "),
		strings.Contains(message, "not be empty"), strings.Contains(message, "read-only"):
		return application.WrapError(application.CodeInvalidArgument, message, cause)
	default:
		return unavailableKnowledge(operation, cause)
	}
}
