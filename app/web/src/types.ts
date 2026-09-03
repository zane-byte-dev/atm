export type TodoStatus = 'open' | 'in_progress' | 'review' | 'done'
export type Todo = {
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
  latest_plan?: { explanation?: string; items: { step: string; status: string }[] }
  sessions?: { session_id: string; agent?: string; summary?: string }[]
  summary?: { sessions: number; queries: number; tool_calls: number }
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
  etag: string
  operationID: string
  // The fields represented by etag, before local edits. Older drafts may lack
  // this snapshot; those must ask about every differing field when merging.
  base?: EditableFields
}
