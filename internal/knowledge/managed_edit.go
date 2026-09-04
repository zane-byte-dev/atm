package knowledge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	DocumentID string    `json:"document_id"`
	ETag       string    `json:"etag"`
	Content    string    `json:"content"`
	Title      *string   `json:"title,omitempty"`
	Status     *string   `json:"status,omitempty"`
	Domains    *[]string `json:"domains,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
	Projects   *[]string `json:"projects,omitempty"`
}

// UpdateManaged only edits a registered, ATM-owned document at its existing
// path. Import provenance cannot grant the browser authority over source files.
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
	if document.Metadata.Source != nil {
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
	document.Content = strings.TrimSpace(input.Content)
	document.Metadata.UpdatedAt = time.Now().UTC()
	content, err := marshalDocument(document)
	if err != nil {
		return VersionedDocument{}, err
	}
	if err := writeManagedDocument(dataDir, document.Path, content); err != nil {
		return VersionedDocument{}, application.WrapError(application.CodeUnavailable, "knowledge document could not be saved", err)
	}
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
