import type { Draft } from './types'

type InputStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
// Keep the existing new-task key so already entered information remains usable.
// Existing-task records and historical draft collections are never read or changed.
const key = 'atm-workspace:draft:v3:new-todo'
function parse(raw: string): Draft {
  const value = JSON.parse(raw)?.draft
  if (
    !value ||
    !['title', 'description', 'project', 'priority', 'etag', 'operationID'].every(
      (field) => typeof value[field] === 'string',
    ) ||
    !value.operationID
  )
    throw new Error('新建任务填写内容无法读取')
  return {
    title: value.title,
    description: value.description,
    project: value.project,
    priority: value.priority,
    etag: '',
    operationID: value.operationID,
  }
}
export function createNewTaskInputStore(storage: () => InputStorage) {
  return {
    async read() {
      const raw = storage().getItem(key)
      return raw === null ? undefined : parse(raw)
    },
    async save(input: Draft) {
      storage().setItem(key, JSON.stringify({ draft: input }))
    },
    async clearIfUnchanged(input: Draft) {
      const store = storage()
      const raw = store.getItem(key)
      if (raw === null) return true
      if (JSON.stringify(parse(raw)) !== JSON.stringify(parse(JSON.stringify({ draft: input }))))
        return false
      store.removeItem(key)
      return true
    },
  }
}
