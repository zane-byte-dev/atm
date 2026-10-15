export type DaySummary = {
  day: string
  state: string
  title: string
  explanation: string
  badge_id: string
  origin: string
  session_count: number
  turn_count: number
  tool_calls: number
  event_count: number
  work_tokens: number
  active_seconds: number
  source_count: number
  generated_at: number
}
export type DayBadge = {
  id: string
  name: string
  description: string
  family: string
  kind: string
  level: number
  unlocked: boolean
  qualified_days: number
  qualified_dates: string[]
  next_level_days: number
  progress: number
  last_qualified?: string
  cooldown_until?: string
}
export type DayPrivacy = {
  semantic_enabled: boolean
  retention_days: number
  raw_content_retained: boolean
  sources: {
    source: string
    enabled: boolean
    semantic_enabled: boolean
    event_count: number
    last_event_at: number
  }[]
}
export type DaySnapshot = {
  from: string
  to: string
  today: string
  timezone: string
  indexed: boolean
  history: DaySummary[]
  atlas: { unlocked: number; total: number; badges: DayBadge[] } | null
  privacy: DayPrivacy | null
}
export type WorkspaceSettings = {
  revision: string
  owner_name: string
  timezone: string
  preferences: {
    session_sync_agents?: string
    grok_live_quota: boolean
    collection_enabled: boolean
    collection_interval_minutes: number
    collection_lookback_minutes: number
    collection_message_retention_days: number
    todo_refine_on_add: boolean
    todo_refine_prompt: string
  }
  model: {
    name: string
    source: string
    base_url: string
    credential_configured: boolean
    credential_status: 'configured' | 'missing' | 'unavailable'
  }
  providers: { name: string; kind: 'quota' | 'collection'; enabled: boolean }[]
  runtime: {
    mode: string
    version: string
    background_sync: boolean
    collection: boolean
    models: boolean
    agent_hooks: boolean
  }
  sync: {
    status: string
    run_status: string
    schema_version: number
    indexed_sessions: number
    retained_sessions: number
    last_attempt_at: string | null
    last_success_at: string | null
    age_seconds: number | null
    last_synced_files: number
    has_error: boolean
    indexed: boolean
  }
}
