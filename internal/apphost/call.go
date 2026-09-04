package apphost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/contract"
)

// Call is an explicit method whitelist, not reflection over services. Unknown
// request fields fail closed, including actor/origin and arbitrary path fields.
func (h *Host) Call(ctx context.Context, call application.Call, method string, input json.RawMessage, idempotencyKey string) (any, error) {
	if _, ok := contract.LookupWorkspaceMethod(method); !ok {
		return nil, application.NewError(application.CodeNotFound, "unknown API method")
	}
	switch method {
	case "todo.list":
		return invoke(input, func(value ListInput) (any, error) { return h.ListTodos(ctx, call, value) })
	case "todo.show":
		return invoke(input, func(value TodoInput) (any, error) { return h.ShowTodo(ctx, call, value) })
	case "todo.doc":
		return invoke(input, func(value TodoInput) (any, error) { return h.Doc(ctx, call, value) })
	case "todo.create":
		return invoke(input, func(value CreateInput) (any, error) {
			value.IdempotencyKey = idempotencyKey
			return h.CreateTodo(ctx, call, value)
		})
	case "todo.update":
		return invoke(input, func(value UpdateInput) (any, error) { return h.UpdateTodo(ctx, call, value) })
	case "todo.start":
		return invoke(input, func(value StartInput) (any, error) { return h.StartTodo(ctx, call, value) })
	case "todo.done":
		return invoke(input, func(value DoneInput) (any, error) { return h.DoneTodo(ctx, call, value) })
	case "todo.archive":
		return invoke(input, func(value TodoInput) (any, error) { return h.ArchiveTodo(ctx, call, value) })
	case "todo.restore":
		return invoke(input, func(value TodoInput) (any, error) { return h.RestoreTodo(ctx, call, value) })
	case "todo.plan.set":
		return invoke(input, func(value PlanInput) (any, error) { return h.SetTodoPlan(ctx, call, value) })
	case "todo.progress.append":
		return invoke(input, func(value ProgressInput) (any, error) { return h.AppendTodoProgress(ctx, call, value) })
	case "todo.dependency.add":
		return invoke(input, func(value DependencyInput) (any, error) { return h.AddTodoDependency(ctx, call, value) })
	case "todo.dependency.remove":
		return invoke(input, func(value DependencyInput) (any, error) { return h.RemoveTodoDependency(ctx, call, value) })
	case "todo.dependency.list":
		return invoke(input, func(value TodoInput) (any, error) { return h.TodoDependencies(ctx, call, value) })
	case "todo.wait.update":
		return invoke(input, func(value WaitInput) (any, error) { return h.UpdateTodoWait(ctx, call, value) })
	case "todo.wake":
		return invoke(input, func(value WakeInput) (any, error) { return h.WakeTodo(ctx, call, value) })
	case "jobs.run", "jobs.list", "jobs.show", "jobs.cancel", "presence.snapshot":
		return h.CallRuntime(ctx, call, method, input, idempotencyKey)
	case "session.list", "session.search", "session.show", "session.status", "usage.snapshot", "quota.cached":
		return h.callActivity(ctx, call, method, input)
	case "knowledge.catalog", "knowledge.query", "knowledge.document.get", "knowledge.document.create", "knowledge.document.update", "knowledge.collection.create",
		"memory.recall", "memory.get", "memory.create", "memory.supersede":
		return h.callKnowledge(ctx, call, method, input)
	case "collect.overview", "collect.items", "collect.item.show", "collect.history", "collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted", "collect.source.save", "collect.source.delete":
		return h.callCollection(ctx, call, method, input)
	case "day.snapshot", "day.show", "day.ledger", "settings.get", "settings.preferences.save", "settings.business.save", "settings.credential.save", "settings.credential.delete":
		return h.callWorkspaceSettings(ctx, call, method, input)
	default:
		return nil, application.NewError(application.CodeInternal, "registered API method is not implemented")
	}
}

func invoke[T any](raw json.RawMessage, run func(T) (any, error)) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	if bytes.TrimSpace(raw)[0] != '{' {
		return nil, invalid("request body must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return nil, invalid("invalid request: " + err.Error())
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, invalid("request must contain one JSON object")
	}
	return run(value)
}
