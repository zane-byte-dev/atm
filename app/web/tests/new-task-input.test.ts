import assert from 'node:assert/strict'
import test from 'node:test'
import { createNewTaskInputStore } from '../src/new-task-input.ts'
import type { Draft } from '../src/types.ts'
function setup() {
  const values = new Map<string, string>()
  const store = createNewTaskInputStore(() => ({
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value)
    },
    removeItem: (key) => {
      values.delete(key)
    },
  }))
  return { values, store }
}
const input: Draft = {
  title: '新任务',
  description: '说明',
  project: 'atm',
  priority: 'P1',
  etag: '',
  operationID: 'create-once',
}
test('remembers new-task inputs and request identity, clearing only after successful creation', async () => {
  const { store, values } = setup()
  assert.equal(await store.read(), undefined)
  assert.equal(values.size, 0)
  await store.save(input)
  assert.deepEqual(await store.read(), input)
  assert.equal(await store.clearIfUnchanged(input), true)
  assert.equal(await store.read(), undefined)
})
test('an earlier creation response preserves newer input from another editor', async () => {
  const { store } = setup()
  await store.save({ ...input, title: '后来填写的内容' })
  assert.equal(await store.clearIfUnchanged(input), false)
  assert.equal((await store.read())?.title, '后来填写的内容')
})
test('reads existing new-task information without touching existing-task or damaged legacy records', async () => {
  const { store, values } = setup()
  const oldKey = 'atm-workspace:draft:v2:new-todo:damaged'
  const editKey = 'atm-workspace:draft:v3:todo%3At1'
  values.set(oldKey, '{broken')
  values.set(editKey, 'old editing content')
  values.set(
    'atm-workspace:draft:v3:new-todo',
    JSON.stringify({ id: 'current', key: 'new-todo', draft: input }),
  )
  assert.deepEqual(await store.read(), input)
  await store.clearIfUnchanged(input)
  assert.deepEqual(
    [...values],
    [
      [oldKey, '{broken'],
      [editKey, 'old editing content'],
    ],
  )
})
test('storage failures and malformed input surface without overwriting remembered information', async () => {
  const { store, values } = setup()
  values.set('atm-workspace:draft:v3:new-todo', '{broken')
  await assert.rejects(store.read())
  assert.equal(values.get('atm-workspace:draft:v3:new-todo'), '{broken')
  const denied = createNewTaskInputStore(() => {
    throw new Error('storage denied')
  })
  await assert.rejects(denied.read())
  await assert.rejects(denied.save(input))
  await assert.rejects(denied.clearIfUnchanged(input))
})
