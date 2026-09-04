import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import {
  ArrowLeft,
  ArrowRight,
  Bot,
  Clock3,
  MessageSquare,
  RefreshCw,
  Search,
  X,
} from 'lucide-react'
import { call } from '../api'
import { Markdown, Notice } from '../editor'
import type { Bootstrap } from '../types'
import type {
  CachedQuota,
  DayStats,
  MetricFields,
  SessionDetail,
  SessionList,
  SessionSearch,
  SessionStatus,
  SessionSummary,
  UsageRange,
  UsageSnapshot,
} from './activity-types'
import './activity.css'
import { RuntimeJobs } from './runtime-jobs'
import { useNativePreferences } from './native-preferences-react'
import { saveNativePreferences } from './native-preferences'
import { filterUsage, nativeClientAgent, usageProjectName } from './usage-filters'

const ranges = [
  ['today', '今天'],
  ['yesterday', '昨天'],
  ['this_week', '本周'],
  ['last_week', '上周'],
  ['this_month', '本月'],
  ['last_7_days', '最近 7 天'],
  ['last_30_days', '最近 30 天'],
] as const
const int = (value: number) => new Intl.NumberFormat('zh-CN').format(value || 0)
const compact = (value: number) =>
  new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 }).format(value || 0)
const money = (value: number) =>
  new Intl.NumberFormat('en', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: value > 0 && value < 0.01 ? 4 : 2,
  }).format(value || 0)
const percent = (value: number) => `${(value || 0).toFixed(1)}%`
const agentName = (value: string) =>
  ({
    codex: 'Codex',
    claude: 'Claude',
    cursor: 'Cursor',
    grokbuild: 'Grok Build',
    antigravity: 'Antigravity',
  })[value] || value
const resultName = (value?: string) =>
  ({
    completed: '已完成',
    complete: '已完成',
    in_progress: '进行中',
    working: '进行中',
    failed: '失败',
    interrupted: '已中断',
    unknown: '未记录',
    final: '已回复',
  })[value || ''] ||
  value ||
  '未记录'
const dateTime = (value?: string) => {
  if (!value) return '尚无记录'
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? value
    : date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
}
const boundedInt = (value: string | null, fallback: number, max: number) => {
  const number = Number(value)
  return value !== null && Number.isInteger(number) && number >= 0 && number <= max
    ? number
    : fallback
}

function useForeground() {
  const [foreground, setForeground] = useState(document.visibilityState === 'visible')
  useEffect(() => {
    const update = () => setForeground(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', update)
    return () => document.removeEventListener('visibilitychange', update)
  }, [])
  return foreground
}

function useActivityStatus(foreground: boolean) {
  return useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: foreground ? 60000 : false,
    refetchIntervalInBackground: false,
  })
}

function EmptyActivity({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div className="activity-empty">
      <MessageSquare size={26} />
      <h3>{title}</h3>
      {children && <p>{children}</p>}
    </div>
  )
}

function PageControls({
  offset,
  limit,
  total,
  maxOffset = 10000,
  onChange,
  label = '条记录',
}: {
  offset: number
  limit: number
  total: number
  maxOffset?: number
  onChange: (offset: number) => void
  label?: string
}) {
  return (
    <div className="activity-pagination">
      <span>
        {total
          ? `${int(offset + 1)}–${int(Math.min(offset + limit, total))} / ${int(total)} ${label}`
          : `0 ${label}`}
      </span>
      <button
        type="button"
        className="button subtle"
        aria-label="上一页"
        disabled={!offset}
        onClick={() => onChange(Math.max(0, offset - limit))}
      >
        <ArrowLeft size={14} />
      </button>
      <button
        type="button"
        className="button subtle"
        aria-label="下一页"
        disabled={offset + limit >= total || offset + limit > maxOffset}
        onClick={() => onChange(offset + limit)}
      >
        <ArrowRight size={14} />
      </button>
    </div>
  )
}

export function AgentsWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const foreground = useForeground()
  const status = useActivityStatus(foreground)
  const query = params.get('q') || ''
  const [searchDraft, setSearchDraft] = useState(query)
  const agent = params.get('agent') || ''
  const project = params.get('project') || ''
  const days = Math.max(1, boundedInt(params.get('days'), 7, 365))
  const selected = params.get('session') || ''
  const offset = boundedInt(params.get('offset'), 0, query ? 1000 : 10000)
  const turnOffset = boundedInt(params.get('turn'), 0, 100000)
  const limit = 30
  const update = (patch: Record<string, string>, reset = false, replace = false) => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (reset) {
          next.delete('offset')
          next.delete('session')
          next.delete('turn')
        }
        for (const [key, value] of Object.entries(patch))
          value ? next.set(key, value) : next.delete(key)
        return next
      },
      { replace },
    )
  }
  useEffect(() => setSearchDraft(query), [query])
  useEffect(() => {
    if (searchDraft === query) return
    const timer = setTimeout(() => update({ q: searchDraft.trim() }, true, true), 300)
    return () => clearTimeout(timer)
  }, [searchDraft, query])
  const input = { agent, project, days, limit, offset }
  const list = useQuery({
    queryKey: ['activity-sessions', agent, project, days, offset],
    queryFn: ({ signal }) => call<SessionList>('session.list', input, signal),
    enabled: !query,
    refetchInterval: foreground && !query ? 60000 : false,
    refetchIntervalInBackground: false,
  })
  const search = useQuery({
    queryKey: ['activity-search', query, agent, project, days, offset],
    queryFn: ({ signal }) =>
      call<SessionSearch>('session.search', { ...input, keyword: query }, signal),
    enabled: !!query,
  })
  const detail = useQuery({
    queryKey: ['activity-session', selected, turnOffset],
    queryFn: ({ signal }) =>
      call<SessionDetail>(
        'session.show',
        { session_id: selected, offset: turnOffset, limit: 10 },
        signal,
      ),
    enabled: !!selected,
  })
  const active = query ? search : list
  const total = (query ? search.data?.total : list.data?.total) || 0
  const items: (SessionSummary & { snippet?: string; role?: string })[] = query
    ? (search.data?.matches || []).map((row) => ({
        ...row,
        q_count: 0,
        snippet: row.content,
        role: row.role,
      }))
    : list.data?.sessions || []
  const refresh = () => {
    void status.refetch()
    void active.refetch()
    if (selected) void detail.refetch()
  }

  return (
    <section className="activity-workspace activity-agents" aria-label="Agent 和会话">
      <div className="activity-heading">
        <div>
          <h1>Agent 会话</h1>
          <p>搜索会话、阅读问答和查看任务关联。</p>
        </div>
        <div className="workspace-actions">
          <RuntimeJobs
            boot={boot}
            kinds={['session.sync']}
            actions={[{ input: { kind: 'session.sync', ...(agent ? { agent } : {}) } }]}
          />
          <button type="button" className="button" onClick={refresh} disabled={active.isFetching}>
            <RefreshCw size={15} className={active.isFetching ? 'spin' : ''} />
            刷新
          </button>
        </div>
      </div>
      {status.data && (
        <div className="activity-agent-strip" role="group" aria-label="按 Agent 筛选会话">
          <button
            type="button"
            className={!agent ? 'activity-agent selected' : 'activity-agent'}
            onClick={() => update({ agent: '' }, true)}
            aria-pressed={!agent}
            aria-label={`全部 Agent，${int(status.data.health.indexed_sessions)} 个会话`}
          >
            <Bot size={18} />
            <span className="activity-agent-info">
              全部 Agent<strong>{int(status.data.health.indexed_sessions)}</strong>
            </span>
          </button>
          {status.data.agents.map((item) => (
            <button
              key={item.agent}
              type="button"
              className={agent === item.agent ? 'activity-agent selected' : 'activity-agent'}
              onClick={() => update({ agent: item.agent }, true)}
              aria-pressed={agent === item.agent}
              aria-label={`${agentName(item.agent)}，${int(item.sessions)} 个会话`}
            >
              <span className={`activity-avatar ${item.agent}`}>
                {agentName(item.agent).slice(0, 1)}
              </span>
              <span className="activity-agent-info">
                {agentName(item.agent)}
                <strong>{int(item.sessions)}</strong>
              </span>
            </button>
          ))}
        </div>
      )}
      <div className="activity-index-row">
        <div className="activity-source-note">
          <Clock3 size={13} />
          <span>
            {status.data?.missing_index
              ? '还没有会话索引。完成一次 ATM 同步后，这里会显示记录。'
              : `索引更新于 ${dateTime(status.data?.health.last_success_at)}`}
          </span>
          {status.data?.health.status === 'stale' && <span className="activity-tag">等待同步</span>}
          {status.data?.agent_hooks && status.data.presence && (
            <span className="activity-tag">
              实时：{status.data.presence.active_count} 个运行中 ·{' '}
              {status.data.presence.attention_count} 个待处理
            </span>
          )}
        </div>
        {!!status.data?.bindings.length && (
          <details className="activity-bindings">
            <summary>任务关联 · {status.data.bindings.length} 个会话</summary>
            <p>这些会话保存了任务关联。运行情况由本机 Agent Hook 单独观察。</p>
            <div>
              {status.data.bindings
                .filter((row) => !agent || row.binding.agent === agent)
                .map((row) => (
                  <article key={row.binding.session_id}>
                    <div>
                      <span className="activity-tag">
                        {agentName(row.binding.agent || 'Agent')}
                      </span>
                      <span className="activity-tag">
                        {row.state === 'bound'
                          ? '任务进行中'
                          : row.state === 'todo_not_in_progress'
                            ? '任务已退出进行中'
                            : '任务已移除'}
                      </span>
                    </div>
                    <strong>
                      {row.todo ? (
                        <Link to={`/tasks/${row.todo.id}`}>{row.todo.title}</Link>
                      ) : (
                        row.binding.todo_id
                      )}
                    </strong>
                    <button
                      type="button"
                      className="text-button"
                      onClick={() => update({ session: row.binding.session_id, turn: '' })}
                    >
                      查看会话 {row.binding.session_id.slice(0, 12)}
                    </button>
                  </article>
                ))}
            </div>
          </details>
        )}
      </div>
      {status.isError && <Notice error={status.error} retry={() => void status.refetch()} />}
      <div className="activity-filters">
        <label className="activity-search">
          <Search size={16} />
          <input
            aria-label="搜索会话内容"
            placeholder="搜索问题、回复或关键词…"
            maxLength={200}
            value={searchDraft}
            onChange={(event) => setSearchDraft(event.target.value)}
          />
          {searchDraft && (
            <button type="button" aria-label="清除搜索" onClick={() => setSearchDraft('')}>
              <X size={14} />
            </button>
          )}
        </label>
        <label>
          项目
          <input
            aria-label="按项目筛选会话"
            list="activity-projects"
            value={project}
            maxLength={200}
            placeholder="全部项目"
            onChange={(event) => update({ project: event.target.value }, true, true)}
          />
          <datalist id="activity-projects">
            {status.data?.projects.map((name) => (
              <option key={name} value={name} />
            ))}
          </datalist>
        </label>
        <label>
          时间
          <select
            aria-label="会话时间范围"
            value={days}
            onChange={(event) => update({ days: event.target.value }, true)}
          >
            <option value="1">今天</option>
            <option value="7">最近 7 天</option>
            <option value="30">最近 30 天</option>
            <option value="90">最近 90 天</option>
            <option value="365">最近一年</option>
          </select>
        </label>
      </div>
      <div className={`activity-session-grid ${selected ? 'has-selection' : ''}`}>
        <section className="activity-list" aria-label={query ? '搜索结果' : '最近会话'}>
          <div className="activity-panel-heading">
            <h2>{query ? '搜索结果' : '最近会话'}</h2>
            <span>
              {int(total)} {query ? '条匹配' : '个会话'}
            </span>
          </div>
          {active.isPending && (
            <div className="activity-loading" role="status">
              正在读取会话…
            </div>
          )}
          {active.isError && <Notice error={active.error} retry={() => void active.refetch()} />}
          {active.isSuccess && !items.length && (
            <EmptyActivity
              title={
                offset ? '这一页没有会话' : query ? '没有找到匹配内容' : '这个范围内还没有会话'
              }
            >
              {offset ? '返回上一页，或调整筛选条件。' : '试试其他关键词、项目或更长的时间范围。'}
            </EmptyActivity>
          )}
          <div className="activity-session-items">
            {items.map((row, index) => (
              <button
                type="button"
                key={`${row.id}-${index}`}
                className={`activity-session-row ${selected === row.id ? 'selected' : ''}`}
                onClick={() => update({ session: row.id, turn: '' })}
                aria-pressed={selected === row.id}
              >
                <div className="activity-session-line">
                  <span className="activity-tag">{agentName(row.agent)}</span>
                  <time>{dateTime(row.last_at || row.created_at)}</time>
                </div>
                <h3>{row.summary || row.first_q || row.snippet || row.short_id}</h3>
                {row.snippet ? (
                  <p className="activity-search-snippet">{row.snippet}</p>
                ) : (
                  <p>
                    {row.latest_progress ||
                      row.final_result ||
                      row.first_q ||
                      '会话尚未记录可展示的内容'}
                  </p>
                )}
                <div className="activity-session-line">
                  <span>
                    {row.project || '未分配项目'} · {row.short_id || row.id.slice(0, 8)}
                  </span>
                  <span>
                    {row.role
                      ? row.role === 'user'
                        ? '用户'
                        : '助手'
                      : `${row.q_count} 轮${row.is_subagent ? ' · 子 Agent' : ''}`}
                  </span>
                </div>
              </button>
            ))}
          </div>
          <PageControls
            offset={offset}
            limit={limit}
            total={total}
            maxOffset={query ? 1000 : 10000}
            onChange={(value) => update({ offset: String(value) })}
          />
          {query && offset + limit > 1000 && total > offset + limit && (
            <p className="activity-source-note">匹配较多，请缩小关键词或时间范围继续查看。</p>
          )}
        </section>
        <section className="activity-session-detail" aria-label="会话详情">
          {!selected ? (
            <EmptyActivity title="选择一个会话">
              阅读完整问答、查看执行工具和已记录的结果。
            </EmptyActivity>
          ) : (
            <>
              <button
                type="button"
                className="button subtle activity-back"
                onClick={() => update({ session: '', turn: '' })}
              >
                <ArrowLeft size={14} />
                返回会话列表
              </button>
              {detail.isPending && (
                <div className="activity-loading" role="status">
                  正在读取对话…
                </div>
              )}
              {detail.isError && (
                <Notice error={detail.error} retry={() => void detail.refetch()} />
              )}
              {detail.data && (
                <Conversation
                  data={detail.data}
                  onPage={(value) => update({ turn: String(value) })}
                  onSelect={(id) => update({ session: id, turn: '' })}
                />
              )}
            </>
          )}
        </section>
      </div>
    </section>
  )
}

function Conversation({
  data,
  onPage,
  onSelect,
}: {
  data: SessionDetail
  onPage: (offset: number) => void
  onSelect: (id: string) => void
}) {
  const tools = Object.entries(data.tools || {}).sort((a, b) => b[1] - a[1])
  return (
    <>
      <div className="activity-conversation-body" key={`${data.id}:${data.offset}`}>
        <div className="activity-conversation-heading">
          <div>
            <span className="activity-tag">{agentName(data.agent)}</span>
            <span className="activity-tag">{resultName(data.result_status)}</span>
            {data.is_subagent && <span className="activity-tag">子 Agent</span>}
          </div>
          <h2 title={data.agent_nickname || data.project || '会话记录'}>
            {data.agent_nickname || data.project || '会话记录'}
          </h2>
          <p className="activity-session-id" title={data.id}>
            {data.id}
          </p>
          {data.parent_session_id && (
            <button
              className="text-button"
              type="button"
              onClick={() => onSelect(data.parent_session_id!)}
            >
              查看父会话
            </button>
          )}
        </div>
        <div className="activity-conversation-meta">
          {(data.final_result || data.latest_progress) && (
            <details className="activity-result">
              <summary>最近结果与进展</summary>
              <Markdown text={data.final_result || data.latest_progress || ''} />
            </details>
          )}
          {!!tools.length && (
            <details className="activity-tools">
              <summary>
                {tools.length} 种工具 · {int(tools.reduce((sum, row) => sum + row[1], 0))} 次调用
              </summary>
              <div>
                {tools.map(([name, count]) => (
                  <span className="activity-tool" key={name}>
                    {name}
                    <b>{int(count)}</b>
                  </span>
                ))}
              </div>
            </details>
          )}
        </div>
        {data.content_truncated && (
          <div className="activity-inline-note" role="status">
            本页包含较长内容，每轮最多展示 16,000 字符。
          </div>
        )}
        {!data.qa.length && (
          <EmptyActivity title={data.offset ? '这一页没有可展示的问答' : '还没有可展示的问答'}>
            {data.content_state === 'metadata_only'
              ? '该会话目前只有元数据，后续索引可能补充对话内容。'
              : '当前索引没有保存这个范围的可见对话。'}
          </EmptyActivity>
        )}
        <div className="activity-transcript">
          {data.qa.map((qa) => (
            <article className="activity-turn" key={qa.turn}>
              <div className="activity-turn-label">第 {qa.turn} 轮</div>
              {qa.q && (
                <div className="activity-message user">
                  <span>你</span>
                  <Markdown text={qa.q} />
                </div>
              )}
              {!!qa.progress?.length && (
                <details className="activity-progress">
                  <summary>{qa.progress.length} 条过程记录</summary>
                  {qa.progress.map((text, i) => (
                    <Markdown key={i} text={text} />
                  ))}
                </details>
              )}
              {qa.a && (
                <div className="activity-message assistant">
                  <span>{agentName(data.agent)}</span>
                  <Markdown text={qa.a} />
                </div>
              )}
            </article>
          ))}
        </div>
      </div>
      <PageControls
        offset={data.offset}
        limit={data.limit}
        total={data.total_turns}
        maxOffset={100000}
        onChange={onPage}
        label="轮对话"
      />
    </>
  )
}

type Metric = 'tokens' | 'cost' | 'requests'
type Breakdown = 'agent' | 'model' | 'project' | 'skill' | 'speed'
type UsageRow = { name: string; agent?: string } & Partial<MetricFields>
const metricValue = (row: Partial<MetricFields>, metric: Metric) =>
  metric === 'cost'
    ? row.cost_usd || 0
    : metric === 'requests'
      ? row.requests || 0
      : row.total_tokens || 0
const formatMetric = (value: number, metric: Metric) =>
  metric === 'cost' ? money(value) : compact(value)

export function UsageWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const nativePreferences = useNativePreferences()
  const [preferenceError, setPreferenceError] = useState('')
  const foreground = useForeground()
  const status = useActivityStatus(foreground)
  const rawRange = params.get('range') || 'last_7_days'
  const range = ranges.some(([key]) => key === rawRange) ? rawRange : 'last_7_days'
  const agent = params.has('agent')
    ? params.get('agent') || ''
    : nativeClientAgent(nativePreferences.usage_filter_client || '')
  const model = params.has('model')
    ? params.get('model') || ''
    : nativePreferences.usage_filter_model || ''
  const project = params.has('project')
    ? params.get('project') || ''
    : nativePreferences.usage_filter_project || ''
  const metric = (
    ['tokens', 'cost', 'requests'].includes(params.get('metric') || '')
      ? params.get('metric')
      : 'tokens'
  ) as Metric
  const requestedGroup = (
    ['agent', 'model', 'project', 'skill', 'speed'].includes(params.get('group') || '')
      ? params.get('group')
      : 'agent'
  ) as Breakdown
  const availableGroups = project
    ? ['agent', 'project']
    : model
      ? ['model', 'speed']
      : ['agent', 'model', 'project', 'skill', 'speed']
  const group = (
    availableGroups.includes(requestedGroup) ? requestedGroup : project ? 'project' : 'model'
  ) as Breakdown
  const update = (key: string, value: string) => {
    if (['agent', 'model', 'project'].includes(key)) {
      try {
        saveNativePreferences({
          [key === 'agent'
            ? 'usage_filter_client'
            : key === 'model'
              ? 'usage_filter_model'
              : 'usage_filter_project']: value,
        })
        setPreferenceError('')
      } catch {
        setPreferenceError('本次筛选已应用，但浏览器未允许记住此筛选。')
      }
    }
    setParams((current) => {
      const next = new URLSearchParams(current)
      if (value || ['agent', 'model', 'project'].includes(key)) next.set(key, value)
      else next.delete(key)
      return next
    })
  }
  const snapshot = useQuery({
    queryKey: ['activity-usage', range, agent],
    queryFn: ({ signal }) => call<UsageSnapshot>('usage.snapshot', { range, agent }, signal),
    refetchInterval: foreground ? 120000 : false,
    refetchIntervalInBackground: false,
  })
  const quota = useQuery({
    queryKey: ['activity-quota', agent],
    queryFn: ({ signal }) => call<CachedQuota>('quota.cached', { agent }, signal),
    refetchInterval: foreground ? 120000 : false,
    refetchIntervalInBackground: false,
  })
  const unfiltered = snapshot.data?.ranges[range]
  const scoped = snapshot.data ? filterUsage(snapshot.data, range, model, project) : undefined
  const data = scoped?.range
  const totals = useMemo(
    () =>
      (scoped?.summary || []).reduce(
        (sum, row) => ({
          tokens: sum.tokens + row.total_tokens,
          requests: sum.requests + row.requests,
          cost: sum.cost + row.cost_usd,
          cache: sum.cache + row.cache_read_tokens,
          input: sum.input + row.total_input_tokens,
          estimated: sum.estimated + row.estimated_cost_usd,
          sessions: sum.sessions + row.sessions,
        }),
        { tokens: 0, requests: 0, cost: 0, cache: 0, input: 0, estimated: 0, sessions: 0 },
      ),
    [scoped?.summary],
  )
  const days = scoped?.days || []
  const refresh = () => {
    void snapshot.refetch()
    void quota.refetch()
    void status.refetch()
  }

  return (
    <section className="activity-workspace activity-usage" aria-label="用量和统计">
      <div className="activity-heading">
        <div>
          <h1>用量统计</h1>
          <p>查看 Token、费用、模型请求与额度记录。</p>
        </div>
        <div className="workspace-actions">
          <RuntimeJobs
            boot={boot}
            kinds={['quota.refresh']}
            actions={[{ input: { kind: 'quota.refresh', ...(agent ? { agent } : {}) } }]}
          />
          <button type="button" className="button" onClick={refresh} disabled={snapshot.isFetching}>
            <RefreshCw size={15} className={snapshot.isFetching ? 'spin' : ''} />
            刷新记录
          </button>
        </div>
      </div>
      <div className="activity-usage-filters">
        <div className="activity-range-picker" role="group" aria-label="统计时间范围">
          {ranges.map(([key, label]) => (
            <button
              type="button"
              key={key}
              className={range === key ? 'selected' : ''}
              onClick={() => update('range', key)}
              aria-pressed={range === key}
            >
              {label}
            </button>
          ))}
        </div>
        <label>
          Agent
          <select
            value={agent}
            aria-label="统计 Agent"
            onChange={(event) => update('agent', event.target.value)}
          >
            <option value="">全部 Agent</option>
            {status.data?.agents.map((row) => (
              <option key={row.agent} value={row.agent}>
                {agentName(row.agent)}
              </option>
            ))}
            {agent && !status.data?.agents.some((row) => row.agent === agent) && (
              <option value={agent}>{agentName(agent)}</option>
            )}
          </select>
        </label>
        <label>
          模型
          <select
            value={model}
            aria-label="统计模型"
            onChange={(event) => update('model', event.target.value)}
          >
            <option value="">全部模型</option>
            {[
              ...new Set([
                ...(unfiltered?.model_stats.map((row) => row.model) || []),
                ...(model ? [model] : []),
              ]),
            ]
              .sort()
              .map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
          </select>
        </label>
        <label>
          项目
          <select
            value={project}
            aria-label="统计项目"
            onChange={(event) => update('project', event.target.value)}
          >
            <option value="">全部项目</option>
            {[
              ...new Set([
                ...(unfiltered?.project_stats.map((row) => usageProjectName(row.project)) || []),
                ...(project ? [project] : []),
              ]),
            ]
              .sort()
              .map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
          </select>
        </label>
        {(agent || model || project) && (
          <button
            type="button"
            className="text-button"
            onClick={() => {
              try {
                saveNativePreferences({
                  usage_filter_client: '',
                  usage_filter_model: '',
                  usage_filter_project: '',
                })
                setPreferenceError('')
              } catch {
                setPreferenceError('本次筛选已清空，但浏览器未允许记住更改。')
              }
              setParams((current) => {
                const next = new URLSearchParams(current)
                ;['agent', 'model', 'project'].forEach((key) => next.set(key, ''))
                return next
              })
            }}
          >
            清除筛选
          </button>
        )}
      </div>
      {preferenceError && (
        <p className="activity-inline-note" role="status">
          {preferenceError}
        </p>
      )}
      {project && model && (
        <p className="activity-inline-note">
          已按项目“{project}”汇总与绘图。模型“{model}
          ”筛选会在清除项目后应用；现有记录不提供模型与项目的联合汇总。
        </p>
      )}
      <div className="activity-source-note">
        <Clock3 size={13} />
        {data ? `${data.start_date} — ${data.end_date}` : '按本地日历计算统计范围'}
        <span>· 索引更新于 {dateTime(status.data?.health.last_success_at)}</span>
      </div>
      {status.data?.missing_index ? (
        <EmptyActivity title="还没有用量记录">
          完成一次 ATM 同步后，可以查看已索引会话的用量与费用。
        </EmptyActivity>
      ) : snapshot.isError ? (
        <Notice error={snapshot.error} retry={() => void snapshot.refetch()} />
      ) : snapshot.isPending ? (
        <div className="activity-loading" role="status">
          正在汇总用量…
        </div>
      ) : (
        data && (
          <>
            <div className="activity-metrics">
              <MetricCard
                label="总 Token"
                value={compact(totals.tokens)}
                note={`缓存读取 ${compact(totals.cache)}`}
              />
              <MetricCard
                label="费用（USD）"
                value={money(totals.cost)}
                note={
                  totals.estimated > 0
                    ? `其中 ${money(totals.estimated)} 采用估算价格`
                    : '按记录与模型价格计算'
                }
              />
              <MetricCard
                label="模型请求"
                value={int(totals.requests)}
                note={
                  model || project
                    ? '所选范围的请求记录'
                    : `详细记录覆盖 ${percent(data.quality.request_coverage_percent)}`
                }
              />
              <MetricCard
                label="活跃会话"
                value={
                  project ? int(totals.sessions) : model ? '—' : int(data.quality.active_sessions)
                }
                note={
                  project
                    ? '所选项目的会话记录'
                    : model
                      ? '模型明细不提供去重会话数'
                      : `${int(data.quality.token_sessions)} 个会话有 Token 记录`
                }
              />
            </div>
            <section className="activity-chart-card">
              <div className="activity-panel-heading">
                <div>
                  <h2>用量趋势</h2>
                  <p>{range === 'today' || range === 'yesterday' ? '每小时' : '每日'}记录</p>
                </div>
                <MetricPicker metric={metric} onChange={(value) => update('metric', value)} />
              </div>
              <UsageChart rows={days} metric={metric} />
              {!days.length && (
                <EmptyActivity title="这个范围内没有用量记录">
                  试试更长的时间范围或其他 Agent。
                </EmptyActivity>
              )}
            </section>
            <section className="activity-breakdown-card">
              <div className="activity-panel-heading">
                <h2>用量明细</h2>
                <div className="activity-segment" role="group" aria-label="用量分组">
                  {(
                    [
                      ['agent', 'Agent'],
                      ['model', '模型'],
                      ['project', '项目'],
                      ['skill', '技能'],
                      ['speed', '速度'],
                    ] as const
                  )
                    .filter(([key]) => availableGroups.includes(key))
                    .map(([key, label]) => (
                      <button
                        type="button"
                        key={key}
                        className={group === key ? 'selected' : ''}
                        onClick={() => update('group', key)}
                        aria-pressed={group === key}
                      >
                        {label}
                      </button>
                    ))}
                </div>
              </div>
              <UsageBreakdown range={data} group={group} metric={metric} />
            </section>
            <div className="activity-quality">
              <strong>{model || project ? '当前 Agent 的完整记录覆盖' : '记录覆盖'}</strong>
              <span>会话 Token 覆盖 {percent(data.quality.session_coverage_percent)}</span>
              <span>请求详情覆盖 {percent(data.quality.request_coverage_percent)}</span>
              <span>速度采样 {percent(data.quality.speed_sample_percent)}</span>
              <p>
                费用来自已索引记录及配置的模型价格；未记录的请求和不可计量的会话不计入 Token 总量。
              </p>
            </div>
          </>
        )
      )}
      <section className="activity-quota-section">
        <div className="activity-panel-heading">
          <div>
            <h2>额度观察</h2>
            <p>最近一次保存的额度快照</p>
          </div>
          <span className="activity-tag">本地记录</span>
        </div>
        {quota.isPending && (
          <div className="activity-loading" role="status">
            正在读取额度记录…
          </div>
        )}
        {quota.isError && <Notice error={quota.error} retry={() => void quota.refetch()} />}
        {quota.isSuccess && !quota.data.windows.length && (
          <div className="activity-inline-note">
            还没有保存的额度记录。后续同步采集到额度后，会在这里显示。
          </div>
        )}
        <div className="activity-quotas">
          {quota.data?.windows.map((window) => (
            <div
              className={`activity-quota-card ${window.stale || window.reset_elapsed ? 'stale' : ''}`}
              key={`${window.agent}-${window.window_minutes}`}
            >
              <div>
                <strong>{agentName(window.agent)}</strong>
                <span>
                  {window.window_minutes >= 1440
                    ? `${window.window_minutes / 1440} 天`
                    : `${window.window_minutes / 60} 小时`}
                  窗口
                </span>
              </div>
              <p>
                <b>{percent(window.used_percent)}</b> 已使用
                {window.reset_elapsed && <span className="activity-tag">重置后待更新</span>}
                {window.stale && !window.reset_elapsed && (
                  <span className="activity-tag">记录较旧</span>
                )}
              </p>
              <div className="activity-meter">
                <span style={{ width: `${Math.min(100, Math.max(0, window.used_percent))}%` }} />
              </div>
              <small>
                记录于 {dateTime(window.observed_at)}
                {window.resets_at > 0 &&
                  ` · 重置 ${dateTime(new Date(window.resets_at * 1000).toISOString())}`}
              </small>
            </div>
          ))}
        </div>
      </section>
    </section>
  )
}

function MetricCard({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="activity-metric">
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{note}</small>
    </div>
  )
}
function MetricPicker({
  metric,
  onChange,
}: {
  metric: Metric
  onChange: (metric: Metric) => void
}) {
  return (
    <div className="activity-segment" role="group" aria-label="趋势指标">
      {(
        [
          ['tokens', 'Token'],
          ['cost', '费用'],
          ['requests', '请求'],
        ] as const
      ).map(([key, label]) => (
        <button
          key={key}
          type="button"
          className={key === metric ? 'selected' : ''}
          onClick={() => onChange(key)}
          aria-pressed={key === metric}
        >
          {label}
        </button>
      ))}
    </div>
  )
}

function UsageChart({ rows, metric }: { rows: DayStats[]; metric: Metric }) {
  const [selected, setSelected] = useState<string>()
  if (!rows.length) return null
  const max = Math.max(...rows.map((row) => metricValue(row, metric)), 1)
  const chosen = rows.find((row) => row.date === selected)
  return (
    <>
      <div className="activity-chart" role="group" aria-label="用量趋势柱状图">
        {rows.map((row, index) => (
          <button
            type="button"
            key={row.date}
            className={`activity-bar-column ${selected === row.date ? 'selected' : ''}`}
            onClick={() => setSelected(row.date)}
            aria-label={`${row.date}：${formatMetric(metricValue(row, metric), metric)}`}
          >
            <span className="activity-bar-value">
              {formatMetric(metricValue(row, metric), metric)}
            </span>
            <span className="activity-bar-track">
              <span
                className="activity-bar"
                style={{
                  height: `${Math.max((metricValue(row, metric) / max) * 100, metricValue(row, metric) > 0 ? 2 : 0)}%`,
                }}
              />
            </span>
            <span className="activity-bar-label">
              {rows.length < 12 ||
              index % Math.ceil(rows.length / 10) === 0 ||
              index === rows.length - 1
                ? row.date.length > 10
                  ? row.date.slice(11, 13) + '时'
                  : row.date.slice(5)
                : ' '}
            </span>
          </button>
        ))}
      </div>
      <div className="activity-chart-detail" aria-live="polite">
        {chosen
          ? `${chosen.date} · ${int(chosen.total_tokens)} Token · ${int(chosen.requests)} 次请求 · ${money(chosen.cost_usd)}`
          : '点击柱形查看该时段的具体数值'}
      </div>
    </>
  )
}

function UsageBreakdown({
  range,
  group,
  metric,
}: {
  range: UsageRange
  group: Breakdown
  metric: Metric
}) {
  if (group === 'skill')
    return range.skill_stats.length ? (
      <div className="activity-table-scroll">
        <table className="activity-table">
          <thead>
            <tr>
              <th>技能</th>
              <th>调用次数</th>
              <th>会话</th>
              <th>Agent 数</th>
            </tr>
          </thead>
          <tbody>
            {range.skill_stats.map((row) => (
              <tr key={row.skill}>
                <td>{row.skill}</td>
                <td>{int(row.calls)}</td>
                <td>{int(row.sessions)}</td>
                <td>{int(row.agents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    ) : (
      <EmptyActivity title="这个范围内没有技能调用记录" />
    )
  if (group === 'speed')
    return (
      <>
        <div className="activity-inline-note">
          生成速度只统计可测量的模型请求。未计时 {int(range.speed.untimed_requests)}{' '}
          次，超出有效测量窗口 {int(range.speed.out_of_window_requests)} 次。
        </div>
        {range.speed.models.length ? (
          <div className="activity-table-scroll">
            <table className="activity-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>Agent</th>
                  <th>输出 Token / 秒</th>
                  <th>请求耗时中位数</th>
                  <th>有效样本 / 请求</th>
                </tr>
              </thead>
              <tbody>
                {range.speed.models.map((row) => (
                  <tr key={`${row.client}-${row.model}`}>
                    <td>{row.model}</td>
                    <td>{agentName(row.client)}</td>
                    <td>{row.tokens_per_second_weighted.toFixed(1)}</td>
                    <td>{row.duration_p50_seconds.toFixed(1)} 秒</td>
                    <td>
                      {int(row.sampled)} / {int(row.requests)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyActivity title="还没有有效的速度样本" />
        )}
        {!!range.speed.turns.length && (
          <div className="activity-table-scroll">
            <table className="activity-table">
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>对话轮数</th>
                  <th>等待中位数</th>
                  <th>等待 P90</th>
                  <th>每轮请求数</th>
                </tr>
              </thead>
              <tbody>
                {range.speed.turns.map((row) => (
                  <tr key={row.agent}>
                    <td>{agentName(row.agent)}</td>
                    <td>{int(row.turns)}</td>
                    <td>{row.wait_p50_seconds.toFixed(1)} 秒</td>
                    <td>{row.wait_p90_seconds.toFixed(1)} 秒</td>
                    <td>{row.requests_per_turn.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </>
    )
  let rows: UsageRow[]
  if (group === 'model')
    rows = range.model_stats.map((row) => ({ ...row, name: row.model, agent: row.client }))
  else if (group === 'project')
    rows = range.project_stats.map((row) => ({ ...row, name: row.project || '未分配项目' }))
  else {
    const grouped = new Map<string, UsageRow>()
    for (const row of range.project_stats) {
      const current = grouped.get(row.agent) || { name: agentName(row.agent), agent: row.agent }
      for (const key of [
        'sessions',
        'total_tokens',
        'total_input_tokens',
        'fresh_input_tokens',
        'output_tokens',
        'cache_read_tokens',
        'cache_create_tokens',
        'requests',
        'cost_usd',
        'estimated_cost_usd',
      ] as const)
        current[key] = (current[key] || 0) + row[key]
      grouped.set(row.agent, current)
    }
    rows = [...grouped.values()]
  }
  rows.sort((a, b) => metricValue(b, metric) - metricValue(a, metric))
  if (!rows.length) return <EmptyActivity title="这个范围内没有用量明细" />
  const max = Math.max(...rows.map((row) => metricValue(row, metric)), 1)
  return (
    <div className="activity-table-scroll">
      <table className="activity-table">
        <thead>
          <tr>
            <th>{group === 'model' ? '模型' : group === 'project' ? '项目' : 'Agent'}</th>
            {group !== 'agent' && <th>Agent</th>}
            <th>Token</th>
            <th>缓存读取</th>
            <th>请求</th>
            <th>费用（USD）</th>
            <th>分布</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${row.agent}-${row.name}`}>
              <td>
                <strong>{row.name}</strong>
              </td>
              {group !== 'agent' && <td>{agentName(row.agent || '')}</td>}
              <td title={int(row.total_tokens || 0)}>{compact(row.total_tokens || 0)}</td>
              <td>{compact(row.cache_read_tokens || 0)}</td>
              <td>{int(row.requests || 0)}</td>
              <td>
                {money(row.cost_usd || 0)}
                {(row.estimated_cost_usd || 0) > 0 && (
                  <span
                    className="activity-estimated"
                    title={`估算部分 ${money(row.estimated_cost_usd || 0)}`}
                  >
                    *
                  </span>
                )}
              </td>
              <td>
                <div
                  className="activity-table-meter"
                  title={formatMetric(metricValue(row, metric), metric)}
                >
                  <span style={{ width: `${(metricValue(row, metric) / max) * 100}%` }} />
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
