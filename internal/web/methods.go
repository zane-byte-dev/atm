package web

// workspaceMethodAccess is an explicit transport policy. Adding a page never
// opens an arbitrary IPC, command, config object, sync job, or model operation.
// All mutations share the same database-upgrade gate before dispatch.
func workspaceMethodAccess(method string) (known, write bool) {
	switch method {
	case "todo.list", "todo.show", "todo.doc",
		"session.list", "session.search", "session.show", "session.status", "usage.snapshot", "quota.cached",
		"knowledge.catalog", "knowledge.query", "knowledge.document.get", "memory.recall", "memory.get",
		"collect.overview", "collect.items", "collect.item.show", "collect.history",
		"day.snapshot", "day.show", "day.ledger", "settings.get":
		return true, false
	case "todo.create", "todo.update", "todo.start", "todo.done", "todo.archive", "todo.restore",
		"knowledge.document.create", "knowledge.collection.create", "memory.create", "memory.supersede",
		"collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted",
		"settings.preferences.save":
		return true, true
	default:
		return false, false
	}
}
