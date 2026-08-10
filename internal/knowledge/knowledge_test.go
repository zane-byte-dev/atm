package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCentralKnowledgeAddSearchAndGet(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{
		Title: "视频专家调研", Content: "# 架构\n\nCoding Agent 通过 MCP 调用视频工作流，并保留引用。",
		Collection: "ink", Domains: []string{"architecture"}, Tags: []string{"video"}, Projects: []string{"mox"}, Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if document.Path != filepath.Join(dataDir, "knowledge", "ink", "视频专家调研.md") {
		t.Fatalf("document path = %s", document.Path)
	}
	hits, err := Search(dataDir, "Coding Agent", SearchOptions{Limit: 5, Domains: []string{"architecture"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].DocumentID != document.Metadata.ID || hits[0].Title != "视频专家调研" || hits[0].LineStart == 0 {
		t.Fatalf("hits = %#v", hits)
	}
	filtered, err := Search(dataDir, "Coding Agent", SearchOptions{Projects: []string{"other"}})
	if err != nil || len(filtered) != 0 {
		t.Fatalf("filtered hits = %#v, err = %v", filtered, err)
	}
	filtered, err = Search(dataDir, "Coding Agent", SearchOptions{Collections: []string{"xifeng"}})
	if err != nil || len(filtered) != 0 {
		t.Fatalf("collection filtered hits = %#v, err = %v", filtered, err)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Content != document.Content || loaded.Metadata.Domains[0] != "architecture" {
		t.Fatalf("loaded document = %#v", loaded)
	}
}

func TestSearchAllowsPartialMatchesRankedByCoverage(t *testing.T) {
	dataDir := newDataDir(t)
	if _, err := Add(dataDir, AddDocumentInput{
		Title: "Skill usage", Content: "Skill 调用记录与趋势。", Collection: "atm",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dataDir, AddDocumentInput{
		Title: "Unrelated statistics", Content: "这里只讨论统计方法。", Collection: "atm",
	}); err != nil {
		t.Fatal(err)
	}

	// Terms are alternatives: one document covers "Skill" and the other covers
	// "统计", so both surface even though neither covers the whole query.
	hits, err := Search(dataDir, "Skill 统计", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("partial-token results = %#v", hits)
	}

	// A fully-covered match still ranks first over partial matches.
	hits, err = Search(dataDir, "Skill 调用", SearchOptions{Limit: 10})
	if err != nil || len(hits) == 0 || hits[0].Title != "Skill usage" {
		t.Fatalf("full-coverage results = %#v, err = %v", hits, err)
	}
}

func TestSearchDoesNotMatchOneCharacterOfCompactChineseTerm(t *testing.T) {
	dataDir := newDataDir(t)
	if _, err := Add(dataDir, AddDocumentInput{
		Title: "搜索设计", Content: "改进搜索结果排序。", Collection: "atm",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dataDir, AddDocumentInput{
		Title: "探索方法", Content: "培养探索力。", Collection: "notes",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := Search(dataDir, "搜索", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0].Title != "搜索设计" {
		t.Fatalf("search results = %#v", hits)
	}
	for _, hit := range hits {
		if hit.Title == "探索方法" {
			t.Fatalf("partial Chinese-character match leaked into results: %#v", hits)
		}
	}
}

func TestMemoryRecallDoesNotMatchOneCharacterOfCompactChineseTerm(t *testing.T) {
	newDataDir(t)
	if _, err := RememberWithMetadata("global", "培养探索力", nil, nil); err != nil {
		t.Fatal(err)
	}
	wanted, err := RememberWithMetadata("global", "优化搜索结果", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := Recall("搜索", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != wanted.ID {
		t.Fatalf("memory results = %#v", hits)
	}
}

func TestMemoryRecallRequiresEveryQueryTerm(t *testing.T) {
	newDataDir(t)
	if _, err := RememberWithMetadata("global", "hub 指代 mm-dio-hub-service", nil, nil); err != nil {
		t.Fatal(err)
	}
	hits, err := Recall("完全不存在 xyz987", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("irrelevant memories = %#v", hits)
	}
}

func TestKnowledgeListReturnsLightweightCollectionSummaries(t *testing.T) {
	dataDir := newDataDir(t)
	ink, err := Add(dataDir, AddDocumentInput{
		Title: "Personal", Content: "private context", Collection: "ink", Tags: []string{"career"}, Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dataDir, AddDocumentInput{Title: "Runbook", Content: "deploy steps", Collection: "atm"}); err != nil {
		t.Fatal(err)
	}

	all, err := List(dataDir, nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("all summaries = %#v, err = %v", all, err)
	}
	filtered, err := List(dataDir, []string{"ink"})
	if err != nil || len(filtered) != 1 {
		t.Fatalf("filtered summaries = %#v, err = %v", filtered, err)
	}
	if filtered[0].DocumentID != ink.Metadata.ID || filtered[0].Title != "Personal" || filtered[0].Collection != "ink" {
		t.Fatalf("filtered summary = %#v", filtered[0])
	}
}

func TestKnowledgeUpdateRewritesNativeDocument(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{
		Title: "Editable", Content: "Original body", Collection: "atm", Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Update(dataDir, document.Metadata.ID, "## Updated\n\nNew body.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.ID != document.Metadata.ID || !updated.Metadata.CreatedAt.Equal(document.Metadata.CreatedAt) {
		t.Fatalf("identity changed: before=%#v after=%#v", document.Metadata, updated.Metadata)
	}
	if updated.Metadata.UpdatedAt.Before(document.Metadata.UpdatedAt) || updated.Content != "## Updated\n\nNew body." {
		t.Fatalf("updated document = %#v", updated)
	}
	loaded, err := Get(dataDir, document.Metadata.ID)
	if err != nil || loaded.Content != updated.Content {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func TestKnowledgeDeleteRemovesManagedDocument(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{
		Title: "Disposable", Content: "Temporary knowledge", Collection: "atm",
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := Delete(dataDir, document.Metadata.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Metadata.ID != document.Metadata.ID {
		t.Fatalf("deleted document = %#v", deleted)
	}
	if _, err := os.Stat(document.Path); !os.IsNotExist(err) {
		t.Fatalf("managed document still exists: %v", err)
	}
	if _, err := Get(dataDir, document.Metadata.ID); err == nil {
		t.Fatal("deleted document should not be readable")
	}
}

func TestKnowledgeDeletePreservesImportedSource(t *testing.T) {
	dataDir := newDataDir(t)
	sourcePath := filepath.Join(t.TempDir(), "source.md")
	source := "# Imported\n\nKeep this source.\n"
	if err := os.WriteFile(sourcePath, []byte(source), 0640); err != nil {
		t.Fatal(err)
	}
	documents, err := Import(dataDir, sourcePath, AddDocumentInput{Collection: "atm"})
	if err != nil || len(documents) != 1 {
		t.Fatalf("imported = %#v, err = %v", documents, err)
	}
	if documents[0].Metadata.Source == nil {
		t.Fatalf("import metadata = %#v", documents[0].Metadata)
	}
	if _, err := Delete(dataDir, documents[0].Metadata.ID); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil || string(data) != source {
		t.Fatalf("source changed after delete: %q, err = %v", data, err)
	}
}

func TestKnowledgeUpdateWritesImportedSourceAndReimports(t *testing.T) {
	dataDir := newDataDir(t)
	sourcePath := filepath.Join(t.TempDir(), "source.md")
	original := "---\ntitle: Imported\ntags: [atm]\n---\n\n# Imported\n\nOriginal body.\n"
	if err := os.WriteFile(sourcePath, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	documents, err := Import(dataDir, sourcePath, AddDocumentInput{Collection: "atm", Producer: "test-import"})
	if err != nil || len(documents) != 1 {
		t.Fatalf("imported = %#v, err = %v", documents, err)
	}
	beforeHash := documents[0].Metadata.Source.Hash
	updated, err := Update(dataDir, documents[0].Metadata.ID, "## Edited\n\nUpdated from ATM.")
	if err != nil {
		t.Fatal(err)
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceData)
	for _, expected := range []string{"title: Imported", "# Imported", "## Edited", "Updated from ATM."} {
		if !strings.Contains(source, expected) {
			t.Fatalf("updated source missing %q:\n%s", expected, source)
		}
	}
	if strings.Contains(source, "Original body.") {
		t.Fatalf("updated source kept old body:\n%s", source)
	}
	if updated.Metadata.ID != documents[0].Metadata.ID || updated.Metadata.Source.Hash == beforeHash || updated.Metadata.Producer != "test-import" {
		t.Fatalf("updated import metadata = %#v", updated.Metadata)
	}
	if !strings.Contains(updated.Content, "## Edited") {
		t.Fatalf("updated imported content = %q", updated.Content)
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("source mode = %v", info.Mode().Perm())
	}
}

func TestKnowledgeEditMovesAndArchivesNativeDocument(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{
		Title: "Original", Content: "Knowledge body", Collection: "atm",
		Domains: []string{"engineering"}, Tags: []string{"old"}, Projects: []string{"atm"}, Producer: "human",
	})
	if err != nil {
		t.Fatal(err)
	}
	originalPath := document.Path
	title, collection, status := "Renamed", "personal", "archived"
	tags := []string{"design", "design", " knowledge "}
	updated, err := Edit(dataDir, document.Metadata.ID, EditDocumentInput{
		Title: &title, Collection: &collection, Status: &status, Tags: &tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.ID != document.Metadata.ID || !updated.Metadata.CreatedAt.Equal(document.Metadata.CreatedAt) {
		t.Fatalf("identity changed: before=%#v after=%#v", document.Metadata, updated.Metadata)
	}
	if updated.Metadata.Title != title || updated.Collection != collection || updated.Metadata.Status != status {
		t.Fatalf("updated document = %#v", updated)
	}
	if got, want := updated.Metadata.Tags, []string{"design", "knowledge"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("tags = %#v, want %#v", got, want)
	}
	if filepath.Dir(updated.Path) != filepath.Join(dataDir, "knowledge", collection) || updated.Path == originalPath {
		t.Fatalf("updated path = %s", updated.Path)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Fatalf("old path still exists: %v", err)
	}
	active, err := Search(dataDir, "Knowledge", SearchOptions{Statuses: []string{"active"}})
	if err != nil || len(active) != 0 {
		t.Fatalf("active hits = %#v, err = %v", active, err)
	}
	archived, err := Search(dataDir, "Knowledge", SearchOptions{Statuses: []string{"archived"}})
	if err != nil || len(archived) != 1 || archived[0].Status != "archived" {
		t.Fatalf("archived hits = %#v, err = %v", archived, err)
	}
	catalog, err := Catalog(dataDir)
	if err != nil || len(catalog) != 1 || catalog[0].DocumentCount != 0 {
		t.Fatalf("archived document should not count as active: %#v, err = %v", catalog, err)
	}
}

func TestKnowledgeEditRenamesImportedSourceAndPreservesMetadataOnUpdate(t *testing.T) {
	dataDir := newDataDir(t)
	sourcePath := filepath.Join(t.TempDir(), "source.md")
	original := "---\ntitle: Imported\ntags: [atm]\n---\n\n# Imported\n\nOriginal body.\n"
	if err := os.WriteFile(sourcePath, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	documents, err := Import(dataDir, sourcePath, AddDocumentInput{Collection: "atm", Producer: "test-import"})
	if err != nil || len(documents) != 1 {
		t.Fatalf("imported = %#v, err = %v", documents, err)
	}
	title, collection, status := "Renamed: 中文", "research", "archived"
	tags := []string{"reviewed"}
	updated, err := Edit(dataDir, documents[0].Metadata.ID, EditDocumentInput{
		Title: &title, Collection: &collection, Status: &status, Tags: &tags,
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceData)
	for _, expected := range []string{"title: 'Renamed: 中文'", "# Renamed: 中文", "Original body."} {
		if !strings.Contains(source, expected) {
			t.Fatalf("renamed source missing %q:\n%s", expected, source)
		}
	}
	if updated.Collection != collection || updated.Metadata.Status != status {
		t.Fatalf("updated import = %#v", updated)
	}
	updated, err = Update(dataDir, updated.Metadata.ID, "## Edited\n\nNew body.")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Metadata.Status != status || fmt.Sprint(updated.Metadata.Tags) != "[reviewed]" {
		t.Fatalf("metadata reset after update = %#v", updated.Metadata)
	}
}

func TestKnowledgeReadIgnoresAndRewritesLegacyKind(t *testing.T) {
	dataDir := newDataDir(t)
	path := filepath.Join(dataDir, "knowledge", "atm", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := `---
id: document:legacy
schemaVersion: 1
title: Legacy
kind: imported
status: active
producer: atm-import
createdAt: 2026-07-20T00:00:00Z
updatedAt: 2026-07-20T00:00:00Z
---

Legacy body.
`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	document, err := Get(dataDir, "document:legacy")
	if err != nil {
		t.Fatal(err)
	}
	if document.Content != "Legacy body." {
		t.Fatalf("legacy content = %q", document.Content)
	}
	if _, err := Update(dataDir, document.Metadata.ID, "Updated legacy body."); err != nil {
		t.Fatal(err)
	}
	rewritten, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rewritten), "\nkind:") {
		t.Fatalf("legacy kind was preserved:\n%s", rewritten)
	}
}

func TestKnowledgeEditRejectsInvalidMetadata(t *testing.T) {
	dataDir := newDataDir(t)
	document, err := Add(dataDir, AddDocumentInput{Title: "Valid", Content: "Body", Collection: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	status := "deleted"
	if _, err := Edit(dataDir, document.Metadata.ID, EditDocumentInput{Status: &status}); err == nil {
		t.Fatal("invalid status should fail")
	}
	collection := "../outside"
	if _, err := Edit(dataDir, document.Metadata.ID, EditDocumentInput{Collection: &collection}); err == nil {
		t.Fatal("invalid collection should fail")
	}
}

func TestKnowledgeFeedbackBuildsQualityAndReranksSearch(t *testing.T) {
	dataDir := newDataDir(t)
	trusted, err := Add(dataDir, AddDocumentInput{Title: "Trusted", Content: "shared ranking marker", Collection: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	weak, err := Add(dataDir, AddDocumentInput{Title: "Weak", Content: "shared ranking marker", Collection: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := Search(dataDir, "shared ranking marker", SearchOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordRetrievals("session-test", "shared ranking marker", append(initial, initial...)); err != nil {
		t.Fatal(err)
	}
	if err := RecordRetrievals("session-test", "shared ranking marker", initial); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: trusted.Metadata.ID, SessionID: fmt.Sprintf("trusted-%d", index), Outcome: "adopted"}); err != nil {
			t.Fatal(err)
		}
		if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: weak.Metadata.ID, SessionID: fmt.Sprintf("weak-%d", index), Outcome: "rejected"}); err != nil {
			t.Fatal(err)
		}
	}
	qualities, err := KnowledgeQualities(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]KnowledgeQuality)
	for _, quality := range qualities {
		byID[quality.DocumentID] = quality
	}
	if byID[trusted.Metadata.ID].Score <= byID[weak.Metadata.ID].Score || byID[trusted.Metadata.ID].Retrievals != 1 || byID[weak.Metadata.ID].Retrievals != 1 {
		t.Fatalf("qualities = %#v", qualities)
	}
	ranked, err := Search(dataDir, "shared ranking marker", SearchOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(ranked) < 2 || ranked[0].DocumentID != trusted.Metadata.ID || ranked[0].Quality <= ranked[1].Quality {
		t.Fatalf("ranked hits = %#v", ranked)
	}
	if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: trusted.Metadata.ID, SessionID: "session", Outcome: "retrieved"}); err == nil {
		t.Fatal("retrieved is reserved for automatic search tracking")
	}
	if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: trusted.Metadata.ID, SessionID: "changed-result", Outcome: "adopted"}); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: trusted.Metadata.ID, SessionID: "changed-result", Outcome: "corrected"}); err != nil {
		t.Fatal(err)
	}
	qualities, err = KnowledgeQualities(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, quality := range qualities {
		if quality.DocumentID == trusted.Metadata.ID && (quality.Adopted != 3 || quality.Corrected != 1) {
			t.Fatalf("latest session result did not supersede prior result: %#v", quality)
		}
	}
}

func TestKnowledgeAuditFindsDuplicatesStaleSourcesAndLowQuality(t *testing.T) {
	dataDir := newDataDir(t)
	first, err := Add(dataDir, AddDocumentInput{Title: "Duplicate：Title", Content: "Same duplicate content", Collection: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Add(dataDir, AddDocumentInput{Title: "Duplicate Title", Content: "Same duplicate content", Collection: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	first.Metadata.UpdatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := writeDocumentAt(first.Path, first); err != nil {
		t.Fatal(err)
	}

	sourcePath := filepath.Join(t.TempDir(), "source.md")
	if err := os.WriteFile(sourcePath, []byte("# Source\n\nOriginal."), 0600); err != nil {
		t.Fatal(err)
	}
	imported, err := Import(dataDir, sourcePath, AddDocumentInput{Collection: "atm"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("# Source\n\nChanged outside ATM."), 0600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if _, err := RecordFeedback(dataDir, FeedbackInput{DocumentID: second.Metadata.ID, SessionID: fmt.Sprintf("reject-%d", index), Outcome: "rejected"}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Audit(dataDir, AuditOptions{StaleDays: 180, Now: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"duplicate_title", "duplicate_content", "stale", "source_drift", "low_quality"} {
		if report.Counts[code] == 0 {
			t.Fatalf("audit missing %s: %#v", code, report)
		}
	}
	if report.Documents != 3 || report.Active != 3 || imported[0].Metadata.Source == nil {
		t.Fatalf("audit report = %#v", report)
	}
}

func TestKnowledgeAuditSerializesCleanIssuesAsArray(t *testing.T) {
	report, err := Audit(t.TempDir(), AuditOptions{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"issues":[]`) {
		t.Fatalf("clean audit should serialize issues as an empty array: %s", encoded)
	}
}

func TestKnowledgeAddUsesReadableUniqueFilenames(t *testing.T) {
	dataDir := newDataDir(t)
	first, err := Add(dataDir, AddDocumentInput{Title: "同名笔记", Content: "First", Collection: "ink"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Add(dataDir, AddDocumentInput{Title: "同名笔记", Content: "Second", Collection: "ink"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := Add(dataDir, AddDocumentInput{Title: "同名笔记", Content: "Third", Collection: "ink"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || second.Path == third.Path || first.Path == third.Path {
		t.Fatalf("readable paths collided: %s, %s, %s", first.Path, second.Path, third.Path)
	}
	if filepath.Base(first.Path) != "同名笔记.md" || !strings.HasPrefix(filepath.Base(second.Path), "同名笔记--") {
		t.Fatalf("unexpected readable paths: %s, %s", first.Path, second.Path)
	}
	documents, err := Discover(dataDir)
	if err != nil || len(documents) != 3 {
		t.Fatalf("documents = %#v, err = %v", documents, err)
	}
}

func TestKnowledgeImportIsExplicitAndIdempotent(t *testing.T) {
	dataDir := newDataDir(t)
	sourceDir := filepath.Join(t.TempDir(), "notebooks")
	path := filepath.Join(sourceDir, "research", "research.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Imported knowledge\n\nFirst version."), 0644); err != nil {
		t.Fatal(err)
	}
	first, err := Import(dataDir, sourceDir, AddDocumentInput{Collection: "research", Domains: []string{"research"}})
	if err != nil || len(first) != 1 {
		t.Fatalf("first import = %#v, err = %v", first, err)
	}
	if err := os.WriteFile(path, []byte("# Imported knowledge\n\nUpdated version."), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := Import(dataDir, sourceDir, AddDocumentInput{Collection: "research", Domains: []string{"research"}})
	if err != nil || len(second) != 1 {
		t.Fatalf("second import = %#v, err = %v", second, err)
	}
	if first[0].Metadata.ID != second[0].Metadata.ID {
		t.Fatalf("import ids differ: %s != %s", first[0].Metadata.ID, second[0].Metadata.ID)
	}
	wantPath := filepath.Join(dataDir, "knowledge", "research", "research", "research.md")
	if second[0].Path != wantPath {
		t.Fatalf("import path = %s, want %s", second[0].Path, wantPath)
	}
	documents, err := Discover(dataDir)
	if err != nil || len(documents) != 1 || !strings.Contains(documents[0].Content, "Updated") {
		t.Fatalf("documents = %#v, err = %v", documents, err)
	}
}

func TestKnowledgeImportMigratesLegacyIDFilename(t *testing.T) {
	dataDir := newDataDir(t)
	sourceDir := filepath.Join(t.TempDir(), "ink")
	path := filepath.Join(sourceDir, "readable.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Readable\n\nKnowledge."), 0644); err != nil {
		t.Fatal(err)
	}
	first, err := Import(dataDir, sourceDir, AddDocumentInput{Collection: "ink"})
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dataDir, "knowledge", "documents", "notebooks", "ink", "readable.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(first[0].Path, legacyPath); err != nil {
		t.Fatal(err)
	}
	second, err := Import(dataDir, sourceDir, AddDocumentInput{Collection: "ink"})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(dataDir, "knowledge", "ink", "readable.md")
	if second[0].Path != wantPath {
		t.Fatalf("migrated path = %s, want %s", second[0].Path, wantPath)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy path still exists: %v", err)
	}
	loaded, err := Get(dataDir, first[0].Metadata.ID)
	if err != nil || loaded.Path != wantPath {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}

func TestKnowledgeDirectoryImportUsesTopLevelCollections(t *testing.T) {
	dataDir := newDataDir(t)
	sourceDir := filepath.Join(t.TempDir(), "notebooks")
	files := map[string]string{
		filepath.Join(sourceDir, "ink", "personal.md"):    "# Personal\n\nLife.",
		filepath.Join(sourceDir, "xifeng", "strategy.md"): "# Strategy\n\nEconomy.",
		filepath.Join(sourceDir, "unclassified.md"):       "# Unclassified\n\nLater.",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	documents, err := Import(dataDir, sourceDir, AddDocumentInput{})
	if err != nil || len(documents) != 3 {
		t.Fatalf("documents = %#v, err = %v", documents, err)
	}
	for _, path := range []string{
		filepath.Join(dataDir, "knowledge", "ink", "personal.md"),
		filepath.Join(dataDir, "knowledge", "xifeng", "strategy.md"),
		filepath.Join(dataDir, "knowledge", "inbox", "unclassified.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected imported path %s: %v", path, err)
		}
	}
}

func TestKnowledgeCatalogUsesCollectionManifests(t *testing.T) {
	dataDir := newDataDir(t)
	if _, err := Add(dataDir, AddDocumentInput{Title: "Personal", Content: "career notes", Collection: "ink", Tags: []string{"career"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(dataDir, AddDocumentInput{Title: "Strategy", Content: "economic strategy", Collection: "xifeng", Domains: []string{"economy"}}); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nid: ink\nname: 个人知识\nrole: primary-context\ntopics: [个人经历, 职业发展]\nuseWhen: [问题涉及用户本人]\navoidWhen: [只需公共事实]\ninstructions: [先读取个人事实, 明确区分事实与推断]\n---\n\n用户个人经历、目标和反思。\n"
	if err := os.WriteFile(filepath.Join(dataDir, "knowledge", "ink", "_collection.md"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Catalog(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 || catalog[0].ID != "ink" || catalog[0].Name != "个人知识" || catalog[0].Role != "primary-context" || catalog[0].DocumentCount != 1 || !strings.Contains(catalog[0].Description, "个人经历") {
		t.Fatalf("catalog = %#v", catalog)
	}
	if len(catalog[0].UseWhen) != 1 || len(catalog[0].AvoidWhen) != 1 || len(catalog[0].Instructions) != 2 {
		t.Fatalf("catalog routing protocol = %#v", catalog[0])
	}
	if catalog[1].ID != "xifeng" || catalog[1].DocumentCount != 1 || len(catalog[1].Topics) != 1 || catalog[1].Topics[0] != "economy" {
		t.Fatalf("derived catalog = %#v", catalog[1])
	}
}

func TestMemoryLifecycleAndRelevantRecall(t *testing.T) {
	newDataDir(t)
	first, err := RememberWithMetadata("project:mox", "ATM 使用 Markdown 作为知识事实源", []string{"architecture"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RememberWithMetadata("project:mox", "午饭吃面条", nil, nil); err != nil {
		t.Fatal(err)
	}
	second, err := SupersedeWithMetadata(first.ID, "project:mox", "ATM 使用中央 Knowledge 作为事实源", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := Recall("Knowledge", "project:mox", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != second.ID || strings.Contains(hits[0].Content, "面条") {
		t.Fatalf("hits after supersede = %#v", hits)
	}
	if _, err := ForgetWithMetadata(second.ID, "global", nil); err == nil {
		t.Fatal("scope mismatch should fail")
	}
	if _, err := ForgetWithMetadata(second.ID, "project:mox", nil); err != nil {
		t.Fatal(err)
	}
	hits, err = Recall("Knowledge", "project:mox", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("hits after forget = %#v, err = %v", hits, err)
	}
}

func TestMemoryProvenanceIsPreserved(t *testing.T) {
	newDataDir(t)
	event, err := RememberWithMetadata("project:atm", "ATM memory capture is agent-driven", []string{"decision"}, map[string]string{
		"source": "session:abc#turn:2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata["source"] != "session:abc#turn:2" {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}
	hits, err := Recall("agent-driven", "project:atm", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Metadata["source"] != "session:abc#turn:2" {
		t.Fatalf("hits = %#v", hits)
	}
}

func TestSessionReviewLifecycleAndIdempotency(t *testing.T) {
	newDataDir(t)
	first, err := MarkSessionReviewed("session-1", "memory", "stored one decision")
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarkSessionReviewed("session-1", "memory", "stored one decision")
	if err != nil {
		t.Fatal(err)
	}
	if first.ReviewedAt != second.ReviewedAt {
		t.Fatalf("idempotent mark appended a new review: %s != %s", first.ReviewedAt, second.ReviewedAt)
	}
	if _, err := MarkSessionReviewed("session-2", "temporary", ""); err == nil {
		t.Fatal("invalid outcome should fail")
	}
	reviews, err := SessionReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews["session-1"].Outcome != "memory" {
		t.Fatalf("reviews = %#v", reviews)
	}
}

func TestConcurrentSessionReviewWrites(t *testing.T) {
	newDataDir(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := MarkSessionReviewed(fmt.Sprintf("session-%d", index), "none", "no durable candidate")
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	reviews, err := SessionReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 12 {
		t.Fatalf("review count = %d, want 12", len(reviews))
	}
}

func TestSaveArtifactUsesCentralUniquePath(t *testing.T) {
	dataDir := newDataDir(t)
	first, err := SaveArtifact(dataDir, "调研报告", "# 结论\n\n第一版。", "pi", "run-1", []SourceRef{{DocumentID: "document:test", LineStart: 2, LineEnd: 4}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveArtifact(dataDir, "调研报告", "# 结论\n\n第二版。", "pi", "run-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("artifact paths collided: %s", first.Path)
	}
	for _, artifact := range []*Artifact{first, second} {
		if !strings.HasPrefix(artifact.Path, filepath.Join(dataDir, "artifacts")) {
			t.Fatalf("artifact escaped central store: %s", artifact.Path)
		}
		if _, err := os.Stat(artifact.Path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidateScope(t *testing.T) {
	for _, scope := range []string{"global", "project:mox", "session:123"} {
		if err := ValidateScope(scope); err != nil {
			t.Fatalf("scope %q: %v", scope, err)
		}
	}
	for _, scope := range []string{"", "project:", "notebook:ink", "/tmp", "other:value"} {
		if err := ValidateScope(scope); err == nil {
			t.Fatalf("scope %q unexpectedly valid", scope)
		}
	}
}
