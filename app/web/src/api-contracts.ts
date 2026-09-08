import type { OperationWarning, Todo, TodoDetail, TodoList } from './types'
import type { TaskGroupBy, TaskPage, TaskWorkbenchInput, TaskWorkbenchMetadata } from './task-query'
import type {
  KnowledgeCollection,
  KnowledgeDocument,
  KnowledgeQueryInput,
  KnowledgeQueryResult,
} from './workspaces/knowledge-types'
import type { RuntimeJob } from './workspaces/runtime-jobs'

type Contract<Input, Output> = { input: Input; output: Output }
type TodoIdentity = { todo_id: string }
type TodoRevision = TodoIdentity & { expected_etag: string }
export type TodoMutationResult = {
  todo: Todo
  etag: string
  replayed?: boolean
  refinement_job?: RuntimeJob
  warnings?: OperationWarning[]
}
export type TodoPlan = NonNullable<TodoDetail['latest_plan']>
export type TodoLegacyListInput = {
  workbench?: false
  status?: string
  query?: string
  project?: string
  limit?: number
  offset?: number
}
type WorkbenchQuery = Partial<Omit<TaskWorkbenchInput, 'workbench' | 'group_by'>> & {
  workbench: true
  group_by?: TaskGroupBy
  group_key?: string
  partition?: 'active' | 'done' | ''
  limit?: number
  offset?: number
}
export type TodoMetadataInput = WorkbenchQuery & { metadata_only: true }
export type TodoPageInput = WorkbenchQuery & { metadata_only?: false }
export type KnowledgeCreateInput = {
  title: string
  content: string
  collection: string
  domains?: string[]
  tags?: string[]
  projects?: string[]
}
export type KnowledgeUpdateInput = {
  document_id: string
  etag: string
  content: string
  title?: string
  collection?: string
  status?: string
  domains?: string[]
  tags?: string[]
  projects?: string[]
}

/** Browser vocabulary mirrors apphost's input DTOs, not CLI/domain privileges. */
export type ApiContracts = {
  'todo.list': Contract<
    TodoLegacyListInput | TodoMetadataInput | TodoPageInput,
    TodoList | TaskWorkbenchMetadata | TaskPage
  >
  'todo.show': Contract<TodoIdentity, TodoDetail>
  'todo.doc': Contract<TodoIdentity, { exists: boolean; content: string }>
  'todo.create': Contract<
    { title: string; description?: string; project?: string; priority?: string },
    TodoMutationResult
  >
  'todo.update': Contract<
    TodoRevision & { title?: string; description?: string; project?: string; priority?: string },
    TodoMutationResult
  >
  'todo.start': Contract<TodoIdentity & { reopen_reason?: string }, TodoMutationResult>
  'todo.done': Contract<TodoIdentity & { reason?: string }, TodoMutationResult>
  'todo.archive': Contract<TodoIdentity, TodoMutationResult>
  'todo.restore': Contract<TodoIdentity, TodoMutationResult>
  'todo.plan.set': Contract<
    TodoRevision & {
      base_revision: number
      explanation?: string
      items: { step: string; status: string }[]
    },
    TodoMutationResult & { plan: TodoPlan }
  >
  'todo.progress.append': Contract<TodoRevision & { message: string }, TodoMutationResult>
  'todo.dependency.add': Contract<TodoRevision & { dependency_id: string }, TodoMutationResult>
  'todo.dependency.remove': Contract<TodoRevision & { dependency_id: string }, TodoMutationResult>
  'todo.dependency.list': Contract<
    TodoIdentity,
    {
      todo_id: string
      dependencies: { id: string; title?: string; status: string; met: boolean }[]
    }
  >
  'todo.wait.update': Contract<
    TodoRevision & { wake_condition: string; review_at: string },
    TodoMutationResult
  >
  'todo.wake': Contract<TodoRevision & { reason: string }, TodoMutationResult>
  'knowledge.catalog': Contract<Record<string, never>, KnowledgeCollection[]>
  'knowledge.query': Contract<KnowledgeQueryInput, KnowledgeQueryResult>
  'knowledge.document.get': Contract<{ document_id: string }, KnowledgeDocument>
  'knowledge.document.create': Contract<KnowledgeCreateInput, KnowledgeDocument>
  'knowledge.document.update': Contract<KnowledgeUpdateInput, KnowledgeDocument>
  'knowledge.collection.create': Contract<
    { id: string; name?: string; description?: string },
    KnowledgeCollection
  >
}
export type ApiMethod = keyof ApiContracts
export type ApiInput<M extends ApiMethod> = ApiContracts[M]['input']
export type ApiOutput<M extends ApiMethod> = ApiContracts[M]['output']
export type TodoMutationMethod = Exclude<
  Extract<ApiMethod, `todo.${string}`>,
  'todo.list' | 'todo.show' | 'todo.doc' | 'todo.dependency.list'
>
export type TodoMutationRequest = {
  [M in TodoMutationMethod]: { method: M; input: ApiInput<M> }
}[TodoMutationMethod]
export type TodoScopedMutationRequest = {
  [M in Exclude<TodoMutationMethod, 'todo.create'>]: {
    method: M
    input: Omit<ApiInput<M>, 'todo_id'> & { todo_id?: never }
  }
}[Exclude<TodoMutationMethod, 'todo.create'>]

/** Temporary output-generic boundary for registered methods not yet mapped above. */
export type LegacyApiMethod =
  | 'jobs.run'
  | 'jobs.list'
  | 'jobs.show'
  | 'jobs.cancel'
  | 'presence.snapshot'
  | 'session.list'
  | 'session.search'
  | 'session.show'
  | 'session.status'
  | 'usage.snapshot'
  | 'quota.cached'
  | 'memory.recall'
  | 'memory.get'
  | 'memory.create'
  | 'memory.supersede'
  | 'collect.overview'
  | 'collect.items'
  | 'collect.item.show'
  | 'collect.history'
  | 'collect.item.read'
  | 'collect.item.archive'
  | 'collect.source.enabled'
  | 'collect.source.muted'
  | 'collect.source.save'
  | 'collect.source.delete'
  | 'collect.source.search'
  | 'day.snapshot'
  | 'day.show'
  | 'day.ledger'
  | 'settings.get'
  | 'settings.preferences.save'
  | 'settings.business.save'
  | 'settings.credential.save'
  | 'settings.credential.delete'
