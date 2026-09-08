import type { Todo } from './types'
import { waitingReason } from './task-attention.ts'
export const statuses = [
  ['open', '待开始'],
  ['in_progress', '进行中'],
  ['review', '待验收'],
  ['done', '已完成'],
] as const
export const fields = [
  ['id', '编号'],
  ['priority', '优先级'],
  ['project', '项目'],
  ['creator', '创建者'],
  ['created', '创建日期'],
  ['tags', '标签'],
] as const
export const viewKeys = [
  'focus',
  'layout',
  'tableGroup',
  'status',
  'project',
  'priority',
  'creator',
  'tag',
  'from',
  'to',
  'sort',
  'direction',
  'fields',
  'density',
  'group',
  'q',
]
export type SavedView = { id: string; name: string; query: string }
export const savedViewsKey = 'atm:task-views:v1'
export function viewQuery(params: URLSearchParams) {
  const clean = new URLSearchParams()
  for (const key of viewKeys) if (params.has(key)) clean.set(key, params.get(key)!)
  return clean.toString()
}
export function parseViews(raw: string | null): SavedView[] {
  try {
    const data = JSON.parse(raw || '[]')
    if (!Array.isArray(data)) return []
    return data
      .filter(
        (item): item is SavedView =>
          !!item &&
          typeof item.id === 'string' &&
          typeof item.name === 'string' &&
          typeof item.query === 'string',
      )
      .map((item) => ({
        id: item.id,
        name: item.name.slice(0, 60),
        query: viewQuery(new URLSearchParams(item.query)),
      }))
  } catch {
    return []
  }
}
export function visibleFields(params: URLSearchParams) {
  return params.has('fields') ? params.get('fields')!.split(',') : ['id', 'priority', 'project']
}
export function filterTasks(items: Todo[], params: URLSearchParams) {
  const status = params.get('status') || ''
  const priorities = (params.get('priority') || '').split(',').filter(Boolean)
  const states = status.split(',')
  const project = params.get('project'),
    creator = params.get('creator'),
    tag = params.get('tag')
  const from = params.get('from'),
    to = params.get('to')
  const query = (params.get('q') || '').trim().toLocaleLowerCase()
  const focus = params.get('focus') || 'all'
  const filtered = items.filter((item) => {
    if (focus === 'unfinished' && item.status === 'done') return false
    if (focus === 'review' && item.status !== 'review') return false
    if (focus === 'waiting' && !waitingReason(item)) return false
    if (status === 'archived' ? !item.archived : item.archived) return false
    if (status && !['all', 'archived'].includes(status) && !states.includes(item.status))
      return false
    if (priorities.length && !priorities.includes(item.priority)) return false
    if (project && item.project !== project) return false
    if (creator && item.creator !== creator) return false
    if (tag && !item.tags?.includes(tag)) return false
    const day = (item.created || '').slice(0, 10)
    if ((from && (!day || day < from)) || (to && (!day || day > to))) return false
    if (
      query &&
      ![item.title, item.id, item.project, item.creator, ...(item.tags || [])]
        .join(' ')
        .toLocaleLowerCase()
        .includes(query)
    )
      return false
    return true
  })
  const sort = params.get('sort') || 'auto'
  const direction = params.get('direction') === 'desc' ? -1 : 1
  return filtered.sort((a, b) => {
    if (sort === 'auto') {
      if (a.status === 'done' && b.status === 'done')
        return (
          (b.updated_at || 0) - (a.updated_at || 0) ||
          b.id.localeCompare(a.id, undefined, { numeric: true })
        )
      if (a.status === 'done' || b.status === 'done') return a.status === 'done' ? 1 : -1
    }
    if (sort === 'updated')
      return (
        direction * ((a.updated_at || 0) - (b.updated_at || 0)) ||
        a.id.localeCompare(b.id, undefined, { numeric: true })
      )

    const av = sort === 'title' ? a.title : sort === 'created' ? a.created : a.priority
    const bv = sort === 'title' ? b.title : sort === 'created' ? b.created : b.priority
    return (
      direction * (av || '').localeCompare(bv || '', 'zh-CN', { numeric: true }) ||
      a.id.localeCompare(b.id, undefined, { numeric: true })
    )
  })
}
