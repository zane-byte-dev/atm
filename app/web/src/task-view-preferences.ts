export const builtinViews = [
  { id: 'builtin:unfinished', name: '未完成', focus: 'unfinished' },
  { id: 'builtin:review', name: '待验收', focus: 'review' },
  { id: 'builtin:waiting', name: '等待中', focus: 'waiting' },
  { id: 'builtin:all', name: '全部', focus: 'all' },
] as const
export type ViewPreferences = { order: string[]; hidden: string[] }
export const viewPreferencesKey = 'atm:task-view-preferences:v1'
export function parseViewPreferences(raw: string | null): ViewPreferences {
  try {
    const value = JSON.parse(raw || '{}')
    const strings = (list: unknown) =>
      Array.isArray(list)
        ? [...new Set(list.filter((id): id is string => typeof id === 'string'))]
        : []
    return {
      order: strings(value?.order),
      hidden: strings(value?.hidden).filter((id) => id !== 'builtin:all'),
    }
  } catch {
    return { order: [], hidden: [] }
  }
}
export function orderViews<T extends { id: string }>(views: T[], order: string[]): T[] {
  const ids = [...new Set([...order, ...views.map((view) => view.id)])]
  const byID = new Map(views.map((view) => [view.id, view]))
  return ids.flatMap((id) => {
    const view = byID.get(id)
    return view ? [view] : []
  })
}
export function moveView(ids: string[], source: string, target: string): string[] {
  if (source === target || !ids.includes(source) || !ids.includes(target)) return ids
  const next = ids.filter((id) => id !== source)
  next.splice(ids.indexOf(target), 0, source)
  return next
}
