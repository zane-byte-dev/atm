package work

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func webMetadataCall(kind application.ActorKind, requestID string) application.Call {
	return application.Call{RequestID: requestID, Actor: application.Actor{Kind: kind, Origin: application.OriginWeb}}
}

func TestAddIdempotencyReplaysOriginalResponseWithoutOverwritingLaterCLIEdit(t *testing.T) {
	withTempWorkStore(t)
	ctx := context.Background()
	input := AddInput{IdempotencyKey: "create-1", Title: "Original", Description: "Initial requirement"}
	first, err := Default.Add(ctx, webMetadataCall(application.ActorHuman, "first-request"), input)
	if err != nil || first.Replayed || len(first.Effects) != 1 {
		t.Fatalf("first Add = %+v, err=%v", first, err)
	}
	_, err = Default.Edit(ctx, metadataTestCall(application.ActorAgent, "codex"), EditInput{
		TodoID: first.Todo.ID, Patch: EditPatch{Title: stringPointerForTest("CLI changed title")},
	})
	if err != nil {
		t.Fatal(err)
	}

	replay, err := Default.Add(ctx, webMetadataCall(application.ActorHuman, "new-request-after-reconnect"), input)
	if err != nil || !replay.Replayed || replay.Todo.ID != first.Todo.ID ||
		replay.Todo.Title != "Original" || TodoETag(replay.Todo) != TodoETag(first.Todo) || len(replay.Effects) != 0 {
		t.Fatalf("replayed Add = %+v, err=%v", replay, err)
	}
	document, err := store.ReadTodoDoc(first.Todo.ID)
	if err != nil || !strings.Contains(document, "# CLI changed title") {
		t.Fatalf("replay rewrote document: %q, err=%v", document, err)
	}

	input.Description = "Different intent with the same key"
	_, err = Default.Add(ctx, webMetadataCall(application.ActorHuman, "conflicting-request"), input)
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("reused key with different payload error = %v", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || todos.Items[0].Title != "CLI changed title" {
		t.Fatalf("persisted todos = %+v, err=%v", todos, err)
	}
}

func TestAddConcurrentIdempotencyCreatesOneTodo(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t)
	const attempts = 6
	type outcome struct {
		result AddResult
		err    error
	}
	results := make(chan outcome, attempts)
	var workers sync.WaitGroup
	for range attempts {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := Default.Add(context.Background(), webMetadataCall(application.ActorHuman, "parallel-request"), AddInput{
				IdempotencyKey: "same-intent", Title: "Create once",
			})
			results <- outcome{result, err}
		}()
	}
	workers.Wait()
	close(results)
	created := 0
	for result := range results {
		if result.err != nil || result.result.Todo.ID != "t1" {
			t.Fatalf("parallel Add = %+v, err=%v", result.result, result.err)
		}
		if !result.result.Replayed {
			created++
		}
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || len(todos.Items) != 1 || created != 1 {
		t.Fatalf("created=%d persisted=%+v err=%v", created, todos, err)
	}
}

func TestWebEditChecksETagAgainstCLIChangeInsideWorkTransaction(t *testing.T) {
	withTempWorkStore(t)
	ctx := context.Background()
	first, err := Default.Add(ctx, metadataTestCall(application.ActorHuman, ""), AddInput{Title: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	oldETag := TodoETag(first.Todo)
	cli, err := Default.Edit(ctx, metadataTestCall(application.ActorAgent, "codex"), EditInput{
		TodoID: first.Todo.ID, Patch: EditPatch{Description: stringPointerForTest("CLI requirement update")},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Default.Edit(ctx, webMetadataCall(application.ActorHuman, "stale-edit"), EditInput{
		TodoID: first.Todo.ID, ExpectedETag: oldETag,
		Patch: EditPatch{Title: stringPointerForTest("Browser title")},
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatalf("stale edit error = %v", err)
	}
	var conflict *application.Error
	if !errors.As(err, &conflict) || conflict.Details["current_etag"] != TodoETag(cli.Todo) || conflict.Details["expected_etag"] != oldETag {
		t.Fatalf("conflict details = %+v", conflict)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || todos.Items[0].Title != "Original" || todos.Items[0].Description != "CLI requirement update" {
		t.Fatalf("conflicting patch changed todo: %+v, err=%v", todos, err)
	}
	updated, err := Default.Edit(ctx, webMetadataCall(application.ActorHuman, "fresh-edit"), EditInput{
		TodoID: first.Todo.ID, ExpectedETag: TodoETag(cli.Todo),
		Patch: EditPatch{Title: stringPointerForTest("Browser title")},
	})
	if err != nil || updated.Todo.Title != "Browser title" || updated.Todo.Description != "CLI requirement update" || TodoETag(updated.Todo) == TodoETag(cli.Todo) {
		t.Fatalf("fresh edit = %+v, err=%v", updated, err)
	}
}

func TestWebMetadataRequiresConcurrencyTokens(t *testing.T) {
	withTempWorkStore(t)
	call := webMetadataCall(application.ActorHuman, "web-missing-token")
	_, err := Default.Add(context.Background(), call, AddInput{Title: "Missing key"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("missing create key error = %v", err)
	}
	_, err = Default.Edit(context.Background(), call, EditInput{TodoID: "t1", Patch: EditPatch{Title: stringPointerForTest("Missing etag")}})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("missing edit etag error = %v", err)
	}
}

func TestTodoETagIncludesStoredContentAndIgnoresDerivedImagePath(t *testing.T) {
	todo := Todo{
		ID: "t1", Title: "Stable", Priority: "P2", Status: store.TodoStatusOpen,
		Tags: []string{"b", "a"}, DependsOn: []string{"t3", "t2"},
		Images: []store.TodoImage{{Name: "image.png", StoredName: "one.png", Path: "/old/home/image.png", MediaType: "image/png", SizeBytes: 10}},
	}
	original := TodoETag(todo)
	copy := cloneTodo(todo)
	copy.Images[0].Path = "/new/home/image.png"
	copy.Tags = []string{"a", "b"}
	copy.DependsOn = []string{"t2", "t3"}
	if TodoETag(copy) != original || todo.Tags[0] != "b" {
		t.Fatal("ETag depends on derived paths/set ordering or mutated the input")
	}
	copy.Images[0].StoredName = "replacement.png"
	if TodoETag(copy) == original {
		t.Fatal("ETag ignores persisted attachment identity")
	}
	copy = cloneTodo(todo)
	copy.Status = store.TodoStatusReview
	if TodoETag(copy) == original {
		t.Fatal("ETag ignores lifecycle changes")
	}
}

func TestWebDoneKeepsHumanAcceptancePolicy(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, Todo{ID: "t1", Title: "Review this", Priority: "P2", Status: store.TodoStatusReview, Created: store.Today()})
	ctx := context.Background()
	_, err := Default.Done(ctx, webMetadataCall(application.ActorAgent, "agent-done"), CloseInput{TodoID: "t1"})
	if !errors.Is(err, application.ErrForbidden) {
		t.Fatalf("Agent Web Done error = %v", err)
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil || todos.Items[0].Status != store.TodoStatusReview {
		t.Fatalf("agent changed status: %+v, err=%v", todos, err)
	}
	result, err := Default.Done(ctx, webMetadataCall(application.ActorHuman, "human-done"), CloseInput{TodoID: "t1"})
	if err != nil || result.Todo.Status != store.TodoStatusDone || result.Todo.ClosedReason == nil || *result.Todo.ClosedReason != store.TodoGUICompletionReceipt {
		t.Fatalf("Human Web Done = %+v, err=%v", result, err)
	}
}
