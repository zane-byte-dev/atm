export type NativePreferences = {
  knowledge_collection_order?: string[]
  collection_source_order?: string[]
  usage_filter_model?: string
  usage_filter_client?: string
  usage_filter_project?: string
}
export type NativePreferenceFile = {
  kind: 'atm-native-preferences'
  version: 1
  source_bundle_id: 'dev.zanebyte.atm.menubar' | 'dev.zanebyte.atm.menubar.dev'
  preferences: NativePreferences
}
export const nativePreferenceKeys = [
  'knowledge_collection_order',
  'collection_source_order',
  'usage_filter_model',
  'usage_filter_client',
  'usage_filter_project',
] as const
export const nativePreferenceLabels: Record<keyof NativePreferences, string> = {
  knowledge_collection_order: '知识集合顺序',
  collection_source_order: '采集来源顺序',
  usage_filter_model: '用量模型筛选',
  usage_filter_client: '用量客户端筛选',
  usage_filter_project: '用量项目筛选',
}
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
    throw new Error('文件包含不支持的偏好字段，请使用旧版 App 的偏好导出工具。')
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
export function parseNativePreferenceFile(text: string): NativePreferenceFile {
  if (text.length > 2 * 1024 * 1024) throw new Error('偏好文件不能大于 2 MB。')
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    throw new Error('这不是有效的 JSON 偏好文件。')
  }
  if (
    !object(value) ||
    value.kind !== 'atm-native-preferences' ||
    value.version !== 1 ||
    !['dev.zanebyte.atm.menubar', 'dev.zanebyte.atm.menubar.dev'].includes(
      String(value.source_bundle_id),
    ) ||
    Object.keys(value).some(
      (field) => !['kind', 'version', 'source_bundle_id', 'preferences'].includes(field),
    )
  )
    throw new Error('请选择旧版 ATM 导出的界面偏好文件（版本 1）。')
  return {
    kind: 'atm-native-preferences',
    version: 1,
    source_bundle_id: value.source_bundle_id as NativePreferenceFile['source_bundle_id'],
    preferences: validatePreferences(value.preferences),
  }
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
export function resetNativePreferences() {
  localStorage.removeItem(key)
  snapshot = empty
  listeners.forEach((listener) => listener())
}
if (typeof window !== 'undefined')
  window.addEventListener('storage', (event) => {
    if (event.key === key || event.key === null) {
      snapshot = read()
      listeners.forEach((listener) => listener())
    }
  })
