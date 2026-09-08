// Real lazy workspaces with fixed responses for layout and navigation regression checks.
import { lazy, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ListTodo } from 'lucide-react'
import { AppShell } from '../../src/workspace-shell'
import { WorkspaceTitle } from '../../src/workspace-title'
import type { Bootstrap } from '../../src/types'
import '../../src/task-workbench.css'
import '../../src/app.css'

const Settings = lazy(() =>
  import('../../src/workspaces/aiday-settings').then((module) => ({
    default: module.SettingsWorkspace,
  })),
)
const Agents = lazy(() =>
  import('../../src/workspaces/activity').then((module) => ({ default: module.AgentsWorkspace })),
)
const Usage = lazy(() =>
  import('../../src/workspaces/usage').then((module) => ({ default: module.UsageWorkspace })),
)
const boot = {
  version: 'layout-check',
  capabilities: { todo_write: false, workspace_write: false },
} as Bootstrap
const timestamp = '2026-09-08T08:00:00Z'
const metrics = {
  sessions: 2,
  total_tokens: 3200,
  total_input_tokens: 2200,
  fresh_input_tokens: 1200,
  output_tokens: 1000,
  cache_read_tokens: 1000,
  cache_create_tokens: 0,
  requests: 3,
  cost_usd: 0.04,
  estimated_cost_usd: 0,
}
const range = {
  start_date: '2026-09-08',
  end_date: '2026-09-08',
  model_stats: [{ ...metrics, client: 'codex', model: 'test-model' }],
  project_stats: [{ ...metrics, agent: 'codex', project: 'atm', queries: 2, tool_calls: 3 }],
  skill_stats: [],
  speed: { models: [], turns: [], untimed_requests: 0, out_of_window_requests: 0 },
  quality: {
    active_sessions: 2,
    token_sessions: 2,
    session_coverage_percent: 100,
    request_coverage_percent: 100,
    speed_sample_percent: 100,
    estimated_cost_share: 0,
    cost_usd: 0.04,
    estimated_cost_usd: 0,
    pricing_sources: [],
  },
}
const responses: Record<string, unknown> = {
  'settings.get': {
    revision: 'fixture',
    owner_name: 'Layout check',
    timezone: 'Asia/Shanghai',
    preferences: {
      grok_live_quota: false,
      collection_enabled: true,
      collection_interval_minutes: 10,
      collection_lookback_minutes: 30,
      collection_message_retention_days: 7,
      todo_refine_on_add: false,
      todo_refine_prompt: '',
    },
    model: {
      name: 'test-model',
      source: 'fixture',
      base_url: 'https://example.test',
      credential_configured: false,
      credential_status: 'missing',
    },
    providers: [],
    runtime: {
      mode: 'local',
      version: 'layout-check',
      background_sync: true,
      collection: true,
      models: false,
      agent_hooks: true,
    },
    sync: {
      status: 'fresh',
      run_status: 'idle',
      schema_version: 57,
      indexed_sessions: 2,
      retained_sessions: 2,
      last_attempt_at: timestamp,
      last_success_at: timestamp,
      age_seconds: 0,
      last_synced_files: 2,
      has_error: false,
      indexed: true,
    },
  },
  'session.status': {
    source: 'fixture',
    generated_at: timestamp,
    missing_index: false,
    health: {
      status: 'fresh',
      schema_version: 57,
      indexed_sessions: 2,
      retained_sessions: 2,
      last_success_at: timestamp,
    },
    agents: [{ agent: 'codex', sessions: 2 }],
    projects: ['atm'],
    bindings: [],
  },
  'session.list': {
    sessions: [
      {
        id: 's1',
        short_id: 's1',
        agent: 'codex',
        project: 'atm',
        created_at: timestamp,
        last_at: timestamp,
        q_count: 2,
        summary: '核对工作区布局',
        latest_progress: '已核对桌面布局',
      },
    ],
    total: 1,
    limit: 30,
    offset: 0,
  },
  'session.children': { sessions: [], total: 0, offset: 0, limit: 20 },
  'usage.snapshot': {
    generated_at: timestamp,
    ranges: { today: range, last_7_days: range },
    day_stats: [{ ...metrics, date: '2026-09-08', queries: 2 }],
    hour_stats: [{ ...metrics, date: '2026-09-08T08:00:00', queries: 2 }],
  },
  'quota.cached': {
    source: 'fixture',
    generated_at: timestamp,
    windows: [
      {
        agent: 'codex',
        window_minutes: 300,
        used_percent: 42,
        resets_at: 0,
        observed_at: timestamp,
        stale: false,
        reset_elapsed: false,
      },
    ],
  },
  'day.snapshot': {
    from: '2026-09-01',
    to: '2026-09-30',
    today: '2026-09-08',
    timezone: 'Asia/Shanghai',
    indexed: true,
    history: [],
    atlas: { unlocked: 0, total: 0, badges: [] },
    privacy: null,
  },
  'jobs.list': { jobs: [] },
}
window.fetch = async (url) => {
  const method = String(url).split('/').at(-1) || ''
  if (!(method in responses)) throw new Error(`Unexpected fixture request: ${method}`)
  return new Response(JSON.stringify({ data: responses[method] }))
}

function Harness() {
  const params = new URLSearchParams(location.search)
  const scene = params.get('scene') || 'settings'
  return (
    <AppShell boot={boot} className={scene === 'detail' ? 'task-view-kanban has-selection' : ''}>
      <Suspense fallback={<p>Loading fixture…</p>}>
        {scene === 'settings' ? (
          <Settings boot={boot} />
        ) : scene === 'agents' ? (
          <Agents boot={boot} />
        ) : scene === 'usage' ? (
          <Usage boot={boot} />
        ) : (
          <div className="content-columns">
            <section className="task-workbench">
              <header className="workbench-heading">
                <WorkspaceTitle icon={ListTodo}>任务</WorkspaceTitle>
              </header>
            </section>
            <section className={`detail-column ${params.has('expanded') ? 'detail-expanded' : ''}`}>
              <div className="detail-scroll">
                <article className="task-detail">
                  <header className="task-detail-header">
                    <h2 className="task-title">布局回归任务</h2>
                  </header>
                  <div className="task-detail-content">
                    <div className="task-reading-surface">
                      {Array.from({ length: 30 }, (_, index) => (
                        <p key={index}>第 {index + 1} 行详情内容。</p>
                      ))}
                    </div>
                  </div>
                </article>
              </div>
              <aside className="task-inspector">
                <h3>任务证据</h3>
                <p>2 个相关会话</p>
              </aside>
            </section>
          </div>
        )}
      </Suspense>
    </AppShell>
  )
}
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <Harness />
    </QueryClientProvider>
  </BrowserRouter>,
)
