// In-memory API: collection edits and deletes never touch real user data.
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { DesignSystemProvider } from '../../src/design-system'
import { AppShell } from '../../src/workspace-shell'
import { KnowledgeWorkspace } from '../../src/features/knowledge/knowledge'
import type { Bootstrap } from '../../src/types'
import '../../src/app.css'
let collections = [
  { id: 'notes', name: '工程笔记', description: '组件约定与技术决策', document_count: 2 },
  { id: 'reference', name: '参考资料', description: '值得反复查阅的资料', document_count: 1 },
  { id: 'empty', name: '待整理', description: '', document_count: 0 },
]
const requests: { method: string; input: Record<string, unknown> }[] = []
Object.assign(window, { collectionRequests: requests })
window.fetch = async (url, init) => {
  const method = String(url).split('/').pop() || ''
  const input = JSON.parse(String(init?.body || '{}'))
  requests.push({ method, input })
  if (method === 'knowledge.catalog') return Response.json({ data: collections })
  if (method === 'knowledge.query')
    return Response.json({ data: { documents: [], total: 0, has_more: false } })
  if (method === 'memory.recall') return Response.json({ data: { hits: [] } })
  const item = collections.find((row) => row.id === input.id)
  if (!item) throw new Error(`Unexpected fixture request: ${method}`)
  if (method === 'knowledge.collection.update') {
    if (input.name === 'error')
      return Response.json(
        { error: { code: 'unavailable', message: '模拟保存失败，请重试' } },
        { status: 503 },
      )
    Object.assign(item, { name: input.name, description: input.description })
    return Response.json({ data: item })
  }
  if (method === 'knowledge.collection.delete') {
    const target = collections.find((row) => row.id === input.move_to && row.id !== item.id)
    if (!input.confirmed || (item.document_count && !target))
      throw new Error('Unsafe fixture delete')
    if (target) target.document_count += item.document_count
    collections = collections.filter((row) => row.id !== item.id)
    return Response.json({
      data: { id: item.id, moved_to: target?.id, moved_documents: item.document_count },
    })
  }
  throw new Error(`Unexpected fixture request: ${method}`)
}
const boot = {
  version: 'fixture',
  capabilities: { workspace_write: !new URLSearchParams(location.search).has('readonly') },
} as Bootstrap
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <DesignSystemProvider>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <AppShell boot={boot}>
          <KnowledgeWorkspace boot={boot} />
        </AppShell>
      </QueryClientProvider>
    </DesignSystemProvider>
  </BrowserRouter>,
)
