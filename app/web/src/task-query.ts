import type { Todo } from './types'
import { readTaskLayout } from './task-navigation.ts'

export type TaskGroupBy = 'none' | 'status' | 'project' | 'project_status'
export type TaskWorkbenchInput = {
  workbench: true
  focus: string
  status: string
  priority: string
  project: string
  creator: string
  tag: string
  from: string
  to: string
  query: string
  sort: string
  direction: string
  group_by: TaskGroupBy
}
export type TaskGroup = {
  key: string
  project: string
  status: string
  total: number
  active_total: number
  done_total: number
}
export type TaskFacet = { value: string; count: number }
export type TaskWorkbenchMetadata = {
  matched_total: number
  selection_revision: string
  groups: TaskGroup[]
  facets: Record<string, TaskFacet[]>
  view_counts: Record<string, number>
}

export function selectedTaskRows(
  rows: Todo[],
  excluded: ReadonlySet<string>,
  limit = Infinity,
): Todo[] {
  const selected = rows.filter((row) => !excluded.has(row.id))
  if (selected.length > limit) throw new Error('筛选结果已变化，请重新选择最多 100 项任务。')
  return selected
}
export type TaskPage = {
  items: Todo[]
  total: number
  limit: number
  offset: number
  has_more: boolean
}
export type RemoteTaskWorkbench = {
  input: TaskWorkbenchInput
  metadata: TaskWorkbenchMetadata
  refetchInterval: number | false
}

export function taskWorkbenchInput(params: URLSearchParams): TaskWorkbenchInput {
  const layout = readTaskLayout(params)
  const group = params.get(layout === 'table' ? 'tableGroup' : 'group')
  const groupBy =
    layout === 'swimlane'
      ? 'project_status'
      : layout === 'kanban'
        ? 'status'
        : group === 'project'
          ? 'project'
          : group === 'status'
            ? 'status'
            : layout === 'list' && group !== 'none'
              ? 'status'
              : 'none'
  return {
    workbench: true,
    focus:
      params.get('focus') || (!params.has('status') && !params.has('view') ? 'unfinished' : 'all'),
    status: params.get('status') || '',
    priority: params.get('priority') || '',
    project: params.get('project') || '',
    creator: params.get('creator') || '',
    tag: params.get('tag') || '',
    from: params.get('from') || '',
    to: params.get('to') || '',
    query: params.get('q') || '',
    sort: params.get('sort') || 'auto',
    direction: params.get('direction') || 'asc',
    group_by: groupBy,
  }
}

/** Selection follows the filtered set and survives layout, column and sorting changes. */
export function taskSelectionScope(params: URLSearchParams): string {
  const {
    sort: _sort,
    direction: _direction,
    group_by: _group,
    ...filter
  } = taskWorkbenchInput(params)
  return JSON.stringify(filter)
}

/** Explicit export/selection only; interactive rendering fetches bounded group pages. */
export async function collectTaskPages(
  fetchPage: (offset: number) => Promise<TaskPage>,
): Promise<Todo[]> {
  const rows = new Map<string, Todo>()
  let offset = 0
  while (true) {
    const page = await fetchPage(offset)
    for (const row of page.items) rows.set(row.id, row)
    if (!page.has_more || !page.items.length) return [...rows.values()]
    offset += page.items.length
  }
}

/** Disabled partition queries retain their cache; only current partitions may contribute rows. */
export function mergeTaskGroupRows(
  partitions: {
    partition: 'active' | 'done'
    total: number
    items: Todo[]
    fetchedAt: number
  }[],
): Todo[] {
  const rows = new Map<string, { todo: Todo; fetchedAt: number }>()
  for (const part of partitions) {
    if (!part.total) continue
    for (const todo of part.items) {
      if ((todo.status === 'done') !== (part.partition === 'done')) continue
      const previous = rows.get(todo.id)
      const version = todo.updated_at || 0,
        oldVersion = previous?.todo.updated_at || 0
      if (
        !previous ||
        version > oldVersion ||
        (version === oldVersion && part.fetchedAt > previous.fetchedAt)
      )
        rows.set(todo.id, { todo, fetchedAt: part.fetchedAt })
    }
  }
  return [...rows.values()].map((row) => row.todo)
}
