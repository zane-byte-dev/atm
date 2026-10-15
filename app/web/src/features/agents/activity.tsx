import { WorkspaceListItem } from '../../components/workspace-list-item'
import {
  WorkspaceSplitLayout,
  WorkspaceNavigator,
  WorkspaceNavigatorHeading,
} from '../../components/workspace-navigator'
import { Button as AntButton, Checkbox, Collapse, Popover, Tag, Tooltip } from 'antd'
import { Select } from '../../components/select'
import { Fragment, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { ArrowLeft, ArrowRight, Filter, Copy, Info } from 'lucide-react'
import { call } from '../../api'
import { SessionChildren } from './session-tree'
import { WorkspaceSearchButton } from '../../global-search'
import { Markdown } from '../../components/markdown'
import { Notice } from '../../components/notice'
import { QueryLoading } from '../../components/query-loading'
import type { Bootstrap } from '../../types'
import type { SessionDetail, SessionList, SessionSearch, SessionSummary } from './activity-types'
import { RuntimeJobs } from '../notifications/runtime-jobs'
import { agentName, dateTime, int } from '../../format'
import { EmptyActivity } from '../agents/activity-shared'
import { useActivityStatus } from '../agents/activity-query'
import { useForeground } from '../../use-foreground'
import './activity.css'

const resultName = (value?: string) =>
  ({
    success: '已完成',
    succeeded: '已完成',
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
  if (!pending && total <= limit && !offset) return null
  return (
    <div className="activity-pagination">
      <span>
        {pending
          ? `— ${label}`
          : total
            ? `${int(offset + 1)}–${int(Math.min(offset + limit, total))} / ${int(total)} ${label}`
            : `0 ${label}`}
      </span>
      <AntButton
        htmlType="button"
        className="button subtle"
        aria-label="上一页"
        disabled={pending || !offset}
        onClick={() => onChange(Math.max(0, offset - limit))}
      >
        <ArrowLeft size={14} />
      </AntButton>
      <AntButton
        htmlType="button"
        className="button subtle"
        aria-label="下一页"
        disabled={pending || offset + limit >= total || offset + limit > maxOffset}
        onClick={() => onChange(offset + limit)}
      >
        <ArrowRight size={14} />
      </AntButton>
    </div>
  )
}

export function AgentsWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const foreground = useForeground()
  const status = useActivityStatus(foreground)
  const query = params.get('q') || ''
  const source = params.get('agent') || 'codex'
  const agent = source === 'all' ? '' : source
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
      <WorkspaceSplitLayout className={`activity-session-grid ${selected ? 'has-selection' : ''}`}>
        <WorkspaceNavigator
          as="section"
          variant="list"
          className="activity-list"
          aria-label={query ? '搜索结果' : '最近会话'}
        >
          <WorkspaceNavigatorHeading
            className="activity-heading"
            title={agent === 'codex' ? 'Codex 会话' : 'Agent 会话'}
            actions={
              <div className="workspace-actions">
                <WorkspaceSearchButton />
                <Popover
                  trigger="click"
                  placement="bottomRight"
                  open={filtersOpen}
                  onOpenChange={setFiltersOpen}
                  content={
                    <div className="agent-filter-content" role="region" aria-label="会话筛选">
                      <label>
                        Agent
                        <Select
                          aria-label="筛选 Agent"
                          value={source}
                          onChange={(e) => update({ agent: e.target.value }, true)}
                        >
                          <option value="codex">Codex · 主力来源</option>
                          <option value="all">全部来源（含辅助）</option>
                          {status.data?.agents
                            .filter((row) => row.agent !== 'codex')
                            .map((row) => (
                              <option key={row.agent} value={row.agent}>
                                {agentName(row.agent)} · 辅助来源
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
                      <Checkbox
                        className="agent-group-control"
                        checked={grouped}
                        disabled={!!query}
                        onChange={(e) => update({ children: e.target.checked ? '' : 'all' }, true)}
                      >
                        聚合子会话
                      </Checkbox>
                      {query && <small>搜索显示所有匹配会话，包括子会话。</small>}
                      <AntButton
                        type="text"
                        onClick={() =>
                          update({ agent: '', project: '', q: '', days: '', children: '' }, true)
                        }
                      >
                        恢复默认（Codex）
                      </AntButton>
                      <RuntimeJobs
                        boot={boot}
                        kinds={['session.sync']}
                        actions={[{ input: { kind: 'session.sync', ...(agent ? { agent } : {}) } }]}
                      />
                      <small>
                        每天 23:00 后后台采集一次，次日 06:00 前可补采。需要最新记录时可手动同步。
                      </small>
                      <AntButton className="button" onClick={() => setFiltersOpen(false)}>
                        收起筛选
                      </AntButton>
                    </div>
                  }
                >
                  <Tooltip title="筛选会话">
                    <AntButton
                      className="button"
                      aria-label="筛选会话"
                      aria-expanded={filtersOpen}
                      icon={<Filter size={15} />}
                    />
                  </Tooltip>
                </Popover>
              </div>
            }
          />
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
                <WorkspaceListItem
                  className="activity-session-row"
                  selected={selected === row.id}
                  onClick={() => update({ session: row.id, turn: '' })}
                  title={
                    row.summary ||
                    row.agent_nickname ||
                    row.first_q ||
                    row.snippet ||
                    `${row.is_subagent ? '子会话' : '会话'} ${row.short_id}`
                  }
                  label={agentName(row.agent)}
                  context={`${row.project || '未分配项目'} · ${row.role ? (row.role === 'user' ? '用户' : '助手') : `${row.q_count} 轮对话${row.is_subagent ? ' · 子会话' : ''}`}`}
                  timestamp={dateTime(row.last_at || row.created_at)}
                />
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
        </WorkspaceNavigator>
        <section className="activity-session-detail" aria-label="会话详情">
          {!selected ? (
            <EmptyActivity title="选择一个会话">
              阅读完整问答、查看执行工具和已记录的结果。
            </EmptyActivity>
          ) : (
            <>
              <AntButton
                htmlType="button"
                className="button subtle activity-back"
                onClick={() => update({ session: '', turn: '' })}
              >
                <ArrowLeft size={14} />
                返回会话列表
              </AntButton>
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
                  title={items.find((item) => item.id === detail.data.id)?.summary}
                  onPage={(value) => update({ turn: String(value) })}
                  onSelect={(id) => update({ session: id, turn: '' })}
                />
              )}
            </>
          )}
        </section>
      </WorkspaceSplitLayout>
    </section>
  )
}

function Conversation({
  data,
  title,
  onPage,
  onSelect,
}: {
  data: SessionDetail
  title?: string
  onPage: (offset: number) => void
  onSelect: (id: string) => void
}) {
  const tools = Object.entries(data.tools || {}).sort((a, b) => b[1] - a[1])
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')
  useEffect(() => setCopyState('idle'), [data.id, data.offset])
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
          <h2 title={title || data.summary || data.agent_nickname || data.first_q || '会话记录'}>
            {title || data.summary || data.agent_nickname || data.first_q || '会话记录'}
          </h2>
          <div className="activity-conversation-actions">
            <Popover
              trigger="click"
              placement="bottomRight"
              title="会话信息"
              content={
                <div className="agent-session-info">
                  <p>
                    {agentName(data.agent)} · {data.project || '未分配项目'} ·{' '}
                    {int(data.total_turns)} 轮对话
                  </p>
                  <p className="activity-session-id">{data.id}</p>
                  {data.parent_session_id && (
                    <AntButton type="link" onClick={() => onSelect(data.parent_session_id!)}>
                      查看父会话
                    </AntButton>
                  )}
                  {!!tools.length && (
                    <Collapse
                      ghost
                      size="small"
                      items={[
                        {
                          key: 'tools',
                          label: '工具调用',
                          children: (
                            <div>
                              {tools.map(([name, count]) => (
                                <Tag key={name}>
                                  {name} · {int(count)}
                                </Tag>
                              ))}
                            </div>
                          ),
                        },
                      ]}
                    />
                  )}
                  {(data.final_result || data.latest_progress) && (
                    <Collapse
                      ghost
                      size="small"
                      items={[
                        {
                          key: 'result',
                          label: '最近进展',
                          children: (
                            <Markdown text={data.final_result || data.latest_progress || ''} />
                          ),
                        },
                      ]}
                    />
                  )}
                </div>
              }
            >
              <Tooltip title="会话信息">
                <AntButton type="text" aria-label="会话信息" icon={<Info size={16} />} />
              </Tooltip>
            </Popover>
            <Tooltip
              title={
                copyState === 'copied' ? '已复制' : copyState === 'failed' ? '复制失败' : '复制对话'
              }
            >
              <AntButton
                type="text"
                aria-label="复制对话"
                onClick={() => void copyPage()}
                icon={<Copy size={16} />}
              />
            </Tooltip>
          </div>
        </div>
        <div className="activity-conversation-surface">
          <div className="agent-conversation-caption">
            <span>{agentName(data.agent)}</span>
            <span>{resultName(data.result_status)}</span>
            {data.is_subagent && <span>子会话</span>}
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
                  <Collapse
                    className="agent-progress-collapse"
                    ghost
                    size="small"
                    items={[
                      {
                        key: 'progress',
                        label: `${qa.progress.length} 条过程记录`,
                        children: qa.progress.map((text, i) => <Markdown key={i} text={text} />),
                      },
                    ]}
                  />
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
