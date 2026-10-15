export type NativePreferences = {
  knowledge_collection_order?: string[]
  collection_source_order?: string[]
  usage_filter_model?: string
  usage_filter_client?: string
  usage_filter_project?: string
}
export const nativePreferenceKeys = [
  'knowledge_collection_order',
  'collection_source_order',
  'usage_filter_model',
  'usage_filter_client',
  'usage_filter_project',
] as const
const key = 'atm.web.native-preferences.v1'
function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}
function validatePreferences(value: unknown): NativePreferences {
  if (
    !object(value) ||
    Object.keys(value).some(
      (field) => !nativePreferenceKeys.includes(field as keyof NativePreferences),
    )
  )
    throw new Error('偏好包含不支持的字段。')
  const result: NativePreferences = {}
  for (const field of nativePreferenceKeys) {
    const item = value[field]
    if (item === undefined) continue
    if (field === 'knowledge_collection_order' || field === 'collection_source_order') {
      if (
        !Array.isArray(item) ||
        item.length > 1000 ||
        item.some((id) => typeof id !== 'string' || !id.trim() || id.length > 512)
      )
        throw new Error('排序偏好格式不正确，最多支持 1,000 个有效条目。')
      result[field] = [...new Set(item)]
    } else {
      if (typeof item !== 'string' || item.length > 512) throw new Error('筛选偏好格式不正确。')
      result[field] = item
    }
  }
  return result
}
/** Unknown/deleted IDs disappear; newly discovered rows retain the server order. */
export function nativeOrdered<T extends { id: string }>(rows: T[], order?: string[]): T[] {
  if (!order?.length) return rows
  const byID = new Map(rows.map((row) => [row.id, row]))
  const seen = new Set<string>()
  const result: T[] = []
  for (const id of [...order, ...rows.map((row) => row.id)]) {
    const row = byID.get(id)
    if (row && !seen.has(id)) {
      result.push(row)
      seen.add(id)
    }
  }
  return result
}
const listeners = new Set<() => void>()
const empty: NativePreferences = {}
function read(): NativePreferences {
  try {
    return typeof window === 'undefined'
      ? empty
      : validatePreferences(JSON.parse(localStorage.getItem(key) || '{}'))
  } catch {
    return empty
  }
}
let snapshot = read()
export const nativePreferencesSnapshot = () => snapshot
export function subscribeNativePreferences(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
export function saveNativePreferences(patch: NativePreferences) {
  const next = validatePreferences({ ...snapshot, ...patch })
  localStorage.setItem(key, JSON.stringify(next))
  snapshot = next
  listeners.forEach((listener) => listener())
}
if (typeof window !== 'undefined')
  window.addEventListener('storage', (event) => {
    if (event.key === key || event.key === null) {
      snapshot = read()
      listeners.forEach((listener) => listener())
    }
  })
