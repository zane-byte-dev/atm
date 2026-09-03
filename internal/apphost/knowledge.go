package apphost

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
)

// This Web vocabulary deliberately omits the service's session_id (which
// records retrievals), import paths, source URIs, and file-backed edits.
type KnowledgeQueryInput struct {
	Text       string `json:"text,omitempty"`
	Collection string `json:"collection,omitempty"`
	Status     string `json:"status,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type KnowledgeCreateInput struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Collection string   `json:"collection"`
	Domains    []string `json:"domains,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Projects   []string `json:"projects,omitempty"`
}

type KnowledgeCollectionCreateInput struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type MemoryGetInput struct {
	MemoryID string `json:"memory_id"`
}

type MemoryCreateInput struct {
	Scope   string   `json:"scope"`
	Content string   `json:"content"`
	Tags    []string `json:"tags,omitempty"`
}

type MemorySupersedeInput struct {
	TargetID string   `json:"target_id"`
	Scope    string   `json:"scope"`
	Content  string   `json:"content"`
	Tags     []string `json:"tags,omitempty"`
}

func (h *Host) callKnowledge(ctx context.Context, call application.Call, method string, input json.RawMessage) (any, error) {
	write := method == "knowledge.document.create" || method == "knowledge.collection.create" || method == "memory.create" || method == "memory.supersede"
	if write {
		h.gate.Lock()
		defer h.gate.Unlock()
		if err := validateWrite(ctx, call); err != nil {
			return nil, err
		}
	} else {
		h.gate.RLock()
		defer h.gate.RUnlock()
		if err := validate(ctx, call); err != nil {
			return nil, err
		}
	}
	service := knowledge.NewService(knowledge.ServiceOptions{DataDir: config.AtmDir})
	// Discover reads Markdown. Inspect its managed tree before it can follow a
	// symlink or open a device/FIFO through a Markdown name. There is no API for
	// creating links or changing this root; it is fixed by startup composition.
	if strings.HasPrefix(method, "knowledge.") {
		if err := checkKnowledgeTree(ctx); err != nil {
			return nil, err
		}
	}
	switch method {
	case "knowledge.catalog":
		return invoke(input, func(struct{}) (any, error) { return service.Catalog(ctx) })
	case "knowledge.query":
		return invoke(input, func(value KnowledgeQueryInput) (any, error) {
			if err := boundedKnowledgeQuery(value.Text, &value.Limit); err != nil {
				return nil, err
			}
			if value.Collection != "" && !managedCollectionID(value.Collection) {
				return nil, invalid("invalid collection ID")
			}
			switch value.Status {
			case "", "active", "draft", "archived":
			default:
				return nil, invalid("invalid knowledge status")
			}
			result, err := service.Query(ctx, knowledge.QueryInput{Text: value.Text, Collection: value.Collection, Status: value.Status, Limit: value.Limit})
			if err != nil {
				return nil, err
			}
			// Search returns matching chunks; present each document once while
			// retaining its strongest matching snippet and the service's ranking.
			seen := map[string]bool{}
			documents := make([]knowledge.DocumentView, 0, len(result.Documents))
			for _, document := range result.Documents {
				if !seen[document.DocumentID] {
					seen[document.DocumentID] = true
					documents = append(documents, document)
				}
			}
			return knowledge.QueryResult{Documents: documents}, nil
		})
	case "knowledge.document.get":
		return invoke(input, func(value knowledge.GetInput) (any, error) {
			if err := knowledgeIdentity(value.DocumentID); err != nil {
				return nil, err
			}
			return service.Get(ctx, value)
		})
	case "knowledge.document.create":
		return invoke(input, func(value KnowledgeCreateInput) (any, error) {
			if !managedDocumentTitle(value.Title) {
				return nil, invalid("a visible document title of at most 1000 bytes is required")
			}
			if err := knowledgeContent(value.Content, value.Tags, value.Domains, value.Projects); err != nil {
				return nil, err
			}
			if !managedCollectionID(value.Collection) {
				return nil, invalid("a registered collection ID is required")
			}
			catalog, err := service.Catalog(ctx)
			if err != nil {
				return nil, err
			}
			registered := false
			for _, collection := range catalog {
				registered = registered || collection.ID == value.Collection
			}
			if !registered {
				return nil, application.NewError(application.CodeNotFound, "create or select a registered collection first")
			}
			return service.SaveDocument(ctx, knowledge.SaveDocumentInput{Create: &knowledge.CreateDocumentInput{
				Title: value.Title, Content: value.Content, Collection: value.Collection,
				Domains: value.Domains, Tags: value.Tags, Projects: value.Projects, Producer: "atm-web",
			}})
		})
	case "knowledge.collection.create":
		return invoke(input, func(value KnowledgeCollectionCreateInput) (any, error) {
			if !managedCollectionID(value.ID) || len(value.Name) > 500 || len(value.Description) > 10000 {
				return nil, invalid("a valid collection ID and bounded name/description are required")
			}
			return service.SaveCollection(ctx, knowledge.SaveCollectionInput{Create: &knowledge.CreateCollectionInput{
				ID: value.ID, Name: &value.Name, Description: &value.Description,
			}})
		})
	case "memory.recall":
		return invoke(input, func(value knowledge.RecallMemoryInput) (any, error) {
			if err := boundedKnowledgeQuery(value.Query, &value.Limit); err != nil {
				return nil, err
			}
			if len(value.Scope) > 500 {
				return nil, invalid("memory scope must not exceed 500 bytes")
			}
			result, err := service.RecallMemory(ctx, value)
			if result.Hits == nil {
				result.Hits = []knowledge.MemoryHit{}
			}
			return result, err
		})
	case "memory.get":
		return invoke(input, func(value MemoryGetInput) (any, error) {
			if err := knowledgeIdentity(value.MemoryID); err != nil {
				return nil, err
			}
			row, err := store.EffectiveMemory(value.MemoryID)
			if err != nil {
				if strings.Contains(err.Error(), "active memory not found:") {
					return nil, application.NewError(application.CodeNotFound, "this memory is no longer active; refresh the list")
				}
				return nil, unavailable(err)
			}
			created, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
			return knowledge.MemoryHit{ID: row.ID, Scope: row.Scope, Content: row.Content, Tags: row.Tags, CreatedAt: created, Source: "memory", Metadata: row.Metadata}, nil
		})
	case "memory.create":
		return invoke(input, func(value MemoryCreateInput) (any, error) {
			if err := validateMemoryContent(value.Scope, value.Content, value.Tags); err != nil {
				return nil, err
			}
			event, err := knowledge.RememberWithMetadata(value.Scope, value.Content, value.Tags, map[string]string{"source": "atm-web"})
			if err != nil {
				return nil, unavailable(err)
			}
			return knowledge.SupersedeMemoryResult{Event: *event}, nil
		})
	case "memory.supersede":
		return invoke(input, func(value MemorySupersedeInput) (any, error) {
			if err := knowledgeIdentity(value.TargetID); err != nil {
				return nil, err
			}
			if err := validateMemoryContent(value.Scope, value.Content, value.Tags); err != nil {
				return nil, err
			}
			// An immutable target ID is the expected revision. The domain's
			// unique target index rejects a second replacement across processes.
			return service.SupersedeMemory(ctx, knowledge.SupersedeMemoryInput{TargetID: value.TargetID, Scope: value.Scope, Content: value.Content, Tags: value.Tags, Source: "atm-web"})
		})
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown knowledge API method")
	}
}

func boundedKnowledgeQuery(text string, limit *int) error {
	if len(text) > 2000 || *limit < 0 || *limit > 200 {
		return invalid("query must not exceed 2000 bytes and limit must be between 1 and 200")
	}
	if *limit == 0 {
		*limit = 100
	}
	return nil
}

func knowledgeIdentity(id string) error {
	if strings.TrimSpace(id) == "" || len(id) > 500 || strings.ContainsAny(id, "/\\\x00\r\n") || id == "." || id == ".." {
		return invalid("a valid registered identity is required")
	}
	return nil
}

func managedCollectionID(id string) bool {
	return id != "" && len(id) <= 150 && strings.TrimSpace(id) == id && !strings.HasPrefix(id, ".") && !strings.HasPrefix(id, "_") && !strings.ContainsAny(id, "<>:\"/\\|?*\x00\r\n")
}

func managedDocumentTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > 1000 || strings.HasPrefix(title, ".") {
		return false
	}
	// Domain creation derives a readable filename from the title. Refuse the
	// reserved manifest name even when sanitizing punctuation would produce it.
	name := strings.Map(func(value rune) rune {
		if value < 32 || strings.ContainsRune(`<>:"/\|?*`, value) {
			return '_'
		}
		return value
	}, title)
	return strings.Trim(name, ".") != "_collection"
}

func knowledgeContent(content string, groups ...[]string) error {
	if strings.TrimSpace(content) == "" || len(content) > 1024*1024 {
		return invalid("content is required and must not exceed 1 MiB")
	}
	for _, group := range groups {
		if len(group) > 100 {
			return invalid("at most 100 metadata values are allowed")
		}
		for _, value := range group {
			if len(value) > 500 {
				return invalid("metadata values must not exceed 500 bytes")
			}
		}
	}
	return nil
}

func validateMemoryContent(scope, content string, tags []string) error {
	if len(scope) > 500 {
		return invalid("memory scope must not exceed 500 bytes")
	}
	if err := knowledge.ValidateScope(scope); err != nil {
		return invalid(err.Error())
	}
	return knowledgeContent(content, tags)
}

func checkKnowledgeTree(ctx context.Context) error {
	root := filepath.Join(config.AtmDir, "knowledge")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if path == root && os.IsNotExist(err) {
				return nil
			}
			return unavailable(err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return application.NewError(application.CodeForbidden, "knowledge contains a symbolic link; Web access requires a managed directory of regular files")
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") && !entry.Type().IsRegular() {
			return application.NewError(application.CodeForbidden, "knowledge Markdown must be a regular file")
		}
		return nil
	})
	return err
}
