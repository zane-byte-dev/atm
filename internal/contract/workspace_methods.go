package contract

// WorkspaceMethod is the shared, transport-neutral capability catalog for the
// resident workspace API. Adapters may enforce additional authentication and
// upgrade gates, but they must not maintain a second method allowlist.
type WorkspaceMethod struct {
	Name    string
	Write   bool
	Domains []string
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
	writes := map[string][]string{
		"todos": {
			"todo.create", "todo.update", "todo.start", "todo.done", "todo.archive", "todo.restore",
			"todo.plan.set", "todo.progress.append", "todo.dependency.add", "todo.dependency.remove", "todo.wait.update", "todo.wake",
		},
		"jobs":      {"jobs.run", "jobs.cancel"},
		"knowledge": {"knowledge.document.create", "knowledge.document.update", "knowledge.collection.create"},
		"memory":    {"memory.create", "memory.supersede"},
		"collection": {
			"collect.item.read", "collect.item.archive", "collect.source.enabled", "collect.source.muted",
			"collect.source.save", "collect.source.delete",
		},
		"settings": {"settings.preferences.save", "settings.business.save", "settings.credential.save", "settings.credential.delete"},
	}
	for domain, names := range writes {
		for _, name := range names {
			result[name] = WorkspaceMethod{Name: name, Write: true, Domains: []string{domain}}
		}
	}
	return result
}()

func LookupWorkspaceMethod(name string) (WorkspaceMethod, bool) {
	method, ok := workspaceMethods[name]
	if !ok {
		return WorkspaceMethod{}, false
	}
	method.Domains = append([]string(nil), method.Domains...)
	return method, true
}
