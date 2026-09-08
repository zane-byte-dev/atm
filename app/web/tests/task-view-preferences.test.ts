import test from 'node:test'
import assert from 'node:assert/strict'
import {
  builtinViews,
  moveView,
  orderViews,
  parseViewPreferences,
} from '../src/task-view-preferences.ts'

test('damaged preferences recover and all remains visible', () => {
  assert.deepEqual(parseViewPreferences('{broken'), { order: [], hidden: [] })
  assert.deepEqual(parseViewPreferences('null'), { order: [], hidden: [] })
  assert.deepEqual(
    parseViewPreferences(
      JSON.stringify({ order: ['a', 'a', 2], hidden: ['builtin:all', 'b', 'b', null] }),
    ),
    { order: ['a'], hidden: ['b'] },
  )
})
test('new and legacy views remain accessible after order changes and deleted IDs are ignored', () => {
  const entries = [...builtinViews, { id: 'old', name: 'Legacy' }, { id: 'new', name: 'New' }]
  assert.deepEqual(
    orderViews(entries, ['old', 'deleted', 'builtin:review']).map((view) => view.id),
    ['old', 'builtin:review', 'builtin:unfinished', 'builtin:waiting', 'builtin:all', 'new'],
  )
})
test('reordering supports movement both ways without dropping entries', () => {
  assert.deepEqual(moveView(['a', 'b', 'c'], 'a', 'c'), ['b', 'c', 'a'])
  assert.deepEqual(moveView(['a', 'b', 'c'], 'c', 'a'), ['c', 'a', 'b'])
  assert.deepEqual(moveView(['a', 'b', 'c'], 'missing', 'a'), ['a', 'b', 'c'])
})
