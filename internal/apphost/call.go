package apphost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/zane-byte-dev/atm/internal/application"
)

// Call is an explicit method whitelist, not reflection over services. Unknown
// request fields fail closed, including actor/origin and arbitrary path fields.
func (h *Host) Call(ctx context.Context, call application.Call, method string, input json.RawMessage, idempotencyKey string) (any, error) {
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
	case "session.list", "session.search", "session.show", "session.status", "usage.snapshot", "quota.cached":
		return h.callActivity(ctx, call, method, input)
	case "knowledge.catalog", "knowledge.query", "knowledge.document.get", "knowledge.document.create", "knowledge.collection.create",
		"memory.recall", "memory.get", "memory.create", "memory.supersede":
		return h.callKnowledge(ctx, call, method, input)
	case "collect.overview", "collect.items", "collect.item.show", "collect.history", "collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted":
		return h.callCollection(ctx, call, method, input)
	case "day.snapshot", "day.show", "day.ledger", "settings.get", "settings.preferences.save":
		return h.callWorkspaceSettings(ctx, call, method, input)
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown API method")
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
