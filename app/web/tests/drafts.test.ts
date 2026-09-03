import assert from 'node:assert/strict'
import test from 'node:test'
import {
  createDraftStore,
  getDraft,
  readStorageNotice,
  removeDraftIfUnchanged,
  saveDraft,
} from '../src/drafts.ts'
import type { Draft } from '../src/types.ts'

class MemoryStorage {
  values = new Map<string, string>()
  maximumLength = Infinity

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    if (value.length > this.maximumLength) {
      throw new DOMException('Storage quota exceeded', 'QuotaExceededError')
    }
    this.values.set(key, value)
  }

  removeItem(key: string): void {
    this.values.delete(key)
  }

  copy(): MemoryStorage {
    const storage = new MemoryStorage()
    storage.values = new Map(this.values)
    return storage
  }
}

function draft(title: string): Draft {
  return {
    title,
    description: '未提交内容',
    project: 'atm',
    priority: 'P2',
    etag: 'etag-1',
    operationID: title,
  }
}

test('the same object key belongs to each tab independently', async () => {
  const a = createDraftStore(new MemoryStorage())
  const b = createDraftStore(new MemoryStorage())
  await a.saveDraft('todo:t42', draft('标签页 A'))
  assert.equal(await b.getDraft('todo:t42'), undefined)
  await b.saveDraft('todo:t42', draft('标签页 B'))

  assert.deepEqual(await a.getDraft('todo:t42'), draft('标签页 A'))
  assert.deepEqual(await b.getDraft('todo:t42'), draft('标签页 B'))
  await a.saveDraft('todo:t42')
  assert.equal(await a.getDraft('todo:t42'), undefined)
  assert.deepEqual(await b.getDraft('todo:t42'), draft('标签页 B'))
})

test('reopening the editor or refreshing with the same tab storage restores its draft', async () => {
  const storage = new MemoryStorage()
  const original: Draft = {
    ...draft('刷新后恢复'),
    base: { title: '编辑前标题', description: '编辑前描述', project: 'atm', priority: 'P1' },
  }
  await createDraftStore(storage).saveDraft('todo:t42', original)

  const reopenedEditor = createDraftStore(storage)
  assert.deepEqual(await reopenedEditor.getDraft('todo:t42'), original)
  const refreshedPage = createDraftStore(storage)
  assert.deepEqual(await refreshedPage.getDraft('todo:t42'), original)
})

test('duplicating a tab copies its initial draft without sharing later edits or deletions', async () => {
  const storageA = new MemoryStorage()
  const a = createDraftStore(storageA)
  await a.saveDraft('todo:t42', draft('复制时的草稿'))
  const b = createDraftStore(storageA.copy())
  assert.deepEqual(await b.getDraft('todo:t42'), draft('复制时的草稿'))

  await b.saveDraft('todo:t42')
  assert.equal(await b.getDraft('todo:t42'), undefined)
  assert.deepEqual(await a.getDraft('todo:t42'), draft('复制时的草稿'))

  await b.saveDraft('todo:t42', draft('复制页的新内容'))
  await a.saveDraft('todo:t42', draft('原页的新内容'))
  assert.deepEqual(await b.getDraft('todo:t42'), draft('复制页的新内容'))
  assert.deepEqual(await a.getDraft('todo:t42'), draft('原页的新内容'))
})

test('storage read, write and deletion failures reject instead of reporting success', async () => {
  const error = new DOMException('Storage access denied', 'SecurityError')
  const unavailable = () => {
    throw error
  }
  const store = createDraftStore({
    getItem: unavailable,
    setItem: unavailable,
    removeItem: unavailable,
  })
  await assert.rejects(store.getDraft('todo:t42'), error)
  await assert.rejects(store.saveDraft('todo:t42', draft('不能保存')), error)
  await assert.rejects(store.saveDraft('todo:t42'), error)
  await assert.rejects(store.removeDraftIfUnchanged('todo:t42', draft('不能清理')), error)
})

test('denied access to browser sessionStorage rejects the public Promise APIs', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage')
  const error = new DOMException('Session storage is disabled', 'SecurityError')
  Object.defineProperty(globalThis, 'sessionStorage', {
    configurable: true,
    get() {
      throw error
    },
  })
  try {
    await assert.rejects(getDraft('todo:t42'), error)
    await assert.rejects(saveDraft('todo:t42', draft('不能保存')), error)
    await assert.rejects(saveDraft('todo:t42'), error)
    await assert.rejects(removeDraftIfUnchanged('todo:t42', draft('不能清理')), error)
  } finally {
    if (descriptor) Object.defineProperty(globalThis, 'sessionStorage', descriptor)
    else Reflect.deleteProperty(globalThis, 'sessionStorage')
  }
})

test('a quota error preserves the previously saved draft', async () => {
  const storage = new MemoryStorage()
  const store = createDraftStore(storage)
  const previous = draft('已保存内容')
  await store.saveDraft('todo:t42', previous)
  storage.maximumLength = 500

  await assert.rejects(
    store.saveDraft('todo:t42', { ...previous, description: '新'.repeat(1000) }),
    { name: 'QuotaExceededError' },
  )
  assert.deepEqual(await store.getDraft('todo:t42'), previous)
})

test('a failed deletion preserves the draft and reports the failure', async () => {
  const storage = new MemoryStorage()
  const previous = draft('待删除的草稿')
  await createDraftStore(storage).saveDraft('todo:t42', previous)
  const store = createDraftStore({
    getItem: (key) => storage.getItem(key),
    setItem: (key, value) => storage.setItem(key, value),
    removeItem: () => {
      throw new Error('Unable to remove draft')
    },
  })

  await assert.rejects(store.saveDraft('todo:t42'), /Unable to remove draft/)
  await assert.rejects(store.removeDraftIfUnchanged('todo:t42', previous), /Unable to remove draft/)
  assert.deepEqual(await store.getDraft('todo:t42'), previous)
})

test('an earlier save response cannot remove a newer editor draft in the same tab', async () => {
  const storage = new MemoryStorage()
  const firstEditor = createDraftStore(storage)
  const submitted = draft('已提交，响应尚未返回')
  await firstEditor.saveDraft('todo:t42', submitted)

  const reopenedEditor = createDraftStore(storage)
  const newer = { ...submitted, description: '重新打开编辑器后输入的新内容' }
  await reopenedEditor.saveDraft('todo:t42', newer)

  assert.equal(await firstEditor.removeDraftIfUnchanged('todo:t42', submitted), false)
  assert.deepEqual(await reopenedEditor.getDraft('todo:t42'), newer)
})

test('a matching submitted snapshot can be removed and an absent draft is already clear', async () => {
  const store = createDraftStore(new MemoryStorage())
  const submitted = draft('已提交快照')
  await store.saveDraft('todo:t42', submitted)

  assert.equal(await store.removeDraftIfUnchanged('todo:t42', { ...submitted }), true)
  assert.equal(await store.getDraft('todo:t42'), undefined)
  assert.equal(await store.removeDraftIfUnchanged('todo:t42', submitted), true)
})

test('malformed or incomplete saved drafts reject without overwriting their source', async () => {
  const invalidDrafts = [
    '{',
    'null',
    '{}',
    JSON.stringify({ ...draft('缺少操作 ID'), operationID: null }),
  ]
  for (const base of [
    null,
    {},
    { title: '损坏的基线', description: '', project: '', priority: 2 },
  ]) {
    invalidDrafts.push(JSON.stringify({ ...draft('基线损坏'), base }))
  }
  for (const invalid of invalidDrafts) {
    const storage = new MemoryStorage()
    storage.values.set('atm-workspace:tab-draft:v1:todo:t42', invalid)
    await assert.rejects(createDraftStore(storage).getDraft('todo:t42'))
    assert.equal(storage.values.get('atm-workspace:tab-draft:v1:todo:t42'), invalid)
  }
})

test('legacy origin-wide drafts are neither imported nor removed', async () => {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, 'indexedDB')
  Object.defineProperty(globalThis, 'indexedDB', {
    configurable: true,
    get() {
      throw new Error('Legacy IndexedDB must remain untouched')
    },
  })
  try {
    const store = createDraftStore(new MemoryStorage())
    assert.equal(await store.getDraft('todo:t42'), undefined)
    await store.saveDraft('todo:t42', draft('当前标签页'))
    await store.saveDraft('todo:t42')
    assert.match(readStorageNotice(), /旧版本的共享草稿不会自动恢复，原数据仍保留/)
  } finally {
    if (descriptor) Object.defineProperty(globalThis, 'indexedDB', descriptor)
    else Reflect.deleteProperty(globalThis, 'indexedDB')
  }
})
