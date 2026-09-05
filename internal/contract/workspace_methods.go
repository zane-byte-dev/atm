package contract

// WorkspaceMethod is the shared, transport-neutral capability catalog for the
// resident workspace API. Adapters may enforce additional authentication, but
// they must not maintain a second method allowlist.
type WorkspaceMethod struct {
	Name  string
	Write bool
}

var workspaceMethods = func() map[string]WorkspaceMethod {
	result := make(map[string]WorkspaceMethod)
	read := []string{
		"todo.list", "todo.show", "todo.doc", "todo.dependency.list",
		"jobs.list", "jobs.show", "presence.snapshot",
		"session.list", "session.search", "session.show", "session.status", "usage.snapshot", "quota.cached",
		"knowledge.catalog", "knowledge.query", "knowledge.document.get", "memory.recall", "memory.get",
		"collect.overview", "collect.items", "collect.item.show", "collect.history",
		"day.snapshot", "day.show", "day.ledger", "settings.get",
	}
	for _, name := range read {
		result[name] = WorkspaceMethod{Name: name}
	}
	writes := [][]string{
		{
			"todo.create", "todo.update", "todo.start", "todo.done", "todo.archive", "todo.restore",
			"todo.plan.set", "todo.progress.append", "todo.dependency.add", "todo.dependency.remove", "todo.wait.update", "todo.wake",
		},
		{"jobs.run", "jobs.cancel"},
		{"knowledge.document.create", "knowledge.document.update", "knowledge.collection.create"},
		{"memory.create", "memory.supersede"},
		{
			"collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted",
			"collect.source.save", "collect.source.delete",
		},
		{"settings.preferences.save", "settings.business.save", "settings.credential.save", "settings.credential.delete"},
	}
	for _, names := range writes {
		for _, name := range names {
			result[name] = WorkspaceMethod{Name: name, Write: true}
		}
	}
	return result
}()

func LookupWorkspaceMethod(name string) (WorkspaceMethod, bool) {
	method, ok := workspaceMethods[name]
	if !ok {
		return WorkspaceMethod{}, false
	}
	return method, true
}
