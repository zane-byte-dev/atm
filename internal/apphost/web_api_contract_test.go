package apphost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

// This fixture is consumed by the Web type checks as well as this Go test. It
// freezes Go's actual JSON null/omission/array behavior, which a hand-written TS
// sample can accidentally hide. The store contains only the synthetic records
// below and is isolated by testHost; no user task or knowledge data is read.
func TestWebAPIWireContracts(t *testing.T) {
	actual, err := json.MarshalIndent(webAPIContractValues(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	fixture := filepath.Join("..", "..", "app", "web", "tests", "fixtures", "api-wire-contracts.json")
	if os.Getenv("UPDATE_WEB_API_FIXTURES") == "1" {
		if err := os.MkdirAll(filepath.Dir(fixture), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixture, actual, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read Web API fixture: %v; regenerate explicitly with UPDATE_WEB_API_FIXTURES=1 go test ./internal/apphost -run '^TestWebAPIWireContracts$' -count=1", err)
	}
	var actualJSON, wantJSON map[string]any
	if err := json.Unmarshal(actual, &actualJSON); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatalf("invalid Web API fixture: %v", err)
	}
	if !reflect.DeepEqual(actualJSON, wantJSON) {
		t.Fatalf("Go Web API JSON changed; review the TypeScript contracts and regenerate explicitly with UPDATE_WEB_API_FIXTURES=1 go test ./internal/apphost -run '^TestWebAPIWireContracts$' -count=1\nactual:\n%s", actual)
	}
}

func webAPIContractValues(t *testing.T) map[string]any {
	t.Helper()
	host := testHost(t)
	ctx, call := context.Background(), webCall()
	fixedTime := time.Date(2026, time.September, 8, 8, 0, 0, 0, time.UTC)
	closed := "2026-09-08"
	closedReason := "Synthetic fixture accepted"
	seed(t,
		store.Todo{ID: "t901", Title: "Fixture: empty task", Status: "open", Priority: "P2", Project: "fixture-project", Created: "2026-09-06", UpdatedAt: fixedTime.Unix()},
		store.Todo{ID: "t902", Title: "Fixture: planned task", Status: "in_progress", Priority: "P1", Project: "fixture-project", Created: "2026-09-07", UpdatedAt: fixedTime.Unix(), Creator: "fixture-agent", Tags: []string{"contract"}, DependsOn: []string{"t901"}},
		store.Todo{ID: "t903", Title: "Fixture: completed task", Status: "done", Priority: "P3", Project: "fixture-project", Created: "2026-09-08", UpdatedAt: fixedTime.Unix(), Closed: &closed, ClosedReason: &closedReason},
	)
	// Store writes project updated_at from their commit clock. Pin only this
	// isolated fixture database so serialized timestamps are reproducible.
	db, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, updateErr := db.Exec(`UPDATE todos SET updated_at=?`, fixedTime.Unix())
	closeErr := db.Close()
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	empty, err := host.ShowTodo(ctx, call, TodoInput{TodoID: "t901"})
	if err != nil {
		t.Fatal(err)
	}
	// ShowTodo, rather than a zero-value ShowResult literal, owns these defaults.
	// In particular nil plans/summaries remain null, while collections are [].
	if empty.LatestPlan != nil || empty.Summary != nil || empty.Todo.Closed != nil || empty.Todo.ClosedReason != nil {
		t.Fatalf("unexpected populated empty detail: %+v", empty)
	}
	if empty.Bindings == nil || empty.Sessions == nil || empty.Todo.DependsOn == nil || empty.Todo.Links == nil || empty.Todo.Images == nil {
		t.Fatalf("ShowTodo must normalize empty collections: %+v", empty)
	}
	planned, err := host.ShowTodo(ctx, call, TodoInput{TodoID: "t902"})
	if err != nil {
		t.Fatal(err)
	}
	// A real PlanSnapshot supplies the populated branch without a write use case
	// introducing wall-clock timestamps or nondeterministic session identities.
	planned.LatestPlan = &work.PlanSnapshot{
		TodoID: "t902", Revision: 1, Explanation: "Synthetic plan contract", CreatedAt: fixedTime.Unix(),
		ActorKind: application.ActorHuman, Origin: application.OriginWeb,
		Items: []work.PlanItem{{Step: "Read the fixture", Status: "completed"}, {Step: "Check the response", Status: "in_progress"}},
	}

	list := func(input ListInput) ListResult {
		t.Helper()
		result, err := host.ListTodos(ctx, call, input)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	todoList := list(ListInput{Status: "all", Limit: 2})
	metadata := list(ListInput{Workbench: true, MetadataOnly: true, GroupBy: "project", Limit: 1})
	if len(metadata.Groups) != 1 {
		t.Fatalf("expected one synthetic workbench group, got %+v", metadata.Groups)
	}
	page := list(ListInput{Workbench: true, GroupBy: "project", GroupKey: metadata.Groups[0].Key, Partition: "active", Limit: 1})
	emptyMetadata := list(ListInput{Workbench: true, MetadataOnly: true, GroupBy: "project", Query: "fixture-no-matching-task", Limit: 1})
	if emptyMetadata.Groups == nil || len(emptyMetadata.Groups) != 0 || emptyMetadata.MatchedTotal == nil || *emptyMetadata.MatchedTotal != 0 || emptyMetadata.SelectionRevision != store.EmptyTodoSelectionRevision {
		t.Fatalf("empty workbench metadata lost its explicit empty selection: %+v", emptyMetadata)
	}
	emptyQueryValue, err := host.Call(ctx, call, "knowledge.query", json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	emptyQuery, ok := emptyQueryValue.(knowledge.QueryPageResult)
	if !ok || emptyQuery.Documents == nil || len(emptyQuery.Documents) != 0 || emptyQuery.Total != 0 || emptyQuery.HasMore || emptyQuery.NextCursor != "" {
		t.Fatalf("empty knowledge page must be an explicit terminal array: %#v", emptyQueryValue)
	}

	document := knowledge.Document{
		Metadata: knowledge.DocumentMetadata{
			ID: "doc:fixture-001", SchemaVersion: knowledge.KnowledgeSchemaVersion, Title: "Fixture document", Status: "active",
			Domains: []string{"testing"}, Tags: []string{"contract"}, Projects: []string{"fixture-project"},
			Producer: "fixture", CreatedAt: fixedTime, UpdatedAt: fixedTime,
		},
		Collection: "fixture-notes", Content: "# Fixture document\n\nSynthetic API contract content.\n",
	}
	catalog := []knowledge.CollectionInfo{{
		ID: "fixture-notes", Name: "Fixture notes", Description: "Synthetic collection", Role: "reference",
		Topics: []string{"API contracts"}, UseWhen: []string{"Checking response types"},
		AvoidWhen: []string{"Reading real user data"}, Instructions: []string{"Use synthetic examples"}, DocumentCount: 2,
	}}
	query := knowledge.QueryPageResult{
		Documents: []knowledge.DocumentView{{
			DocumentID: document.Metadata.ID, Title: document.Metadata.Title, Collection: document.Collection, Status: document.Metadata.Status,
			Domains: document.Metadata.Domains, Tags: document.Metadata.Tags, Projects: document.Metadata.Projects, Producer: document.Metadata.Producer,
			CreatedAt: &fixedTime, UpdatedAt: &fixedTime, Snippet: "Synthetic API contract content.",
		}},
		Total: 2, HasMore: true, NextCursor: "synthetic-opaque-next-page-cursor",
	}
	return map[string]any{
		"todoDetailEmpty": empty, "todoDetailWithPlan": planned, "todoList": todoList,
		"taskMetadata": metadata, "taskMetadataEmpty": emptyMetadata, "taskPage": page,
		"knowledgeDocument": knowledge.VersionDocument(document), "knowledgeCatalog": catalog,
		"knowledgeQuery": query, "knowledgeQueryEmpty": emptyQuery,
	}
}
