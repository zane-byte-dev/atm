package apphost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/work"
)

func testHost(t *testing.T) *Host {
	t.Helper()
	beforeDir, beforeDB, beforeConfig, beforeAliases := config.AtmDir, config.AtmDB, config.ConfigPath, config.ProjectAliases
	config.AtmDir = t.TempDir()
	config.AtmDB = filepath.Join(config.AtmDir, "atm.db")
	config.ConfigPath = filepath.Join(config.AtmDir, "config.json")
	config.ProjectAliases = nil
	t.Cleanup(func() {
		config.AtmDir, config.AtmDB, config.ConfigPath, config.ProjectAliases = beforeDir, beforeDB, beforeConfig, beforeAliases
	})
	return New("test")
}
func webCall() application.Call {
	return application.Call{RequestID: "host-test", Actor: application.Actor{Kind: application.ActorHuman, Origin: application.OriginWeb}}
}
func seed(t *testing.T, values ...store.Todo) {
	t.Helper()
	if err := store.UpdateWorkState(func(tx *store.WorkStateTx) error { tx.Todos.Items = append(tx.Todos.Items, values...); return nil }); err != nil {
		t.Fatal(err)
	}
}
func card(id, title, status, project string) store.Todo {
	return store.Todo{ID: id, Title: title, Status: status, Priority: "P2", Project: project, Created: "2026-09-03"}
}

func TestReadOnlyMissingIndexAndDocument(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	call := webCall()
	result, err := h.ListTodos(ctx, call, ListInput{})
	if err != nil || result.Total != 0 {
		t.Fatalf("missing index: %+v, %v", result, err)
	}
	if _, err := os.Stat(config.AtmDB); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("list created database: %v", err)
	}
	seed(t, card("t1", "no document", "open", "atm"))
	doc, err := h.Doc(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil || doc.Exists || doc.Content != "" {
		t.Fatalf("missing doc: %+v, %v", doc, err)
	}
	if store.TodoDocExists("t1") {
		t.Fatal("HTTP read materialized a document")
	}
}

func TestListTotalsFiltersStableOrderAndArchive(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	call := webCall()
	seed(t, card("t1", "older", "open", "atm"), card("t2", "archived", "done", "other"), card("t9", "ninth", "open", "atm"), card("t10", "tenth", "review", "atm"))
	if _, err := h.ArchiveTodo(ctx, call, TodoInput{TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	result, err := h.ListTodos(ctx, call, ListInput{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 3 || len(result.Items) != 1 || result.Items[0].ID != "t9" || result.Counts["archived"] != 1 || result.Counts["review"] != 1 {
		t.Fatalf("pagination lost total/count/order: %+v", result)
	}
	if !reflect.DeepEqual(result.Projects, []string{"atm", "other"}) {
		t.Fatalf("projects=%v", result.Projects)
	}
	result, err = h.ListTodos(ctx, call, ListInput{Status: "open", Project: "atm", Limit: 1})
	if err != nil || result.Total != 2 || result.Counts["review"] != 1 {
		t.Fatalf("filter counts=%+v err=%v", result, err)
	}
	archived, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t2"})
	if err != nil || archived.Todo.ID != "t2" || !archived.Todo.Archived {
		t.Fatalf("archived show=%+v err=%v", archived, err)
	}
	if _, err := h.RestoreTodo(ctx, call, TodoInput{TodoID: "t2"}); err != nil {
		t.Fatal(err)
	}
	result, err = h.ListTodos(ctx, call, ListInput{})
	if err != nil || result.Total != 4 || result.Counts["archived"] != 0 {
		t.Fatalf("restore=%+v err=%v", result, err)
	}
}

func TestTypedDispatchCreationAndConcurrentEdit(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	call := webCall()
	for _, raw := range []string{`{"title":"safe","actor":{"kind":"human"}}`, `{"title":"safe","on_done":"touch /tmp/no"}`, `null`, `{} {}`} {
		if _, err := h.Call(ctx, call, "todo.create", json.RawMessage(raw), "safe-create"); err == nil {
			t.Fatalf("accepted malformed/unknown fields: %s", raw)
		}
	}
	first, err := h.CreateTodo(ctx, call, CreateInput{Title: "original", IdempotencyKey: "create-one"})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := h.CreateTodo(ctx, call, CreateInput{Title: "original", IdempotencyKey: "create-one"})
	if err != nil || !replay.Replayed || replay.Todo.ID != first.Todo.ID {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	cliTitle := "CLI changed it"
	cli := call
	cli.Actor.Origin = application.OriginCLI
	if _, err := work.Default.Edit(ctx, cli, work.EditInput{TodoID: first.Todo.ID, Patch: work.EditPatch{Title: &cliTitle}}); err != nil {
		t.Fatal(err)
	}
	webTitle := "stale browser"
	_, err = h.UpdateTodo(ctx, call, UpdateInput{TodoID: first.Todo.ID, ExpectedETag: first.ETag, Title: &webTitle})
	var appErr *application.Error
	if !errors.As(err, &appErr) || appErr.Code != application.CodeConflict {
		t.Fatalf("lost concurrent update: %v", err)
	}
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: first.Todo.ID})
	if err != nil || shown.Todo.Title != cliTitle {
		t.Fatalf("CLI edit overwritten: %+v %v", shown, err)
	}
	if _, err = h.Call(ctx, call, "guard.approve", json.RawMessage(`{"todo_id":"t1"}`), ""); err == nil {
		t.Fatal("Guard exposed through Web whitelist")
	}
	call.Actor.Kind = application.ActorAgent
	if _, err = h.DoneTodo(ctx, call, DoneInput{TodoID: first.Todo.ID}); !errors.As(err, &appErr) || appErr.Code != application.CodeForbidden {
		t.Fatalf("agent done error=%v", err)
	}
}

func TestAttachmentUsesManagedIdentityAndRejectsEscapingSymlink(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	call := webCall()
	root := filepath.Join(config.AtmDir, "todos", "assets", "t1")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "one.png")
	if err := os.WriteFile(path, []byte("test image"), 0600); err != nil {
		t.Fatal(err)
	}
	todo := card("t1", "image", "open", "atm")
	todo.Images = []store.TodoImage{{Name: "one.png", StoredName: "one.png", Path: path, MediaType: "image/png", SizeBytes: 10}}
	seed(t, todo)
	shown, err := h.ShowTodo(ctx, call, TodoInput{TodoID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(shown.Todo.Images)
	if strings.Contains(string(encoded), config.AtmDir) || strings.Contains(string(encoded), `"path"`) {
		t.Fatalf("leaked path: %s", encoded)
	}
	got, media, err := h.Attachment(ctx, call, shown.Todo.Images[0].ID)
	realPath, _ := filepath.EvalSymlinks(path)
	if err != nil || got != realPath || media != "image/png" {
		t.Fatalf("attachment=%q %q %v", got, media, err)
	}
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.Attachment(ctx, call, shown.Todo.Images[0].ID); err == nil {
		t.Fatal("served symlink outside managed image directory")
	}
	traversal := base64.RawURLEncoding.EncodeToString([]byte("t1/../../secret"))
	if _, _, err := h.Attachment(ctx, call, traversal); err == nil {
		t.Fatal("accepted traversal identity")
	}
}

type countingEffects struct{ calls atomic.Int32 }

func (e *countingEffects) ApplyWorkEffect(work.Effect) error { e.calls.Add(1); return nil }

func TestRepeatedConcurrentDoneDeliversOneEffect(t *testing.T) {
	h := testHost(t)
	ctx := context.Background()
	call := webCall()
	seed(t, card("t1", "finish", "in_progress", "atm"))
	effects := &countingEffects{}
	h.SetWorkEffects(effects)
	var wg sync.WaitGroup
	errors := make(chan error, 6)
	for range 6 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := h.DoneTodo(ctx, call, DoneInput{TodoID: "t1"}); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if effects.calls.Load() != 1 {
		t.Fatalf("double-click/reconnect delivered effect %d times", effects.calls.Load())
	}
}

func TestConfigureDataDirCannotRedirectFixtureToAnotherDatabase(t *testing.T) {
	h := testHost(t)
	selected := config.AtmDir
	foreign := t.TempDir()
	foreignDB := filepath.Join(foreign, "atm.db")
	if err := os.WriteFile(foreignDB, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(map[string]any{"data_dir": foreign})
	if err := os.WriteFile(filepath.Join(selected, "config.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	beforeHome, beforeCodex := config.Home, config.CodexSessions
	if err := ConfigureDataDir(selected); err != nil {
		t.Fatal(err)
	}
	realSelected, _ := filepath.EvalSymlinks(selected)
	if config.AtmDir != realSelected || config.AtmDB != filepath.Join(realSelected, "atm.db") || config.ConfigPath != filepath.Join(realSelected, "config.json") {
		t.Fatalf("data paths escaped explicit override: %s %s %s", config.AtmDir, config.AtmDB, config.ConfigPath)
	}
	if config.Home != beforeHome || config.CodexSessions != beforeCodex {
		t.Fatal("data override changed HOME or Agent source paths")
	}
	if _, err := h.CreateTodo(context.Background(), webCall(), CreateInput{Title: "isolated", IdempotencyKey: "fixture"}); err != nil {
		t.Fatal(err)
	}
	untouched, err := os.ReadFile(foreignDB)
	if err != nil || string(untouched) != "untouched" {
		t.Fatalf("unrelated database changed: %q %v", untouched, err)
	}
}

func TestUnavailableKeepsInfrastructureCauseOutOfPublicJSON(t *testing.T) {
	cause := errors.New("open /private/account/atm.db: permission denied")
	err := unavailable(cause)
	if !errors.Is(err, cause) || !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("error did not retain cause and category: %v", err)
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), "/private/account") || strings.Contains(string(encoded), "permission denied") {
		t.Fatalf("public error leaked infrastructure detail: %s", encoded)
	}
}
