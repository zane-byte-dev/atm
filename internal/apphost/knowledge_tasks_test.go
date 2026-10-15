package apphost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

func TestKnowledgeTaskLinksRoundTripAndCreationReplay(t *testing.T) {
	h := testHost(t)
	ctx, call := context.Background(), webCall()
	seed(t, card("t1", "Existing", "open", "atm"))
	document, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{Title: "Reference", Content: "Notes", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	id := document.Metadata.ID
	link := TodoKnowledgeInput{TodoID: "t1", DocumentID: id}
	for i := 0; i < 2; i++ {
		body, _ := json.Marshal(link)
		if _, err := h.Call(ctx, call, "todo.knowledge.add", body, ""); err != nil {
			t.Fatal(err)
		}
	}
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil || len(shown.Todo.Links) != 1 || shown.Todo.Links[0].URL != work.KnowledgeLinkURL(id) {
		t.Fatalf("links=%+v err=%v", shown.Todo.Links, err)
	}
	input := CreateInput{Title: "Follow up", KnowledgeDocumentID: id, IdempotencyKey: "knowledge-create"}
	first, err := h.CreateTodo(ctx, call, input)
	if err != nil || len(first.Todo.Links) != 1 {
		t.Fatalf("create=%+v err=%v", first, err)
	}
	// A source rename must not change the creation intent on transport replay.
	title := "Renamed reference"
	if _, err := knowledge.Edit(config.AtmDir, id, knowledge.EditDocumentInput{Title: &title}); err != nil {
		t.Fatal(err)
	}
	again, err := h.CreateTodo(ctx, call, input)
	if err != nil || !again.Replayed || again.Todo.ID != first.Todo.ID {
		t.Fatalf("replay=%+v err=%v", again, err)
	}
	tasks, err := h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: id})
	if err != nil || len(tasks.Items) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	if _, err := h.ArchiveTodo(ctx, call, TodoInput{TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	tasks, err = h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: id})
	if err != nil || len(tasks.Items) != 2 || !tasks.Items[1].Archived {
		t.Fatalf("archived backlinks=%+v err=%v", tasks, err)
	}
	if _, err := h.LinkTodoKnowledge(ctx, call, link, true); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("archived mutation: %v", err)
	}
	if _, err := h.RestoreTodo(ctx, call, TodoInput{TodoID: "t1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.LinkTodoKnowledge(ctx, call, link, true); err != nil {
		t.Fatal(err)
	}
	tasks, err = h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: id})
	if err != nil || len(tasks.Items) != 1 || tasks.Items[0].ID != first.Todo.ID {
		t.Fatalf("removed backlink=%+v err=%v", tasks, err)
	}
}

func TestKnowledgeTaskLinksBoundariesAndPagination(t *testing.T) {
	h := testHost(t)
	ctx, call := context.Background(), webCall()
	doc, err := knowledge.Add(config.AtmDir, knowledge.AddDocumentInput{Title: "Reference", Content: "Notes", Collection: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	todos := make([]store.Todo, 51)
	for i := range todos {
		todos[i] = card(fmt.Sprintf("t%d", i+1), "Task", "open", "")
		todos[i].Links = []store.TodoLink{{URL: work.KnowledgeLinkURL(doc.Metadata.ID), Kind: "knowledge"}}
	}
	seed(t, todos...)
	first, err := h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: doc.Metadata.ID})
	if err != nil || len(first.Items) != 50 || !first.HasMore {
		t.Fatalf("page1=%+v err=%v", first, err)
	}
	second, err := h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: doc.Metadata.ID, Offset: 50})
	if err != nil || len(second.Items) != 1 || second.HasMore {
		t.Fatalf("page2=%+v err=%v", second, err)
	}
	agent := call
	agent.Actor.Kind = application.ActorAgent
	if _, err := h.LinkTodoKnowledge(ctx, agent, TodoKnowledgeInput{TodoID: "t1", DocumentID: doc.Metadata.ID}, false); !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("agent mutation=%v", err)
	}
	if _, err := h.CreateTodo(ctx, call, CreateInput{Title: "Invalid", KnowledgeDocumentID: "unknown", IdempotencyKey: "missing-source"}); !errors.Is(err, application.ErrNotFound) {
		t.Fatalf("missing source create=%v", err)
	}
	for _, body := range []string{`{"todo_id":"t1","document_id":"../secret"}`, `{"todo_id":"t1","document_id":"unknown"}`, `{"todo_id":"t1","document_id":"x","url":"https://example.com"}`} {
		if _, err := h.Call(ctx, call, "todo.knowledge.add", json.RawMessage(body), ""); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := h.KnowledgeTasks(ctx, call, KnowledgeTasksInput{DocumentID: doc.Metadata.ID, Offset: -1}); err == nil {
		t.Fatal("negative offset accepted")
	}
	current, err := store.LoadTodosReadOnly()
	if err != nil || len(current.Items) != 51 {
		t.Fatalf("invalid creation wrote tasks: %v", err)
	}
}
