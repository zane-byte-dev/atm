import { Select } from '../components/select'
import { Fragment, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { ArrowLeft, ArrowRight, ArrowDownUp, Bot, Filter, Clock3, Copy } from 'lucide-react'
import { call } from '../api'
import { SessionChildren } from '../session-tree'
import { WorkspaceTitle } from '../workspace-title'
import { WorkspaceSearchButton } from '../global-search'
import { Markdown, Notice } from '../markdown'
import { QueryLoading } from '../query-loading'
import type { Bootstrap } from '../types'
import type { SessionDetail, SessionList, SessionSearch, SessionSummary } from './activity-types'
import { RuntimeJobs } from './runtime-jobs'
import {
  agentName,
  dateTime,
  int,
  EmptyActivity,
  useActivityStatus,
  useForeground,
} from './activity-shared'
import './activity.css'

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
const boundedInt = (value: string | null, fallback: number, max: number) => {
  const number = Number(value)
  return value !== null && Number.isInteger(number) && number >= 0 && number <= max
    ? number
    : fallback
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
