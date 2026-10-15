import test from 'node:test'
import assert from 'node:assert/strict'
import { InfiniteQueryObserver, QueryClient } from '@tanstack/react-query'
import { loadedKnowledgeDocuments, nextKnowledgeCursor } from '../src/features/knowledge/knowledge-pagination.ts'
import type { KnowledgeQueryResult } from '../src/features/knowledge/knowledge-types.ts'

const documents = () =>
  Array.from({ length: 205 }, (_, index) => ({
    document_id: `document:${index}`,
    title: `Document ${index}`,
    collection: 'notes',
    status: 'active',
  }))

test('document pagination loads 205 documents only when another page is requested', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const source = documents()
  const requests: number[] = []
  const observer = new InfiniteQueryObserver(client, {
    queryKey: ['knowledge', 'query', 'notes'],
    initialPageParam: '',
    queryFn: async ({ pageParam }): Promise<KnowledgeQueryResult> => {
      const start = Number(pageParam || 0),
        end = Math.min(start + 50, source.length)
      requests.push(start)
      return {
        documents: source.slice(start, end),
        total: source.length,
        has_more: end < source.length,
        next_cursor: end < source.length ? String(end) : undefined,
      }
    },
    getNextPageParam: nextKnowledgeCursor,
  })
  const unsubscribe = observer.subscribe(() => {})
  try {
    await observer.refetch()
    assert.deepEqual(requests, [0])
    assert.equal(loadedKnowledgeDocuments(observer.getCurrentResult().data!.pages).length, 50)
    while (observer.getCurrentResult().hasNextPage) await observer.fetchNextPage()
    assert.deepEqual(requests, [0, 50, 100, 150, 200])
    assert.deepEqual(loadedKnowledgeDocuments(observer.getCurrentResult().data!.pages), source)
  } finally {
    unsubscribe()
    client.clear()
  }
})

test('a move/archive refresh replaces the loaded sequence using fresh cursors without duplicate or missing rows', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  let source = documents(),
    revision = 1
  const queryKey = ['knowledge', 'query', 'notes']
  const observer = new InfiniteQueryObserver(client, {
    queryKey,
    initialPageParam: '',
    queryFn: async ({ pageParam }): Promise<KnowledgeQueryResult> => {
      const [version, offset] = pageParam ? pageParam.split(':').map(Number) : [revision, 0]
      assert.equal(version, revision, 'refresh must derive later cursors from the new first page')
      const end = Math.min(offset + 50, source.length)
      return {
        documents: source.slice(offset, end),
        total: source.length,
        has_more: end < source.length,
        next_cursor: end < source.length ? `${revision}:${end}` : undefined,
      }
    },
    getNextPageParam: nextKnowledgeCursor,
  })
  const unsubscribe = observer.subscribe(() => {})
  try {
    await observer.refetch()
    await observer.fetchNextPage()
    const before = loadedKnowledgeDocuments(observer.getCurrentResult().data!.pages)
    assert.equal(before.length, 100)
    source = source.filter(
      (row) => ![before[0].document_id, before[70].document_id].includes(row.document_id),
    )
    revision++
    await client.invalidateQueries({ queryKey })
    const refreshed = loadedKnowledgeDocuments(observer.getCurrentResult().data!.pages)
    assert.deepEqual(refreshed, source.slice(0, 100))
    assert.equal(observer.getCurrentResult().data!.pages[0].total, 203)
    while (observer.getCurrentResult().hasNextPage) await observer.fetchNextPage()
    assert.deepEqual(loadedKnowledgeDocuments(observer.getCurrentResult().data!.pages), source)
  } finally {
    unsubscribe()
    client.clear()
  }
})

test('selection candidates contain only loaded pages and repeated delivery keeps document identity unique', () => {
  const source = documents()
  const page = {
    documents: source.slice(0, 50),
    total: source.length,
    has_more: true,
    next_cursor: '50',
  }
  const candidates = loadedKnowledgeDocuments([page, page])
  assert.equal(candidates.length, 50)
  assert.equal(
    candidates.some((row) => row.document_id === source[200].document_id),
    false,
  )
  assert.equal(nextKnowledgeCursor({ documents: [], total: 0, has_more: false }), undefined)
})
