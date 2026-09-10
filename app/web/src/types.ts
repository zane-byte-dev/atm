export type OperationWarning = { code: string; message: string }
export type TodoStatus = 'open' | 'in_progress' | 'review' | 'done'
export type Todo = {
  updated_at?: number
  id: string
  title: string
  status: TodoStatus
  priority: string
  project?: string
  created: string
  archived?: boolean
  description?: string
  creator?: string
  wake_condition?: string
  review_at?: string
  depends_on?: string[]
  tags?: string[]
  closed?: string | null
  closed_reason?: string | null
  links?: { url: string; title?: string; kind?: string }[]
  images?: { name: string; url?: string; media_type?: string }[]
}
export type TodoList = {
  items: Todo[]
  total: number
  limit: number
  offset: number
  projects?: string[]
  counts?: Record<string, number>
}
export type TodoDetail = {
  todo: Todo
  etag: string
  latest_plan?: {
    revision: number
    explanation?: string
    items: { step: string; status: string }[]
  } | null
  bindings?: {
    session_id: string
    todo_id: string
    agent?: string
    project?: string
    cwd?: string
    bound_at: number
    unbound_at?: number
    reason?: string
  }[]
  sessions?: {
    session_id: string
    indexed_id?: string
    short_id: string
    agent: string
    project: string
    summary?: string
    latest_result?: string
    indexed: boolean
    cwd?: string
    started_at?: number
    last_at?: number
    binding_count: number
    first_bound_at: number
    bound_at: number
    unbound_at?: number
    reason?: string
    queries: number
    tool_calls: number
    input_tokens: number
    output_tokens: number
    cost_usd: number
  }[]
  summary?: { sessions: number; queries: number; tool_calls: number; cost_usd: number } | null
}
export type Bootstrap = {
  csrf_token: string
  version: string
  mode: string
  capabilities?: { todo_write?: boolean; workspace_write?: boolean; [key: string]: unknown }
}
export type EditableFields = {
  title: string
  description: string
  project: string
  priority: string
}
export type Draft = EditableFields & {
  creation?: { id: string; etag: string; uploaded: Record<string, string> }
  etag: string
  operationID: string
  // The fields represented by etag, before local edits. Older drafts may lack
  // this snapshot; those must ask about every differing field when merging.
  base?: EditableFields
}
