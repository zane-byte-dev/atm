import type { Draft } from './types'

type DraftStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem' | 'key' | 'length'>
export type DraftRecord = { id: string; key: string; updatedAt: string; draft: Draft }
export type DraftListing = { records: DraftRecord[]; damaged: number }
const keyPrefix = 'atm-workspace:draft:v2:'
const legacyPrefix = 'atm-workspace:tab-draft:v1:'

export function readStorageNotice(): string {
  return '草稿保存在当前浏览器，关闭标签页或重启后仍可从恢复列表找回。每个编辑器独立保存；恢复会创建副本，不覆盖其他标签页。清除浏览器网站数据会删除草稿。旧版本的共享草稿不会自动恢复，原数据仍保留。'
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
  if (!fields.operationID) return false
  if (fields.base === undefined) return true
  if (!fields.base || typeof fields.base !== 'object') return false
  const base = fields.base as Record<string, unknown>
  return ['title', 'description', 'project', 'priority'].every(
    (key) => typeof base[key] === 'string',
  )
}

function parseRecord(value: string): DraftRecord {
  const record = JSON.parse(value) as DraftRecord
  if (
    !record ||
    typeof record.id !== 'string' ||
    typeof record.key !== 'string' ||
    typeof record.updatedAt !== 'string' ||
    !isDraft(record.draft)
  )
    throw new Error('草稿数据格式无效，原数据已保留')
  return record
}

// Each mounted editor gets a new random owner. In particular, neither a cloned
// sessionStorage nor a recovered record can make two tabs share a writable key.
export function createDraftStore(
  storage: DraftStorage,
  ownerID = crypto.randomUUID(),
  now = () => new Date().toISOString(),
) {
  const prefix = (key: string) => `${keyPrefix}${encodeURIComponent(key)}:`
  const storageKey = (key: string) => prefix(key) + ownerID
  return {
    async listDrafts(key: string): Promise<DraftListing> {
      const records: DraftRecord[] = []
      let damaged = 0
      for (let index = 0; index < storage.length; index++) {
        const name = storage.key(index)
        if (!name?.startsWith(prefix(key))) continue
        const value = storage.getItem(name)
        if (value === null) continue
        try {
          const record = parseRecord(value)
          if (record.key !== key || name !== prefix(key) + record.id)
            throw new Error('Invalid owner')
          records.push(record)
        } catch {
          damaged++
        }
      }
      records.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
      return { records, damaged }
    },
    async getDraft(key: string): Promise<Draft | undefined> {
      const value = storage.getItem(storageKey(key))
      return value === null ? undefined : parseRecord(value).draft
    },
    async saveDraft(key: string, value?: Draft): Promise<void> {
      if (value === undefined) storage.removeItem(storageKey(key))
      else
        storage.setItem(
          storageKey(key),
          JSON.stringify({ id: ownerID, key, updatedAt: now(), draft: value }),
        )
    },
    async removeDraftIfUnchanged(key: string, expected: Draft): Promise<boolean> {
      const name = storageKey(key)
      const value = storage.getItem(name)
      if (value === null) return true
      if (JSON.stringify(parseRecord(value).draft) !== JSON.stringify(expected)) return false
      // Only this editor writes its own key. A response can never delete the
      // source of a recovered draft or a different editor's newer work.
      storage.removeItem(name)
      return true
    },
  }
}

export function createDraftSession(ownerID = crypto.randomUUID()) {
  const currentStore = () => {
    const storage = globalThis.localStorage
    if (!storage) throw new Error('当前浏览器无法访问草稿存储')
    return createDraftStore(storage, ownerID)
  }
  return {
    async listDrafts(key: string): Promise<DraftListing> {
      const listing = await currentStore().listDrafts(key)
      // A previous per-tab draft is offered explicitly, never silently claimed.
      try {
        const legacy = globalThis.sessionStorage?.getItem(legacyPrefix + key)
        if (legacy) {
          const draft: unknown = JSON.parse(legacy)
          if (isDraft(draft)) listing.records.push({ id: 'legacy-tab', key, updatedAt: '', draft })
          else listing.damaged++
        }
      } catch {
        /* Local persistent drafts remain usable when session storage is denied. */
      }
      return listing
    },
    async getDraft(key: string) {
      return currentStore().getDraft(key)
    },
    async saveDraft(key: string, value?: Draft) {
      return currentStore().saveDraft(key, value)
    },
    async removeDraftIfUnchanged(key: string, expected: Draft) {
      return currentStore().removeDraftIfUnchanged(key, expected)
    },
  }
}

const defaultOwner = crypto.randomUUID()
export async function getDraft(key: string) {
  return createDraftSession(defaultOwner).getDraft(key)
}
export async function saveDraft(key: string, value?: Draft) {
  return createDraftSession(defaultOwner).saveDraft(key, value)
}
export async function removeDraftIfUnchanged(key: string, expected: Draft) {
  return createDraftSession(defaultOwner).removeDraftIfUnchanged(key, expected)
}
