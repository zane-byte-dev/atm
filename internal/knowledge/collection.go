package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EditCollectionInput struct {
	Name         *string
	Description  *string
	Role         *string
	Topics       *[]string
	UseWhen      *[]string
	AvoidWhen    *[]string
	Instructions *[]string
}

type DeleteCollectionOptions struct {
	Force  bool
	MoveTo string
}

type DeleteCollectionResult struct {
	ID             string `json:"id"`
	MovedTo        string `json:"moved_to,omitempty"`
	MovedDocuments int    `json:"moved_documents"`
}

func CreateCollection(dataDir, id string, input EditCollectionInput) (*CollectionInfo, error) {
	id, err := requireCollection("knowledge collection id", id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(knowledgeRoot(dataDir), id)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("knowledge collection already exists: %s", id)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info := CollectionInfo{ID: id, Name: id}
	applyCollectionEdit(&info, input)
	if strings.TrimSpace(info.Name) == "" {
		info.Name = id
	}
	if err := writeCollectionManifest(filepath.Join(dir, "_collection.md"), info, ""); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return &info, nil
}

func EditCollection(dataDir, id string, input EditCollectionInput) (*CollectionInfo, error) {
	id, err := requireCollection("knowledge collection id", id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(knowledgeRoot(dataDir), id)
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		if id != "inbox" || !collectionHasRootInboxDocuments(dataDir) {
			return nil, fmt.Errorf("knowledge collection not found: %s", id)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
	}
	manifestPath := filepath.Join(dir, "_collection.md")
	info := CollectionInfo{ID: id, Name: id}
	body := ""
	if data, readErr := os.ReadFile(manifestPath); readErr == nil {
		frontmatter, existingBody, splitErr := splitFrontmatter(string(data))
		if splitErr != nil {
			return nil, splitErr
		}
		if err := yaml.Unmarshal([]byte(frontmatter), &info); err != nil {
			return nil, err
		}
		info.ID = id
		body = existingBody
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	applyCollectionEdit(&info, input)
	if err := writeCollectionManifest(manifestPath, info, body); err != nil {
		return nil, err
	}
	return &info, nil
}

func RenameCollection(dataDir, id, newID string) (*CollectionInfo, error) {
	id, err := requireCollection("knowledge collection id", id)
	if err != nil {
		return nil, err
	}
	newID, err = requireCollection("new knowledge collection id", newID)
	if err != nil {
		return nil, err
	}
	if id == newID {
		return nil, fmt.Errorf("new knowledge collection id must differ from the current id")
	}
	root := knowledgeRoot(dataDir)
	oldDir := filepath.Join(root, id)
	newDir := filepath.Join(root, newID)
	if _, err := os.Stat(newDir); err == nil {
		return nil, fmt.Errorf("knowledge collection already exists: %s", newID)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	documents, err := documentsInCollection(dataDir, id)
	if err != nil {
		return nil, err
	}
	oldExists := false
	if info, statErr := os.Stat(oldDir); statErr == nil && info.IsDir() {
		oldExists = true
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if !oldExists && len(documents) == 0 {
		return nil, fmt.Errorf("knowledge collection not found: %s", id)
	}
	if oldExists {
		if err := os.Rename(oldDir, newDir); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(newDir, 0700); err != nil {
		return nil, err
	}
	if id == "inbox" {
		if err := moveRootInboxDocuments(dataDir, newDir); err != nil {
			if oldExists {
				_ = os.Rename(newDir, oldDir)
			}
			return nil, err
		}
	}
	manifestPath := filepath.Join(newDir, "_collection.md")
	if data, readErr := os.ReadFile(manifestPath); readErr == nil {
		frontmatter, body, splitErr := splitFrontmatter(string(data))
		if splitErr != nil {
			return nil, splitErr
		}
		var info CollectionInfo
		if err := yaml.Unmarshal([]byte(frontmatter), &info); err != nil {
			return nil, err
		}
		info.ID = newID
		if err := writeCollectionManifest(manifestPath, info, body); err != nil {
			return nil, err
		}
	}
	return collectionByID(dataDir, newID)
}

func DeleteCollection(dataDir, id string, options DeleteCollectionOptions) (*DeleteCollectionResult, error) {
	id, err := requireCollection("knowledge collection id", id)
	if err != nil {
		return nil, err
	}
	documents, err := documentsInCollection(dataDir, id)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(knowledgeRoot(dataDir), id)
	dirExists := false
	if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
		dirExists = true
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if !dirExists && len(documents) == 0 {
		return nil, fmt.Errorf("knowledge collection not found: %s", id)
	}
	result := &DeleteCollectionResult{ID: id}
	if strings.TrimSpace(options.MoveTo) != "" {
		target, normalizeErr := normalizeCollection(options.MoveTo)
		if normalizeErr != nil || target == "" || target == id {
			if normalizeErr != nil {
				return nil, normalizeErr
			}
			return nil, fmt.Errorf("--move-to must name a different knowledge collection")
		}
		if _, err := collectionByID(dataDir, target); err != nil {
			return nil, fmt.Errorf("move target: %w", err)
		}
		targetDir := filepath.Join(knowledgeRoot(dataDir), target)
		if err := os.MkdirAll(targetDir, 0700); err != nil {
			return nil, err
		}
		if err := moveDocumentsToDirectory(documents, filepath.Join(knowledgeRoot(dataDir), id), targetDir); err != nil {
			return nil, err
		}
		result.MovedTo = target
		result.MovedDocuments = len(documents)
	} else if len(documents) > 0 && !options.Force {
		return nil, fmt.Errorf("knowledge collection %s is not empty (%d documents); use --force or --move-to", id, len(documents))
	}
	if dirExists {
		if err := os.RemoveAll(dir); err != nil {
			return nil, err
		}
	}
	if id == "inbox" && result.MovedTo == "" && options.Force {
		for _, document := range documents {
			if filepath.Dir(document.Path) == knowledgeRoot(dataDir) {
				if err := os.Remove(document.Path); err != nil && !os.IsNotExist(err) {
					return nil, err
				}
			}
		}
	}
	return result, nil
}

func applyCollectionEdit(info *CollectionInfo, input EditCollectionInput) {
	if input.Name != nil {
		info.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		info.Description = strings.TrimSpace(*input.Description)
	}
	if input.Role != nil {
		info.Role = strings.TrimSpace(*input.Role)
	}
	if input.Topics != nil {
		info.Topics = normalizeOrderedValues(*input.Topics)
	}
	if input.UseWhen != nil {
		info.UseWhen = normalizeOrderedValues(*input.UseWhen)
	}
	if input.AvoidWhen != nil {
		info.AvoidWhen = normalizeOrderedValues(*input.AvoidWhen)
	}
	if input.Instructions != nil {
		info.Instructions = normalizeOrderedValues(*input.Instructions)
	}
}

func writeCollectionManifest(path string, info CollectionInfo, body string) error {
	info.DocumentCount = 0
	frontmatter, err := yaml.Marshal(info)
	if err != nil {
		return err
	}
	content := append([]byte("---\n"), frontmatter...)
	content = append(content, []byte("---\n")...)
	if strings.TrimSpace(body) != "" {
		content = append(content, []byte("\n"+strings.TrimSpace(body)+"\n")...)
	}
	return atomicWrite(path, content, 0600)
}

func collectionByID(dataDir, id string) (*CollectionInfo, error) {
	catalog, err := Catalog(dataDir)
	if err != nil {
		return nil, err
	}
	for _, info := range catalog {
		if info.ID == id {
			copy := info
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("knowledge collection not found: %s", id)
}

func documentsInCollection(dataDir, id string) ([]Document, error) {
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	result := make([]Document, 0)
	for _, document := range documents {
		if document.Collection == id {
			result = append(result, document)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func collectionHasRootInboxDocuments(dataDir string) bool {
	documents, err := documentsInCollection(dataDir, "inbox")
	if err != nil {
		return false
	}
	root := knowledgeRoot(dataDir)
	for _, document := range documents {
		if filepath.Dir(document.Path) == root {
			return true
		}
	}
	return false
}

func moveRootInboxDocuments(dataDir, targetDir string) error {
	documents, err := documentsInCollection(dataDir, "inbox")
	if err != nil {
		return err
	}
	root := knowledgeRoot(dataDir)
	var rootDocuments []Document
	for _, document := range documents {
		if filepath.Dir(document.Path) == root {
			rootDocuments = append(rootDocuments, document)
		}
	}
	return moveDocumentsToDirectory(rootDocuments, root, targetDir)
}

func moveDocumentsToDirectory(documents []Document, sourceDir, targetDir string) error {
	targets := make([]string, len(documents))
	reserved := make(map[string]bool, len(documents))
	for index, document := range documents {
		relative, err := filepath.Rel(sourceDir, document.Path)
		if err != nil || relative == "." || relative == "" || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			relative = filepath.Base(document.Path)
		}
		target := filepath.Join(targetDir, relative)
		if reserved[target] {
			return fmt.Errorf("multiple knowledge documents would move to the same destination: %s", target)
		}
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("knowledge document already exists at destination: %s", target)
		} else if !os.IsNotExist(err) {
			return err
		}
		reserved[target] = true
		targets[index] = target
	}
	for index, document := range documents {
		if err := os.MkdirAll(filepath.Dir(targets[index]), 0700); err != nil {
			return err
		}
		if err := os.Rename(document.Path, targets[index]); err != nil {
			return err
		}
	}
	return nil
}
