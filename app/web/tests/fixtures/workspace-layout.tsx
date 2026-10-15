// Real lazy workspaces with fixed responses for layout and navigation regression checks.
import { lazy, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, useLocation } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ListTodo } from 'lucide-react'
import { AppShell } from '../../src/workspace-shell'
import { WorkspaceNavigatorHeading } from '../../src/components/workspace-navigator'
import type { Bootstrap } from '../../src/types'
import '../../src/features/tasks/task-workbench.css'
import '../../src/app.css'
import { RuntimeJobNotifications } from '../../src/features/notifications/runtime-job-notifications'
import { MessagesWorkspace } from '../../src/features/notifications/messages'
import { DesignSystemProvider } from '../../src/design-system'

const Settings = lazy(() =>
  import('../../src/features/settings/aiday-settings').then((module) => ({
    default: module.SettingsWorkspace,
  })),
)
const Agents = lazy(() =>
  import('../../src/features/agents/activity').then((module) => ({ default: module.AgentsWorkspace })),
)
const Usage = lazy(() =>
  import('../../src/features/usage/usage').then((module) => ({ default: module.UsageWorkspace })),
)
const Knowledge = lazy(() =>
  import('../../src/features/knowledge/knowledge').then((module) => ({
    default: module.KnowledgeWorkspace,
  })),
)
const Collection = lazy(() =>
  import('../../src/features/collection/collection').then((module) => ({
    default: module.CollectionWorkspace,
  })),
)
const inboxItems = [
  {
    id: 'ci_layout',
    source_id: 'cs_layout',
    connector: 'dingtalk',
    title: '核对收件箱布局',
    summary: '布局记录',
    sender: '测试用户',
    action: 'insight',
    status: 'succeeded',
    read_at: 0,
    archived_at: 0,
    occurred_at: 1788854400,
    updated_at: 1788854400,
  },
  {
    id: 'ci_long',
    source_id: 'cs_layout',
    connector: 'dingtalk',
    title: '长标题'.repeat(35),
    summary: '',
    action: 'insight',
    status: 'failed',
    read_at: 1788854400,
    archived_at: 0,
    occurred_at: 1788854400,
    updated_at: 1788854400,
  },
]
const boot = {
  version: 'layout-check',
  capabilities: {
    todo_write: false,
    workspace_write: new URLSearchParams(location.search).has('interactive'),
    runtime_jobs: new URLSearchParams(location.search).has('interactive'),
  },
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
  'session.show': {
    id: 's1',
    agent: 'codex',
    project: 'atm',
    agent_nickname: '布局会话',
    result_status: 'success',
    qa: [{ turn: 1, q: '三栏是否对齐？', a: '列宽和内容起点已统一。' }],
    tools: {},
    total_turns: 1,
    returned_turns: 1,
    truncated: false,
    content_truncated: false,
    offset: 0,
    limit: 10,
  },
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
  'knowledge.catalog': new URLSearchParams(location.search).has('interactive')
    ? [{ id: 'test', name: 'AI Agent', document_count: 6, description: '' }]
    : [],
  'knowledge.query': {
    documents: [
      {
        document_id: 'doc1',
        title: '布局记录',
        collection: 'test',
        status: 'active',
        updated_at: timestamp,
      },
    ],
    total: 1,
    has_more: false,
  },
  'knowledge.document.get': {
    etag: 'v1',
    editable: false,
    collection: 'test',
    metadata: {
      id: 'doc1',
      title: '布局记录',
      status: 'active',
      producer: 'test',
      createdAt: timestamp,
      updatedAt: timestamp,
    },
    content: '布局测试正文',
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
  'collect.overview': {
    enabled: true,
    interval_minutes: 5,
    lookback_minutes: 30,
    message_retention_days: 7,
    worker_owned: true,
    worker_status: 'running',
    summary: {
      sources: 1,
      enabled_sources: 1,
      fetched_today: 2,
      insight_today: 2,
      created_today: 0,
      appended_today: 0,
      unread_count: 1,
      failed_today: 0,
      retry_stopped: 0,
    },
    sources: [
      {
        id: 'cs_layout',
        name: '布局来源'.repeat(15),
        connector: 'dingtalk',
        kind: 'group',
        external_id: 'layout',
        strategy: 'tasks',
        decision_unit: 'window',
        interval_minutes: 5,
        priority: 'P1',
        enabled: true,
        muted: false,
      },
    ],
    runs: [],
    messages: { total: 2, conversations: 1 },
    connector_usage: [],
  },
  'collect.items': { items: inboxItems, total: 2, limit: 50, offset: 0 },
  'collect.item.show': { item: inboxItems[0] },
  'jobs.list': { jobs: [] },
}
if (new URLSearchParams(location.search).has('badges')) {
  const families = ['crystal', 'lens', 'orbit', 'grid']
  const days = [1, 2, 3, 5, 6, 8, 9, 10, 11, 14, 16, 17, 19, 21, 22, 23, 25, 27, 29]
  responses['day.snapshot'] = {
    from: '2026-09-01',
    to: '2026-09-30',
    today: '2026-10-03',
    indexed: true,
    history: days.map((day, index) => ({
      day: `2026-09-${String(day).padStart(2, '0')}`,
      badge_id: families[index % 4],
      title: ['深度共创', '细节显微镜', '自动驾驶', '代码架构师'][index % 4],
    })),
    atlas: {
      unlocked: 4,
      total: 4,
      badges: families.map((family) => ({ id: family, family, name: family })),
    },
  }
}
let jobStarted = 0
let jobSequence = 0
const holdJob = new URLSearchParams(location.search).has('stayRunning')
const failJob = new URLSearchParams(location.search).has('failJob')
window.fetch = async (url) => {
  const method = String(url).split('/').at(-1) || ''
  if (method === 'jobs.cancel') {
    const current = (responses['jobs.list'] as { jobs: Record<string, unknown>[] }).jobs[0]
    const canceled = { ...current, status: 'canceled', phase: '' }
    responses['jobs.list'] = { jobs: [canceled] }
    jobStarted = 0
    return new Response(JSON.stringify({ data: canceled }))
  }
  if (method === 'jobs.run') {
    jobStarted = Date.now()
    jobSequence++
    responses['jobs.list'] = {
      jobs: [
        {
          id: `fixture-job-${jobSequence}`,
          kind: 'quota.refresh',
          status: 'queued',
          phase: '等待额度服务',
          created_at: jobStarted,
        },
      ],
    }
    return new Response(
      JSON.stringify({ data: (responses['jobs.list'] as { jobs: unknown[] }).jobs[0] }),
    )
  }
  if (method === 'jobs.list' && !holdJob && jobStarted && Date.now() - jobStarted > 1800)
    responses['jobs.list'] = {
      jobs: [
        {
          id: `fixture-job-${jobSequence}`,
          kind: 'quota.refresh',
          status: failJob ? 'failed' : 'succeeded',
          phase: '',
          created_at: jobStarted,
          ...(failJob ? { error: { code: 'fixture', message: '模拟额度服务不可用' } } : {}),
        },
      ],
    }
  if (!(method in responses)) throw new Error(`Unexpected fixture request: ${method}`)
  return new Response(JSON.stringify({ data: responses[method] }))
}

const initialScene = new URLSearchParams(location.search).get('scene') || 'settings'
function Harness() {
  const routeLocation = useLocation()
  const params = new URLSearchParams(routeLocation.search)
  const scene =
    routeLocation.pathname === '/messages'
      ? 'messages'
      : routeLocation.pathname === '/settings'
        ? 'settings'
        : params.get('scene') || initialScene
  return (
    <>
      <RuntimeJobNotifications boot={boot} />
      <AppShell boot={boot} className={scene === 'detail' ? 'task-view-kanban has-selection' : ''}>
        <Suspense fallback={<p>Loading fixture…</p>}>
          {scene === 'messages' ? (
            <MessagesWorkspace boot={boot} />
          ) : scene === 'settings' ? (
            <Settings boot={boot} />
          ) : scene === 'agents' ? (
            <Agents boot={boot} />
          ) : scene === 'usage' ? (
            <Usage boot={boot} />
          ) : scene === 'collection' ? (
            <Collection boot={boot} />
          ) : scene === 'knowledge' ? (
            <Knowledge boot={boot} />
          ) : (
            <div className="content-columns">
              <section className="task-workbench">
                <WorkspaceNavigatorHeading
                  className="workbench-heading"
                  icon={ListTodo}
                  title="任务"
                />
              </section>
              <section
                className={`detail-column ${params.has('expanded') ? 'detail-expanded' : ''}`}
              >
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
    </>
  )
}
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <DesignSystemProvider>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <Harness />
      </QueryClientProvider>
    </DesignSystemProvider>
  </BrowserRouter>,
)
