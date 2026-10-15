import test from 'node:test'
import assert from 'node:assert/strict'
import { updateDocumentBatch } from '../src/features/knowledge/knowledge-batch.ts'
import type { KnowledgeDocument, KnowledgeDocumentRow } from '../src/features/knowledge/knowledge-types.ts'
const rows = ['one', 'two', 'three'].map((id) => ({
  document_id: id,
  title: id,
  collection: 'inbox',
  status: 'draft',
})) as KnowledgeDocumentRow[]
const doc = (id: string, status = 'draft'): KnowledgeDocument => ({
  etag: 'latest',
  editable: true,
  collection: 'inbox',
  content: 'fresh content',
  metadata: { id, title: id, status, producer: '', createdAt: '', updatedAt: '' },
})
test('batch uses fresh documents and continues after a conflict, reporting each result', async () => {
  const calls: string[] = [],
    progress: number[] = []
  const results = await updateDocumentBatch(
    rows,
    { status: 'archived' },
    {
      get: async (id) => {
        calls.push(`get:${id}`)
        return doc(id)
      },
      update: async (document, patch) => {
        assert.equal(document.etag, 'latest')
        assert.equal(document.content, 'fresh content')
        assert.equal(patch.status, 'archived')
        calls.push(`update:${document.metadata.id}`)
        if (document.metadata.id === 'two') throw new Error('version conflict')
        return document
      },
    },
    (count) => progress.push(count),
  )
  assert.deepEqual(calls, [
    'get:one',
    'update:one',
    'get:two',
    'update:two',
    'get:three',
    'update:three',
  ])
  assert.deepEqual(
    results.map((item) => item.outcome),
    ['updated', 'failed', 'updated'],
  )
  assert.equal(results[1].error, 'version conflict')
  assert.deepEqual(progress, [1, 2, 3])
})
test('unchanged documents are skipped, missing ones fail without update', async () => {
  let writes = 0
  const results = await updateDocumentBatch(
    rows.slice(0, 2),
    { status: 'archived', collection: 'inbox' },
    {
      get: async (id) => {
        if (id === 'two') throw new Error('not found')
        return doc(id, 'archived')
      },
      update: async (document) => {
        writes++
        return document
      },
    },
  )
  assert.equal(writes, 0)
  assert.deepEqual(
    results.map((item) => item.outcome),
    ['skipped', 'failed'],
  )
})
test('moving does not change status or other document metadata', async () => {
  await updateDocumentBatch(
    rows.slice(0, 1),
    { collection: 'reference' },
    {
      get: async (id) => doc(id, 'archived'),
      update: async (document, patch) => {
        assert.deepEqual(patch, { collection: 'reference' })
        assert.equal(document.metadata.status, 'archived')
        return document
      },
    },
  )
})
