// Package taskrun owns the application-facing management surface for detached
// Todo Agent runs. The launch controller remains a separate internal engine;
// callers use this package to inspect, follow, and explicitly interrupt runs.
package taskrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/store"
)

type Run = store.TaskRun

type ProcessPort interface {
	LookPath(binary string) (string, error)
	Interrupt(pid int) error
}

type LogReader interface {
	io.Reader
	io.Seeker
	io.Closer
	Size() (int64, error)
}

type LogPort interface {
	Open(path string) (LogReader, error)
	Append(path string, data []byte) error
}

type SessionEventPort interface {
	ReportEnded(run Run, at time.Time) error
}

type Clock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type Dependencies struct {
	Process ProcessPort
	Logs    LogPort
	Events  SessionEventPort
	Clock   Clock
}

type Service struct {
	process ProcessPort
	logs    LogPort
	events  SessionEventPort
	clock   Clock
}

func NewService(dependencies Dependencies) Service {
	if dependencies.Process == nil {
		dependencies.Process = localProcess{}
	}
	if dependencies.Logs == nil {
		dependencies.Logs = localLogs{}
	}
	if dependencies.Events == nil {
		dependencies.Events = localSessionEvents{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = localClock{}
	}
	return Service{
		process: dependencies.Process,
		logs:    dependencies.Logs,
		events:  dependencies.Events,
		clock:   dependencies.Clock,
	}
}

var Default = NewService(Dependencies{})

func validateCall(ctx context.Context, call application.Call) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return unavailable("task run request canceled", err)
	}
	return call.Validate()
}

func requireTodo(todoID string) (string, error) {
	value := strings.TrimSpace(todoID)
	if value == "" || !store.LooksLikeTodoID(value) {
		return "", invalidArgument("valid todo ID is required", "todo_id", todoID)
	}
	id := store.NormalizeTodoID(value)
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return "", unavailable("load todo for task run", err)
	}
	if store.FindTodo(todos, id) == nil {
		return "", notFound(fmt.Sprintf("todo not found: %s", id), "todo_id", id, store.TodoNotFoundError(todos, id))
	}
	return id, nil
}

func invalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func notFound(message, field string, value any, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, message, cause)
	err.Details = map[string]any{field: value}
	return err
}

func conflict(message string, details map[string]any, cause error) *application.Error {
	err := application.WrapError(application.CodeConflict, message, cause)
	err.Details = details
	return err
}

func forbidden(message string, details map[string]any) *application.Error {
	err := application.NewError(application.CodeForbidden, message)
	err.Details = details
	return err
}

func busy(message string, details map[string]any) *application.Error {
	err := application.NewError(application.CodeBusy, message)
	err.Details = details
	err.Retryable = true
	return err
}

func unavailable(message string, cause error) *application.Error {
	if cause == nil {
		cause = errors.New(message)
	}
	err := application.WrapError(application.CodeUnavailable, message, cause)
	err.Retryable = true
	return err
}
