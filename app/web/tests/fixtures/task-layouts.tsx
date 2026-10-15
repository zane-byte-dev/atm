// Isolated visual fixture: no real task data or mutations.
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, useSearchParams } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { DesignSystemProvider } from '../../src/design-system'
import { AppShell } from '../../src/workspace-shell'
import { TaskWorkbench } from '../../src/features/tasks/task-workbench'
import type { Bootstrap, Todo } from '../../src/types'
import '../../src/app.css'

const titles = [
  '统一工作台的组件与交互规范',
  '整理知识库目录与文档归档流程',
  '优化任务看板在窄窗口下的阅读与选择体验，保留较长标题的完整上下文',
  '核对发布前的回归结果',
  '补充跨项目任务的执行说明',
  '完善异常提示与重试入口',
]
const items: Todo[] = Array.from({ length: 18 }, (_, index) => ({
  id: `t${410 + index}`,
  title: titles[index % titles.length],
  status: (['open', 'in_progress', 'review', 'done'] as const)[index % 4],
  priority: (['P0', 'P1', 'P2'] as const)[index % 3],
  project: ['atm', '设计系统', ''][Math.floor(index / 6)],
  created: '2026-10-04',
  creator: 'codex',
  tags: ['前端', '体验优化'],
  ...(index === 1 ? { wake_condition: '等待文档索引更新后重新验证' } : {}),
}))
window.fetch = async (url) => {
  if (String(url).endsWith('/session.status')) return Response.json({ data: { bindings: [] } })
  throw new Error(`Unexpected fixture request: ${url}`)
}
const boot = { version: 'fixture', capabilities: { todo_write: true } } as Bootstrap
function Harness() {
  const [params, setParams] = useSearchParams()
  const [opened, setOpened] = useState('')
  return (
    <AppShell boot={boot}>
      <TaskWorkbench
        items={items}
        params={params}
        onChange={setParams}
        onOpen={setOpened}
        onCreate={() => {}}
        writable
        loading={false}
        onRetry={() => {}}
      />
      <output aria-label="已打开任务" style={{ position: 'fixed', bottom: 0, right: 0 }}>
        {opened}
      </output>
    </AppShell>
  )
}
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <DesignSystemProvider>
      <QueryClientProvider client={new QueryClient()}>
        <Harness />
      </QueryClientProvider>
    </DesignSystemProvider>
  </BrowserRouter>,
)
