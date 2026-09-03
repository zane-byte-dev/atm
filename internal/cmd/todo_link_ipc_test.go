package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/zane-byte-dev/atm/internal/appipc"
	"github.com/zane-byte-dev/atm/internal/application"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

func TestIPCTodoLinkWorkflow(t *testing.T) {
	withIsolatedCommandEnv(t)
	created := runTodoIPCSuccess[appipc.TodoCreateRequest, workapp.Todo](t, "todo.create", appipc.TodoCreateRequest{Title: "Related content"})
	added := runTodoIPCSuccess[workapp.SaveLinkInput, workapp.Todo](t, "todo.link.save", workapp.SaveLinkInput{
		TodoID: created.ID, URL: "https://example.test/cr/1", Title: "CR", Relation: "evidence",
	})
	if len(added.Links) != 1 || added.Links[0].Kind != "cr" || added.Status != "open" {
		t.Fatalf("added = %+v", added)
	}
	updated := runTodoIPCSuccess[workapp.SaveLinkInput, workapp.Todo](t, "todo.link.save", workapp.SaveLinkInput{
		TodoID: created.ID, OriginalURL: added.Links[0].URL, URL: "https://example.test/preview", Kind: "preview",
	})
	if len(updated.Links) != 1 || updated.Links[0].Title != "" || updated.Links[0].Relation != "" || updated.Links[0].Kind != "preview" {
		t.Fatalf("updated = %+v", updated)
	}
	removed := runTodoIPCSuccess[workapp.RemoveLinkInput, workapp.Todo](t, "todo.link.remove", workapp.RemoveLinkInput{TodoID: created.ID, URL: updated.Links[0].URL})
	if len(removed.Links) != 0 || removed.Status != created.Status {
		t.Fatalf("removed = %+v", removed)
	}
}

func TestIPCTodoLinksRejectUnsafeAndExtraParameters(t *testing.T) {
	withIsolatedCommandEnv(t)
	for _, test := range []struct{ method, body string }{
		{"todo.link.save", `{"todo_id":"t1","url":"javascript:alert(1)"}`},
		{"todo.link.save", `{"todo_id":"t1","url":"https://example.test/?token=secret"}`},
		{"todo.link.save", `{"todo_id":"t1","url":"https://example.test","status":"done"}`},
		{"todo.link.remove", `{"todo_id":"t1","url":"https://example.test","argv":["delete"]}`},
	} {
		var output bytes.Buffer
		if err := rawTodoIPC(test.method, test.body, &output); !errors.Is(err, application.ErrInvalidArgument) {
			t.Fatalf("%s: %v", test.body, err)
		}
	}
}
