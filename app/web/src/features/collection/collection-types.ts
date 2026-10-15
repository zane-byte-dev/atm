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

/**
 * The same projection `atm collect status` prints. `status` is one of
 * ready / flaky / auth_required / permission_required / error / not_checked;
 * the recent_* window is the last 20 runs, which is what the ledger page holds.
 */
export type CollectionConnectorHealth = {
  connector: string
  status: string
  error?: string
  login_command?: string
  checked_at?: number
  consecutive_failures?: number
  recent_runs?: number
  recent_failures?: number
}

/**
 * Run totals for one connector in one window. The unit is a collection run, not
 * an external CLI call: one dingtalk run issues several `dws` calls and the
 * connector protocol does not report how many, so this is the volume ATM
 * actually observed rather than an invented call count.
 */
export type CollectionRunWindow = {
  runs: number
  failed: number
  fetched: number
  last_run_at?: number
  last_failed_at?: number
}

export type CollectionConnectorUsage = {
  health: CollectionConnectorHealth
  today: CollectionRunWindow
  week: CollectionRunWindow
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
  /** Optional because a dev UI served against an older binary will not have it. */
  connector_usage?: CollectionConnectorUsage[]
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
