import assert from 'node:assert/strict'
import test from 'node:test'
import { createDraftStore, createDraftSession, readStorageNotice } from '../src/drafts.ts'
import { applyMergeReview, buildMergeReview, freshDraft } from '../src/editor-state.ts'
import type { Draft, TodoDetail } from '../src/types.ts'

class MemoryStorage {
  values = new Map<string, string>()
  maximumLength = Infinity
  get length() {
    return this.values.size
  }
  key(index: number) {
    return [...this.values.keys()][index] ?? null
  }
  getItem(key: string) {
    return this.values.get(key) ?? null
  }
  setItem(key: string, value: string) {
    if (value.length > this.maximumLength)
      throw new DOMException('Storage quota exceeded', 'QuotaExceededError')
    this.values.set(key, value)
  }
  removeItem(key: string) {
    this.values.delete(key)
  }
}
function draft(title: string): Draft {
  return {
    title,
    description: '未提交内容',
    project: 'atm',
    priority: 'P2',
    etag: 'v1',
    operationID: 'same-create-operation',
    base: { title: '初始标题', description: '', project: 'atm', priority: 'P2' },
  }
}

test('editors in different tabs keep independent durable records for the same task', async () => {
  const storage = new MemoryStorage()
  const a = createDraftStore(storage, 'editor-a')
  const b = createDraftStore(storage, 'editor-b')
  await a.saveDraft('todo:t42', draft('窗口 A'))
  await b.saveDraft('todo:t42', draft('窗口 B'))
  assert.deepEqual(await a.getDraft('todo:t42'), draft('窗口 A'))
  assert.deepEqual(await b.getDraft('todo:t42'), draft('窗口 B'))
  assert.equal((await a.listDrafts('todo:t42')).records.length, 2)
  await a.removeDraftIfUnchanged('todo:t42', draft('窗口 A'))
  assert.deepEqual(await b.getDraft('todo:t42'), draft('窗口 B'))
})

test('closed-tab and browser restart recovery is explicit and preserves source, baseline, and idempotency identity', async () => {
  const storage = new MemoryStorage()
  const old = createDraftStore(storage, 'closed-tab')
  await old.saveDraft('new-todo', draft('关闭前内容'))
  const restarted = createDraftStore(storage, 'new-browser-document')
  assert.equal(await restarted.getDraft('new-todo'), undefined)
  const recoveries = await restarted.listDrafts('new-todo')
  assert.equal(recoveries.records.length, 1)
  const recovered = recoveries.records[0].draft
  assert.deepEqual(recovered, draft('关闭前内容'))
  await restarted.saveDraft('new-todo', recovered)
  await restarted.saveDraft('new-todo', { ...recovered, title: '恢复后修改' })
  assert.deepEqual(await old.getDraft('new-todo'), draft('关闭前内容'))
  assert.equal((await restarted.getDraft('new-todo'))?.operationID, 'same-create-operation')
  await restarted.removeDraftIfUnchanged('new-todo', { ...recovered, title: '恢复后修改' })
  assert.deepEqual(await old.getDraft('new-todo'), draft('关闭前内容'))
})

test('two tabs restoring the same creation draft cannot overwrite each other or alter create identity', async () => {
  const storage = new MemoryStorage()
  const source = createDraftStore(storage, 'source')
  await source.saveDraft('new-todo', draft('原稿'))
  const a = createDraftStore(storage, 'restored-a')
  const b = createDraftStore(storage, 'restored-b')
  const snapshot = (await source.listDrafts('new-todo')).records[0].draft
  await a.saveDraft('new-todo', { ...snapshot, title: 'A 的修改' })
  await b.saveDraft('new-todo', { ...snapshot, title: 'B 的修改' })
  await a.saveDraft('new-todo')
  assert.equal((await b.getDraft('new-todo'))?.title, 'B 的修改')
  assert.equal((await b.getDraft('new-todo'))?.operationID, snapshot.operationID)
  assert.equal((await source.getDraft('new-todo'))?.title, '原稿')
})

test('an old response cannot delete a later snapshot from its own editor', async () => {
  const store = createDraftStore(new MemoryStorage(), 'same-editor')
  const submitted = draft('提交时的内容')
  await store.saveDraft('todo:t42', submitted)
  const newer = { ...submitted, description: '响应前继续输入' }
  await store.saveDraft('todo:t42', newer)
  assert.equal(await store.removeDraftIfUnchanged('todo:t42', submitted), false)
  assert.deepEqual(await store.getDraft('todo:t42'), newer)
  assert.equal(await store.removeDraftIfUnchanged('todo:t42', newer), true)
  assert.equal(await store.removeDraftIfUnchanged('todo:t42', newer), true)
})

test('restored ETag and baseline detect remote conflicts instead of rebasing silently', async () => {
  const original: TodoDetail = {
    todo: {
      id: 't1',
      title: '原始标题',
      description: '原始说明',
      priority: 'P2',
      project: 'atm',
      created: '2026-09-03',
      status: 'open',
    },
    etag: 'v1',
  }
  const local = { ...freshDraft(original, '', 'persisted-operation'), title: '本地标题' }
  const storage = new MemoryStorage()
  await createDraftStore(storage, 'old').saveDraft('todo:t1', local)
  const recovered = (await createDraftStore(storage, 'new').listDrafts('todo:t1')).records[0].draft
  const latest = {
    ...original,
    todo: { ...original.todo, title: '远程标题', description: '远程说明' },
    etag: 'v2',
  }
  const review = buildMergeReview(recovered, latest)
  assert.equal(recovered.etag, 'v1')
  assert.throws(() => applyMergeReview(recovered, review, {}), /冲突字段/)
  const merged = applyMergeReview(recovered, review, { title: 'local' })
  assert.equal(merged.title, '本地标题')
  assert.equal(merged.description, '远程说明')
  assert.equal(merged.etag, 'v2')
  assert.equal(merged.operationID, 'persisted-operation')
})

test('quota and deletion failures preserve the last persisted draft and reject', async () => {
  const storage = new MemoryStorage()
  const store = createDraftStore(storage, 'editor')
  const previous = draft('已保存')
  await store.saveDraft('todo:t42', previous)
  storage.maximumLength = 500
  await assert.rejects(
    store.saveDraft('todo:t42', { ...previous, description: '新'.repeat(1000) }),
    { name: 'QuotaExceededError' },
  )
  assert.deepEqual(await store.getDraft('todo:t42'), previous)
  const remove = storage.removeItem
  storage.removeItem = () => {
    throw new Error('Unable to remove')
  }
  await assert.rejects(store.removeDraftIfUnchanged('todo:t42', previous), /Unable to remove/)
  assert.deepEqual(await store.getDraft('todo:t42'), previous)
  storage.removeItem = remove
})

test('damaged records are reported and retained without hiding valid recovery records', async () => {
  const storage = new MemoryStorage()
  const invalid = 'atm-workspace:draft:v2:todo%3At42:damaged'
  storage.values.set(invalid, '{')
  const store = createDraftStore(storage, 'valid')
  await store.saveDraft('todo:t42', draft('有效草稿'))
  const listing = await store.listDrafts('todo:t42')
  assert.equal(listing.damaged, 1)
  assert.equal(listing.records.length, 1)
  assert.equal(storage.getItem(invalid), '{')
  await assert.rejects(createDraftStore(storage, 'damaged').getDraft('todo:t42'))
})

test('unavailable persistent storage rejects public APIs rather than claiming a saved draft', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  const error = new DOMException('Storage disabled', 'SecurityError')
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    get() {
      throw error
    },
  })
  try {
    const session = createDraftSession('test')
    await assert.rejects(session.listDrafts('todo:t42'), error)
    await assert.rejects(session.getDraft('todo:t42'), error)
    await assert.rejects(session.saveDraft('todo:t42', draft('保留在编辑器')), error)
    await assert.rejects(session.removeDraftIfUnchanged('todo:t42', draft('保留在编辑器')), error)
  } finally {
    if (descriptor) Object.defineProperty(globalThis, 'localStorage', descriptor)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})

test('recovery is sorted by last update and scoped to the requested task', async () => {
  const storage = new MemoryStorage()
  await createDraftStore(storage, 'a', () => '2026-09-03T01:00:00Z').saveDraft(
    'todo:t1',
    draft('早'),
  )
  await createDraftStore(storage, 'b', () => '2026-09-03T02:00:00Z').saveDraft(
    'todo:t1',
    draft('晚'),
  )
  await createDraftStore(storage, 'c').saveDraft('todo:t2', draft('其他任务'))
  assert.deepEqual(
    (await createDraftStore(storage, 'reader').listDrafts('todo:t1')).records.map(
      (record) => record.draft.title,
    ),
    ['晚', '早'],
  )
  assert.match(readStorageNotice(), /关闭标签页或重启后仍可/)
})
