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
    async (method, input) => {
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
    async (method, input) => {
      calls.push({ method, input })
    },
    () => {},
  )
  assert.deepEqual(calls, [{ method: 'todo.archive', input: { todo_id: 't1' } }])
})
