package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/executionlock"
)

type VersionedDocument struct {
	Document
	ETag     string `json:"etag"`
	Editable bool   `json:"editable"`
}

// VersionDocument hashes the returned snapshot, never a second file read that
// might have already advanced beyond the body displayed in the editor.
func VersionDocument(document Document) VersionedDocument {
	data, _ := json.Marshal(document)
	sum := sha256.Sum256(data)
	return VersionedDocument{Document: document, ETag: hex.EncodeToString(sum[:]), Editable: document.Metadata.Source == nil}
}

type UpdateManagedInput struct {
	Collection *string   `json:"collection,omitempty"`
	DocumentID string    `json:"document_id"`
	ETag       string    `json:"etag"`
	Content    string    `json:"content"`
	Title      *string   `json:"title,omitempty"`
	Status     *string   `json:"status,omitempty"`
	Domains    *[]string `json:"domains,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
	Projects   *[]string `json:"projects,omitempty"`
}

// UpdateManaged edits ATM-owned content and moves or archives managed records.
// Imported records permit only unchanged-body status/collection updates; import
// provenance never grants the browser authority over source files.
// CLI body/metadata writes and collection moves share this corpus lock.
func UpdateManaged(ctx context.Context, dataDir string, input UpdateManagedInput) (VersionedDocument, error) {
	if strings.TrimSpace(input.DocumentID) == "" || len(input.ETag) != 64 || strings.TrimSpace(input.Content) == "" {
		return VersionedDocument{}, application.NewError(application.CodeInvalidArgument, "document ID, revision and content are required")
	}
	lock, err := executionlock.Acquire(ctx, dataDir, "knowledge")
	if err != nil {
		return VersionedDocument{}, err
	}
	defer lock.Close()
	document, err := Get(dataDir, input.DocumentID)
	if err != nil {
		return VersionedDocument{}, application.WrapError(application.CodeNotFound, "knowledge document could not be found", err)
	}
	metadataOnly := (input.Status != nil || input.Collection != nil) && input.Title == nil && input.Domains == nil && input.Tags == nil && input.Projects == nil && input.Content == document.Content
	if document.Metadata.Source != nil && !metadataOnly {
		return VersionedDocument{}, application.NewError(application.CodeForbidden, "imported documents must be edited at their original source")
	}
	if VersionDocument(*document).ETag != input.ETag {
		return VersionedDocument{}, application.NewError(application.CodeConflict, "document changed in another window or process; reload before saving")
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return VersionedDocument{}, application.NewError(application.CodeInvalidArgument, "document title is required")
		}
		document.Metadata.Title = title
	}
	if input.Status != nil {
		switch *input.Status {
		case "active", "draft", "archived":
			document.Metadata.Status = *input.Status
		default:
			return VersionedDocument{}, application.NewError(application.CodeInvalidArgument, "invalid document status")
		}
	}
	for _, value := range []struct {
		target *[]string
		input  *[]string
	}{{&document.Metadata.Tags, input.Tags}, {&document.Metadata.Domains, input.Domains}, {&document.Metadata.Projects, input.Projects}} {
		if value.input != nil {
			*value.target = normalizeValues(*value.input)
		}
	}
	targetPath := document.Path
	if input.Collection != nil && *input.Collection != document.Collection {
		target, err := normalizeCollection(*input.Collection)
		if err != nil || target == "" {
			return VersionedDocument{}, application.NewError(application.CodeInvalidArgument, "invalid target collection")
		}
		if _, err := collectionByID(dataDir, target); err != nil {
			return VersionedDocument{}, application.WrapError(application.CodeNotFound, "target collection not found", err)
		}
		targetPath = filepath.Join(knowledgeRoot(dataDir), target, filepath.Base(document.Path))
		document.Collection = target
	}
	document.Content = strings.TrimSpace(input.Content)
	document.Metadata.UpdatedAt = time.Now().UTC()
	content, err := marshalDocument(document)
	if err != nil {
		return VersionedDocument{}, err
	}
	if err := saveOrMoveManagedDocument(dataDir, document.Path, targetPath, content); err != nil {
		return VersionedDocument{}, application.WrapError(application.CodeUnavailable, "knowledge document could not be saved", err)
	}
	document.Path = targetPath
	return VersionDocument(*document), nil
}

func writeManagedDocument(dataDir, path string, content []byte) error {
	rootPath := knowledgeRoot(dataDir)
	relative, err := filepath.Rel(rootPath, path)
	if err != nil || relative == "." || !filepath.IsLocal(relative) {
		return application.NewError(application.CodeForbidden, "document is outside the managed knowledge directory")
	}
	// Root confines both creation and rename even if a directory is replaced by
	// a symlink after the host's tree inspection.
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err := root.Lstat(relative)
	if err != nil || !info.Mode().IsRegular() {
		return application.NewError(application.CodeForbidden, "document must be a regular managed file")
	}
	temporary := filepath.Join(filepath.Dir(relative), ".atm-edit-"+rand.Text())
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temporary, relative)
}

// A collection move stays within the corpus and never overwrites a target file.
func saveOrMoveManagedDocument(dataDir, source, target string, content []byte) error {
	if source == target {
		return writeManagedDocument(dataDir, source, content)
	}
	root, err := os.OpenRoot(knowledgeRoot(dataDir))
	if err != nil {
		return err
	}
	defer root.Close()
	from, err := filepath.Rel(knowledgeRoot(dataDir), source)
	if err != nil || !filepath.IsLocal(from) {
		return application.NewError(application.CodeForbidden, "invalid document path")
	}
	to, err := filepath.Rel(knowledgeRoot(dataDir), target)
	if err != nil || !filepath.IsLocal(to) {
		return application.NewError(application.CodeForbidden, "invalid target path")
	}
	if _, err = root.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return application.NewError(application.CodeConflict, "target file already exists or is inaccessible")
	}
	if err = root.Rename(from, to); err != nil {
		return err
	}
	if err = writeManagedDocument(dataDir, target, content); err != nil {
		return errors.Join(err, root.Rename(to, from))
	}
	return nil
}
