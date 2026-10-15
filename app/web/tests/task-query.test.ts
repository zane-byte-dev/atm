import test from 'node:test'
import assert from 'node:assert/strict'
import { collectTaskPages, mergeTaskGroupRows, selectedTaskRows, taskSelectionScope, taskWorkbenchInput } from '../src/features/tasks/task-query.ts'
import { groupTasks, matchingTasks } from '../src/features/tasks/task-view-model.ts'
import type { Todo } from '../src/types.ts'

test('workbench sends complete filters and derives groups from every display mode', () => {
  const params = new URLSearchParams(
    'layout=swimlane&focus=waiting&status=in_progress,review&priority=P0,P1&project=atm&creator=codex&tag=UI&from=2026-01-01&to=2026-09-08&q=needle&sort=updated&direction=desc',
  )
  assert.deepEqual(taskWorkbenchInput(params), {
    workbench: true,
    focus: 'waiting',
    status: 'in_progress,review',
    priority: 'P0,P1',
    project: 'atm',
    creator: 'codex',
    tag: 'UI',
    from: '2026-01-01',
    to: '2026-09-08',
    query: 'needle',
    sort: 'updated',
    direction: 'desc',
    group_by: 'project_status',
  })
  assert.equal(taskWorkbenchInput(new URLSearchParams()).focus, 'unfinished')
  for (const [query, expected] of [
    ['layout=kanban', 'status'],
    ['layout=list', 'status'],
    ['layout=list&group=none', 'none'],
    ['layout=table', 'none'],
    ['layout=table&tableGroup=project', 'project'],
  ])
    assert.equal(taskWorkbenchInput(new URLSearchParams(query)).group_by, expected)
})

test('selection survives presentation changes and resets only when filter scope changes', () => {
  const base = new URLSearchParams('focus=all&project=atm')
  const display = new URLSearchParams(
    'focus=all&project=atm&layout=table&tableGroup=project&sort=title&direction=desc&fields=id&density=comfortable',
  )
  assert.equal(taskSelectionScope(base), taskSelectionScope(display))
  display.set('status', 'review')
  assert.notEqual(taskSelectionScope(base), taskSelectionScope(display))
})

test('explicit exports collect every page and stop on the server has_more boundary', async () => {
  const offsets: number[] = []
  const result = await collectTaskPages(async (offset) => {
    offsets.push(offset)
    return {
      items: [{ id: `t${offset}` } as Todo],
      total: 3,
      offset,
      limit: 1,
      has_more: offset < 2,
    }
  })
  assert.deepEqual(offsets, [0, 1, 2])
  assert.deepEqual(
    result.map((item) => item.id),
    ['t0', 't1', 't2'],
  )
  await assert.rejects(
    collectTaskPages(async () => {
      throw new Error('unavailable')
    }),
    /unavailable/,
  )
})

test('matching counts retain input order and grouping keeps arbitrary project names distinct', () => {
  const rows = [
    { id: 't2', status: 'open', project: '', priority: 'P2' },
    { id: 't1', status: 'done', project: '无项目', priority: 'P0' },
    { id: 't3', status: 'open', project: 'a:b', priority: 'P1' },
  ] as Todo[]
  assert.deepEqual(matchingTasks(rows, new URLSearchParams('focus=all&sort=priority')), rows)
  const groups = groupTasks(rows)
  assert.deepEqual(
    groups.byProject.get('')?.map((item) => item.id),
    ['t2'],
  )
  assert.deepEqual(
    groups.byProject.get('无项目')?.map((item) => item.id),
    ['t1'],
  )
  assert.deepEqual(
    groups.byProjectStatus.get(JSON.stringify(['a:b', 'open']))?.map((item) => item.id),
    ['t3'],
  )
})

test('partition transitions discard disabled caches and retain the newest state across overlapping refreshes', () => {
  const oldDone = { id: 't1', status: 'done', updated_at: 100 } as Todo
  const reopened = { ...oldDone, status: 'in_progress' } as Todo
  assert.deepEqual(
    mergeTaskGroupRows([
      { partition: 'active', total: 1, items: [reopened], fetchedAt: 200 },
      { partition: 'done', total: 0, items: [oldDone], fetchedAt: 100 },
    ]),
    [reopened],
  )
  assert.deepEqual(
    mergeTaskGroupRows([
      { partition: 'active', total: 0, items: [reopened], fetchedAt: 200 },
      { partition: 'done', total: 1, items: [{ ...oldDone, updated_at: 101 }], fetchedAt: 300 },
    ]),
    [{ ...oldDone, updated_at: 101 }],
  )
  assert.deepEqual(
    mergeTaskGroupRows([
      { partition: 'active', total: 2, items: [reopened], fetchedAt: 200 },
      { partition: 'done', total: 2, items: [oldDone], fetchedAt: 100 },
    ]),
    [reopened],
  )
})

test('removed exclusions never reduce the actual batch selection limit', () => {
  const rows = Array.from({ length: 101 }, (_, index) => ({ id: `t${index + 1}` }) as Todo)
  assert.throws(() => selectedTaskRows(rows, new Set(['t999']), 100), /最多 100/)
  assert.equal(selectedTaskRows(rows, new Set(['t1']), 100).length, 100)
})
