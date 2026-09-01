package work

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

func todoLogCall() application.Call {
	return application.Call{
		RequestID: "todo-log-request",
		Actor: application.Actor{
			Kind:      application.ActorAgent,
			Origin:    application.OriginCLI,
			SessionID: "session-1",
			Agent:     "codex",
		},
	}
}

func TestLogSyncsTodoMetadataAndAppendsKnownReference(t *testing.T) {
	withTempWorkStore(t)
	original := store.Todo{
		ID: "t1", Title: "Old title", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	}
	seedWorkTodos(t, original,
		store.Todo{ID: "t2", Title: "Known dependency", Priority: "P2", Status: store.TodoStatusDone, Created: store.Today()},
	)
	if _, err := store.InitTodoDoc(&original); err != nil {
		t.Fatal(err)
	}
	updated := original
	updated.Title = "Current title"
	updated.Description = "Current requirement"
	seedWorkTodos(t, updated,
		store.Todo{ID: "t2", Title: "Known dependency", Priority: "P2", Status: store.TodoStatusDone, Created: store.Today()},
	)

	result, err := Default.Log(context.Background(), todoLogCall(), LogInput{
		TodoID:  "#T01",
		Message: "结果：t2 已核验；证据：服务测试通过；下一步：接入 CLI",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if result.TodoID != "t1" || result.Path != store.TodoDocPath("t1") || result.Section != "进展" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Entry, "结果：t2 已核验") || !strings.HasSuffix(result.Entry, "\n") {
		t.Fatalf("entry = %q", result.Entry)
	}

	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Current title", "Current requirement", "结果：t2 已核验"} {
		if !strings.Contains(doc, want) {
			t.Errorf("todo doc does not contain %q:\n%s", want, doc)
		}
	}
}

func TestLogCreatesMissingDocumentAndPreservesAnalysisBody(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Analyze", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	message := "结论：保留服务边界\n\n```go\nservice.Log()\n```"
	result, err := Default.Log(nil, todoLogCall(), LogInput{
		TodoID: "1", Message: message, Section: "分析",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if result.Section != "分析" || !store.TodoDocExists("t1") {
		t.Fatalf("result = %+v, doc exists = %v", result, store.TodoDocExists("t1"))
	}
	doc, err := store.ReadTodoDoc("t1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, "service.Log()") {
		t.Fatalf("analysis body missing:\n%s", doc)
	}
}

func TestLogAcceptsReferenceToArchivedTodo(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t,
		store.Todo{ID: "t1", Title: "Live", Priority: "P1", Status: store.TodoStatusInProgress, Created: store.Today()},
		store.Todo{ID: "t2", Title: "Archived evidence", Priority: "P2", Status: store.TodoStatusDone, Created: store.Today()},
	)
	if err := Default.Mutate(func(transaction *Transaction) error {
		_, err := transaction.ArchiveTodos([]string{"t2"})
		return err
	}); err != nil {
		t.Fatalf("archive referenced todo: %v", err)
	}

	if _, err := Default.Log(context.Background(), todoLogCall(), LogInput{
		TodoID: "t1", Message: "结果：复用了 t2 的验证证据",
	}); err != nil {
		t.Fatalf("Log with archived reference: %v", err)
	}
}

func TestLogRejectsTypedValidationAndLookupFailuresWithoutWriting(t *testing.T) {
	tests := []struct {
		name      string
		call      application.Call
		input     LogInput
		wantError error
		wantField string
	}{
		{
			name:      "invalid call",
			call:      application.Call{Actor: application.Actor{Kind: application.ActorAgent, Origin: application.OriginCLI}},
			input:     LogInput{TodoID: "t1", Message: "result"},
			wantError: application.ErrInvalidArgument, wantField: "request_id",
		},
		{
			name: "missing todo id", call: todoLogCall(), input: LogInput{Message: "result"},
			wantError: application.ErrInvalidArgument, wantField: "todo_id",
		},
		{
			name: "malformed todo id", call: todoLogCall(), input: LogInput{TodoID: "work", Message: "result"},
			wantError: application.ErrInvalidArgument, wantField: "todo_id",
		},
		{
			name: "empty message", call: todoLogCall(), input: LogInput{TodoID: "t1"},
			wantError: application.ErrInvalidArgument, wantField: "message",
		},
		{
			name: "multiline progress", call: todoLogCall(), input: LogInput{TodoID: "t1", Message: "result\nevidence"},
			wantError: application.ErrInvalidArgument, wantField: "message",
		},
		{
			name: "generated section", call: todoLogCall(), input: LogInput{TodoID: "t1", Message: "overwrite", Section: "需求"},
			wantError: application.ErrInvalidArgument, wantField: "section",
		},
		{
			name: "unknown reference", call: todoLogCall(), input: LogInput{TodoID: "t1", Message: "split t999"},
			wantError: application.ErrInvalidArgument, wantField: "message",
		},
		{
			name: "unknown todo", call: todoLogCall(), input: LogInput{TodoID: "t404", Message: "result"},
			wantError: application.ErrNotFound,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withTempWorkStore(t)
			seedWorkTodos(t, store.Todo{
				ID: "t1", Title: "Do not write", Priority: "P1",
				Status: store.TodoStatusInProgress, Created: store.Today(),
			})
			_, err := Default.Log(context.Background(), test.call, test.input)
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Log error = %v, want %v", err, test.wantError)
			}
			if test.wantField != "" {
				var appErr *application.Error
				if !errors.As(err, &appErr) || appErr.Details["field"] != test.wantField {
					t.Fatalf("Log error details = %#v, want field %q", appErr, test.wantField)
				}
			}
			if store.TodoDocExists("t1") {
				t.Fatal("rejected log created a Todo document")
			}
		})
	}
}

func TestLogReportsUnknownReferencesAndMissingInfrastructureAsTypedErrors(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Typed errors", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	_, err := Default.Log(context.Background(), todoLogCall(), LogInput{TodoID: "t1", Message: "split t99"})
	var appErr *application.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("Log error type = %T", err)
	}
	unknown, ok := appErr.Details["unknown_todo_ids"].([]string)
	if !ok || len(unknown) != 1 || unknown[0] != "t99" {
		t.Fatalf("unknown todo details = %#v", appErr.Details)
	}

	// Pointing at a fresh ATM directory makes the read-only store unavailable;
	// Log must not leak the persistence sentinel as an untyped transport error.
	withTempWorkStore(t)
	_, err = Default.Log(context.Background(), todoLogCall(), LogInput{TodoID: "t1", Message: "result"})
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("Log error = %v, want unavailable", err)
	}
}

func TestLogHonorsCancelledContext(t *testing.T) {
	withTempWorkStore(t)
	seedWorkTodos(t, store.Todo{
		ID: "t1", Title: "Cancelled", Priority: "P1",
		Status: store.TodoStatusInProgress, Created: store.Today(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Default.Log(ctx, todoLogCall(), LogInput{TodoID: "t1", Message: "result"})
	if !errors.Is(err, application.ErrUnavailable) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Log error = %v, want unavailable wrapping context cancellation", err)
	}
}
