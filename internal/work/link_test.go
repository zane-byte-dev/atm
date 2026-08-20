package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func linkTestCall() application.Call {
	return application.Call{
		RequestID: "link-request",
		Actor:     application.Actor{Kind: application.ActorAgent, Origin: application.OriginCLI, Agent: "codex"},
	}
}

func TestLinkServiceNormalizesUpdatesListsAndRemoves(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Track a change", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})

	first, err := Default.AddLink(context.Background(), linkTestCall(), AddLinkInput{
		TodoID: "#T01", URL: "HTTPS://Example.COM/cr/42?b=2&a=1", Title: "Release CR", Relation: "tracks",
	})
	if err != nil {
		t.Fatalf("AddLink: %v", err)
	}
	if !first.Created || first.TodoID != "t1" || first.Link.URL != "https://example.com/cr/42?a=1&b=2" ||
		first.Link.Kind != "cr" || first.Link.Title != "Release CR" {
		t.Fatalf("first = %+v", first)
	}

	updated, err := Default.AddLink(context.Background(), linkTestCall(), AddLinkInput{
		TodoID: "t1", URL: first.Link.URL, Title: "Updated title",
	})
	if err != nil {
		t.Fatalf("update AddLink: %v", err)
	}
	if updated.Created || updated.Link.Title != "Updated title" || updated.Link.Relation != "tracks" {
		t.Fatalf("updated = %+v", updated)
	}

	listed, err := Default.ListLinks(context.Background(), linkTestCall(), ListLinksInput{TodoID: "1"})
	if err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if !reflect.DeepEqual(listed.Links, []TodoLink{updated.Link}) {
		t.Fatalf("links = %+v", listed.Links)
	}
	listed.Links[0].Title = "caller mutation"
	again, err := Default.ListLinks(context.Background(), linkTestCall(), ListLinksInput{TodoID: "t1"})
	if err != nil || again.Links[0].Title != "Updated title" {
		t.Fatalf("persisted links changed through result: %+v, err=%v", again.Links, err)
	}

	removed, err := Default.RemoveLink(context.Background(), linkTestCall(), RemoveLinkInput{
		TodoID: "t1", URL: first.Link.URL,
	})
	if err != nil || removed.Removed.URL != first.Link.URL {
		t.Fatalf("RemoveLink = %+v, err=%v", removed, err)
	}
	empty, err := Default.ListLinks(context.Background(), linkTestCall(), ListLinksInput{TodoID: "t1"})
	if err != nil || empty.Links == nil || len(empty.Links) != 0 {
		t.Fatalf("empty links = %#v, err=%v", empty.Links, err)
	}
}

func TestLinkServiceRejectsUnsafeURLBeforeMutation(t *testing.T) {
	unsafe := []string{
		"example.com/cr/1",
		"ftp://example.com/cr/1",
		"https://user:password@example.com/cr/1",
		"https://example.com/cr/1?access_token=secret",
		"https://example.com/cr/1#access_token=secret",
	}
	for _, raw := range unsafe {
		t.Run(raw, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Safe links", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
			})
			_, err := Default.AddLink(context.Background(), linkTestCall(), AddLinkInput{TodoID: "t1", URL: raw})
			if !errors.Is(err, application.ErrInvalidArgument) {
				t.Fatalf("AddLink error = %v, want invalid_argument", err)
			}
			listed, listErr := Default.ListLinks(context.Background(), linkTestCall(), ListLinksInput{TodoID: "t1"})
			if listErr != nil || len(listed.Links) != 0 {
				t.Fatalf("invalid URL mutated links: %+v, err=%v", listed.Links, listErr)
			}
		})
	}
}

func TestLinkServiceUsesTypedNotFoundErrors(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Known", Priority: "P1", Status: store.TodoStatusOpen, Created: store.Today(),
	})
	_, err := Default.AddLink(context.Background(), linkTestCall(), AddLinkInput{
		TodoID: "t2", URL: "https://example.com/cr/1",
	})
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing todo error = %v, want not_found", err)
	}
	_, err = Default.RemoveLink(context.Background(), linkTestCall(), RemoveLinkInput{
		TodoID: "t1", URL: "https://example.com/cr/1",
	})
	if !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing link error = %v, want not_found", err)
	}
}
