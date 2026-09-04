package web

// workspaceMethodAccess is an explicit transport policy. Adding a page never
// opens an arbitrary IPC, command, config object, sync job, or model operation.
// All mutations share the same database-upgrade gate before dispatch.
func workspaceMethodAccess(method string) (known, write bool) {
	switch method {
	case "todo.list", "todo.show", "todo.doc", "todo.dependency.list", "jobs.list", "jobs.show", "presence.snapshot",
		"session.list", "session.search", "session.show", "session.status", "usage.snapshot", "quota.cached",
		"knowledge.catalog", "knowledge.query", "knowledge.document.get", "memory.recall", "memory.get",
		"collect.overview", "collect.items", "collect.item.show", "collect.history",
		"day.snapshot", "day.show", "day.ledger", "settings.get":
		return true, false
	default:
		write = len(workspaceWriteDomains(method)) > 0
		return write, write
	}
}

// workspaceWriteDomains describes the smallest live-query surface changed by a
// successful browser mutation. Runtime work publishes any additional domains
// when that work completes; this mapping only covers the mutation accepted by
// the HTTP request itself.
func workspaceWriteDomains(method string) []string {
	switch method {
	case "todo.create", "todo.update", "todo.start", "todo.done", "todo.archive", "todo.restore",
		"todo.plan.set", "todo.progress.append", "todo.dependency.add", "todo.dependency.remove", "todo.wait.update", "todo.wake":
		return []string{"todos"}
	case "jobs.run", "jobs.cancel":
		return []string{"jobs"}
	case "knowledge.document.create", "knowledge.document.update", "knowledge.collection.create":
		return []string{"knowledge"}
	case "memory.create", "memory.supersede":
		return []string{"memory"}
	case "collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted",
		"collect.source.save", "collect.source.delete":
		return []string{"collection"}
	case "settings.preferences.save", "settings.business.save", "settings.credential.save", "settings.credential.delete":
		return []string{"settings"}
	default:
		return nil
	}
}
