import test from 'node:test'
import assert from 'node:assert/strict'
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { domainsForJob, invalidateDomains, subscriptionDomains } from '../src/query-domains.ts'

test('knowledge changes refresh open global search without refetching unrelated tasks', async () => {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: Infinity } },
  })
  const reads = { search: 0, knowledge: 0, tasks: 0 }
  let title = 'before'
  const observers = [
    new QueryObserver(client, {
      queryKey: ['global-search', 'note', 'all', ''],
      queryFn: async () => {
        reads.search++
        return title
      },
    }),
    new QueryObserver(client, {
      queryKey: ['knowledge', 'query', 'inbox'],
      queryFn: async () => {
        reads.knowledge++
        return title
      },
    }),
    new QueryObserver(client, {
      queryKey: ['todos', 'workbench'],
      queryFn: async () => {
        reads.tasks++
        return 'task'
      },
    }),
  ]
  const unsubscribes = observers.map((observer) => observer.subscribe(() => {}))
  try {
    await Promise.all(observers.map((observer) => observer.refetch()))
    const before = { ...reads }
    title = 'after external edit'
    await invalidateDomains(client, ['knowledge'])
    assert.equal(observers[0].getCurrentResult().data, title)
    assert.equal(observers[1].getCurrentResult().data, title)
    assert.equal(reads.tasks, before.tasks)
    assert.equal(reads.search, before.search + 1)
  } finally {
    unsubscribes.forEach((unsubscribe) => unsubscribe())
    client.clear()
  }
})

test('search overlays expand subscriptions only while their query is active', () => {
  const client = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } })
  const options = {
    queryKey: ['global-search', 'term', 'all', ''],
    queryFn: async () => [],
    enabled: false,
  }
  const observer = new QueryObserver(client, options)
  const unsubscribe = observer.subscribe(() => {})
  try {
    assert.deepEqual(subscriptionDomains(client, 'tasks'), ['jobs', 'presence', 'todos'])
    observer.setOptions({ ...options, enabled: true })
    assert.deepEqual(subscriptionDomains(client, 'tasks'), [
      'collection',
      'jobs',
      'knowledge',
      'memory',
      'presence',
      'sessions',
      'todos',
    ])
    observer.setOptions(options)
    assert.deepEqual(subscriptionDomains(client, 'tasks'), ['jobs', 'presence', 'todos'])
  } finally {
    unsubscribe()
    client.clear()
  }
})

test('job completion invalidates its effects while retaining conservative fallback', () => {
  assert.deepEqual(domainsForJob('quota.refresh'), ['usage'])
  assert.deepEqual(domainsForJob('todo.refine'), ['todos'])
  assert.ok(domainsForJob('collect.reprocess').includes('todos'))
  assert.ok(domainsForJob('session.sync').includes('sessions'))
  assert.ok(domainsForJob('future.kind').includes('knowledge'))
})
