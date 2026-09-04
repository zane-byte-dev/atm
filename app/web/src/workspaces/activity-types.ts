export type SessionSummary = {
  id: string
  short_id: string
  agent: string
  project: string
  created_at: string
  last_at?: string
  q_count: number
  summary?: string
  first_q?: string
  latest_progress?: string
  final_result?: string
  content_state?: string
  result_status?: string
  is_subagent?: boolean
  agent_nickname?: string
  parent_session_id?: string
}
export type SessionList = {
  sessions: SessionSummary[]
  total: number
  limit: number
  offset: number
}
export type SessionSearch = {
  matches: (Pick<SessionSummary, 'id' | 'short_id' | 'agent' | 'project' | 'created_at'> & {
    role: string
    content: string
  })[]
  total: number
  returned: number
  truncated: boolean
  limit: number
  offset: number
}
export type SessionDetail = Omit<SessionSummary, 'short_id' | 'created_at' | 'q_count'> & {
  qa: { turn: number; q?: string; a?: string; progress?: string[] }[]
  tools: Record<string, number> | null
  total_turns: number
  returned_turns: number
  truncated: boolean
  content_truncated: boolean
  offset: number
  limit: number
}
export type SessionStatus = {
  agent_hooks?: boolean
  presence?: {
    owner: string
    active_count: number
    attention_count: number
    last_event_at?: string
    sessions: {
      id: string
      session_id?: string
      resume_id?: string
      source: string
      state: string
      hook_backed: boolean
      attention?: { reason: string; tool?: string; text?: string }
      updated_at: string
    }[]
  }
  source: string
  generated_at: string
  missing_index: boolean
  health: {
    status: string
    schema_version: number
    indexed_sessions: number
    retained_sessions: number
    last_success_at?: string
    last_error?: string
  }
  agents: { agent: string; sessions: number; latest_at?: string }[]
  projects: string[]
  bindings: {
    state: string
    binding: {
      session_id: string
      todo_id: string
      agent?: string
      project?: string
      bound_at: number
    }
    todo?: { id: string; title: string; status: string }
  }[]
}
export type MetricFields = {
  sessions: number
  total_tokens: number
  total_input_tokens: number
  fresh_input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_create_tokens: number
  requests: number
  cost_usd: number
  estimated_cost_usd: number
  cost_estimated?: boolean
}
export type ProjectStats = MetricFields & {
  project: string
  agent: string
  queries: number
  tool_calls: number
}
export type ModelStats = MetricFields & { client: string; model: string }
export type DayStats = MetricFields & { date: string; queries: number }
export type UsageRange = {
  start_date: string
  end_date: string
  project_stats: ProjectStats[]
  model_stats: ModelStats[]
  skill_stats: { skill: string; calls: number; sessions: number; agents: number }[]
  speed: {
    models: {
      client: string
      model: string
      requests: number
      sampled: number
      tokens_per_second_weighted: number
      tokens_per_second_p50: number
      duration_p50_seconds: number
    }[]
    turns: {
      agent: string
      turns: number
      wait_p50_seconds: number
      wait_p90_seconds: number
      requests_per_turn: number
    }[]
    untimed_requests: number
    out_of_window_requests: number
  }
  quality: {
    active_sessions: number
    token_sessions: number
    session_coverage_percent: number
    request_coverage_percent: number
    speed_sample_percent: number
    estimated_cost_share: number
    cost_usd: number
    estimated_cost_usd: number
    pricing_sources: string[]
  }
}
export type UsageSnapshot = {
  generated_at: string
  ranges: Record<string, UsageRange>
  day_stats: DayStats[]
  hour_stats: DayStats[]
  model_day_stats?: (MetricFields & { date: string; model: string; client: string })[]
  model_hour_stats?: (MetricFields & { date: string; model: string; client: string })[]
  project_day_stats?: (MetricFields & { date: string; project: string; client: string })[]
  project_hour_stats?: (MetricFields & { date: string; project: string; client: string })[]
}
export type CachedQuota = {
  source: string
  generated_at: string
  windows: {
    agent: string
    window_minutes: number
    used_percent: number
    resets_at: number
    observed_at: string
    stale: boolean
    reset_elapsed: boolean
  }[]
}
