import test from 'node:test'
import assert from 'node:assert/strict'
import { applyTaskBatch } from '../src/task-batch.ts'
test('batch edits use preview versions, report partial failures and never retry writes', async () => {
  const calls = [],
    progress = []
  const snapshots = [1, 2, 3].map((n) => ({
    todo: { id: `t${n}`, title: `Task ${n}` },
    etag: `v${n}`,
  }))
  const result = await applyTaskBatch(
    snapshots,
    'project',
    ' atm ',
    async ({ method, input }) => {
      calls.push({ method, input })
      if (input.todo_id === 't2') throw new Error('version conflict')
    },
    (item) => progress.push(item),
  )
  assert.equal(calls.length, 3)
  assert.deepEqual(calls[0], {
    method: 'todo.update',
    input: { todo_id: 't1', expected_etag: 'v1', project: 'atm' },
  })
  assert.deepEqual(
    result.map((r) => r.ok),
    [true, false, true],
  )
  assert.equal(result[1].error, 'version conflict')
  assert.deepEqual(progress, result)
})
test('archive uses the retention action, never completion', async () => {
  const calls = []
  await applyTaskBatch(
    [{ todo: { id: 't1', title: 'A' }, etag: 'v1' }],
    'archive',
    '',
    async ({ method, input }) => {
      calls.push({ method, input })
    },
    () => {},
  )
  assert.deepEqual(calls, [{ method: 'todo.archive', input: { todo_id: 't1' } }])
})

test('undo uses each saved version and restores its original field only', async () => {
  const { undoTaskBatch } = await import('../src/task-batch.ts')
  const saved = await applyTaskBatch(
    [{ todo: { id: 't1', title: 'A', priority: 'P2' }, etag: 'before' }],
    'priority',
    'P0',
    async () => ({ etag: 'after' }),
    () => {},
  )
  const writes = []
  const undone = await undoTaskBatch(saved, async (request) => {
    writes.push(request)
  })
  assert.deepEqual(writes, [
    { method: 'todo.update', input: { todo_id: 't1', expected_etag: 'after', priority: 'P2' } },
  ])
  assert.equal(undone[0].ok, true)
})

test('undo preserves conflicts for review and never retries or overwrites newer data', async () => {
  const { undoTaskBatch } = await import('../src/task-batch.ts')
  let count = 0
  const result = await undoTaskBatch(
    [{ id: 't1', title: 'A', ok: true, undo: { operation: 'project', value: '', etag: 'v2' } }],
    async () => {
      count++
      throw new Error('version conflict')
    },
  )
  assert.equal(count, 1)
  assert.equal(result[0].ok, false)
  assert.equal(result[0].undo.etag, 'v2')
})
