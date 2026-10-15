// Visual fixture for the real shell, title, and menus. No backend requests.
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ListTodo } from 'lucide-react'
import { AppShell } from '../../src/workspace-shell'
import { WorkspaceNavigatorHeading } from '../../src/components/workspace-navigator'
import { TaskViews } from '../../src/features/tasks/task-views'
import { KnowledgeManager } from '../../src/features/knowledge/knowledge-manager'
import type { Bootstrap } from '../../src/types'
import '../../src/app.css'
import '../../src/features/tasks/task-workbench.css'
import { DesignSystemProvider } from '../../src/design-system'
const boot = { capabilities: { todo_write: true }, version: 'test' } as Bootstrap
window.fetch = async (url) => {
  if (String(url).endsWith('/knowledge.query'))
    return new Response(JSON.stringify({ data: { documents: [] } }))
  throw new Error('Unexpected fixture request')
}
function Harness() {
  const [params, setParams] = useState(new URLSearchParams('focus=all&layout=kanban'))
  const [manager, setManager] = useState(false)
  return (
    <AppShell boot={boot}>
      <section className="task-workbench">
        <WorkspaceNavigatorHeading className="workbench-heading" icon={ListTodo} title="任务" />
        <div className="workbench-toolbar">
          <TaskViews params={params} items={[]} onChange={setParams} onOpenMenu={() => {}} />
        </div>
        <div style={{ padding: 24 }}>
          <button className="button subtle" onClick={() => setManager(true)}>
            文档管理预览
          </button>
        </div>
        {manager && (
          <KnowledgeManager
            initialCollection=""
            collections={[]}
            writable={false}
            onClose={() => setManager(false)}
            onOpen={() => {}}
          />
        )}
      </section>
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
