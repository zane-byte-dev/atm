package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

func seedQueryPageDocuments(t *testing.T, count int) (string, Service) {
	t.Helper()
	dir := newDataDir(t)
	if _, err := CreateCollection(dir, "notes", EditCollectionInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCollection(dir, "other", EditCollectionInput{}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < count; index++ {
		document := Document{Collection: "notes", Content: "marker knowledge content", Metadata: DocumentMetadata{
			ID: fmt.Sprintf("doc:%04d", index), SchemaVersion: KnowledgeSchemaVersion, Title: fmt.Sprintf("Document %04d", index),
			Status: "active", Producer: "test", CreatedAt: time.Unix(100, 0).UTC(), UpdatedAt: time.Unix(100, 0).UTC(),
		}}
		if index == 0 {
			document.Metadata.Title = "marker"
			document.Content = strings.Repeat("marker strongest repeated chunk "+strings.Repeat("detail ", 90)+"\n\n", 20)
		}
		data, err := marshalDocument(&document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "knowledge", "notes", fmt.Sprintf("%04d.md", index)), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, NewService(ServiceOptions{DataDir: dir, FeedbackStore: &stubFeedbackStore{}})
}

func readEveryKnowledgePage(t *testing.T, service Service, input QueryPageInput) []DocumentView {
	t.Helper()
	var rows []DocumentView
	seen := make(map[string]bool)
	for pageNumber := 0; ; pageNumber++ {
		if pageNumber > 20 {
			t.Fatal("page cursor did not terminate")
		}
		page, err := service.QueryPage(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Documents) > input.Limit {
			t.Fatalf("page exceeded limit: %d > %d", len(page.Documents), input.Limit)
		}
		for _, document := range page.Documents {
			if seen[document.DocumentID] {
				t.Fatalf("document repeated across pages: %s", document.DocumentID)
			}
			seen[document.DocumentID] = true
			rows = append(rows, document)
		}
		if !page.HasMore {
			if page.NextCursor != "" || len(rows) != page.Total {
				t.Fatalf("incomplete terminal page: loaded=%d page=%+v", len(rows), page)
			}
			return rows
		}
		if len(page.Documents) != input.Limit || page.NextCursor == "" || page.NextCursor == input.Cursor {
			t.Fatalf("incomplete nonterminal page: %+v", page)
		}
		input.Cursor = page.NextCursor
	}
}

func TestKnowledgeQueryPageBrowsesAndSearchesAll205Documents(t *testing.T) {
	_, service := seedQueryPageDocuments(t, 205)
	browse := readEveryKnowledgePage(t, service, QueryPageInput{Collection: "notes", Limit: 50})
	if len(browse) != 205 {
		t.Fatalf("browse returned %d documents", len(browse))
	}
	search := readEveryKnowledgePage(t, service, QueryPageInput{Text: "marker", Collection: "notes", Limit: 50})
	if len(search) != 205 {
		t.Fatalf("search returned %d documents", len(search))
	}
	// Compare with the existing chunk ranking, taking only the strongest hit
	// for each document. A many-chunk document must consume one page slot.
	ranked, err := service.Search(context.Background(), SearchInput{Query: "marker", Options: SearchOptions{Limit: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked.Hits) <= 205 {
		t.Fatal("fixture must contain duplicate document chunks")
	}
	seen := map[string]bool{}
	var expected []SearchHit
	for _, hit := range ranked.Hits {
		if !seen[hit.DocumentID] {
			seen[hit.DocumentID] = true
			expected = append(expected, hit)
		}
	}
	for index, document := range search {
		if document.DocumentID != expected[index].DocumentID || document.Score == nil || *document.Score != expected[index].Score || document.Snippet != expected[index].Snippet {
			t.Fatalf("ranking changed at %d: document=%+v hit=%+v", index, document, expected[index])
		}
	}
}

func TestKnowledgeQueryCursorRejectsChangedScopeOrCorpusAndRefreshIsComplete(t *testing.T) {
	dir, service := seedQueryPageDocuments(t, 205)
	input := QueryPageInput{Collection: "notes", ExcludeArchived: true, Limit: 50}
	first, err := service.QueryPage(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	expectStale := func(input QueryPageInput) {
		t.Helper()
		_, err := service.QueryPage(context.Background(), input)
		var appErr *application.Error
		if !errors.As(err, &appErr) || appErr.Code != application.CodeConflict || appErr.Details["reason"] != "knowledge_cursor_stale" {
			t.Fatalf("expected stale cursor, got %v", err)
		}
	}
	changedScope := input
	changedScope.Collection, changedScope.Cursor = "other", first.NextCursor
	expectStale(changedScope)
	archivedID, movedID := first.Documents[0].DocumentID, first.Documents[1].DocumentID
	for _, change := range []struct{ id, status, collection string }{{archivedID, "archived", "notes"}, {movedID, "active", "other"}} {
		document, err := service.Get(context.Background(), GetInput{DocumentID: change.id})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := UpdateManaged(context.Background(), dir, UpdateManagedInput{DocumentID: change.id, ETag: VersionDocument(document).ETag,
			Content: document.Content, Status: &change.status, Collection: &change.collection}); err != nil {
			t.Fatal(err)
		}
	}
	input.Cursor = first.NextCursor
	expectStale(input)
	input.Cursor = ""
	refreshed := readEveryKnowledgePage(t, service, input)
	if len(refreshed) != 203 {
		t.Fatalf("refreshed source has %d documents", len(refreshed))
	}
	for _, document := range refreshed {
		if document.DocumentID == archivedID || document.DocumentID == movedID {
			t.Fatalf("removed document remains in source: %+v", document)
		}
	}
	target := readEveryKnowledgePage(t, service, QueryPageInput{Collection: "other", Limit: 50})
	if len(target) != 1 || target[0].DocumentID != movedID {
		t.Fatalf("move destination = %+v", target)
	}
	archived := readEveryKnowledgePage(t, service, QueryPageInput{Collection: "notes", Status: "archived", Limit: 50})
	if len(archived) != 1 || archived[0].DocumentID != archivedID {
		t.Fatalf("archive = %+v", archived)
	}
}

func TestKnowledgeQueryPageBoundsAndEmptyWireShape(t *testing.T) {
	service := NewService(ServiceOptions{DataDir: newDataDir(t), FeedbackStore: &stubFeedbackStore{}})
	page, err := service.QueryPage(context.Background(), QueryPageInput{Limit: 50})
	if err != nil || page.Total != 0 || page.HasMore || page.NextCursor != "" || !reflect.DeepEqual(page.Documents, []DocumentView{}) {
		t.Fatalf("empty page = %+v, err=%v", page, err)
	}
	for _, input := range []QueryPageInput{{Limit: 201}, {Limit: -1}, {Cursor: "broken"}, {Cursor: strings.Repeat("x", 1025)}, {Status: "deleted"}} {
		if _, err := service.QueryPage(context.Background(), input); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("accepted invalid page input %+v: %v", input, err)
		}
	}
}
