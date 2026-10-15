// Isolated API state; never reaches a real ATM instance.
import { createRoot } from 'react-dom/client'
import { BrowserRouter, useLocation } from 'react-router'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { DesignSystemProvider } from '../../src/design-system'
import { AppShell } from '../../src/workspace-shell'
import { TaskKnowledge, KnowledgeTasks } from '../../src/features/knowledge/task-knowledge-links'
import { call } from '../../src/api'
import type { Bootstrap, Todo } from '../../src/types'
import type { KnowledgeDocument } from '../../src/features/knowledge/knowledge-types'
import '../../src/app.css'

const readonly = new URLSearchParams(location.search).has('readonly')
const doc: KnowledgeDocument = {
  metadata: {
    id: 'document:reference',
    title: '部署回滚说明',
    status: 'active',
    producer: 'fixture',
    createdAt: '',
    updatedAt: '',
    projects: ['atm'],
  },
  etag: 'v1',
  editable: true,
  collection: 'notes',
  content: '回滚前核对版本。',
}
let tasks: Todo[] = [
  {
    id: 't1',
    title: '核对发布流程',
    status: 'open',
    priority: 'P2',
    created: '2026-10-10',
    links: [],
  },
]
const created = new Map<string, Todo>()
let writes = 0
Object.assign(window, {
  fixtureStats: () => ({ count: tasks.length, writes }),
  failNextLink: false,
})
window.fetch = async (url, init) => {
  const method = String(url).split('/').at(-1)
  const body = init?.body ? JSON.parse(String(init.body)) : {}
  let data: unknown
  if (['todo.knowledge.add', 'todo.knowledge.remove', 'todo.create'].includes(method!)) {
    if (readonly) throw new Error('Read-only fixture received a mutation')
    writes++
  }
  const href = '/knowledge?document=document%3Areference'
  const link = { url: href, title: doc.metadata.title, kind: 'knowledge' }
  switch (method) {
    case 'session.status':
      data = { bindings: [] }
      break
    case 'knowledge.query':
      data = {
        documents: [{ document_id: doc.metadata.id, title: doc.metadata.title }],
        has_more: false,
      }
      break
    case 'todo.list':
      data = { items: tasks, has_more: false }
      break
    case 'todo.show':
      data = { todo: tasks.find((t) => t.id === body.todo_id), etag: 'v1' }
      break
    case 'knowledge.tasks':
      data = {
        items: tasks.filter((t) => t.links?.length).map((t) => ({ ...t, archived: false })),
        has_more: false,
      }
      break
    case 'todo.knowledge.add':
      if ((window as any).failNextLink) {
        ;(window as any).failNextLink = false
        return Response.json(
          { error: { code: 'unavailable', message: '关联暂时失败，请重试' } },
          { status: 503 },
        )
      }
      tasks = tasks.map((t) => (t.id === body.todo_id ? { ...t, links: [link] } : t))
      data = {}
      break
    case 'todo.knowledge.remove':
      tasks = tasks.map((t) => (t.id === body.todo_id ? { ...t, links: [] } : t))
      data = {}
      break
    case 'todo.create': {
      const key = new Headers(init?.headers).get('Idempotency-Key') || ''
      let todo = created.get(key)
      if (!todo) {
        if (body.knowledge_document_id !== doc.metadata.id)
          throw new Error('Missing atomic knowledge association')
        todo = {
          id: `t${tasks.length + 1}`,
          title: body.title,
          description: body.description,
          project: body.project,
          status: 'open',
          priority: 'P2',
          created: '',
          links: [link],
        }
        tasks.push(todo)
        created.set(key, todo)
      }
      data = { todo, etag: 'v1' }
      break
    }
    default:
      throw new Error(`Unexpected fixture method: ${method}`)
  }
  return Response.json({ data })
}
function Fixture() {
  const path = useLocation().pathname
  const id = path.match(/^\/tasks\/(t\d+)/)?.[1] || 't1'
  const todo = useQuery({
    queryKey: ['todo', id],
    queryFn: () => call('todo.show', { todo_id: id }),
  })
  return (
    <AppShell
      boot={{ capabilities: { todo_write: !readonly, workspace_write: !readonly } } as Bootstrap}
    >
      <div style={{ padding: 24, maxWidth: 780, width: '100%', boxSizing: 'border-box' }}>
        {path === '/knowledge' ? (
          <>
            <h1>{doc.metadata.title}</h1>
            <KnowledgeTasks document={doc} writable={!readonly} />
          </>
        ) : path === '/usage' ? (
          <h1>统计</h1>
        ) : (
          <>
            <h1>{todo.data?.todo.title}</h1>
            {todo.data && <TaskKnowledge todo={todo.data.todo} writable={!readonly} />}
          </>
        )}
      </div>
    </AppShell>
  )
}
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <DesignSystemProvider>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <Fixture />
      </QueryClientProvider>
    </DesignSystemProvider>
  </BrowserRouter>,
)
