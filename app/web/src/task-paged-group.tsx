import { useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { call, errorText } from './api'
import { sortTasks } from './task-view-model'
import type { Todo } from './types'
import {
  mergeTaskGroupRows,
  type RemoteTaskWorkbench,
  type TaskGroup,
  type TaskPage,
} from './task-query'

export type TaskGroupPageState = {
  rows: Todo[]
  pending: boolean
  failure: string
  activeRemaining: number
  doneRemaining: number
  loadingMore: boolean
  moreActive: () => void
  moreDone: () => void
  retry: () => void
}

export function TaskPagedGroup({
  remote,
  group,
  children,
  enabled = true,
}: {
  remote: RemoteTaskWorkbench
  group: TaskGroup
  enabled?: boolean
  children: (state: TaskGroupPageState, ref: RefObject<HTMLElement | null>) => ReactNode
}) {
  const root = useRef<HTMLElement>(null)
  const [visible, setVisible] = useState(false)
  useEffect(() => {
    if (!enabled) return
    if (!('IntersectionObserver' in window)) {
      setVisible(true)
      return
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '300px' },
    )
    if (root.current) observer.observe(root.current)
    return () => observer.disconnect()
  }, [enabled])
  const page = (partition: 'active' | 'done', total: number, limit: number) => ({
    queryKey: ['todos', 'group', remote.input, group.key, partition],
    queryFn: ({ signal, pageParam }: { signal: AbortSignal; pageParam: number }) =>
      call(
        'todo.list',
        { ...remote.input, group_key: group.key, partition, limit, offset: pageParam },
        signal,
      ),
    initialPageParam: 0,
    getNextPageParam: (last: TaskPage) =>
      last.has_more ? last.offset + last.items.length : undefined,
    enabled: enabled && visible && total > 0,
    refetchInterval: remote.refetchInterval,
    refetchIntervalInBackground: false,
  })
  const active = useInfiniteQuery(page('active', group.active_total, 50))
  const done = useInfiniteQuery(page('done', group.done_total, 5))
  const rows = useMemo(() => {
    const merged = mergeTaskGroupRows([
      {
        partition: 'active',
        total: group.active_total,
        items: active.data?.pages.flatMap((page) => page.items) || [],
        fetchedAt: active.dataUpdatedAt,
      },
      {
        partition: 'done',
        total: group.done_total,
        items: done.data?.pages.flatMap((page) => page.items) || [],
        fetchedAt: done.dataUpdatedAt,
      },
    ])
    return sortTasks(merged, remote.input.sort, remote.input.direction)
  }, [
    active.data,
    active.dataUpdatedAt,
    done.data,
    done.dataUpdatedAt,
    group.active_total,
    group.done_total,
    remote.input.sort,
    remote.input.direction,
  ])
  const loadedActive = rows.filter((row) => row.status !== 'done').length
  const loadedDone = rows.length - loadedActive
  const failure = (group.active_total > 0 && active.error) || (group.done_total > 0 && done.error)
  return children(
    {
      rows,
      pending:
        (group.active_total > 0 && active.isPending) || (group.done_total > 0 && done.isPending),
      failure: failure ? errorText(failure) : '',
      activeRemaining: Math.max(0, group.active_total - loadedActive),
      doneRemaining: Math.max(0, group.done_total - loadedDone),
      loadingMore: active.isFetchingNextPage || done.isFetchingNextPage,
      moreActive: () => {
        void active.fetchNextPage()
      },
      moreDone: () => {
        void done.fetchNextPage()
      },
      retry: () => {
        if (group.active_total > 0) void active.refetch()
        if (group.done_total > 0) void done.refetch()
      },
    },
    root,
  )
}

export function TaskPageControls({ page }: { page: TaskGroupPageState }) {
  if (page.failure)
    return (
      <p role="alert">
        {page.failure} <button onClick={page.retry}>重试</button>
      </p>
    )
  if (page.pending) return <p role="status">正在读取任务…</p>
  return (
    <>
      {page.activeRemaining > 0 && (
        <button
          className="workbench-load-more"
          disabled={page.loadingMore}
          onClick={page.moreActive}
        >
          查看更多任务 · 还有 {page.activeRemaining} 项
        </button>
      )}
      {page.doneRemaining > 0 && (
        <button className="workbench-load-more" disabled={page.loadingMore} onClick={page.moreDone}>
          查看更多已完成 · 还有 {page.doneRemaining} 项
        </button>
      )}
    </>
  )
}
