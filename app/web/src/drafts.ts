import type { Draft } from './types'

type DraftStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

const keyPrefix = 'atm-workspace:tab-draft:v1:'
const storageNotice =
  '草稿保存在当前标签页，刷新页面或关闭编辑器后可恢复；关闭标签页后可能丢失。旧版本的共享草稿不会自动恢复，原数据仍保留。'

export function readStorageNotice(): string {
  return storageNotice
}

function isDraft(value: unknown): value is Draft {
  if (!value || typeof value !== 'object') return false
  const fields = value as Record<string, unknown>
  if (
    !['title', 'description', 'project', 'priority', 'etag', 'operationID'].every(
      (key) => typeof fields[key] === 'string',
    )
  )
    return false
  if (fields.base === undefined) return true
  if (!fields.base || typeof fields.base !== 'object') return false
  const base = fields.base as Record<string, unknown>
  return ['title', 'description', 'project', 'priority'].every(
    (key) => typeof base[key] === 'string',
  )
}

// sessionStorage belongs to the browsing context. A duplicated tab may start
// with a copy, but later saves and removals never change the original tab.
// Do not import the old origin-wide IndexedDB records: they have no tab owner.
export function createDraftStore(storage: DraftStorage) {
  return {
    async getDraft(key: string): Promise<Draft | undefined> {
      const value = storage.getItem(keyPrefix + key)
      if (value === null) return undefined
      const draft: unknown = JSON.parse(value)
      if (!isDraft(draft)) throw new Error('当前标签页的草稿数据格式无效，未覆盖原数据')
      return draft
    },
    async saveDraft(key: string, value?: Draft): Promise<void> {
      if (value === undefined) storage.removeItem(keyPrefix + key)
      else storage.setItem(keyPrefix + key, JSON.stringify(value))
    },
    async removeDraftIfUnchanged(key: string, expected: Draft): Promise<boolean> {
      const storageKey = keyPrefix + key
      const current = storage.getItem(storageKey)
      if (current === null) return true
      if (current !== JSON.stringify(expected)) return false
      // Keep the comparison and deletion synchronous so another editor in the
      // same tab cannot save a newer draft between them.
      storage.removeItem(storageKey)
      return true
    },
  }
}

function currentDraftStore() {
  // Reading sessionStorage itself can throw when browser storage is disabled.
  // Resolve it inside the async public methods so callers receive a rejection.
  const storage = globalThis.sessionStorage
  if (!storage) throw new Error('当前标签页无法访问草稿存储')
  return createDraftStore(storage)
}

export async function getDraft(key: string): Promise<Draft | undefined> {
  return currentDraftStore().getDraft(key)
}

export async function saveDraft(key: string, value?: Draft): Promise<void> {
  return currentDraftStore().saveDraft(key, value)
}

export async function removeDraftIfUnchanged(key: string, expected: Draft): Promise<boolean> {
  return currentDraftStore().removeDraftIfUnchanged(key, expected)
}
