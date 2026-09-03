package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestSaveLinkAtomicEditAndFailureSafety(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{ID: "t1", Title: "Related content", Priority: "P1", Status: "in_progress", Created: store.Today(), DependsOn: []string{"t2"}},
		store.Todo{ID: "t2", Title: "Dependency", Priority: "P2", Status: "done", Created: store.Today()})
	ctx, call := context.Background(), linkTestCall()
	save := func(input SaveLinkInput) Todo {
		t.Helper()
		result, err := Default.SaveLink(ctx, call, input)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := save(SaveLinkInput{TodoID: "#T01", URL: "HTTPS://Example.COM/cr/1?b=2&a=1", Title: "Title", Relation: "evidence"})
	old := first.Links[0].URL
	if old != "https://example.com/cr/1?a=1&b=2" || first.Links[0].Kind != "cr" {
		t.Fatalf("first = %+v", first)
	}
	save(SaveLinkInput{TodoID: "t1", URL: "https://example.com/release/2"})
	updated := save(SaveLinkInput{TodoID: "t1", OriginalURL: old, URL: "https://example.com/docs/3", Title: "", Relation: ""})
	if len(updated.Links) != 2 || updated.Links[0].Title != "" || updated.Links[0].Relation != "" || updated.Links[0].Kind != "document" {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.Status != "in_progress" || !reflect.DeepEqual(updated.DependsOn, []string{"t2"}) {
		t.Fatalf("changed lifecycle/dependencies: %+v", updated)
	}
	for _, test := range []struct {
		name  string
		input SaveLinkInput
		want  error
	}{
		{"duplicate add", SaveLinkInput{TodoID: "t1", URL: updated.Links[0].URL}, application.ErrConflict},
		{"edit collision", SaveLinkInput{TodoID: "t1", OriginalURL: updated.Links[0].URL, URL: updated.Links[1].URL}, application.ErrConflict},
		{"stale edit", SaveLinkInput{TodoID: "t1", OriginalURL: old, URL: "https://example.com/new"}, application.ErrNotFound},
		{"unsafe edit", SaveLinkInput{TodoID: "t1", OriginalURL: updated.Links[0].URL, URL: "https://example.com/?token=secret"}, application.ErrInvalidArgument},
		{"unknown task", SaveLinkInput{TodoID: "t9", URL: "https://example.com"}, application.ErrNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Default.SaveLink(ctx, call, test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			listed, err := Default.ListLinks(ctx, call, ListLinksInput{TodoID: "t1"})
			if err != nil || !reflect.DeepEqual(listed.Links, updated.Links) {
				t.Fatalf("failed save changed links: %+v, %v", listed, err)
			}
		})
	}
	removed, err := Default.RemoveLink(ctx, call, RemoveLinkInput{TodoID: "t1", URL: updated.Links[0].URL})
	if err != nil || len(removed.Todo.Links) != 1 || removed.Todo.Status != updated.Status {
		t.Fatalf("remove = %+v, %v", removed, err)
	}
}

func TestArchivedTodoRetainsReadOnlyLinks(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{ID: "t1", Title: "Archive links", Priority: "P2", Status: "open", Created: store.Today()})
	ctx, call := context.Background(), linkTestCall()
	input := SaveLinkInput{TodoID: "t1", URL: "https://example.com/docs/1", Title: "Design"}
	saved, err := Default.SaveLink(ctx, call, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Default.Archive(ctx, call, RetentionInput{TodoIDs: []string{"t1"}}); err != nil {
		t.Fatal(err)
	}
	archived, err := store.LoadArchivedTodos()
	if err != nil || len(archived) != 1 || !reflect.DeepEqual(archived[0].Links, saved.Links) {
		t.Fatalf("archive = %+v, %v", archived, err)
	}
	if _, err := Default.SaveLink(ctx, call, input); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("archived save = %v", err)
	}
	if _, err := Default.RemoveLink(ctx, call, RemoveLinkInput{TodoID: "t1", URL: input.URL}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("archived remove = %v", err)
	}
}

func TestRelatedContentLinkInference(t *testing.T) {
	for url, kind := range map[string]string{
		"https://code.alibaba-inc.com/a/b/codereview/1": "mr",
		"https://github.com/a/b/pull/1":                 "mr",
		"https://example.com/cr/1":                      "cr",
		"https://example.com/pipelines/1":               "pipeline",
		"https://example.com/releases/1":                "release",
		"https://example.com/issues/1":                  "workitem",
		"https://www.yuque.com/a/b":                     "document",
		"https://alidocs.dingtalk.com/i/nodes/1":        "document",
		"https://example.com/REPORT.PDF":                "document",
		"https://yuque.com.evil.test/a":                 "",
		"https://example.com":                           "",
	} {
		if got := InferTodoLinkKind(url); got != kind {
			t.Errorf("infer(%s) = %q, want %q", url, got, kind)
		}
	}
}
