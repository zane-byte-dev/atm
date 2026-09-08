// Opt-in browser integration checks: open this HTML through the local Vite server.
// All API calls are intercepted here; no real knowledge data is read or written.
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useInlineKnowledge } from '../../src/workspaces/knowledge-inline'
import type { KnowledgeDocument } from '../../src/workspaces/knowledge-types'

const id = `inline-test-${crypto.randomUUID()}`
const key = `atm.knowledge.inline:${id}`
const original: KnowledgeDocument = {
  etag: 'v1',
  editable: true,
  collection: 'test',
  metadata: {
    id,
    title: 'Test title',
    status: 'active',
    producer: 'test',
    createdAt: '',
    updatedAt: '',
  },
  content: 'Original content',
}
let server = original
let conflict = false
let requests: Record<string, string>[] = []
window.fetch = async (url, init) => {
  if (String(url) !== '/api/v1/knowledge.document.update')
    throw new Error(`Unexpected API call: ${url}`)
  const input = JSON.parse(String(init?.body))
  requests.push(input)
  await new Promise((resolve) => setTimeout(resolve, 180))
  if (conflict || input.etag !== server.etag)
    return new Response(
      JSON.stringify({ error: { code: 'conflict', message: 'Changed elsewhere' } }),
      { status: 409 },
    )
  server = {
    ...server,
    etag: `v${requests.length + 1}`,
    metadata: { ...server.metadata, title: input.title },
    content: input.content,
  }
  return new Response(JSON.stringify({ data: server }))
}
// Harness assigns this before the checks proceed past until(() => !!editor).
let editor!: ReturnType<typeof useInlineKnowledge>
function Harness({ enabled = true }: { enabled?: boolean }) {
  editor = useInlineKnowledge(server, enabled)
  return (
    <>
      <p>{editor.status}</p>
      {editor.feedback}
    </>
  )
}
const root = createRoot(document.getElementById('root')!)
const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
const render = (version: number, enabled = true) =>
  root.render(
    <QueryClientProvider client={client}>
      <Harness key={version} enabled={enabled} />
    </QueryClientProvider>,
  )
const pause = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
const check = (condition: unknown, message: string) => {
  if (!condition) throw new Error(message)
}
async function until(predicate: () => boolean) {
  for (let i = 0; i < 150; i++) {
    if (predicate()) return
    await pause(25)
  }
  throw new Error('Timed out waiting for editor state')
}
const passed: string[] = []
try {
  render(1)
  await until(() => !!editor)
  await pause(1000)
  check(requests.length === 0, 'Opening must not write')
  passed.push('Opening a document does not save unchanged content')
  editor.patch({ title: 'New title' })
  await until(() => editor.pending)
  editor.patch({ content: 'Typed during save' })
  await until(() => !editor.dirty && !editor.pending)
  check(
    server.metadata.title === 'New title' && server.content === 'Typed during save',
    'Queued typing was lost',
  )
  check(requests.length === 2 && requests[1].etag === 'v2', 'Queued save must use next revision')
  check(sessionStorage.getItem(key) === null, 'Saved draft should be cleared')
  passed.push('Typing during a save is queued with the updated revision; saved draft is cleared')
  editor.setComposing(true)
  editor.patch({ content: 'Chinese input in progress' })
  await pause(1100)
  check(requests.length === 2, 'Must not save during IME composition')
  editor.setComposing(false)
  await until(() => !editor.dirty && !editor.pending)
  passed.push('IME composition defers saving until composition ends')
  conflict = true
  editor.patch({ content: 'Local conflict draft' })
  await until(() => editor.status === '保存失败')
  const count = requests.length
  await pause(1100)
  check(
    requests.length === count && editor.draft.content === 'Local conflict draft',
    'Conflict must pause, keeping local content',
  )
  check(
    JSON.parse(sessionStorage.getItem(key)!).content === 'Local conflict draft',
    'Conflict draft must be persisted',
  )
  passed.push('Conflict pauses automatic retries and preserves the local draft')
  render(2)
  await pause(100)
  check(editor.draft.content === 'Local conflict draft', 'Remount must recover unsaved draft')
  passed.push('Navigating away and returning recovers the unsaved draft')
  render(3, false)
  await pause(1100)
  check(requests.length === count, 'Read-only document must not save')
  passed.push('Read-only mode never writes')
  root.unmount()
  sessionStorage.removeItem(key)
  document.body.innerText = `PASS (${passed.length})\n\n${passed.join('\n')}`
} catch (error) {
  root.unmount()
  sessionStorage.removeItem(key)
  document.body.innerText = `FAIL: ${error instanceof Error ? error.message : String(error)}\n\n${passed.join('\n')}`
}
