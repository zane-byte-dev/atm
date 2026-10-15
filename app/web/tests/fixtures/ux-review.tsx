// Isolated interactions for the UX fixes. No requests reach real ATM records.
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Routes, Route } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Button } from 'antd'
import { DesignSystemProvider } from '../../src/design-system'
import { TaskWorkspace } from '../../src/features/tasks/task-workspace'
import { CollectionContext } from '../../src/features/collection/collection-context'
import { MessagesWorkspace } from '../../src/features/notifications/messages'
import { AppShell } from '../../src/workspace-shell'
import { notifyRuntimeJob } from '../../src/features/notifications/runtime-job-notifications'
import { useWorkspaceNotifications } from '../../src/features/notifications/workspace-notifications'
import type { Bootstrap, Todo, TodoDetail } from '../../src/types'
import '../../src/app.css'
const boot = {
  csrf_token: 'fixture',
  version: 'dev-fixture',
  capabilities: { todo_write: true },
} as Bootstrap
const todo: Todo = {
  id: 't1',
  title: '核对任务交互',
  description: '标题和描述应当独立编辑。',
  status: 'open',
  priority: 'P2',
  project: 'atm',
  created: '2026-10-04',
}
let latestPlan: TodoDetail['latest_plan']
const progress =
  '已完成任务详情的交互梳理，状态切换与属性修改都保留当前阅读位置。' +
  '验证记录包含完整的判断依据、执行结果与下一步安排，长内容应当完整显示。'.repeat(9) +
  '【完整进展末尾】'
let failNextMutation = new URLSearchParams(location.search).has('failOnce')
window.fetch = async (url, init) => {
  const method = String(url).split('/').at(-1)
  if (
    [
      'todo.start',
      'todo.submit',
      'todo.return_to_open',
      'todo.done',
      'todo.archive',
      'todo.restore',
      'todo.update',
      'todo.plan.set',
      'todo.wait.update',
      'todo.wake',
      'todo.dependency.add',
      'todo.dependency.remove',
    ].includes(method || '')
  ) {
    await new Promise((resolve) => setTimeout(resolve, 400))
    if (failNextMutation) {
      failNextMutation = false
      return new Response(
        JSON.stringify({ error: { code: 'TEST', message: '测试：状态更新失败，请重试' } }),
        { status: 500 },
      )
    }
    const input = JSON.parse(String(init?.body || '{}'))
    if (method === 'todo.update') {
      if ('priority' in input) todo.priority = input.priority
      if ('project' in input) todo.project = input.project
      if ('title' in input) todo.title = input.title
      if ('description' in input) todo.description = input.description
    }
    if (method === 'todo.plan.set')
      latestPlan = {
        revision: 1,
        explanation: input.explanation,
        items: input.items,
      } as TodoDetail['latest_plan']
    if (method === 'todo.wait.update') {
      todo.wake_condition = input.wake_condition
      todo.review_at = input.review_at
    }
    if (method === 'todo.wake') {
      todo.wake_condition = ''
      todo.review_at = ''
    }
    if (method === 'todo.dependency.add') todo.depends_on = [input.dependency_id]
    if (method === 'todo.dependency.remove') todo.depends_on = []
    if (method === 'todo.submit') todo.status = 'review'
    if (method === 'todo.return_to_open') todo.status = 'open'
    if (method === 'todo.start') todo.status = 'in_progress'
    if (method === 'todo.done') todo.status = 'done'
    if (method === 'todo.archive') todo.archived = true
    if (method === 'todo.restore') todo.archived = false
    return new Response(JSON.stringify({ data: { todo, etag: 'fixture-etag' } }))
  }
  const data = {
    'todo.list': {
      items: [todo],
      total: 1,
      limit: 50,
      offset: 0,
      projects: ['atm', 'wanda'],
      matched_total: 1,
      selection_revision: 'fixture',
      facets: {},
      view_counts: { open: 1, all: 1, unfinished: 1 },
      groups: [
        { key: 'open', status: 'open', project: '', total: 1, active_total: 1, done_total: 0 },
      ],
    },
    'todo.show': { todo, etag: 'fixture-etag', latest_plan: latestPlan },
    'todo.doc': { content: `## 进展\n- [2026-10-04 17:00] ${progress}` },
    'todo.dependency.list': {
      dependencies: (todo.depends_on || []).map((id) => ({
        id,
        title: '测试依赖任务',
        met: false,
      })),
      dependents: [],
    },
    'session.status': { sessions: [] },
  }[method || '']
  if (!data) throw new Error(`Unexpected fixture request: ${method}`)
  return new Response(JSON.stringify({ data }), { headers: { 'Content-Type': 'application/json' } })
}
function Notifications() {
  const notification = useWorkspaceNotifications()
  return (
    <AppShell boot={boot}>
      <div>
        <Button
          onClick={() =>
            notifyRuntimeJob(
              notification,
              { id: 'passive-success', kind: 'session.sync', status: 'succeeded', created_at: 1 },
              true,
            )
          }
        >
          模拟后台成功
        </Button>
        <Button
          onClick={() =>
            notifyRuntimeJob(
              notification,
              {
                id: 'passive-error',
                kind: 'session.sync',
                phase: '',
                status: 'failed',
                created_at: 1,
                error: { code: 'TEST', message: '测试连接失败' },
              },
              true,
            )
          }
        >
          模拟后台失败
        </Button>
        <Button
          onClick={() =>
            notifyRuntimeJob(notification, {
              id: 'manual-success',
              kind: 'collect.run',
              phase: '',
              status: 'succeeded',
              created_at: 1,
            })
          }
        >
          模拟主动操作成功
        </Button>
        <MessagesWorkspace boot={boot} />
      </div>
    </AppShell>
  )
}
const scene = new URLSearchParams(location.search).get('scene')
createRoot(document.getElementById('root')!).render(
  <MemoryRouter initialEntries={['/tasks/t1?layout=list']}>
    <DesignSystemProvider>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        {scene === 'context' ? (
          <div style={{ maxWidth: 760, margin: '30px auto', padding: 20 }}>
            <CollectionContext
              text={Array.from(
                { length: 65 },
                (_, i) =>
                  `2026-10-04 10:00:00 [${i === 48 ? '小明' : '同事'}] ${i === 48 ? '重点发布计划' : `上下文 ${i + 1}`}`,
              ).join('\n')}
            />
          </div>
        ) : scene === 'notifications' ? (
          <Notifications />
        ) : (
          <Routes>
            <Route path="/tasks/:id?" element={<TaskWorkspace boot={boot} />} />
          </Routes>
        )}
      </QueryClientProvider>
    </DesignSystemProvider>
  </MemoryRouter>,
)
