// Isolated manual integration fixture. API writes affect only the in-memory test document.
import React, { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { InlineKnowledgeField, useInlineKnowledge } from '../../src/workspaces/knowledge-inline'
import type { KnowledgeDocument } from '../../src/workspaces/knowledge-types'
import '../../src/themes.css'
import '../../src/style.css'
import '../../src/workspaces/knowledge.css'
let server: KnowledgeDocument = {
  etag: 'v1',
  editable: true,
  collection: 'preview',
  metadata: {
    id: `crepe-preview-${crypto.randomUUID()}`,
    title: '知识编辑体验',
    status: 'active',
    producer: 'test',
    createdAt: '',
    updatedAt: '',
  },
  content:
    '## 写作与阅读\n\n这是 **加粗** 和 *斜体*，以及 [文档链接](https://example.com)。\n\n- [x] 保留待办清单\n- [ ] 检查自动保存\n\n> 好的编辑器让注意力留在内容上。\n\n| 功能 | 状态 |\n| --- | --- |\n| 表格 | 保留 |\n\n```ts\nconst answer = 42\n```\n\n继续书写。',
}
let writes = 0
window.fetch = async (url, init) => {
  if (String(url) !== '/api/v1/knowledge.document.update') throw new Error('Unexpected API call')
  const input = JSON.parse(String(init?.body))
  if (input.etag !== server.etag)
    return new Response(JSON.stringify({ error: { code: 'conflict', message: 'Conflict' } }), {
      status: 409,
    })
  writes++
  server = {
    ...server,
    etag: `v${writes + 1}`,
    metadata: { ...server.metadata, title: input.title },
    content: input.content,
  }
  return new Response(JSON.stringify({ data: server }))
}
function Fixture() {
  const [doc, setDoc] = useState(server)
  const inline = useInlineKnowledge(doc, true)
  return (
    <div
      style={
        {
          '--surface': '#fff',
          '--text': '#242424',
          '--muted': '#888',
          '--line': '#ddd',
          '--soft': '#f4f4f4',
          '--accent': '#333',
          '--accent-soft': '#e9e9e9',
          background: 'white',
          minHeight: '100vh',
        } as React.CSSProperties
      }
    >
      <header style={{ padding: 16 }}>
        独立测试文档 · <span role="status">{inline.status}</span> · 保存次数：{writes}
        <button
          onClick={() =>
            setDoc({ ...server, etag: 'remote', content: '## 外部更新\n\n新的正文。' })
          }
        >
          模拟外部更新
        </button>
        <button
          onClick={() =>
            setDoc({
              ...server,
              etag: 'special',
              content: '```ts title=example.ts\nconst value = 1\n```\n\n原始代码块属性需要保留。',
            })
          }
        >
          特殊语法文档
        </button>
      </header>
      <article className="knowledge-page knowledge-article" style={{ paddingTop: 36 }}>
        <InlineKnowledgeField
          title
          value={inline.draft.title}
          onChange={(title) => inline.patch({ title })}
          onComposing={inline.setComposing}
        />
        {inline.feedback}
        <InlineKnowledgeField
          value={inline.draft.content}
          onChange={(content) => inline.patch({ content })}
          onComposing={inline.setComposing}
        />
        <details style={{ marginTop: 32 }}>
          <summary>已保存 Markdown</summary>
          <pre>{server.content}</pre>
        </details>
      </article>
    </div>
  )
}
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={new QueryClient()}>
      <Fixture />
    </QueryClientProvider>
  </StrictMode>,
)
