import test from 'node:test'
import assert from 'node:assert/strict'
import { progressiveTasks } from '../src/task-progressive.ts'
import type { Todo } from '../src/types.ts'
test('limits completed tasks without hiding active tasks or changing order', () => {
  const rows: Todo[] = Array.from({ length: 20 }, (_, i) => ({
    id: `t${i}`,
    title: '任务',
    priority: 'P1',
    status: i % 2 ? 'open' : 'done',
  }))
  assert.equal(progressiveTasks(rows).length, 15)
  assert.equal(progressiveTasks(rows).filter((row) => row.status === 'done').length, 5)
  assert.equal(progressiveTasks(rows, 10).length, 20)
  assert.deepEqual(
    progressiveTasks(rows).filter((row) => row.status === 'open'),
    rows.filter((row) => row.status === 'open'),
  )
  assert.equal(rows.length, 20)
})
