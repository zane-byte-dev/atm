package knowledge

import (
	"context"
	"errors"
	"github.com/zane-byte-dev/atm/internal/application"
	"os"
	"testing"
)

func TestManagedMoveAndArchiveRestore(t *testing.T) {
	dir := t.TempDir()
	doc, err := Add(dir, AddDocumentInput{Title: "Actions", Content: "Keep body", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateCollection(dir, "target", EditCollectionInput{})
	if err != nil {
		t.Fatal(err)
	}
	target := "target"
	moved, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: doc.Metadata.ID, ETag: VersionDocument(*doc).ETag, Content: doc.Content, Collection: &target})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Collection != target || moved.Metadata.ID != doc.Metadata.ID || moved.Content != doc.Content {
		t.Fatalf("bad move: %+v", moved)
	}
	if _, err := os.Stat(doc.Path); !os.IsNotExist(err) {
		t.Fatal("original still exists", err)
	}
	if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: doc.Metadata.ID, ETag: VersionDocument(*doc).ETag, Content: "stale"}); !errors.Is(err, application.ErrConflict) {
		t.Fatal("expected stale revision conflict", err)
	}
	for _, status := range []string{"archived", "active"} {
		moved, err = UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: moved.Metadata.ID, ETag: moved.ETag, Content: moved.Content, Status: &status})
		if err != nil {
			t.Fatal(err)
		}
		if moved.Metadata.Status != status || moved.Content != "Keep body" {
			t.Fatal("status change lost content")
		}
	}
	missing := "missing"
	if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: moved.Metadata.ID, ETag: moved.ETag, Content: "bad", Collection: &missing}); err == nil {
		t.Fatal("unknown collection accepted")
	}
	current, err := Get(dir, moved.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if VersionDocument(*current).ETag != moved.ETag {
		t.Fatal("failed move changed document")
	}
}

func TestManagedImportedActionsPreserveSource(t *testing.T) {
	dir := t.TempDir()
	external := t.TempDir() + "/source.md"
	const content = "# Source\n\nKeep original\n"
	if err := os.WriteFile(external, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	docs, err := Import(dir, external, AddDocumentInput{Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	version := VersionDocument(docs[0])
	status := "archived"
	archived, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: version.Metadata.ID, ETag: version.ETag, Content: version.Content, Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	if archived.Editable || archived.Metadata.Source.URI != version.Metadata.Source.URI {
		t.Fatal("provenance changed")
	}
	if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: archived.Metadata.ID, ETag: archived.ETag, Content: "replacement", Status: &status}); !errors.Is(err, application.ErrForbidden) {
		t.Fatal("metadata action permitted body edit", err)
	}
	actual, err := os.ReadFile(external)
	if err != nil || string(actual) != content {
		t.Fatal("external source changed", err)
	}
}
