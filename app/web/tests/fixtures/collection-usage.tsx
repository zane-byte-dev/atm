// Isolated fixture: the collection workspace renders against an in-memory
// connector ledger, never real ATM data. The payload uses the shape the Go read
// model emits, so the card cannot pass here and then fail against a server.
// Pick a scenario with ?scenario=0 (healthy) or ?scenario=1 (expired login).
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { CollectionWorkspace } from '../../src/features/collection/collection'
import type { CollectionConnectorHealth, CollectionOverview } from '../../src/features/collection/collection-types'
import type { Bootstrap } from '../../src/types'
import '../../src/app.css'
import { DesignSystemProvider } from '../../src/design-system'

const boot: Bootstrap = { csrf_token: 'fixture', version: 'fixture', mode: 'fixture' }

const source = {
  id: 'cs_fixture',
  connector: 'dingtalk',
  kind: 'group',
  external_id: 'fixture-group',
  name: 'Fixture 群',
  project: 'atm',
  strategy: 'tasks',
  decision_unit: 'window',
  interval_minutes: 5,
  priority: 'P1',
  enabled: true,
  muted: false,
}

const health: CollectionConnectorHealth[] = [
  {
    connector: 'dingtalk',
    status: 'ready',
    checked_at: 1790779800,
    recent_runs: 20,
    recent_failures: 6,
    login_command: '/Users/example/.qoderwork/bin/dws auth login',
  },
  {
    connector: 'dingtalk',
    status: 'auth_required',
    error:
      'collection connector dingtalk fetch: dws returned an error: 未登录，请先执行 dws auth login',
    checked_at: 1790779800,
    consecutive_failures: 4,
    recent_runs: 20,
    recent_failures: 4,
    login_command: '/Users/example/.qoderwork/bin/dws auth login',
  },
]

function overview(connector: CollectionConnectorHealth): CollectionOverview {
  return {
    enabled: true,
    interval_minutes: 5,
    lookback_minutes: 30,
    message_retention_days: 90,
    worker_owned: true,
    worker_status: 'running',
    summary: {
      sources: 1,
      enabled_sources: 1,
      fetched_today: 1278,
      insight_today: 5,
      created_today: 0,
      appended_today: 0,
      unread_count: 4,
      failed_today: 14,
      retry_stopped: 0,
    },
    sources: [source],
    runs: [
      {
        id: 'cr_1',
        source_id: source.id,
        connector: 'dingtalk',
        status: 'succeeded',
        started_at: 1790779799,
        finished_at: 1790779800,
        fetched_count: 9,
        analyzed_count: 9,
        failed_count: 0,
      },
    ],
    messages: { total: 28967, conversations: 34, newest: 1790779799 },
    connector_usage: [
      {
        health: connector,
        today: {
          runs: 251,
          failed: 14,
          fetched: 1278,
          last_run_at: 1790779799,
          last_failed_at: 1790778745,
        },
        week: {
          runs: 484,
          failed: 45,
          fetched: 26503,
          last_run_at: 1790779799,
          last_failed_at: 1790778745,
        },
      },
    ],
  }
}

const scenario = Number(new URLSearchParams(location.search).get('scenario') || '0')
const editingSource = scenario === 2
let deleteCalls = 0
Object.assign(window, { fixtureDeleteCalls: () => deleteCalls })

window.fetch = async (url) => {
  const method = String(url).split('/').pop()
  if (method === 'collect.overview')
    return new Response(JSON.stringify({ data: overview(health[editingSource ? 0 : scenario]) }))
  if (method === 'collect.items')
    return new Response(JSON.stringify({ data: { items: [], total: 0, limit: 50, offset: 0 } }))
  if (method === 'settings.get') return new Response(JSON.stringify({ data: { providers: [] } }))
  if (method === 'knowledge.catalog') return new Response(JSON.stringify({ data: [] }))
  if (method === 'collect.source.delete') {
    deleteCalls += 1
    return new Response(JSON.stringify({ data: {} }))
  }
  throw new Error(`Unexpected fixture method: ${method}`)
}

createRoot(document.getElementById('root')!).render(
  <DesignSystemProvider>
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <MemoryRouter
        initialEntries={[
          editingSource
            ? '/collection?view=sources&panel=edit-source&source=cs_fixture'
            : '/collection?view=sources&panel=overview',
        ]}
      >
        <CollectionWorkspace
          boot={editingSource ? { ...boot, capabilities: { workspace_write: true } } : boot}
        />
      </MemoryRouter>
    </QueryClientProvider>
  </DesignSystemProvider>,
)
