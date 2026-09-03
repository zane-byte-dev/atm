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
export type DayEvidence = {
  metric: string
  value: number
  unit?: string
  comparison?: string
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
export type DayResult = {
  day: string
  state: string
  timezone: string
  features: {
    session_count: number
    event_count: number
    turn_count: number
    tool_calls: number
    source_count: number
    input_tokens: number
    output_tokens: number
    cache_create_tokens: number
    cache_read_tokens: number
    active_seconds: number
    foreground_seconds: number
    background_seconds: number
    generation_seconds: number
    semantic_counts?: Record<string, number>
    modality_counts?: Record<string, number>
  }
  concept?: {
    id: string
    title: string
    explanation: string
    tags: string[]
    evidence: DayEvidence[]
    confidence: number
    evidence_strength: number
    origin: string
    computed_title?: string
  }
  badge?: DayBadge
  baseline_days: number
  provisional: boolean
  percentiles?: Record<string, number>
  coverage?: {
    complete: boolean
    expected_sources: number
    present_sources: number
    missing_sources?: string[]
    data_through: number
  }
  feedback?: { verdict: string; corrected_badge_id?: string; updated_at: number }
  generated_at: number
}
export type DayLedger = {
  day: string
  total: number
  limit: number
  offset: number
  items: {
    occurred_at: number
    source: string
    event_type: string
    quantity: number
    modality: string
    execution_mode: string
    input_tokens: number
    output_tokens: number
    cache_create_tokens: number
    cache_read_tokens: number
    duration_ms: number
  }[]
}
export type WorkspaceSettings = {
  owner_name: string
  timezone: string
  preferences: {
    grok_live_quota: boolean
    collection_enabled: boolean
    collection_interval_minutes: number
    collection_lookback_minutes: number
    collection_message_retention_days: number
    todo_refine_on_add: boolean
  }
  model: {
    name: string
    source: string
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
