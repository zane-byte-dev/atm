// Isolated fixture: all API calls use in-memory documents, never real ATM data.
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { KnowledgeManager } from '../../src/workspaces/knowledge-manager'
import type { KnowledgeDocument } from '../../src/workspaces/knowledge-types'
import '../../src/app.css'
import '../../src/workspaces/knowledge.css'
const documents = new Map(
  ['one', 'two', 'three'].map((id, index) => [
    id,
    {
      etag: 'v1',
      editable: true,
      collection: index === 2 ? 'reference' : 'inbox',
      content: '保留正文',
      metadata: {
        id,
        title: ['开发约定', '接口设计草稿', '归档笔记'][index],
        status: ['active', 'draft', 'archived'][index],
        producer: 'test',
        createdAt: '',
        updatedAt: '2026-09-07',
      },
    } as KnowledgeDocument,
  ]),
)
let conflict = true
window.fetch = async (url, init) => {
  const input = JSON.parse(String(init?.body || '{}'))
  const method = String(url).split('/').pop()
  if (method === 'knowledge.query')
    return new Response(
      JSON.stringify({
        data: {
          documents: [...documents.values()]
            .filter(
              (doc) =>
                (!input.collection || doc.collection === input.collection) &&
                (!input.status || doc.metadata.status === input.status),
            )
            .map((doc) => ({
              document_id: doc.metadata.id,
              title: doc.metadata.title,
              collection: doc.collection,
              status: doc.metadata.status,
              updated_at: doc.metadata.updatedAt,
            })),
        },
      }),
    )
  const doc = documents.get(input.document_id)
  if (!doc) throw new Error('Unknown fixture document')
  if (method === 'knowledge.document.get') return new Response(JSON.stringify({ data: doc }))
  if (method !== 'knowledge.document.update') throw new Error('Unexpected method')
  if ((conflict && doc.metadata.id === 'two') || doc.etag !== input.etag) {
    conflict = false
    return new Response(
      JSON.stringify({ error: { code: 'conflict', message: '文档已修改，请重试' } }),
      { status: 409 },
    )
  }
  if (input.content !== doc.content) throw new Error('Content overwritten')
  const next = {
    ...doc,
    etag: doc.etag + 'x',
    collection: input.collection || doc.collection,
    metadata: { ...doc.metadata, status: input.status || doc.metadata.status },
  }
  documents.set(input.document_id, next)
  return new Response(JSON.stringify({ data: next }))
}
const collections = [
  { id: 'inbox', name: '收件箱', description: '', document_count: 2 },
  { id: 'reference', name: '参考资料', description: '', document_count: 1 },
]
function Harness() {
  const [scope, setScope] = useState<string | null>(null)
  const [opened, setOpened] = useState('')
  const writable = !new URLSearchParams(location.search).has('readonly')
  return (
    <main style={{ padding: 32 }}>
      <h1>知识</h1>
      <button onClick={() => setScope('')}>文档管理</button>
      <button onClick={() => setScope('inbox')}>管理收件箱的文档</button>
      <output>{opened}</output>
      {scope !== null && (
        <KnowledgeManager
          initialCollection={scope}
          collections={collections}
          writable={writable}
          onClose={() => setScope(null)}
          onOpen={(doc) => {
            setOpened(doc.title)
            setScope(null)
          }}
        />
      )}
    </main>
  )
}
createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
    <Harness />
  </QueryClientProvider>,
)
