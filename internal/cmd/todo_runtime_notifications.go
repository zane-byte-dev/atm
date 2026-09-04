package cmd

import (
	"encoding/json"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/presence"
	"github.com/zane-byte-dev/atm/internal/store"
)

// forwardTodoNotification returns ownership, not delivery success. An uncertain
// acknowledgement must not make a CLI spawn a second banner after the runtime
// already accepted the transition. When no Go owner has been selected, the
// historical standalone CLI notification path remains available.
func forwardTodoNotification(todo *store.Todo, event, title, subtitle, body string) bool {
	notification := todoRuntimeNotification(todo, event, title, subtitle, body)
	owned, _ := presence.Forward(config.AtmDir, "", notification)
	return owned
}

func todoRuntimeNotification(todo *store.Todo, event, title, subtitle, body string) presence.Notification {
	// Lifecycle timestamps identify a reopened turn without incorporating
	// unrelated edits to the title or tags. Retries of the same Work effect keep
	// the exact same key across CLI/server restarts.
	transition, _ := json.Marshal(struct {
		ID      string  `json:"id"`
		Event   string  `json:"event"`
		Created string  `json:"created"`
		Start   *int64  `json:"start"`
		Done    *int64  `json:"done"`
		Closed  *string `json:"closed"`
	}{todo.ID, event, todo.Created, todo.StartTS, todo.DoneTS, todo.Closed})
	return presence.Notification{ID: "todo-" + todo.ID, Kind: "todo_" + event, Action: "post", Title: title, Subtitle: subtitle, Body: body, ObjectID: todo.ID, DedupKey: string(transition)}
}
