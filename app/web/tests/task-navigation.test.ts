import assert from 'node:assert/strict'
import test from 'node:test'

import { consumeNewTaskRequest, readTaskLayout, updateTaskLayout } from '../src/task-navigation.ts'

test('new task route opens once and preserves task filters', () => {
  const original = new URLSearchParams('status=review&project=atm&q=menu&new=1')

  const request = consumeNewTaskRequest(original, true)

  assert.ok(request)
  assert.equal(request.open, true)
  assert.equal(request.params.toString(), 'status=review&project=atm&q=menu')
  assert.equal(original.get('new'), '1', 'helper must not mutate router-owned params')
  assert.equal(consumeNewTaskRequest(request.params, true), undefined)
})

test('read-only workspace consumes the signal without opening the dialog', () => {
  const request = consumeNewTaskRequest(new URLSearchParams('new=1&project=atm'), false)

  assert.ok(request)
  assert.equal(request.open, false)
  assert.equal(request.params.toString(), 'project=atm')
})

test('only the explicit new=1 signal is consumed', () => {
  for (const value of ['', '0', 'true', 'yes']) {
    const params = new URLSearchParams(value ? `new=${value}&status=open` : 'status=open')
    assert.equal(consumeNewTaskRequest(params, true), undefined)
  }
})

test('task layout defaults to list and only accepts the kanban value', () => {
  assert.equal(readTaskLayout(new URLSearchParams()), 'list')
  assert.equal(readTaskLayout(new URLSearchParams('layout=list')), 'list')
  assert.equal(readTaskLayout(new URLSearchParams('layout=unknown')), 'list')
  assert.equal(readTaskLayout(new URLSearchParams('layout=kanban')), 'kanban')
})

test('switching task layout preserves shared filters and clears lane filters for kanban', () => {
  const original = new URLSearchParams('status=review&project=atm&q=menu&new=1')

  const kanban = updateTaskLayout(original, 'kanban')
  assert.equal(kanban.toString(), 'project=atm&q=menu&new=1&layout=kanban')
  assert.equal(original.toString(), 'status=review&project=atm&q=menu&new=1')

  const list = updateTaskLayout(kanban, 'list')
  assert.equal(list.toString(), 'project=atm&q=menu&new=1')
})
