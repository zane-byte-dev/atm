package knowledge

import (
	"context"

	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/zane-byte-dev/atm/internal/executionlock"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"

	"gopkg.in/yaml.v3"
)

type AddDocumentInput struct {
	Title      string
	Content    string
	Collection string
	Domains    []string
	Tags       []string
	Projects   []string
	Producer   string
	Source     *SourceInfo
}

type EditDocumentInput struct {
	Title      *string
	Collection *string
	Status     *string
	Domains    *[]string
	Tags       *[]string
	Projects   *[]string
}

func Add(dataDir string, input AddDocumentInput) (*Document, error) {
	mutationLock, lockErr := executionlock.Acquire(context.Background(), dataDir, "knowledge")
	if lockErr != nil {
		return nil, lockErr
	}
	defer mutationLock.Close()

	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	if input.Title == "" || input.Content == "" {
		return nil, fmt.Errorf("knowledge title and content must not be empty")
	}
	if input.Producer == "" {
		input.Producer = "human"
	}
	collection, err := normalizeCollection(input.Collection)
	if err != nil {
		return nil, err
	}
	if collection == "" {
		collection = "inbox"
	}
	now := time.Now().UTC()
	document := &Document{
		Metadata: DocumentMetadata{
			ID:            newID("document"),
			SchemaVersion: KnowledgeSchemaVersion,
			Title:         input.Title,
			Status:        "active",
			Domains:       normalizeValues(input.Domains),
			Tags:          normalizeValues(input.Tags),
			Projects:      normalizeValues(input.Projects),
			Producer:      strings.TrimSpace(input.Producer),
			CreatedAt:     now,
			UpdatedAt:     now,
			Source:        input.Source,
		},
		Collection: collection,
		Content:    input.Content,
	}
	targetPath := filepath.Join(knowledgeRoot(dataDir), collection, safeMarkdownName(input.Title)+".md")
	if err := createDocumentAt(targetPath, document); err != nil {
		return nil, err
	}
	return document, nil
}

// Update replaces a document body while preserving its identity and metadata.
// File-backed imports are written to their canonical source and re-imported so
// ATM never creates a divergent editable copy.
func Update(dataDir, documentID, content string) (*Document, error) {
	mutationLock, lockErr := executionlock.Acquire(context.Background(), dataDir, "knowledge")
	if lockErr != nil {
		return nil, lockErr
	}
	defer mutationLock.Close()

	content = strings.TrimSpace(content)
	if strings.TrimSpace(documentID) == "" || content == "" {
		return nil, fmt.Errorf("knowledge document id and content must not be empty")
	}
	document, err := Get(dataDir, documentID)
	if err != nil {
		return nil, err
	}
	if document.Metadata.Source == nil {
		document.Content = content
		document.Metadata.UpdatedAt = time.Now().UTC()
		if err := writeDocumentAt(document.Path, document); err != nil {
			return nil, err
		}
		return document, nil
	}
	if document.Metadata.Source.Type != "file" {
		return nil, fmt.Errorf("knowledge document source type %q is read-only", document.Metadata.Source.Type)
	}
	return updateImportedFile(document, content)
}

// Delete permanently removes a document from ATM's central knowledge store.
// External imported source files are intentionally preserved; only ATM's
// managed copy is removed.
func Delete(dataDir, documentID string) (*Document, error) {
	mutationLock, lockErr := executionlock.Acquire(context.Background(), dataDir, "knowledge")
	if lockErr != nil {
		return nil, lockErr
	}
	defer mutationLock.Close()

	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, fmt.Errorf("knowledge document id must not be empty")
	}
	document, err := Get(dataDir, documentID)
	if err != nil {
		return nil, err
	}

	root, err := filepath.Abs(knowledgeRoot(dataDir))
	if err != nil {
		return nil, err
	}
	path, err := filepath.Abs(document.Path)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("knowledge document path escapes knowledge root: %s", document.Path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect knowledge document %s: %w", document.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("knowledge document must be a regular file: %s", document.Path)
	}
	if err := os.Remove(path); err != nil {
		return nil, fmt.Errorf("delete knowledge document %s: %w", document.Path, err)
	}
	// A markdown file cannot cascade. Feedback about a document that no longer
	// exists would otherwise sit in the database forever, counted by nothing and
	// visible in nothing, so the delete path removes it here — and
	// OrphanedFeedbackDocuments reports whatever a hand-deleted file leaves behind.
	if err := store.DeleteKnowledgeFeedback(documentID); err != nil {
		return nil, fmt.Errorf("delete feedback for %s: %w", documentID, err)
	}
	return document, nil
}

// Edit updates document metadata, preserving the document identity. Renaming a
// file-backed document also updates its canonical source Markdown title.
func Edit(dataDir, documentID string, input EditDocumentInput) (*Document, error) {
	mutationLock, lockErr := executionlock.Acquire(context.Background(), dataDir, "knowledge")
	if lockErr != nil {
		return nil, lockErr
	}
	defer mutationLock.Close()

	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil, fmt.Errorf("knowledge document id must not be empty")
	}
	document, err := Get(dataDir, documentID)
	if err != nil {
		return nil, err
	}
	updated := *document
	updated.Metadata = document.Metadata
	if document.Metadata.Source != nil {
		source := *document.Metadata.Source
		updated.Metadata.Source = &source
	}

	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return nil, fmt.Errorf("knowledge title must not be empty")
		}
		updated.Metadata.Title = title
	}
	if input.Collection != nil {
		collection, err := normalizeCollection(*input.Collection)
		if err != nil {
			return nil, err
		}
		if collection == "" {
			return nil, fmt.Errorf("knowledge collection must not be empty")
		}
		updated.Collection = collection
	}
	if input.Status != nil {
		status := strings.TrimSpace(*input.Status)
		if status != "active" && status != "draft" && status != "archived" {
			return nil, fmt.Errorf("invalid knowledge status %q", status)
		}
		updated.Metadata.Status = status
	}
	if input.Domains != nil {
		updated.Metadata.Domains = normalizeValues(*input.Domains)
	}
	if input.Tags != nil {
		updated.Metadata.Tags = normalizeValues(*input.Tags)
	}
	if input.Projects != nil {
		updated.Metadata.Projects = normalizeValues(*input.Projects)
	}

	var originalSource []byte
	var sourcePath string
	var sourceMode os.FileMode
	if updated.Metadata.Source != nil && updated.Metadata.Source.Type == "file" && updated.Metadata.Title != document.Metadata.Title {
		sourcePath, err = filePathFromURI(updated.Metadata.Source.URI)
		if err != nil {
			return nil, err
		}
		info, err := os.Lstat(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read imported knowledge source: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("imported knowledge source must be a regular file: %s", sourcePath)
		}
		originalSource, err = os.ReadFile(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read imported knowledge source: %w", err)
		}
		sourceMode = info.Mode().Perm()
		newSource := []byte(replaceImportedTitle(string(originalSource), updated.Metadata.Title))
		if err := atomicWrite(sourcePath, newSource, sourceMode); err != nil {
			return nil, fmt.Errorf("write imported knowledge source: %w", err)
		}
		updated.Content = strings.TrimSpace(string(newSource))
		hash := sha256.Sum256(newSource)
		updated.Metadata.Source.Hash = "sha256:" + hex.EncodeToString(hash[:])
		updated.Metadata.Source.ImportedAt = time.Now().UTC()
	}

	updated.Metadata.UpdatedAt = time.Now().UTC()
	targetPath := document.Path
	if updated.Collection != document.Collection || updated.Metadata.Title != document.Metadata.Title {
		targetPath, err = readableDocumentPath(dataDir, updated.Collection, updated.Metadata.Title, updated.Metadata.ID)
		if err != nil {
			rollbackImportedSource(sourcePath, originalSource, sourceMode)
			return nil, err
		}
	}
	if err := writeDocumentAt(targetPath, &updated); err != nil {
		rollbackImportedSource(sourcePath, originalSource, sourceMode)
		return nil, err
	}
	if targetPath != document.Path {
		if err := os.Remove(document.Path); err != nil {
			_ = os.Remove(targetPath)
			rollbackImportedSource(sourcePath, originalSource, sourceMode)
			return nil, fmt.Errorf("remove previous knowledge document %s: %w", document.Path, err)
		}
	}
	return &updated, nil
}

func rollbackImportedSource(path string, content []byte, mode os.FileMode) {
	if path != "" && content != nil {
		_ = atomicWrite(path, content, mode)
	}
}

func replaceImportedTitle(original, title string) string {
	lines := strings.Split(strings.ReplaceAll(original, "\r\n", "\n"), "\n")
	frontmatterEnd := -1
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for index := 1; index < len(lines); index++ {
			if strings.TrimSpace(lines[index]) == "---" {
				frontmatterEnd = index
				break
			}
			if strings.HasPrefix(strings.TrimSpace(lines[index]), "title:") {
				encoded, _ := yaml.Marshal(map[string]string{"title": title})
				lines[index] = strings.TrimSpace(string(encoded))
			}
		}
	}
	start := frontmatterEnd + 1
	for index := start; index < len(lines); index++ {
		if strings.HasPrefix(strings.TrimSpace(lines[index]), "# ") {
			lines[index] = "# " + title
			return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
		}
	}
	insertAt := start
	for insertAt < len(lines) && strings.TrimSpace(lines[insertAt]) == "" {
		insertAt++
	}
	prefix := append([]string{}, lines[:insertAt]...)
	prefix = append(prefix, "# "+title, "")
	prefix = append(prefix, lines[insertAt:]...)
	return strings.TrimRight(strings.Join(prefix, "\n"), "\n") + "\n"
}

func updateImportedFile(document *Document, content string) (*Document, error) {
	sourcePath, err := filePathFromURI(document.Metadata.Source.URI)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read imported knowledge source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("imported knowledge source must be a regular file: %s", sourcePath)
	}
	original, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read imported knowledge source: %w", err)
	}
	updatedSource := replaceImportedBody(string(original), content, document.Metadata.Title)
	if err := atomicWrite(sourcePath, []byte(updatedSource), info.Mode().Perm()); err != nil {
		return nil, fmt.Errorf("write imported knowledge source: %w", err)
	}

	template := AddDocumentInput{
		Title:      document.Metadata.Title,
		Collection: document.Collection,
		Domains:    document.Metadata.Domains,
		Tags:       document.Metadata.Tags,
		Projects:   document.Metadata.Projects,
		Producer:   document.Metadata.Producer,
	}
	updated, importErr := importFile(
		sourcePath,
		document.Path,
		document.Collection,
		document.Metadata.ID,
		document,
		template,
	)
	if importErr == nil {
		return updated, nil
	}
	if rollbackErr := atomicWrite(sourcePath, original, info.Mode().Perm()); rollbackErr != nil {
		return nil, fmt.Errorf("re-import edited source: %v; rollback source: %w", importErr, rollbackErr)
	}
	return nil, fmt.Errorf("re-import edited source: %w", importErr)
}

func filePathFromURI(rawURI string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURI))
	if err != nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") {
		return "", fmt.Errorf("invalid imported knowledge file URI %q", rawURI)
	}
	path, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || !filepath.IsAbs(path) {
		return "", fmt.Errorf("invalid imported knowledge file URI %q", rawURI)
	}
	return filepath.Clean(path), nil
}

func replaceImportedBody(original, content, title string) string {
	prefix := ""
	body := strings.TrimLeft(original, "\r\n")
	if strings.HasPrefix(body, "---\n") {
		if closing := strings.Index(body[4:], "\n---\n"); closing >= 0 {
			end := 4 + closing + len("\n---\n")
			prefix = strings.TrimRight(body[:end], "\r\n")
			body = strings.TrimLeft(body[end:], "\r\n")
		}
	}
	heading := leadingTitleHeading(body, title)
	parts := make([]string, 0, 3)
	if prefix != "" {
		parts = append(parts, prefix)
	}
	if heading != "" && leadingTitleHeading(content, title) == "" {
		parts = append(parts, heading)
	}
	parts = append(parts, strings.TrimSpace(content))
	return strings.Join(parts, "\n\n") + "\n"
}

func leadingTitleHeading(content, title string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimLeft(content, "\r\n"), "\n", 2)[0])
	if !strings.HasPrefix(line, "# ") {
		return ""
	}
	if normalizedKnowledgeTitle(strings.TrimPrefix(line, "# ")) != normalizedKnowledgeTitle(title) {
		return ""
	}
	return line
}

func normalizedKnowledgeTitle(value string) string {
	value = strings.NewReplacer("“", "\"", "”", "\"").Replace(value)
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func Import(dataDir, sourcePath string, template AddDocumentInput) ([]Document, error) {
	mutationLock, lockErr := executionlock.Acquire(context.Background(), dataDir, "knowledge")
	if lockErr != nil {
		return nil, lockErr
	}
	defer mutationLock.Close()

	canonical, err := filepath.Abs(sourcePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("knowledge import does not follow symbolic links: %s", sourcePath)
	}
	var paths []string
	var importRoot string
	if info.IsDir() {
		importRoot = canonical
		err = filepath.WalkDir(canonical, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if path != canonical && strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == "_collection.md" {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".md") {
				paths = append(paths, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		if !strings.EqualFold(filepath.Ext(canonical), ".md") {
			return nil, fmt.Errorf("knowledge import only accepts Markdown files")
		}
		paths = append(paths, canonical)
		importRoot = filepath.Dir(canonical)
	}
	sort.Strings(paths)
	existingDocuments, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	existingByID := make(map[string]Document, len(existingDocuments))
	for _, document := range existingDocuments {
		existingByID[document.Metadata.ID] = document
	}
	documents := make([]Document, 0, len(paths))
	for _, path := range paths {
		targetPath, collection, err := readableImportPath(dataDir, importRoot, path, info.IsDir(), template.Collection)
		if err != nil {
			return nil, err
		}
		pathHash := sha256.Sum256([]byte(path))
		id := "document:import:" + hex.EncodeToString(pathHash[:8])
		existing, found := existingByID[id]
		var existingDocument *Document
		if found {
			existingDocument = &existing
		}
		document, err := importFile(path, targetPath, collection, id, existingDocument, template)
		if err != nil {
			return nil, err
		}
		documents = append(documents, *document)
		existingByID[id] = *document
	}
	return documents, nil
}

func importFile(path, targetPath, collection, id string, existing *Document, template AddDocumentInput) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return nil, fmt.Errorf("cannot import empty knowledge document: %s", path)
	}
	contentHash := sha256.Sum256(data)
	targetPath, err = availableImportPath(targetPath, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	createdAt := now
	if targetExisting, err := readDocument(targetPath); err == nil {
		createdAt = targetExisting.Metadata.CreatedAt
	} else if existing != nil {
		createdAt = existing.Metadata.CreatedAt
	}
	title := strings.TrimSpace(template.Title)
	if title == "" {
		title = markdownTitle(content, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}
	producer := strings.TrimSpace(template.Producer)
	if producer == "" {
		producer = "atm-import"
	}
	status := "active"
	if existing != nil {
		status = existing.Metadata.Status
	}
	document := &Document{
		Metadata: DocumentMetadata{
			ID:            id,
			SchemaVersion: KnowledgeSchemaVersion,
			Title:         title,
			Status:        status,
			Domains:       normalizeValues(template.Domains),
			Tags:          normalizeValues(template.Tags),
			Projects:      normalizeValues(template.Projects),
			Producer:      producer,
			CreatedAt:     createdAt,
			UpdatedAt:     now,
			Source: &SourceInfo{
				Type:       "file",
				URI:        "file://" + filepath.ToSlash(path),
				Hash:       "sha256:" + hex.EncodeToString(contentHash[:]),
				ImportedAt: now,
			},
		},
		Collection: collection,
		Content:    content,
	}
	if err := writeDocumentAt(targetPath, document); err != nil {
		return nil, err
	}
	if existing != nil && existing.Path != targetPath {
		if err := os.Remove(existing.Path); err != nil {
			return nil, fmt.Errorf("remove previous knowledge document %s: %w", existing.Path, err)
		}
	}
	return document, nil
}

func readableImportPath(dataDir, importRoot, path string, directoryImport bool, requestedCollection string) (string, string, error) {
	relative, err := filepath.Rel(importRoot, path)
	if err != nil {
		return "", "", err
	}
	if relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("knowledge import path escapes source root: %s", path)
	}
	collection, err := normalizeCollection(requestedCollection)
	if err != nil {
		return "", "", err
	}
	root := knowledgeRoot(dataDir)
	if collection != "" {
		root = filepath.Join(root, collection)
	} else if !directoryImport {
		collection = "inbox"
		root = filepath.Join(root, collection)
	} else if filepath.Dir(relative) == "." {
		collection = "inbox"
		root = filepath.Join(root, collection)
	}
	target := filepath.Join(root, relative)
	if collection == "" {
		collection = collectionFromPath(knowledgeRoot(dataDir), target)
	}
	return target, collection, nil
}

func availableImportPath(path, id string) (string, error) {
	existing, err := readDocument(path)
	if err == nil && existing.Metadata.ID == id {
		return path, nil
	}
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return path, nil
		}
	}
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	idHash := sha256.Sum256([]byte(id))
	suffix := hex.EncodeToString(idHash[:4])
	return base + "--" + suffix + extension, nil
}

func readDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	frontmatter, content, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("read knowledge document %s: %w", path, err)
	}
	var metadata DocumentMetadata
	if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
		return nil, fmt.Errorf("read knowledge document %s: %w", path, err)
	}
	if metadata.SchemaVersion != KnowledgeSchemaVersion {
		return nil, fmt.Errorf("read knowledge document %s: unsupported schema version %d", path, metadata.SchemaVersion)
	}
	if strings.TrimSpace(metadata.ID) == "" || strings.TrimSpace(metadata.Title) == "" {
		return nil, fmt.Errorf("read knowledge document %s: id and title are required", path)
	}
	return &Document{Metadata: metadata, Path: path, Content: strings.TrimSpace(content)}, nil
}

func writeDocumentAt(path string, document *Document) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	content, err := marshalDocument(document)
	if err != nil {
		return err
	}
	if err := atomicWrite(path, content, 0600); err != nil {
		return err
	}
	document.Path = path
	return nil
}

// createDocumentAt publishes a complete new document without ever replacing
// an existing entry. The name decision and publication are one OS operation,
// so CLI and Web creates do not rely on sharing an in-process mutex. A loser
// retries with its own document identity while preserving the winning file.
func createDocumentAt(path string, document *Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	content, err := marshalDocument(document)
	if err != nil {
		return err
	}
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)
	idHash := sha256.Sum256([]byte(document.Metadata.ID))
	suffix := hex.EncodeToString(idHash[:4])
	for attempt := 0; attempt < 256; attempt++ {
		target := path
		if attempt == 1 {
			target = base + "--" + suffix + extension
		} else if attempt > 1 {
			target = fmt.Sprintf("%s--%s-%d%s", base, suffix, attempt, extension)
		}
		if err := atomicWriteNew(target, content, 0600); err != nil {
			if os.IsExist(err) {
				continue
			}
			return err
		}
		document.Path = target
		return nil
	}
	return fmt.Errorf("cannot choose an unused knowledge document filename: %s", path)
}

func marshalDocument(document *Document) ([]byte, error) {
	frontmatter, err := yaml.Marshal(document.Metadata)
	if err != nil {
		return nil, err
	}
	content := append([]byte("---\n"), frontmatter...)
	return append(content, []byte("---\n\n"+strings.TrimSpace(document.Content)+"\n")...), nil
}

func knowledgeRoot(dataDir string) string {
	return filepath.Join(dataDir, "knowledge")
}

func readableDocumentPath(dataDir, collection, title, id string) (string, error) {
	base := filepath.Join(knowledgeRoot(dataDir), collection, safeMarkdownName(title)+".md")
	return availableImportPath(base, id)
}

// requireCollection is normalizeCollection for the callers that also refuse an
// empty id. normalizeCollection returns ("", nil) for empty because Add treats
// that as "the default collection", so every collection command had to unpack
// the two cases by hand — five copies of the same nested re-check. label names
// which id was missing, so rename can tell its two arguments apart.
func requireCollection(label, value string) (string, error) {
	id, err := normalizeCollection(value)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("%s must not be empty", label)
	}
	return id, nil
}

func normalizeCollection(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if value == "." || value == ".." || strings.HasPrefix(value, "_") || filepath.Base(value) != value || strings.ContainsAny(value, `<>:"/\\|?*`) {
		return "", fmt.Errorf("invalid knowledge collection %q", value)
	}
	return value, nil
}

func safeMarkdownName(title string) string {
	title = strings.TrimSpace(title)
	var builder strings.Builder
	for _, r := range title {
		if strings.ContainsRune(`<>:"/\\|?*`, r) || r < 32 {
			builder.WriteRune('_')
		} else {
			builder.WriteRune(r)
		}
	}
	name := strings.Trim(strings.TrimSpace(builder.String()), ".")
	if name == "" {
		return "untitled"
	}
	return name
}

func splitFrontmatter(content string) (string, string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", "", fmt.Errorf("missing YAML frontmatter")
	}
	rest := strings.TrimPrefix(content, "---\n")
	index := strings.Index(rest, "\n---\n")
	if index < 0 {
		return "", "", fmt.Errorf("unterminated YAML frontmatter")
	}
	return rest[:index], rest[index+5:], nil
}

func normalizeValues(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	return atomicPublish(path, content, mode, os.Rename)
}

// Link publishes the fully written temporary inode only if path is still
// absent. Unlike Rename, it cannot overwrite a concurrent creator, a symlink,
// or an unrelated existing file. Both names are in one directory/filesystem.
func atomicWriteNew(path string, content []byte, mode os.FileMode) error {
	return atomicPublish(path, content, mode, os.Link)
}

func atomicPublish(path string, content []byte, mode os.FileMode, publish func(string, string) error) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".atm-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return publish(temporaryPath, path)
}
