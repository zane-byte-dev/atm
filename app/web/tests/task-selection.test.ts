import test from 'node:test'
import assert from 'node:assert/strict'
import { updateTaskGroupSelection } from '../src/task-selection.ts'

test('selecting a partially selected group completes it and preserves other groups', () => {
  assert.deepEqual(updateTaskGroupSelection(['a', 'other'], new Set(['a', 'b']), true, false), [
    'a',
    'other',
    'b',
  ])
  assert.deepEqual(
    updateTaskGroupSelection(['a', 'other', 'b'], new Set(['a', 'b']), false, false),
    ['other'],
  )
})
test('group changes under global selection maintain exclusions from other groups', () => {
  assert.deepEqual(updateTaskGroupSelection(['other'], new Set(['a', 'b']), false, true), [
    'other',
    'a',
    'b',
  ])
  assert.deepEqual(updateTaskGroupSelection(['other', 'a'], new Set(['a', 'b']), true, true), [
    'other',
  ])
})
test('group selection includes members beyond the first page without duplicates', () => {
  const ids = new Set(Array.from({ length: 205 }, (_, n) => `t${n}`))
  const selected = updateTaskGroupSelection(['t0', 'other'], ids, true, false)
  assert.equal(selected.length, 206)
  assert.equal(new Set(selected).size, 206)
  assert.deepEqual(updateTaskGroupSelection(selected, ids, false, false), ['other'])
})
