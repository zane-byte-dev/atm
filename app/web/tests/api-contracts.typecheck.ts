import { call, callScopedTodoMutation, callTodoMutation } from '../src/api'
import type { ApiOutput, TodoMutationRequest } from '../src/api-contracts'
import type { TodoDetail, TodoList } from '../src/types'
import type { TaskPage, TaskWorkbenchMetadata } from '../src/task-query'
import type { KnowledgeDocument, KnowledgeQueryResult } from '../src/workspaces/knowledge-types'
import wire from './fixtures/api-wire-contracts.json'

// JSON imports widen literal strings. Widen only those enum leaves while keeping
// nullable values, required fields, arrays and nested shapes checked by tsc.
type WireShape<T> = T extends string
  ? string
  : T extends (infer Item)[]
    ? WireShape<Item>[]
    : T extends object
      ? { [K in keyof T]: WireShape<T[K]> }
      : T
const detailEmpty: WireShape<TodoDetail> = wire.todoDetailEmpty
const detailWithPlan: WireShape<TodoDetail> = wire.todoDetailWithPlan
const list: WireShape<TodoList> = wire.todoList
const metadata: WireShape<TaskWorkbenchMetadata> = wire.taskMetadata
const metadataEmpty: WireShape<TaskWorkbenchMetadata> = wire.taskMetadataEmpty
const page: WireShape<TaskPage> = wire.taskPage
const document: WireShape<KnowledgeDocument> = wire.knowledgeDocument
const catalog: WireShape<ApiOutput<'knowledge.catalog'>> = wire.knowledgeCatalog
const query: WireShape<KnowledgeQueryResult> = wire.knowledgeQuery
const queryEmpty: WireShape<KnowledgeQueryResult> = wire.knowledgeQueryEmpty
void [
  detailEmpty,
  detailWithPlan,
  list,
  metadata,
  metadataEmpty,
  page,
  document,
  catalog,
  query,
  queryEmpty,
]

// Type-check only: this function is deliberately never executed or bundled.
function calls() {
  const detail: Promise<TodoDetail> = call('todo.show', { todo_id: 't1' })
  const metadata: Promise<TaskWorkbenchMetadata> = call('todo.list', {
    workbench: true,
    metadata_only: true,
  })
  const page: Promise<TaskPage> = call('todo.list', { workbench: true, limit: 20, offset: 0 })
  const list: Promise<TodoList> = call('todo.list', { query: 'contract', limit: 20 })
  const query: Promise<KnowledgeQueryResult> = call('knowledge.query', {
    text: 'contract',
    cursor: 'opaque',
    limit: 20,
  })
  const document: Promise<KnowledgeDocument> = call('knowledge.document.get', {
    document_id: 'doc:1',
  })
  const jobs: Promise<{ jobs: unknown[] }> = call<{ jobs: unknown[] }>('jobs.list', {})
  callTodoMutation({
    method: 'todo.update',
    input: { todo_id: 't1', expected_etag: 'v1', title: 'New title' },
  })
  callScopedTodoMutation('t1', {
    method: 'todo.progress.append',
    input: { expected_etag: 'v1', message: 'Progress' },
  })
  void [detail, metadata, page, list, query, document, jobs]

  // @ts-expect-error Unknown method names cannot use the generic fallback.
  call('todo.typo', {})
  // @ts-expect-error Required identity is missing.
  call('todo.show', {})
  // @ts-expect-error Wrong field names are rejected.
  call('todo.show', { id: 't1' })
  // @ts-expect-error Wrong field type is rejected.
  call('todo.show', { todo_id: 1 })
  // @ts-expect-error A write must carry its optimistic revision.
  call('todo.update', { todo_id: 't1', title: 'stale' })
  const extraFields = { todo_id: 't1', path: '/not-a-browser-capability' }
  // @ts-expect-error Variable inputs cannot smuggle unknown fields either.
  call('todo.show', extraFields)
  // @ts-expect-error Catalog takes an empty browser request.
  call('knowledge.catalog', { path: '/private' })
  // @ts-expect-error Domain-only session identity is not a Web query capability.
  call('knowledge.query', { session_id: 'session:1' })
  // @ts-expect-error URL filters must be narrowed to supported document states.
  call('knowledge.query', { status: 'deleted' })
  // @ts-expect-error Knowledge update requires content and etag.
  call('knowledge.document.update', { document_id: 'doc:1', title: 'stale' })
  // @ts-expect-error Caller-selected output cannot bypass a mapped method.
  call<TodoDetail>('todo.show', {})
  // @ts-expect-error Caller-selected output cannot bypass a mapped knowledge method.
  call<KnowledgeDocument>('knowledge.document.get', {})
  // @ts-expect-error Typo cannot bypass even through explicit output type.
  call<unknown>('jobs.typo', {})
  const arbitraryMethod: string = 'todo.show'
  // @ts-expect-error A widened string needs narrowing to a registered method.
  call(arbitraryMethod, {})
  // @ts-expect-error Metadata calls do not masquerade as item pages.
  const wrongPage: Promise<TaskPage> = call('todo.list', { workbench: true, metadata_only: true })
  // @ts-expect-error Metadata requires the workbench mode discriminator.
  call('todo.list', { metadata_only: true })
  // @ts-expect-error Mutation requests preserve method/input correlation.
  const wrongMutation: TodoMutationRequest = { method: 'todo.update', input: { todo_id: 't1' } }
  // @ts-expect-error Scoped helpers do not silently accept an overriding todo ID.
  callScopedTodoMutation('t1', { method: 'todo.archive', input: { todo_id: 't2' } })
  // @ts-expect-error Read methods cannot use the mutation helper.
  callTodoMutation({ method: 'todo.show', input: { todo_id: 't1' } })
  const ambiguousMethod = Math.random() > 0.5 ? 'todo.show' : 'todo.update'
  // @ts-expect-error Dynamic methods need a discriminated request to preserve required inputs.
  call(ambiguousMethod, { todo_id: 't1' })
  void [wrongPage, wrongMutation]
}
void calls
