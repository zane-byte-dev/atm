package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
)

func knowledgeRequest(t *testing.T, h *Host, method string, input any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return h.callKnowledge(context.Background(), webCall(), method, raw)
}

func TestKnowledgeReadsDoNotMaterializeMissingData(t *testing.T) {
	h := testHost(t)
	for _, method := range []string{"knowledge.catalog", "knowledge.query", "memory.recall"} {
		if _, err := knowledgeRequest(t, h, method, map[string]any{}); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if _, err := knowledgeRequest(t, h, "knowledge.query", map[string]any{"text": "search"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{config.AtmDB, filepath.Join(config.AtmDir, "knowledge")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read materialized %s: %v", path, err)
		}
	}
}

func TestKnowledgeBoundaryRejectsAmbientAuthorityAndUnboundedRequests(t *testing.T) {
	h := testHost(t)
	cases := []struct{ method, body string }{
		{"knowledge.catalog", `{"path":"/tmp"}`},
		{"knowledge.query", `{"text":"hello","session_id":"s1"}`},
		{"knowledge.query", `{"limit":201}`},
		{"knowledge.query", `{"limit":-1}`},
		{"knowledge.query", `{"cursor":"invalid"}`},
		{"knowledge.query", `{"collection":"../outside"}`},
		{"knowledge.query", `{"status":"deleted"}`},
		{"knowledge.document.get", `{"document_id":"../../outside.md"}`},
		{"knowledge.document.create", `{"title":"x","content":"x","collection":"notes","source":{"uri":"file:///tmp/outside"}}`},
		{"knowledge.document.create", `{"title":"_collection","content":"x","collection":"notes"}`},
		{"knowledge.document.create", `{"title":"/collection","content":"x","collection":"notes"}`},
		{"knowledge.document.create", `{"title":".hidden","content":"x","collection":"notes"}`},
		{"knowledge.collection.create", `{"id":"../outside"}`},
		{"knowledge.collection.create", `{"id":".hidden"}`},
		{"knowledge.collection.update", `{"id":"../outside","name":"Name"}`},
		{"knowledge.collection.update", `{"id":"notes","name":"  "}`},
		{"knowledge.collection.update", `{"id":"notes","role":"unsupported"}`},
		{"knowledge.collection.delete", `{"id":"notes","move_to":"../outside","confirmed":true}`},
		{"memory.recall", `{"limit":10000}`},
		{"memory.get", `{"memory_id":"/tmp/file"}`},
		{"memory.create", `{"scope":"global","content":"x","metadata":{"actor":"human"}}`},
		{"memory.supersede", `{"target_id":"memory:1","scope":"global","content":"x","source":"fake"}`},
		{"knowledge.document.import", `{"path":"/tmp"}`},
		{"knowledge.document.save", `{"content":{"document_id":"document:1","content":"x"}}`},
		{"knowledge.document.delete", `{"document_id":"document:1","confirmed":true}`},
	}
	for _, test := range cases {
		if _, err := h.callKnowledge(context.Background(), webCall(), test.method, json.RawMessage(test.body)); err == nil {
			t.Fatalf("accepted %s %s", test.method, test.body)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.callKnowledge(canceled, webCall(), "knowledge.catalog", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	agent := webCall()
	agent.Actor.Kind = application.ActorAgent
	for _, method := range []string{"memory.create", "knowledge.collection.update", "knowledge.collection.delete"} {
		_, err := h.callKnowledge(context.Background(), agent, method, json.RawMessage(`{}`))
		var appErr *application.Error
		if !errors.As(err, &appErr) || appErr.Code != application.CodeForbidden {
			t.Fatalf("agent write accepted for %s: %v", method, err)
		}
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("rejected request created database: %v", err)
	}
}

func TestKnowledgeQueryPagesDocumentsAndForwardsArchiveScope(t *testing.T) {
	h := testHost(t)
	for _, title := range []string{"One", "Two", "Three"} {
		document, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{Title: title, Content: "page marker", Collection: "notes"})
		if err != nil {
			t.Fatal(err)
		}
		if title == "Two" {
			status := "archived"
			if _, err := knowledge.Edit(config.AtmDir, document.Metadata.ID, knowledge.EditDocumentInput{Status: &status}); err != nil {
				t.Fatal(err)
			}
		}
	}
	firstRaw, err := knowledgeRequest(t, h, "knowledge.query", KnowledgeQueryInput{Collection: "notes", ExcludeArchived: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	first := firstRaw.(knowledge.QueryPageResult)
	if first.Total != 2 || !first.HasMore || first.NextCursor == "" || len(first.Documents) != 1 || first.Documents[0].Status == "archived" {
		t.Fatalf("first document page = %+v", first)
	}
	secondRaw, err := knowledgeRequest(t, h, "knowledge.query", KnowledgeQueryInput{Collection: "notes", ExcludeArchived: true, Limit: 1, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	second := secondRaw.(knowledge.QueryPageResult)
	if second.Total != 2 || second.HasMore || second.NextCursor != "" || len(second.Documents) != 1 || second.Documents[0].DocumentID == first.Documents[0].DocumentID {
		t.Fatalf("second document page = %+v", second)
	}
	allRaw, err := knowledgeRequest(t, h, "knowledge.query", KnowledgeQueryInput{Collection: "notes", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if all := allRaw.(knowledge.QueryPageResult); all.Total != 3 || len(all.Documents) != 3 {
		t.Fatalf("manager must include archived documents: %+v", all)
	}
}

func TestKnowledgeRejectsSymlinkCorpusBeforeReadOrWrite(t *testing.T) {
	h := testHost(t)
	outside := t.TempDir()
	path := filepath.Join(outside, "private.md")
	if err := os.WriteFile(path, []byte("private content"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(config.AtmDir, "knowledge")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"knowledge.catalog", "knowledge.query", "knowledge.collection.create", "knowledge.collection.update", "knowledge.collection.delete"} {
		_, err := knowledgeRequest(t, h, method, map[string]any{})
		var appErr *application.Error
		if !errors.As(err, &appErr) || appErr.Code != application.CodeForbidden {
			t.Fatalf("%s followed symbolic link: %v", method, err)
		}
	}
	content, _ := os.ReadFile(path)
	if string(content) != "private content" {
		t.Fatal("external file changed")
	}
}

func TestKnowledgeCreateRequiresRegisteredCollectionAndPreservesOriginal(t *testing.T) {
	h := testHost(t)
	input := KnowledgeCreateInput{Title: "Reusable idea", Content: "the first content", Collection: "notes"}
	if _, err := knowledgeRequest(t, h, "knowledge.document.create", input); err == nil {
		t.Fatal("unregistered collection accepted")
	}
	if _, err := knowledgeRequest(t, h, "knowledge.collection.create", KnowledgeCollectionCreateInput{ID: "notes", Name: "Notes"}); err != nil {
		t.Fatal(err)
	}
	created, err := knowledgeRequest(t, h, "knowledge.document.create", input)
	if err != nil {
		t.Fatal(err)
	}
	original := created.(knowledge.VersionedDocument)
	input.Content = "the copied content"
	copied, err := knowledgeRequest(t, h, "knowledge.document.create", input)
	if err != nil {
		t.Fatal(err)
	}
	if copied.(knowledge.VersionedDocument).Metadata.ID == original.Metadata.ID {
		t.Fatal("new creation reused document identity")
	}
	actual, err := knowledgeRequest(t, h, "knowledge.document.get", knowledge.GetInput{DocumentID: original.Metadata.ID})
	if err != nil || actual.(knowledge.VersionedDocument).Content != "the first content" {
		t.Fatalf("original changed: %+v, %v", actual, err)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatal("file-backed document creation touched the database")
	}
}

func TestKnowledgeCreateReturnsRevisionUsableForImmediateUpdate(t *testing.T) {
	h := testHost(t)
	if _, err := knowledgeRequest(t, h, "knowledge.collection.create", KnowledgeCollectionCreateInput{ID: "notes", Name: "Notes"}); err != nil {
		t.Fatal(err)
	}
	created, err := knowledgeRequest(t, h, "knowledge.document.create", KnowledgeCreateInput{
		Title: "Created through Web", Content: "Initial content", Collection: "notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	original, ok := created.(knowledge.VersionedDocument)
	if !ok || !original.Editable || len(original.ETag) != 64 {
		t.Fatalf("creation did not return an editable revision: %#v", created)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		ETag     string `json:"etag"`
		Editable *bool  `json:"editable"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ETag != original.ETag || wire.Editable == nil || !*wire.Editable {
		t.Fatalf("creation JSON omitted the editor contract: %s", encoded)
	}
	if strings.Contains(string(encoded), config.AtmDir) || strings.Contains(string(encoded), `"path"`) {
		t.Fatalf("creation JSON exposed a local document path: %s", encoded)
	}

	// No intervening get: a browser can edit immediately using the revision
	// from create, and the next get observes the same updated revision.
	title := "Edited through Web"
	updated, err := knowledgeRequest(t, h, "knowledge.document.update", knowledge.UpdateManagedInput{
		DocumentID: original.Metadata.ID, ETag: original.ETag, Title: &title, Content: "Updated content",
	})
	if err != nil {
		t.Fatal(err)
	}
	saved := updated.(knowledge.VersionedDocument)
	if saved.Metadata.ID != original.Metadata.ID || saved.Metadata.Title != title || saved.Content != "Updated content" || !saved.Editable || saved.ETag == original.ETag {
		t.Fatalf("immediate edit lost its document or revision: %+v", saved)
	}
	loaded, err := knowledgeRequest(t, h, "knowledge.document.get", knowledge.GetInput{DocumentID: original.Metadata.ID})
	if err != nil || loaded.(knowledge.VersionedDocument).ETag != saved.ETag {
		t.Fatalf("saved revision was not readable: %#v, %v", loaded, err)
	}
}

func TestMemoryReplacementSingleWinnerAndHistory(t *testing.T) {
	h := testHost(t)
	created, err := knowledgeRequest(t, h, "memory.create", MemoryCreateInput{Scope: "project:atm", Content: "original fact", Tags: []string{"rule"}})
	if err != nil {
		t.Fatal(err)
	}
	target := created.(knowledge.SupersedeMemoryResult).Event.ID
	// Both browser requests share the server's composition gate. The immutable
	// target ID is still checked by the service (including external CLI edits).
	hosts := []*Host{h, h}
	results := make(chan error, len(hosts))
	var group sync.WaitGroup
	for _, host := range hosts {
		group.Add(1)
		go func(host *Host) {
			defer group.Done()
			raw, _ := json.Marshal(MemorySupersedeInput{TargetID: target, Scope: "project:atm", Content: "updated fact", Tags: []string{"rule"}})
			_, err := host.callKnowledge(context.Background(), webCall(), "memory.supersede", raw)
			results <- err
		}(host)
	}
	group.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
			continue
		}
		var appErr *application.Error
		if !errors.As(err, &appErr) || (appErr.Code != application.CodeConflict && appErr.Code != application.CodeNotFound) {
			t.Fatalf("unexpected replacement error: %v (cause: %v)", err, errors.Unwrap(err))
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent winners=%d", winners)
	}
	result, err := knowledgeRequest(t, h, "memory.recall", knowledge.RecallMemoryInput{Scope: "project:atm"})
	if err != nil {
		t.Fatal(err)
	}
	hits := result.(knowledge.RecallMemoryResult).Hits
	if len(hits) != 1 || hits[0].ID == target || hits[0].Content != "updated fact" || hits[0].Metadata["source"] != "atm-web" {
		t.Fatalf("effective memory result=%+v", hits)
	}
	if count, err := store.CountMemoryEvents(); err != nil || count != 2 {
		t.Fatalf("history rows=%d err=%v", count, err)
	}
}

func TestKnowledgeCollectionManagementPreservesDocuments(t *testing.T) {
	h := testHost(t)
	for _, id := range []string{"source", "target", "empty"} {
		if _, err := knowledgeRequest(t, h, "knowledge.collection.create", map[string]any{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	created, err := knowledgeRequest(t, h, "knowledge.document.create", map[string]any{"title": "Keep me", "content": "Original content", "collection": "source"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(created)
	if _, err := knowledgeRequest(t, h, "knowledge.collection.update", map[string]any{"id": "source", "name": "New name", "description": "New description"}); err != nil {
		t.Fatal(err)
	}
	catalog, err := knowledge.NewService(knowledge.ServiceOptions{DataDir: config.AtmDir}).Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range catalog {
		if item.ID == "source" {
			found = item.Name == "New name" && item.Description == "New description" && item.DocumentCount == 1
		}
	}
	if !found {
		t.Fatalf("rename lost collection metadata/documents: %+v", catalog)
	}
	for _, input := range []map[string]any{
		{"id": "source", "confirmed": true},
		{"id": "source", "confirmed": true, "force": true},
		{"id": "source", "confirmed": false, "move_to": "target"},
		{"id": "source", "confirmed": true, "move_to": "source"},
		{"id": "source", "confirmed": true, "move_to": "missing"},
	} {
		if _, err := knowledgeRequest(t, h, "knowledge.collection.delete", input); err == nil {
			t.Fatalf("accepted unsafe delete: %+v", input)
		}
	}
	var doc struct {
		Metadata struct {
			ID string `json:"id"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(before, &doc); err != nil {
		t.Fatal(err)
	}
	after, err := knowledgeRequest(t, h, "knowledge.document.get", map[string]any{"document_id": doc.Metadata.ID})
	if err != nil {
		t.Fatal(err)
	}
	unchanged, _ := json.Marshal(after)
	if string(before) != string(unchanged) {
		t.Fatalf("rename/rejected delete changed document: %s", unchanged)
	}
	if _, err := knowledgeRequest(t, h, "knowledge.collection.delete", map[string]any{"id": "source", "confirmed": true, "move_to": "target"}); err != nil {
		t.Fatal(err)
	}
	moved, err := knowledgeRequest(t, h, "knowledge.document.get", map[string]any{"document_id": doc.Metadata.ID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(moved)
	if !strings.Contains(string(raw), `"collection":"target"`) || !strings.Contains(string(raw), "Original content") {
		t.Fatalf("move lost document: %s", raw)
	}
	if _, err := knowledgeRequest(t, h, "knowledge.collection.delete", map[string]any{"id": "empty", "confirmed": true}); err != nil {
		t.Fatal(err)
	}
}
