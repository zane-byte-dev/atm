import test from 'node:test'
import assert from 'node:assert/strict'
import { waitingReason, taskActivity, taskActivityLabel } from '../src/features/tasks/task-attention.ts'
const todo = {
  id: 't1',
  title: 'Test',
  status: 'in_progress',
  priority: 'P2',
  created: '2026-09-06',
}
const status = {
  agent_hooks: true,
  bindings: [{ state: 'bound', binding: { todo_id: 't1', session_id: 's1' } }],
  presence: {
    sessions: [{ id: 'p1', session_id: 's1', state: 'active', updated_at: '2026-09-06T01:00:00Z' }],
  },
}
test('waiting remains a facet of in-progress and includes review time', () => {
  assert.equal(waitingReason({ ...todo, wake_condition: '等待发布' }), '等待发布')
  assert.equal(waitingReason({ ...todo, review_at: '2026-09-07' }), '复查时间 2026-09-07')
  assert.equal(waitingReason({ ...todo, status: 'done', wake_condition: '旧条件' }), '')
})
test('a binding never implies activity; only explicit matching observed IDs do', () => {
  assert.equal(taskActivity(todo, status).active, 1)
  assert.equal(
    taskActivity(todo, {
      ...status,
      presence: { sessions: [{ ...status.presence.sessions[0], state: 'busy' }] },
    }).active,
    1,
  )
  assert.equal(taskActivity(todo, { ...status, agent_hooks: false }).active, 0)
  assert.equal(taskActivity(todo, { ...status, bindings: [] }).active, 0)
  assert.equal(taskActivity({ ...todo, status: 'done' }, status).bound, 0)
  assert.equal(
    taskActivity(todo, {
      ...status,
      bindings: [{ state: 'todo_not_in_progress', binding: { todo_id: 't1', session_id: 's1' } }],
    }).active,
    0,
  )
  const value = taskActivity(todo, { ...status, presence: { sessions: [] } })
  assert.equal(taskActivityLabel(value), '1 个会话已关联')
})
