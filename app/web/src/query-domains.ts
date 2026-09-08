import type { QueryClient, QueryKey } from '@tanstack/react-query'

export const allDomains = [
  'todos',
  'sessions',
  'usage',
  'knowledge',
  'memory',
  'collection',
  'day',
  'settings',
  'jobs',
  'presence',
] as const
export type Domain = (typeof allDomains)[number]

const routes: Record<string, readonly Domain[]> = {
  tasks: ['todos', 'presence', 'jobs'],
  collection: ['collection', 'jobs'],
  agents: ['sessions', 'presence', 'jobs'],
  knowledge: ['knowledge', 'memory'],
  usage: ['usage', 'day', 'jobs'],
  settings: ['settings', 'jobs', 'presence'],
}
const prefixes: Record<string, readonly Domain[]> = {
  todos: ['todos'],
  todo: ['todos'],
  doc: ['todos'],
  dependencies: ['todos'],
  collection: ['collection'],
  'collect.source.search': ['collection', 'settings'],
  'activity-status': ['sessions', 'presence'],
  'activity-sessions': ['sessions'],
  'activity-search': ['sessions'],
  'activity-session': ['sessions'],
  'activity-usage': ['usage'],
  'activity-quota': ['usage'],
  knowledge: ['knowledge', 'memory'],
  'day.snapshot': ['day'],
  'settings.get': ['settings'],
  'runtime-jobs': ['jobs'],
  'runtime-job': ['jobs'],
  presence: ['presence'],
}
const searchDomains: Record<string, readonly Domain[]> = {
  tasks: ['todos'],
  agents: ['sessions'],
  collection: ['collection'],
  knowledge: ['knowledge', 'memory'],
  all: ['todos', 'sessions', 'collection', 'knowledge', 'memory'],
}

export function domainsForQuery(key: QueryKey): readonly Domain[] {
  if (key[0] === 'global-search') return searchDomains[String(key[2])] ?? searchDomains.all
  return prefixes[String(key[0])] ?? []
}

// Include active overlays (not only pathname), so all-workspace search remains
// live while opened above a different route. Disabled cached queries add no work.
export function subscriptionDomains(client: QueryClient, route: string): Domain[] {
  const domains = new Set(routes[route] ?? routes.tasks)
  for (const query of client.getQueryCache().findAll({ type: 'active' }))
    for (const domain of domainsForQuery(query.queryKey)) domains.add(domain)
  return [...domains].sort()
}

export function invalidateDomains(client: QueryClient, domains: readonly string[]) {
  const changed = new Set(domains)
  return client.invalidateQueries({
    predicate: (query) => domainsForQuery(query.queryKey).some((domain) => changed.has(domain)),
  })
}

export function domainsForJob(kind: string): readonly Domain[] {
  switch (kind) {
    case 'session.sync':
      return ['sessions', 'usage', 'day', 'presence', 'todos']
    case 'collect.run':
    case 'collect.reprocess':
      return ['collection', 'todos', 'knowledge', 'memory']
    case 'day.rebuild':
      return ['day']
    case 'quota.refresh':
      return ['usage']
    case 'todo.refine':
      return ['todos']
    default:
      return allDomains
  }
}
