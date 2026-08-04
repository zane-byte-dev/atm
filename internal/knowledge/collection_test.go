package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stringPointer(value string) *string       { return &value }
func stringsPointer(values []string) *[]string { return &values }

func TestCollectionCreateEditAndCatalogEmptyManifest(t *testing.T) {
	dataDir := newDataDir(t)
	created, err := CreateCollection(dataDir, "research", EditCollectionInput{
		Name: stringPointer("Research"), Description: stringPointer("Primary research"), Topics: stringsPointer([]string{"AI", "Agents"}),
	})
	if err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if created.ID != "research" || created.Name != "Research" {
		t.Fatalf("created = %#v", created)
	}
	catalog, err := Catalog(dataDir)
	if err != nil || len(catalog) != 1 || catalog[0].DocumentCount != 0 {
		t.Fatalf("catalog = %#v, err = %v", catalog, err)
	}
	manifestPath := filepath.Join(dataDir, "knowledge", "research", "_collection.md")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(manifest, []byte("\nPreserved body.\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	empty := ""
	edited, err := EditCollection(dataDir, "research", EditCollectionInput{Name: stringPointer("Lab"), Role: &empty, UseWhen: stringsPointer([]string{"deep analysis"})})
	if err != nil {
		t.Fatalf("edit collection: %v", err)
	}
	if edited.Name != "Lab" || edited.Role != "" || len(edited.UseWhen) != 1 {
		t.Fatalf("edited = %#v", edited)
	}
	updatedManifest, _ := os.ReadFile(manifestPath)
	if !strings.Contains(string(updatedManifest), "Preserved body.") {
		t.Fatalf("manifest body was not preserved:\n%s", updatedManifest)
	}
}

func TestCollectionRenameMovesDocumentsAndManifestID(t *testing.T) {
	dataDir := newDataDir(t)
	if _, err := CreateCollection(dataDir, "research", EditCollectionInput{Name: stringPointer("Research")}); err != nil {
		t.Fatal(err)
	}
	document, err := Add(dataDir, AddDocumentInput{Title: "Finding", Content: "Evidence", Collection: "research"})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := RenameCollection(dataDir, "research", "evidence")
	if err != nil {
		t.Fatalf("rename collection: %v", err)
	}
	if renamed.ID != "evidence" || renamed.DocumentCount != 1 {
		t.Fatalf("renamed = %#v", renamed)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil || loaded.Collection != "evidence" {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dataDir, "knowledge", "evidence", "_collection.md"))
	if !strings.Contains(string(manifest), "id: evidence") {
		t.Fatalf("manifest id not updated:\n%s", manifest)
	}
}

func TestCollectionDeleteRequiresForceOrMovesDocuments(t *testing.T) {
	dataDir := newDataDir(t)
	for _, id := range []string{"source", "archive"} {
		if _, err := CreateCollection(dataDir, id, EditCollectionInput{}); err != nil {
			t.Fatal(err)
		}
	}
	document, err := Add(dataDir, AddDocumentInput{Title: "Move me", Content: "Body", Collection: "source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteCollection(dataDir, "source", DeleteCollectionOptions{}); err == nil {
		t.Fatal("expected non-empty collection deletion to fail")
	}
	result, err := DeleteCollection(dataDir, "source", DeleteCollectionOptions{MoveTo: "archive"})
	if err != nil {
		t.Fatalf("delete with move: %v", err)
	}
	if result.MovedDocuments != 1 || result.MovedTo != "archive" {
		t.Fatalf("result = %#v", result)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil || loaded.Collection != "archive" {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func TestCollectionDeleteMovePreservesNestedDocumentPaths(t *testing.T) {
	dataDir := newDataDir(t)
	for _, id := range []string{"source", "archive"} {
		if _, err := CreateCollection(dataDir, id, EditCollectionInput{}); err != nil {
			t.Fatal(err)
		}
	}
	document, err := Add(dataDir, AddDocumentInput{Title: "Nested", Content: "Body", Collection: "source"})
	if err != nil {
		t.Fatal(err)
	}
	nestedDir := filepath.Join(dataDir, "knowledge", "source", "year", "month")
	if err := os.MkdirAll(nestedDir, 0700); err != nil {
		t.Fatal(err)
	}
	nestedPath := filepath.Join(nestedDir, filepath.Base(document.Path))
	if err := os.Rename(document.Path, nestedPath); err != nil {
		t.Fatal(err)
	}

	if _, err := DeleteCollection(dataDir, "source", DeleteCollectionOptions{MoveTo: "archive"}); err != nil {
		t.Fatalf("delete with nested move: %v", err)
	}
	wantPath := filepath.Join(dataDir, "knowledge", "archive", "year", "month", filepath.Base(document.Path))
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("nested destination was not preserved: %v", err)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil || loaded.Collection != "archive" || loaded.Path != wantPath {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func TestCollectionRenameAggregatesRootInboxDocuments(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{Title: "Loose note", Content: "Body", Collection: "inbox"})
	if err != nil {
		t.Fatal(err)
	}
	root := knowledgeRoot(dataDir)
	rootPath := filepath.Join(root, filepath.Base(document.Path))
	if err := os.Rename(document.Path, rootPath); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "inbox")); err != nil {
		t.Fatal(err)
	}
	renamed, err := RenameCollection(dataDir, "inbox", "captured")
	if err != nil {
		t.Fatalf("rename inbox: %v", err)
	}
	if renamed.DocumentCount != 1 {
		t.Fatalf("renamed = %#v", renamed)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil || loaded.Collection != "captured" {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}
