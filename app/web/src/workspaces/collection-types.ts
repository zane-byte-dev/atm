export type CollectionSource = {
  id: string
  connector: string
  kind: string
  external_id: string
  name?: string
  project?: string
  instruction?: string
  exclude_pattern?: string
  knowledge_collection?: string
  strategy: string
  decision_unit: string
  interval_minutes: number
  priority: string
  enabled: boolean
  muted: boolean
}

export type CollectionRun = {
  id: string
  source_id?: string
  connector: string
  status: string
  started_at: number
  finished_at?: number
  fetched_count: number
  analyzed_count: number
  failed_count: number
  error?: string
}

export type CollectionOverview = {
  enabled: boolean
  interval_minutes: number
  lookback_minutes: number
  message_retention_days: number
  worker_owned: boolean
  worker_status: string
  summary: {
    sources: number
    enabled_sources: number
    fetched_today: number
    insight_today: number
    created_today: number
    appended_today: number
    unread_count: number
    failed_today: number
    retry_stopped: number
  }
  sources: CollectionSource[]
  runs: CollectionRun[]
  messages: { total: number; conversations: number; newest?: number }
}

export type CollectionItem = {
  id: string
  source_id: string
  connector: string
  title: string
  summary: string
  sender?: string
  action: string
  proposed_action?: string
  status: string
  project?: string
  todo_id?: string
  todo_status?: string
  todo_archived?: boolean
  read_at: number
  archived_at: number
  occurred_at: number
  updated_at: number
  raw_context?: string
  reason?: string
  confidence?: number
  error?: string
  attempts?: number
  retry_stopped?: boolean
  knowledge_document_id?: string
  knowledge_collection?: string
  message_ids?: string[]
}

export type CollectionList = {
  items: CollectionItem[]
  total: number
  limit: number
  offset: number
}

export type CollectionMessage = {
  message_id: string
  sender?: string
  content: string
  created_at: number
  synced_at?: number
}

export type CollectionHistory = {
  source: CollectionSource
  messages: CollectionMessage[]
  local: true
  limit: number
}
