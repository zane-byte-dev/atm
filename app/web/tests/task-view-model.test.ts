import test from 'node:test'
import assert from 'node:assert/strict'
import { filterTasks, parseViews, viewQuery, visibleFields } from '../src/features/tasks/task-view-model.ts'
const items = [
  {
    id: 't1',
    title: '验收',
    status: 'review',
    priority: 'P1',
    project: 'atm',
    creator: 'me',
    tags: ['ui'],
    created: '2026-09-01',
  },
  {
    id: 't2',
    title: '开发',
    status: 'in_progress',
    priority: 'P0',
    project: 'atm',
    creator: 'bot',
    tags: ['ui'],
    created: '2026-09-05',
  },
  {
    id: 't3',
    title: '完成',
    status: 'done',
    priority: 'P2',
    project: 'other',
    created: '2026-08-01',
  },
  {
    id: 't4',
    title: '归档',
    status: 'done',
    priority: 'P1',
    project: 'atm',
    created: '2026-09-01',
    archived: true,
  },
] as const
const run = (query: string) =>
  filterTasks(JSON.parse(JSON.stringify(items)), new URLSearchParams(query)).map((item) => item.id)
test('composes multi-status and metadata filters before sorting', () => {
  assert.deepEqual(
    run(
      'status=review,in_progress&priority=P0,P1&project=atm&tag=ui&from=2026-09-01&to=2026-09-05',
    ),
    ['t2', 't1'],
  )
  assert.deepEqual(run('creator=me&tag=ui'), ['t1'])
  assert.deepEqual(run('from=2026-09-05&to=2026-09-05'), ['t2'])
  assert.deepEqual(run('project=missing'), [])
})
test('archive is isolated and dates can sort in either direction', () => {
  assert.deepEqual(run('status=archived'), ['t4'])
  assert.deepEqual(run('sort=created&direction=desc'), ['t2', 't1', 't3'])
  assert.deepEqual(run('sort=created'), ['t3', 't1', 't2'])
})
test('saved views strip one-shot creation and selection while preserving empty properties', () => {
  const query = viewQuery(
    new URLSearchParams('new=1&view=old&layout=table&fields=&priority=P1&density=compact'),
  )
  assert.equal(new URLSearchParams(query).has('new'), false)
  assert.equal(new URLSearchParams(query).has('view'), false)
  assert.deepEqual(visibleFields(new URLSearchParams(query)), [''])
  assert.equal(parseViews(JSON.stringify([{ id: 'one', name: 'UI', query }]))[0].query, query)
  assert.deepEqual(parseViews('{broken'), [])
  assert.deepEqual(parseViews('[null,1,{}]'), [])
})

test('completed defaults to latest update, missing dates last, explicit sorting wins', () => {
  const rows = [
    { id: 't1', title: 'A', status: 'done' as const, priority: 'P0', updated_at: 10 },
    { id: 't2', title: 'B', status: 'done' as const, priority: 'P2', updated_at: 30 },
    { id: 't3', title: 'C', status: 'done' as const, priority: 'P1' },
  ]
  assert.deepEqual(
    filterTasks(rows, new URLSearchParams()).map((t) => t.id),
    ['t2', 't1', 't3'],
  )
  assert.deepEqual(
    filterTasks(rows, new URLSearchParams('sort=priority')).map((t) => t.id),
    ['t1', 't3', 't2'],
  )
})

test('attention views compose with project filters and never show completed waiting tasks', () => {
  const rows = [
    {
      id: 't1',
      title: 'Working',
      status: 'in_progress',
      project: 'atm',
      priority: 'P2',
      wake_condition: 'External',
    },
    { id: 't2', title: 'Review', status: 'review', project: 'atm', priority: 'P2' },
    {
      id: 't3',
      title: 'Done',
      status: 'done',
      project: 'atm',
      priority: 'P2',
      wake_condition: 'Old',
    },
    {
      id: 't4',
      title: 'Review later',
      status: 'in_progress',
      project: 'other',
      priority: 'P2',
      review_at: '2026-09-07',
    },
  ]
  const ids = (q) => filterTasks(rows, new URLSearchParams(q)).map((t) => t.id)
  assert.deepEqual(ids('focus=waiting&project=atm'), ['t1'])
  assert.deepEqual(ids('focus=waiting'), ['t1', 't4'])
  assert.deepEqual(ids('focus=review'), ['t2'])
  assert.deepEqual(ids('focus=unfinished&project=atm'), ['t1', 't2'])
})
