package work

import (
	"context"
	"fmt"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

// LogInput is the transport-independent intent for adding one entry to a Todo
// document. Adapters remain responsible for resolving an omitted/current Todo
// ID and for reading an inline body, file, or stdin before calling the service.
type LogInput struct {
	TodoID  string `json:"todo_id"`
	Message string `json:"message"`
	Section string `json:"section,omitempty"`
}

// LogResult contains the stable facts a transport needs to render a successful
// log operation without reaching back into the filesystem or persistence
// packages. Entry retains the Markdown line ending used by AppendTodoLog so the
// existing CLI text output stays byte-for-byte compatible.
type LogResult struct {
	TodoID  string `json:"todo_id"`
	Path    string `json:"path"`
	Section string `json:"section"`
	Entry   string `json:"entry"`
}

// Log validates and appends one Todo document entry. Todo metadata is refreshed
// first when the document already exists; a missing document is initialized by
// AppendTodoLog. The Todo database is read-only for this use case.
func (Service) Log(
	ctx context.Context,
	call application.Call,
	input LogInput,
) (LogResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return LogResult{}, logUnavailable("log todo", err)
	}
	if err := call.Validate(); err != nil {
		return LogResult{}, err
	}

	if strings.TrimSpace(input.TodoID) == "" {
		return LogResult{}, logInvalidArgument("todo ID is required", "todo_id", input.TodoID)
	}
	if !store.LooksLikeTodoID(input.TodoID) {
		return LogResult{}, logInvalidArgument(
			fmt.Sprintf("invalid todo ID %q", input.TodoID), "todo_id", input.TodoID,
		)
	}
	if err := store.ValidateTodoLogMessage(input.Message, input.Section); err != nil {
		field, value := "message", any(input.Message)
		if input.Section == "需求" {
			field, value = "section", input.Section
		}
		return LogResult{}, logInvalidArgument(err.Error(), field, value)
	}

	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return LogResult{}, logUnavailable("load todos for log", err)
	}
	todo := store.FindTodo(todos, input.TodoID)
	if todo == nil {
		canonical := store.NormalizeTodoID(input.TodoID)
		cause := store.TodoNotFoundError(todos, input.TodoID)
		appErr := application.WrapError(
			application.CodeNotFound,
			cause.Error(),
			cause,
		)
		appErr.Details = map[string]any{"todo_id": canonical}
		return LogResult{}, appErr
	}
	if unknown := store.UnknownTodoReferences(todos, input.Message); len(unknown) > 0 {
		appErr := logInvalidArgument(
			fmt.Sprintf(
				"todo log references unknown todo IDs: %s; create and verify structured todos before logging them",
				strings.Join(unknown, ", "),
			),
			"message",
			input.Message,
		)
		appErr.Details["unknown_todo_ids"] = unknown
		return LogResult{}, appErr
	}

	if store.TodoDocExists(todo.ID) {
		if err := store.SyncTodoDocMetadata(todo); err != nil {
			return LogResult{}, logUnavailable("sync todo doc", err)
		}
	}
	entry, err := store.AppendTodoLog(todo, input.Message, input.Section)
	if err != nil {
		return LogResult{}, logUnavailable("append todo log", err)
	}
	section := input.Section
	if section == "" {
		section = "进展"
	}
	return LogResult{
		TodoID:  todo.ID,
		Path:    store.TodoDocPath(todo.ID),
		Section: section,
		Entry:   entry,
	}, nil
}

func logInvalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func logUnavailable(message string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}
