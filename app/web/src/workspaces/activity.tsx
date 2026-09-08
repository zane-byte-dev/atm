import { Select } from '../components/select'
import { BadgeCalendar } from './badge-calendar'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import {
  ArrowLeft,
  ArrowRight,
  ArrowDownUp,
  Bot,
  ChartNoAxesCombined,
  RefreshCw,
  Filter,
  Clock3,
  Copy,
  MessageSquare,
} from 'lucide-react'
import { call } from '../api'
import { SessionChildren } from '../session-tree'
import { WorkspaceTitle } from '../workspace-title'
import { WorkspaceSearchButton } from '../global-search'
import { Markdown, Notice } from '../editor'
import { QueryLoading } from '../query-loading'
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
import './usage.css'
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
  pending = false,
  maxOffset = 10000,
  onChange,
  label = '条记录',
}: {
  offset: number
  limit: number
  total: number
  pending?: boolean
  maxOffset?: number
  onChange: (offset: number) => void
  label?: string
}) {
  return (
    <div className="activity-pagination">
      <span>
        {pending
          ? `— ${label}`
          : total
            ? `${int(offset + 1)}–${int(Math.min(offset + limit, total))} / ${int(total)} ${label}`
            : `0 ${label}`}
      </span>
      <button
        type="button"
        className="button subtle"
        aria-label="上一页"
        disabled={pending || !offset}
        onClick={() => onChange(Math.max(0, offset - limit))}
      >
        <ArrowLeft size={14} />
      </button>
      <button
        type="button"
        className="button subtle"
        aria-label="下一页"
        disabled={pending || offset + limit >= total || offset + limit > maxOffset}
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
  const agent = params.get('agent') || ''
  const project = params.get('project') || ''
  const days = Math.max(1, boundedInt(params.get('days'), 7, 365))
  const selected = params.get('session') || ''
  const offset = boundedInt(params.get('offset'), 0, query ? 1000 : 10000)
  const turnOffset = boundedInt(params.get('turn'), 0, 100000)
  const limit = 30
  const [filtersOpen, setFiltersOpen] = useState(false)
  const grouped = params.get('children') !== 'all'
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
  const input = { agent, project, days, limit, offset }
  const list = useQuery({
    queryKey: ['activity-sessions', agent, project, days, offset, grouped],
    queryFn: ({ signal }) =>
      call<SessionList>('session.list', { ...input, group_children: grouped }, signal),
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
  const total = query ? search.data?.total : list.data?.total
  const items: (SessionSummary & { snippet?: string; role?: string })[] = query
    ? (search.data?.matches || []).map((row) => ({
        ...row,
        q_count: 0,
        snippet: row.content,
        role: row.role,
      }))
    : list.data?.sessions || []
  return (
    <section
      className="activity-workspace activity-agents improved-agents"
      aria-label="Agent 和会话"
    >
      <div className="activity-heading">
        <div>
          <WorkspaceTitle icon={Bot}>Agent 会话</WorkspaceTitle>
          <p>搜索会话、阅读问答和查看任务关联。</p>
        </div>
        <div className="workspace-actions">
          <WorkspaceSearchButton />
          <button
            className="button"
            aria-expanded={filtersOpen}
            onClick={() => setFiltersOpen(!filtersOpen)}
          >
            <Filter size={15} />
            筛选
          </button>
          <RuntimeJobs
            boot={boot}
            kinds={['session.sync']}
            actions={[{ input: { kind: 'session.sync', ...(agent ? { agent } : {}) } }]}
          />
        </div>
      </div>
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
      <div className="agent-filter-bar">
        <span>
          {agent ? agentName(agent) : '全部 Agent'} · {project || '全部项目'} ·{' '}
          {days === 1 ? '今天' : `最近 ${days} 天`} ·{' '}
          {query ? '搜索结果' : grouped ? '按主会话聚合' : '全部会话'}
        </span>
        {(agent || project || query || days !== 7 || !grouped) && (
          <button
            onClick={() => update({ agent: '', project: '', q: '', days: '', children: '' }, true)}
          >
            清除筛选
          </button>
        )}
        {filtersOpen && (
          <div className="agent-filter-popover" role="region" aria-label="会话筛选">
            <label>
              Agent
              <Select
                aria-label="筛选 Agent"
                value={agent}
                onChange={(e) => update({ agent: e.target.value }, true)}
              >
                <option value="">全部 Agent</option>
                {status.data?.agents.map((row) => (
                  <option key={row.agent} value={row.agent}>
                    {agentName(row.agent)}
                  </option>
                ))}
              </Select>
            </label>
            <label>
              项目
              <Select
                aria-label="按项目筛选会话"
                value={project}
                onChange={(e) => update({ project: e.target.value }, true)}
              >
                <option value="">全部项目</option>
                {status.data?.projects.map((name) => (
                  <option key={name}>{name}</option>
                ))}
              </Select>
            </label>
            <label>
              时间
              <Select
                aria-label="会话时间范围"
                value={days}
                onChange={(e) => update({ days: e.target.value }, true)}
              >
                {[1, 7, 30, 90, 365].map((value) => (
                  <option key={value} value={value}>
                    {value === 1 ? '今天' : `最近 ${value} 天`}
                  </option>
                ))}
              </Select>
            </label>
            <label className="agent-group-control">
              <input
                type="checkbox"
                checked={grouped}
                disabled={!!query}
                onChange={(e) => update({ children: e.target.checked ? '' : 'all' }, true)}
              />
              聚合子会话
            </label>
            {query && <small>搜索显示所有匹配会话，包括子会话。</small>}
            <button className="button" onClick={() => setFiltersOpen(false)}>
              收起筛选
            </button>
          </div>
        )}
      </div>
      <div className={`activity-session-grid ${selected ? 'has-selection' : ''}`}>
        <section className="activity-list" aria-label={query ? '搜索结果' : '最近会话'}>
          <div className="activity-panel-heading">
            <h2>{query ? '搜索结果' : '最近会话'}</h2>
            <span>
              {active.isPending ? '—' : int(total ?? 0)} {query ? '条匹配' : '个会话'}
            </span>
          </div>
          {active.isPending && (
            <QueryLoading
              className="activity-loading"
              text="正在读取会话…"
              onRetry={() => void active.refetch()}
            />
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
              <Fragment key={`${row.id}-${index}`}>
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
                  <h3>
                    {row.summary ||
                      row.agent_nickname ||
                      row.first_q ||
                      row.snippet ||
                      `${row.is_subagent ? '子会话' : '会话'} ${row.short_id}`}
                  </h3>
                  {(row.latest_progress || row.final_result) && (
                    <p className="session-latest" title={row.latest_progress || row.final_result}>
                      {row.latest_progress || row.final_result}
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
                {!query && grouped && (
                  <SessionChildren
                    parent={row}
                    agent={agent}
                    project={project}
                    days={days}
                    selected={selected}
                    onSelect={(id) => update({ session: id, turn: '' })}
                  />
                )}
              </Fragment>
            ))}
          </div>
          <PageControls
            offset={offset}
            limit={limit}
            total={total ?? 0}
            pending={active.isPending}
            maxOffset={query ? 1000 : 10000}
            onChange={(value) => update({ offset: String(value) })}
          />
          {query && offset + limit > 1000 && (total ?? 0) > offset + limit && (
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
                <QueryLoading
                  className="activity-loading"
                  text="正在读取对话…"
                  onRetry={() => void detail.refetch()}
                />
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
  const [filter, setFilter] = useState<'all' | 'progress' | 'result'>('all')
  const [newestFirst, setNewestFirst] = useState(false)
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  useEffect(() => {
    setFilter('all')
    setNewestFirst(false)
    setCopyState('idle')
  }, [data.id, data.offset])
  const timeline = data.qa.flatMap((qa) => [
    ...(qa.q
      ? [{ key: `${qa.turn}-question`, kind: 'question', label: `第 ${qa.turn} 轮 · 用户` }]
      : []),
    ...(qa.progress ?? []).map((_, index) => ({
      key: `${qa.turn}-progress-${index}`,
      kind: 'progress',
      label: `第 ${qa.turn} 轮 · 过程 ${index + 1}`,
    })),
    ...(qa.a
      ? [
          {
            key: `${qa.turn}-answer`,
            kind: 'answer',
            label: `第 ${qa.turn} 轮 · ${agentName(data.agent)}`,
          },
        ]
      : []),
  ])
  const transcript = [...data.qa]
    .sort((left, right) => (newestFirst ? right.turn - left.turn : left.turn - right.turn))
    .filter((qa) => filter === 'all' || (filter === 'progress' ? qa.progress?.length : qa.a))
  const copyPage = async () => {
    const text = data.qa
      .flatMap((qa) => [
        `第 ${qa.turn} 轮`,
        qa.q ? `你\n${qa.q}` : '',
        ...(qa.progress ?? []).map((item) => `过程\n${item}`),
        qa.a ? `${agentName(data.agent)}\n${qa.a}` : '',
      ])
      .filter(Boolean)
      .join('\n\n')
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
    } catch {
      setCopyState('failed')
    }
    window.setTimeout(() => setCopyState('idle'), 1600)
  }
  return (
    <>
      <div className="activity-conversation-body" key={`${data.id}:${data.offset}`}>
        <div className="activity-conversation-heading">
          <h2 title={data.agent_nickname || data.project || '会话记录'}>
            {data.agent_nickname || data.project || '会话记录'}
          </h2>
        </div>
        <div className="activity-conversation-surface">
          <div>
            <span className="activity-tag">{agentName(data.agent)}</span>
            <span className="activity-tag">{resultName(data.result_status)}</span>
            {data.is_subagent && <span className="activity-tag">子 Agent</span>}
          </div>
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
          <div className="activity-run-overview">
            <div className="activity-run-facts" aria-label="会话执行概览">
              <span>
                <b>{int(data.total_turns)}</b>轮对话
              </span>
              <span>
                <b>{int(tools.reduce((sum, row) => sum + row[1], 0))}</b>次工具
              </span>
              <span>
                <b>{timeline.length}</b>个事件
              </span>
            </div>
            <div className="activity-run-controls">
              <div className="activity-run-filter" role="group" aria-label="筛选会话事件">
                {(
                  [
                    ['all', '全部'],
                    ['progress', '过程'],
                    ['result', '结果'],
                  ] as const
                ).map(([value, label]) => (
                  <button
                    type="button"
                    key={value}
                    className={filter === value ? 'selected' : ''}
                    aria-pressed={filter === value}
                    onClick={() => setFilter(value)}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <button
                type="button"
                className="button subtle"
                aria-pressed={newestFirst}
                onClick={() => setNewestFirst((value) => !value)}
              >
                <ArrowDownUp size={13} />
                {newestFirst ? '最新优先' : '按时间'}
              </button>
              <button type="button" className="button subtle" onClick={() => void copyPage()}>
                <Copy size={13} />
                {copyState === 'copied'
                  ? '已复制'
                  : copyState === 'failed'
                    ? '复制失败'
                    : '复制本页'}
              </button>
            </div>
            {timeline.length > 0 && (
              <div className="activity-run-timeline" aria-label={`${timeline.length} 个会话事件`}>
                {timeline.map((event) => (
                  <span
                    key={event.key}
                    className={`activity-run-event ${event.kind}`}
                    title={event.label}
                  />
                ))}
              </div>
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
            {transcript.map((qa) => (
              <article className="activity-turn" key={qa.turn}>
                <div className="activity-turn-label">第 {qa.turn} 轮</div>
                {filter === 'all' && qa.q && (
                  <div className="activity-message user">
                    <span>你</span>
                    <Markdown text={qa.q} />
                  </div>
                )}
                {filter !== 'result' && !!qa.progress?.length && (
                  <details className="activity-progress">
                    <summary>{qa.progress.length} 条过程记录</summary>
                    {qa.progress.map((text, i) => (
                      <Markdown key={i} text={text} />
                    ))}
                  </details>
                )}
                {filter !== 'progress' && qa.a && (
                  <div className="activity-message assistant">
                    <span>{agentName(data.agent)}</span>
                    <Markdown text={qa.a} />
                  </div>
                )}
              </article>
            ))}
          </div>
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

function UsageViewTabs() {
  const [params, setParams] = useSearchParams()
  const view = params.get('view') || 'usage'
  return (
    <div className="usage-view-tabs" role="group" aria-label="统计视图">
      {(
        [
          ['usage', '用量'],
          ['quota', '额度'],
          ['badges', '徽章日历'],
        ] as const
      ).map(([key, label]) => (
        <button
          key={key}
          className={view === key ? 'selected' : ''}
          aria-pressed={view === key}
          onClick={() =>
            setParams((current) => {
              const next = new URLSearchParams(current)
              next.set('view', key)
              return next
            })
          }
        >
          {label}
        </button>
      ))}
    </div>
  )
}
export function UsageWorkspace({ boot }: { boot: Bootstrap }) {
  const [params] = useSearchParams()
  return params.get('view') === 'badges' ? (
    <section
      className="activity-workspace activity-usage usage-view-badges"
      aria-label="用量和统计"
    >
      <div className="activity-heading">
        <WorkspaceTitle icon={ChartNoAxesCombined}>统计</WorkspaceTitle>
      </div>
      <div className="activity-usage-filters">
        <UsageViewTabs />
      </div>
      <BadgeCalendar />
    </section>
  ) : (
    <UsageMetrics boot={boot} />
  )
}

function UsageMetrics({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const nativePreferences = useNativePreferences()
  const [preferenceError, setPreferenceError] = useState('')
  const foreground = useForeground()
  const status = useActivityStatus(foreground)
  const view = params.get('view') === 'quota' ? 'quota' : 'usage'
  const rawRange = params.get('range') || 'today'
  const range = ranges.some(([key]) => key === rawRange) ? rawRange : 'today'
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
  return (
    <section
      className={`activity-workspace activity-usage usage-view-${view}`}
      aria-label="用量和统计"
    >
      <div className="activity-heading">
        <WorkspaceTitle icon={ChartNoAxesCombined}>统计</WorkspaceTitle>
        <div className="usage-refresh">
          <span>
            更新于{' '}
            {dateTime(view === 'usage' ? snapshot.data?.generated_at : quota.data?.generated_at)}
          </span>
          <button
            type="button"
            className="icon-button"
            aria-label="刷新统计"
            disabled={snapshot.isFetching || quota.isFetching}
            onClick={() => {
              void snapshot.refetch()
              void quota.refetch()
              void status.refetch()
            }}
          >
            <RefreshCw
              size={16}
              className={snapshot.isFetching || quota.isFetching ? 'spin' : ''}
            />
          </button>
        </div>
      </div>
      <div className="activity-usage-filters">
        <UsageViewTabs />
        {view === 'usage' && (
          <label className="usage-range-select">
            <Select
              value={range}
              aria-label="统计时间范围"
              onChange={(event) => update('range', event.target.value)}
            >
              {ranges.map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </Select>
          </label>
        )}
        <label>
          Agent
          <Select
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
          </Select>
        </label>
        {view === 'usage' && (
          <>
            <label>
              模型
              <Select
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
              </Select>
            </label>
            <label>
              项目
              <Select
                value={project}
                aria-label="统计项目"
                onChange={(event) => update('project', event.target.value)}
              >
                <option value="">全部项目</option>
                {[
                  ...new Set([
                    ...(unfiltered?.project_stats.map((row) => usageProjectName(row.project)) ||
                      []),
                    ...(project ? [project] : []),
                  ]),
                ]
                  .sort()
                  .map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
              </Select>
            </label>
          </>
        )}
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
      {view === 'usage' && (
        <>
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
            <div className="usage-empty-state">
              <ChartNoAxesCombined size={30} />
              <strong>暂无用量数据</strong>
              <p>同步会话后，Token 用量与费用会显示在这里。</p>
            </div>
          ) : snapshot.isError ? (
            <Notice error={snapshot.error} retry={() => void snapshot.refetch()} />
          ) : snapshot.isPending ? (
            <QueryLoading
              className="activity-loading"
              text="正在汇总用量…"
              onRetry={() => void snapshot.refetch()}
            />
          ) : !data ||
            (!totals.tokens &&
              !totals.requests &&
              !totals.cost &&
              !totals.sessions &&
              !days.length) ? (
            <div className="usage-empty-state">
              <ChartNoAxesCombined size={30} />
              <strong>暂无用量数据</strong>
              <p>当前范围内没有用量记录，试试其他时间范围或筛选条件。</p>
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
                      project
                        ? int(totals.sessions)
                        : model
                          ? '—'
                          : int(data.quality.active_sessions)
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
                  <UsageChart
                    rows={days}
                    metric={metric}
                    hourly={range === 'today' || range === 'yesterday'}
                    today={range === 'today'}
                  />
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
                  <details>
                    <summary>统计口径</summary>
                    <p>
                      费用来自已索引记录及配置的模型价格；未记录的请求和不可计量的会话不计入 Token
                      总量。
                    </p>
                  </details>
                </div>
              </>
            )
          )}
        </>
      )}
      {view === 'quota' && (
        <section className="activity-quota-section">
          <div className="activity-panel-heading">
            <div>
              <h2>额度观察</h2>
              <p>最近一次保存的额度快照</p>
            </div>
            <RuntimeJobs
              boot={boot}
              kinds={['quota.refresh']}
              actions={[{ input: { kind: 'quota.refresh', ...(agent ? { agent } : {}) } }]}
            />
          </div>
          {quota.isPending && (
            <QueryLoading
              className="activity-loading"
              text="正在读取额度记录…"
              onRetry={() => void quota.refetch()}
            />
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
      )}
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

function UsageChart({
  rows,
  metric,
  hourly,
  today,
}: {
  rows: DayStats[]
  metric: Metric
  hourly: boolean
  today: boolean
}) {
  const [selected, setSelected] = useState<string>()
  if (!rows.length) return null
  const peak = Math.max(
    ...rows.map((row) => metricValue(row, metric)),
    metric === 'cost' ? 0.0001 : 1,
  )
  const magnitude = 10 ** Math.floor(Math.log10(peak / 4))
  const step = [1, 2, 5, 10].find((factor) => factor * magnitude >= peak / 4)! * magnitude
  const max = Math.max(metric === 'requests' ? 1 : 0, step) * 4
  const chosen = rows.find((row) => row.date === selected)
  const slots = hourly
    ? Array.from({ length: 24 }, (_, hour) => ({
        label: `${String(hour).padStart(2, '0')}时`,
        row: rows.find((row) => Number(row.date.slice(11, 13)) === hour),
        future: today && hour > new Date().getHours(),
      }))
    : rows.map((row) => ({ label: row.date.slice(5), row, future: false }))
  return (
    <>
      <div className="usage-chart-layout">
        <div className="usage-chart-axis" aria-hidden="true">
          {[1, 0.75, 0.5, 0.25, 0].map((ratio) => (
            <span key={ratio}>{formatMetric(max * ratio, metric)}</span>
          ))}
        </div>
        <div className="activity-chart" role="group" aria-label="用量趋势柱状图">
          {slots.map(({ row, label, future }, index) => (
            <button
              type="button"
              key={row?.date || label}
              className={`activity-bar-column ${row && selected === row.date ? 'selected' : ''} ${future ? 'usage-future' : ''}`}
              onClick={() => setSelected(row?.date)}
              aria-label={
                row
                  ? `${row.date}：${formatMetric(metricValue(row, metric), metric)}`
                  : `${label}：${future ? '尚未到达' : '无记录'}`
              }
              aria-pressed={!!row && selected === row.date}
            >
              <span className="activity-bar-value">
                {row
                  ? metric === 'tokens' || metric === 'requests'
                    ? int(metricValue(row, metric))
                    : money(metricValue(row, metric))
                  : future
                    ? '尚未到达'
                    : '无记录'}
              </span>
              <span className="activity-bar-track">
                <span
                  className="activity-bar"
                  style={{
                    height: row
                      ? `${Math.max((metricValue(row, metric) / max) * 100, metricValue(row, metric) > 0 ? 2 : 0)}%`
                      : '0%',
                  }}
                />
              </span>
              <span className="activity-bar-label">
                {slots.length < 12 ||
                index % Math.ceil(slots.length / 8) === 0 ||
                index === slots.length - 1
                  ? label
                  : ' '}
              </span>
            </button>
          ))}
        </div>
      </div>
      <div className="activity-chart-detail" aria-live="polite">
        {chosen &&
          `${chosen.date} · ${int(chosen.total_tokens)} Token · ${int(chosen.requests)} 次请求 · ${money(chosen.cost_usd)}`}
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
  const total = rows.reduce((sum, row) => sum + metricValue(row, metric), 0)
  return (
    <div className="activity-table-scroll">
      <table
        className={`activity-table usage-numeric-table ${group !== 'agent' ? 'usage-has-agent' : ''}`}
      >
        <thead>
          <tr>
            <th>{group === 'model' ? '模型' : group === 'project' ? '项目' : 'Agent'}</th>
            {group !== 'agent' && <th>Agent</th>}
            <th>Token</th>
            <th>缓存读取</th>
            <th>请求</th>
            <th>费用（USD）</th>
            <th>{metric === 'tokens' ? 'Token' : metric === 'cost' ? '费用' : '请求'}占比</th>
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
                <div className="usage-share">
                  <div
                    className="activity-table-meter"
                    title={formatMetric(metricValue(row, metric), metric)}
                  >
                    <span
                      style={{
                        width: `${(total > 0 ? metricValue(row, metric) / total : 0) * 100}%`,
                      }}
                    />
                  </div>
                  <span>{percent(total > 0 ? (metricValue(row, metric) / total) * 100 : 0)}</span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
