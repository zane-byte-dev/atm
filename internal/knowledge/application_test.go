package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

func TestServiceQueryIsOneStableReadModelForBrowseAndSearch(t *testing.T) {
	dataDir := t.TempDir()
	active, err := Add(dataDir, AddDocumentInput{
		Title: "Active", Content: "typed knowledge marker", Collection: "notes", Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := Add(dataDir, AddDocumentInput{
		Title: "Archived", Content: "typed knowledge marker", Collection: "notes", Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	status := "archived"
	if _, err := Edit(dataDir, archived.Metadata.ID, EditDocumentInput{Status: &status}); err != nil {
		t.Fatal(err)
	}
	ledger := &stubFeedbackStore{}
	service := NewService(ServiceOptions{DataDir: dataDir, FeedbackStore: ledger})

	browse, err := service.Query(context.Background(), QueryInput{Collection: "notes", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if len(browse.Documents) != 1 || browse.Documents[0].DocumentID != active.Metadata.ID ||
		browse.Documents[0].CreatedAt == nil || browse.Documents[0].Snippet != "" {
		t.Fatalf("browse = %#v", browse)
	}

	search, err := service.Query(context.Background(), QueryInput{
		Text: "typed knowledge marker", Status: "active", SessionID: "s-query", Limit: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Documents) != 1 || search.Documents[0].DocumentID != active.Metadata.ID ||
		search.Documents[0].Score == nil || search.Documents[0].Snippet == "" {
		t.Fatalf("search = %#v", search)
	}
	if len(ledger.recorded) != 1 || ledger.recorded[0].DocumentID != active.Metadata.ID {
		t.Fatalf("retrievals = %#v", ledger.recorded)
	}
}

func TestServiceDocumentSaveUsesClosedTypedVariants(t *testing.T) {
	service := NewService(ServiceOptions{DataDir: t.TempDir(), FeedbackStore: &stubFeedbackStore{}})

	created, err := service.SaveDocument(context.Background(), SaveDocumentInput{
		Create: &CreateDocumentInput{
			Title: "Typed save", Content: "first body", Collection: "notes", Producer: "human",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Metadata.Title != "Typed save" || created.Collection != "notes" {
		t.Fatalf("created = %#v", created)
	}

	updated, err := service.SaveDocument(context.Background(), SaveDocumentInput{
		Content: &SetDocumentContentInput{DocumentID: created.Metadata.ID, Content: "second body"},
	})
	if err != nil || updated.Content != "second body" {
		t.Fatalf("content save = %#v, err = %v", updated, err)
	}

	title := "Renamed"
	tags := []string{}
	updated, err = service.SaveDocument(context.Background(), SaveDocumentInput{
		Metadata: &SetDocumentMetadataInput{DocumentID: created.Metadata.ID, Title: &title, Tags: &tags},
	})
	if err != nil || updated.Metadata.Title != title || len(updated.Metadata.Tags) != 0 {
		t.Fatalf("metadata save = %#v, err = %v", updated, err)
	}

	if _, err := service.SaveDocument(context.Background(), SaveDocumentInput{}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("empty variant error = %v", err)
	}
	if _, err := service.SaveDocument(context.Background(), SaveDocumentInput{
		Create:  &CreateDocumentInput{Title: "x", Content: "x"},
		Content: &SetDocumentContentInput{DocumentID: created.Metadata.ID, Content: "x"},
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("multiple variant error = %v", err)
	}
}

func TestServicePermanentDeletesRequireExplicitConfirmation(t *testing.T) {
	service := NewService(ServiceOptions{DataDir: t.TempDir(), FeedbackStore: &stubFeedbackStore{}})
	if _, err := service.DeleteDocument(context.Background(), DeleteDocumentInput{
		DocumentID: "document:any",
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed document delete error = %v", err)
	}
	if _, err := service.DeleteCollection(context.Background(), DeleteCollectionInput{
		ID: "notes", Force: true,
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("unconfirmed collection delete error = %v", err)
	}
}

func TestServiceCollectionSaveImportAndGovernance(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	service := NewService(ServiceOptions{
		DataDir: t.TempDir(), Now: func() time.Time { return now }, FeedbackStore: &stubFeedbackStore{},
	})

	created, err := service.SaveCollection(context.Background(), SaveCollectionInput{
		Create: &CreateCollectionInput{ID: "research", Name: applicationStringPointer("Research")},
	})
	if err != nil || created.ID != "research" || created.Name != "Research" {
		t.Fatalf("create collection = %#v, err = %v", created, err)
	}
	updated, err := service.SaveCollection(context.Background(), SaveCollectionInput{
		Update: &UpdateCollectionInput{ID: "research", Name: applicationStringPointer("研究")},
	})
	if err != nil || updated.Name != "研究" {
		t.Fatalf("update collection = %#v, err = %v", updated, err)
	}

	source := filepath.Join(t.TempDir(), "source.md")
	if err := os.WriteFile(source, []byte("# Imported\n\nTyped import body\n"), 0600); err != nil {
		t.Fatal(err)
	}
	documents, err := service.ImportDocument(context.Background(), ImportDocumentInput{
		Path: source, Collection: "research", Producer: "atm-desktop",
	})
	if err != nil || len(documents) != 1 || documents[0].Collection != "research" {
		t.Fatalf("import = %#v, err = %v", documents, err)
	}

	governance, err := service.Governance(context.Background(), GovernanceInput{StaleDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	if governance.Audit.GeneratedAt != now || governance.Audit.StaleDays != 30 ||
		governance.Quality.Totals.Documents != 1 {
		t.Fatalf("governance = %#v", governance)
	}
}

func applicationStringPointer(value string) *string { return &value }
