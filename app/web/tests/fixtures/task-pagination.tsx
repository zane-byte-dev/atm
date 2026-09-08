// Isolated pagination fixture: no real ATM data or API is accessed.
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { TaskWorkbench } from '../../src/task-workbench'
import { filterTasks, groupTasks } from '../../src/task-view-model'
import { taskWorkbenchInput, type TaskWorkbenchMetadata } from '../../src/task-query'
import type { Todo } from '../../src/types'
import '../../src/app.css'

const rows: Todo[] = Array.from({ length: 129 }, (_, index) => ({
  id: `t${index + 1}`,
  title: `任务 ${index + 1}`,
  status: index < 120 ? 'open' : 'done',
  priority: 'P1',
  project: 'atm',
  created: '2026-09-08',
  updated_at: index,
  creator: 'test',
  tags: ['UI'],
}))
const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: 60000 } } })
const requests: Record<string, unknown>[] = []
Object.assign(window, {
  paginationRequests: requests,
  removePaginationRow: async (id: string) => {
    const index = rows.findIndex((row) => row.id === id)
    if (index >= 0) rows.splice(index, 1)
    await client.invalidateQueries({ queryKey: ['todos'] })
  },
})
window.fetch = async (url, init) => {
  if (String(url).endsWith('/session.status')) return Response.json({ data: { bindings: [] } })
  if (!String(url).endsWith('/todo.list')) throw new Error(`Unexpected request: ${url}`)
  const input = JSON.parse(String(init?.body || '{}'))
  requests.push(input)
  const params = new URLSearchParams(input)
  params.set('q', input.query || '')
  const matching = filterTasks(rows, params)
  const grouped = groupTasks(matching)
  const groups =
    input.group_by === 'status'
      ? [...grouped.byStatus].map(([status, rows]) => ({ project: '', status, rows }))
      : input.group_by === 'project'
        ? [...grouped.byProject].map(([project, rows]) => ({ project, status: '', rows }))
        : input.group_by === 'project_status'
          ? [...grouped.byProjectStatus].map(([key, rows]) => {
              const [project, status] = JSON.parse(key)
              return { project, status, rows }
            })
          : [{ project: '', status: '', rows: matching }]
  if (input.metadata_only)
    return Response.json({
      data: {
        matched_total: matching.length,
        selection_revision: matching
          .map((row) => row.id)
          .sort()
          .join(','),
        groups: groups.map(({ project, status, rows }) => ({
          key: JSON.stringify([project, status]),
          project,
          status,
          total: rows.length,
          active_total: rows.filter((row) => row.status !== 'done').length,
          done_total: rows.filter((row) => row.status === 'done').length,
        })),
        facets: {
          project: [{ value: 'atm', count: matching.length }],
          creator: [{ value: 'test', count: matching.length }],
          tag: [{ value: 'UI', count: matching.length }],
        },
        view_counts: { all: 129, unfinished: 120, review: 0, waiting: 0 },
      },
    })
  let pageRows = input.group_key
    ? groups.find((group) => JSON.stringify([group.project, group.status]) === input.group_key)
        ?.rows || []
    : matching
  if (input.partition)
    pageRows = pageRows.filter((row) => (row.status === 'done') === (input.partition === 'done'))
  const offset = input.offset || 0,
    limit = input.limit || 50
  return Response.json({
    data: {
      items: pageRows.slice(offset, offset + limit),
      total: pageRows.length,
      limit,
      offset,
      has_more: offset + limit < pageRows.length,
    },
  })
}
function Harness() {
  const [params, setParams] = useState(new URLSearchParams('focus=all&layout=list&group=none'))
  const input = taskWorkbenchInput(params)
  const metadata = useQuery({
    queryKey: ['todos', 'fixture', input],
    queryFn: async () =>
      (
        await (
          await fetch('/api/v1/todo.list', {
            method: 'POST',
            body: JSON.stringify({ ...input, metadata_only: true }),
          })
        ).json()
      ).data as TaskWorkbenchMetadata,
  })
  return (
    <main>
      <nav aria-label="测试布局">
        {['list', 'kanban', 'table', 'swimlane'].map((layout) => (
          <button
            key={layout}
            onClick={() =>
              setParams((previous) => {
                const next = new URLSearchParams(previous)
                next.set('layout', layout)
                return next
              })
            }
          >
            布局 {layout}
          </button>
        ))}
      </nav>
      <TaskWorkbench
        items={[]}
        params={params}
        onChange={setParams}
        onCreate={() => {}}
        onOpen={() => {}}
        writable
        loading={metadata.isPending}
        onRetry={() => void metadata.refetch()}
        remote={
          metadata.data ? { input, metadata: metadata.data, refetchInterval: false } : undefined
        }
      />
    </main>
  )
}
createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={client}>
    <BrowserRouter>
      <Harness />
    </BrowserRouter>
  </QueryClientProvider>,
)
