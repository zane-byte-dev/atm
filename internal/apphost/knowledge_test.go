package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		{"knowledge.query", `{"collection":"../outside"}`},
		{"knowledge.query", `{"status":"deleted"}`},
		{"knowledge.document.get", `{"document_id":"../../outside.md"}`},
		{"knowledge.document.create", `{"title":"x","content":"x","collection":"notes","source":{"uri":"file:///tmp/outside"}}`},
		{"knowledge.document.create", `{"title":"_collection","content":"x","collection":"notes"}`},
		{"knowledge.document.create", `{"title":"/collection","content":"x","collection":"notes"}`},
		{"knowledge.document.create", `{"title":".hidden","content":"x","collection":"notes"}`},
		{"knowledge.collection.create", `{"id":"../outside"}`},
		{"knowledge.collection.create", `{"id":".hidden"}`},
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
	_, err := h.callKnowledge(context.Background(), agent, "memory.create", json.RawMessage(`{"scope":"global","content":"bad"}`))
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeForbidden {
		t.Fatalf("agent write accepted: %v", err)
	}
	if _, err := os.Stat(config.AtmDB); !os.IsNotExist(err) {
		t.Fatalf("rejected request created database: %v", err)
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
	for _, method := range []string{"knowledge.catalog", "knowledge.query", "knowledge.collection.create"} {
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
	original := created.(knowledge.Document)
	input.Content = "the copied content"
	copied, err := knowledgeRequest(t, h, "knowledge.document.create", input)
	if err != nil {
		t.Fatal(err)
	}
	if copied.(knowledge.Document).Metadata.ID == original.Metadata.ID {
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
