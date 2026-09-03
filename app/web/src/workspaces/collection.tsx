import { useEffect, useRef, useState } from 'react'
import { useSearchParams, Link } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  ArrowLeft,
  ArrowRight,
  BellOff,
  CheckCheck,
  ChevronRight,
  Circle,
  Inbox,
  LoaderCircle,
  RefreshCw,
  Search,
  Settings2,
  X,
} from 'lucide-react'
import { call } from '../api'
import { Markdown, Notice } from '../editor'
import type { Bootstrap } from '../types'
import type {
  CollectionHistory,
  CollectionItem,
  CollectionList,
  CollectionOverview,
  CollectionRun,
  CollectionSource,
} from './collection-types'
import './collection.css'

const states = [
  ['active', '收件箱'],
  ['unread', '未读'],
  ['read', '已读'],
  ['archived', '已归档'],
  ['all', '全部'],
] as const
const actions: Record<string, string> = {
  create: '新建任务',
  append: '任务补充',
  insight: '结论',
  ignore: '已忽略',
  pending: '待处理',
  failed: '处理失败',
  reverted: '已撤销',
}
const runLabels: Record<string, string> = {
  running: '账本记录运行中',
  succeeded: '最近采集成功',
  failed: '最近采集失败',
}

function stamp(value?: number) {
  if (!value) return '尚无记录'
  return new Date(value * 1000).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function sourceName(source?: CollectionSource) {
  return source?.name || source?.external_id || '未知来源'
}

export function CollectionWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const sourceID = params.get('source') || ''
  const itemID = params.get('item') || ''
  const query = params.get('q') || ''
  const state = states.some(([id]) => id === params.get('state')) ? params.get('state')! : 'active'
  const offset = Math.max(0, Number.parseInt(params.get('offset') || '0', 10) || 0)
  const view = params.get('view') === 'history' && sourceID ? 'history' : 'items'
  const sourcePanelOpen = params.get('panel') === 'source' && !!sourceID
  const showingItem = !!itemID && view === 'items'
  const [search, setSearch] = useState(query)
  const [announcement, setAnnouncement] = useState('')
  const listRef = useRef<HTMLDivElement>(null)
  const detailRef = useRef<HTMLDivElement>(null)
  const client = useQueryClient()
  const writable = boot.capabilities?.workspace_write === true
  const overview = useQuery({
    queryKey: ['collection', 'overview'],
    queryFn: ({ signal }) => call<CollectionOverview>('collect.overview', {}, signal),
    refetchInterval: 30_000,
  })
  const items = useQuery({
    queryKey: ['collection', 'items', sourceID, state, query, offset],
    queryFn: ({ signal }) =>
      call<CollectionList>(
        'collect.items',
        { source_id: sourceID, state, query, limit: 50, offset },
        signal,
      ),
    enabled: view === 'items',
  })
  const detail = useQuery({
    queryKey: ['collection', 'detail', itemID],
    queryFn: ({ signal }) =>
      call<{ item: CollectionItem }>('collect.item.show', { item_id: itemID }, signal),
    enabled: !!itemID && view === 'items',
  })
  const history = useQuery({
    queryKey: ['collection', 'history', sourceID, query],
    queryFn: ({ signal }) =>
      call<CollectionHistory>(
        'collect.history',
        { source_id: sourceID, query, limit: 100 },
        signal,
      ),
    enabled: !!sourceID && view === 'history',
  })
  const mutation = useMutation({
    mutationFn: ({ method, input }: { method: string; input: unknown; message: string }) =>
      call(method, input),
    onSuccess: async (_, variables) => {
      setAnnouncement(variables.message)
      await client.invalidateQueries({ queryKey: ['collection'] })
    },
  })
  useEffect(() => {
    setSearch(query)
  }, [query])
  useEffect(() => {
    mutation.reset()
    setAnnouncement('')
    if (detailRef.current) detailRef.current.scrollTop = 0
  }, [itemID, sourceID])
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0
  }, [sourceID, state, query, offset, view])

  function filter(changes: Record<string, string | undefined>) {
    setParams((current) => {
      const next = new URLSearchParams(current)
      for (const [key, value] of Object.entries(changes)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      return next
    })
  }
  function change(method: string, input: unknown, message: string) {
    setAnnouncement('')
    mutation.mutate({ method, input, message })
  }
  const data = overview.data
  const source = data?.sources.find((value) => value.id === sourceID)
  const selected = detail.data?.item
  const selectedSource = data?.sources.find((value) => value.id === selected?.source_id)
  const refreshing =
    overview.isFetching || (view === 'items' ? items.isFetching : history.isFetching)
  const latestRun = (id: string) => data?.runs.find((run) => run.source_id === id)

  return (
    <section
      className={`collection-workspace ${showingItem ? 'has-selection' : ''} ${sourcePanelOpen ? 'show-source-detail' : ''}`}
      aria-label="收集工作区"
    >
      <div className="collection-heading">
        <div>
          <h1>收集</h1>
          <p>消息、结论与下一步。</p>
        </div>
        <button
          className="button"
          onClick={() => void client.invalidateQueries({ queryKey: ['collection'] })}
          disabled={refreshing}
        >
          <RefreshCw size={14} className={refreshing ? 'spin' : ''} /> 刷新
        </button>
      </div>
      {overview.isError && <Notice error={overview.error} retry={() => void overview.refetch()} />}
      {overview.isPending && (
        <div className="collection-loading" role="status">
          <LoaderCircle className="spin" size={16} /> 正在读取采集状态…
        </div>
      )}
      {data && (
        <>
          <div className="collection-metrics">
            <div>
              <span>待关注</span>
              <strong>{data.summary.unread_count}</strong>
              <small>待处理的收集结果</small>
            </div>
            <div>
              <span>今日消息</span>
              <strong>{data.summary.fetched_today}</strong>
              <small>
                {data.summary.insight_today} 条结论 · {data.summary.created_today} 个新任务
              </small>
            </div>
            <div>
              <span>已启用来源</span>
              <strong>
                {data.summary.enabled_sources}
                <em> / {data.summary.sources}</em>
              </strong>
              <small>全部 {data.summary.sources} 个来源</small>
            </div>
            <div>
              <span>本地消息</span>
              <strong>{data.messages.total}</strong>
              <small>{data.messages.conversations} 个会话</small>
            </div>
          </div>
          <details className="collection-background">
            <summary>
              <span className={`collection-dot ${data.enabled ? 'enabled' : ''}`} />
              <span>自动采集{data.enabled ? '已配置开启' : '已关闭'}</span>
              <span className="collection-background-note">由桌面端执行</span>
              <ChevronRight size={13} />
            </summary>
            <p>
              Web
              展示已保存的收集结果。后台采集仍由桌面端或其他本地进程执行，运行状态以最近记录为准。
            </p>
          </details>
        </>
      )}
      <div className="collection-layout">
        <aside className="collection-sources" aria-label="采集来源">
          <div className="collection-column-label">
            来源 <span>{data?.sources.length ?? '—'}</span>
          </div>
          <button
            className={`collection-source ${!sourceID ? 'selected' : ''}`}
            aria-pressed={!sourceID}
            onClick={() =>
              filter({
                source: undefined,
                item: undefined,
                offset: undefined,
                view: undefined,
                panel: undefined,
              })
            }
          >
            <Inbox size={16} />
            <span>
              <strong>全部来源</strong>
              <small>查看所有收集结果</small>
            </span>
          </button>
          {data?.sources.map((value) => (
            <button
              key={value.id}
              className={`collection-source ${sourceID === value.id ? 'selected' : ''}`}
              title={sourceName(value)}
              aria-pressed={sourceID === value.id}
              onClick={() =>
                filter({ source: value.id, item: undefined, offset: undefined, panel: undefined })
              }
            >
              <span className={`collection-dot ${value.enabled ? 'enabled' : ''}`} />
              <span>
                <strong>{sourceName(value)}</strong>
                <small className="collection-source-meta">
                  <span>{value.connector === 'dingtalk' ? '钉钉' : value.connector}</span>
                  <span>{value.enabled ? '已启用' : '已停用'}</span>
                  {value.muted && <span>静音</span>}
                </small>
                <small
                  className={latestRun(value.id)?.status === 'failed' ? 'collection-failure' : ''}
                >
                  {latestRun(value.id)
                    ? runLabels[latestRun(value.id)!.status] || latestRun(value.id)!.status
                    : '尚未采集'}
                </small>
              </span>
            </button>
          ))}
          {data?.sources.length === 0 && (
            <p className="collection-muted collection-source-empty">
              尚未配置采集来源。配置来源后，可在这里查看结果与本地消息。
            </p>
          )}
        </aside>
        <div className="collection-results">
          <div className="collection-results-heading">
            <h2>{sourceID ? sourceName(source) : '全部来源'}</h2>
            {sourceID && (
              <button
                className="collection-source-details-button"
                onClick={() => filter({ panel: 'source', item: undefined })}
              >
                <Settings2 size={14} /> 来源设置
              </button>
            )}
            {sourceID && (
              <div className="collection-view-switch" aria-label="内容类型">
                <button
                  aria-pressed={view === 'items'}
                  onClick={() => filter({ view: undefined, item: undefined, offset: undefined })}
                >
                  收集结果
                </button>
                <button
                  aria-pressed={view === 'history'}
                  onClick={() => filter({ view: 'history', item: undefined, offset: undefined })}
                >
                  本地消息
                </button>
              </div>
            )}
          </div>
          <form
            className="collection-search"
            onSubmit={(event) => {
              event.preventDefault()
              filter({ q: search.trim(), offset: undefined, item: undefined })
            }}
          >
            <Search size={15} />
            <input
              aria-label={view === 'history' ? '搜索本地消息' : '搜索收集结果'}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={view === 'history' ? '搜索本地消息内容…' : '搜索标题、内容、发送者…'}
            />
            {search && (
              <button
                type="button"
                aria-label="清除搜索"
                onClick={() => {
                  setSearch('')
                  filter({ q: undefined, offset: undefined })
                }}
              >
                <X size={13} />
              </button>
            )}
            <button type="submit">搜索</button>
          </form>
          {view === 'items' ? (
            <>
              <div className="collection-tabs" aria-label="结果状态">
                {states.map(([id, label]) => (
                  <button
                    key={id}
                    aria-pressed={state === id}
                    onClick={() => filter({ state: id, offset: undefined, item: undefined })}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {items.isPending && (
                <div className="collection-loading" role="status">
                  <LoaderCircle className="spin" size={16} /> 正在读取结果…
                </div>
              )}
              {items.isError && <Notice error={items.error} retry={() => void items.refetch()} />}
              {items.data && (
                <>
                  <div className="collection-count">
                    {items.data.total} 条结果{query && ` · “${query}”`}
                  </div>
                  {items.data.items.length === 0 ? (
                    <div className="collection-empty">
                      <Inbox size={28} />
                      <h3>{query ? '没有匹配结果' : '这里还没有结果'}</h3>
                      <p>
                        {query ? '试试其他关键词，或清除搜索。' : '可切换来源或状态查看已有记录。'}
                      </p>
                    </div>
                  ) : (
                    <div className="collection-item-list" ref={listRef}>
                      {items.data.items.map((item) => (
                        <button
                          className={`collection-item ${itemID === item.id ? 'selected' : ''}`}
                          key={item.id}
                          onClick={() => filter({ item: item.id, panel: undefined })}
                          aria-pressed={itemID === item.id}
                        >
                          <div className="collection-item-top">
                            <span
                              className={`collection-kind ${item.status === 'failed' ? 'failed' : ''}`}
                            >
                              {item.proposed_action
                                ? '待确认'
                                : actions[item.action] || item.action}
                            </span>
                            <time>{stamp(item.occurred_at || item.updated_at)}</time>
                          </div>
                          <h3>
                            {item.read_at === 0 && (
                              <span className="collection-unread-dot" aria-label="未读" />
                            )}
                            {item.title || item.summary.slice(0, 60) || '未命名记录'}
                          </h3>
                          {item.summary && <p>{item.summary}</p>}
                          <div className="collection-item-meta">
                            <span>
                              {sourceName(
                                data?.sources.find((value) => value.id === item.source_id),
                              )}
                            </span>
                            {item.sender && <span>{item.sender}</span>}
                            <ChevronRight size={13} />
                          </div>
                        </button>
                      ))}
                    </div>
                  )}
                  {items.data.total > 50 && (
                    <div className="collection-pagination">
                      <button
                        className="button"
                        disabled={offset === 0}
                        onClick={() =>
                          filter({ offset: String(Math.max(0, offset - 50)), item: undefined })
                        }
                      >
                        <ArrowLeft size={13} /> 上一页
                      </button>
                      <span>
                        {Math.floor(offset / 50) + 1} / {Math.ceil(items.data.total / 50)}
                      </span>
                      <button
                        className="button"
                        disabled={offset + 50 >= items.data.total}
                        onClick={() => filter({ offset: String(offset + 50), item: undefined })}
                      >
                        下一页 <ArrowRight size={13} />
                      </button>
                    </div>
                  )}
                </>
              )}
            </>
          ) : (
            <>
              <p className="collection-history-note">
                仅显示已同步的本地消息，最多 100 条；搜索不会连接外部服务。
              </p>
              {history.isPending && (
                <div className="collection-loading" role="status">
                  <LoaderCircle className="spin" size={16} /> 正在读取本地消息…
                </div>
              )}
              {history.isError && (
                <Notice error={history.error} retry={() => void history.refetch()} />
              )}
              {history.data?.messages.length === 0 && (
                <div className="collection-empty">
                  <Inbox size={28} />
                  <h3>没有本地消息</h3>
                  <p>{query ? '没有匹配的本地消息。' : '该来源尚未保存本地消息历史。'}</p>
                </div>
              )}
              <div className="collection-history-list" ref={listRef}>
                {history.data?.messages.map((message) => (
                  <article className="collection-message" key={message.message_id}>
                    <div>
                      <strong>{message.sender || '未知发送者'}</strong>
                      <time>{stamp(message.created_at)}</time>
                    </div>
                    <Markdown text={message.content} />
                  </article>
                ))}
              </div>
              {history.data && history.data.messages.length === history.data.limit && (
                <p className="collection-history-note">
                  已达到 100 条显示上限，可输入关键词缩小范围。
                </p>
              )}
            </>
          )}
        </div>
        <aside
          className="collection-detail"
          aria-label={itemID ? '收集结果详情' : '来源与运行状态'}
        >
          <div className="collection-detail-bar">
            <span>{showingItem ? '结果详情' : source ? '来源设置' : '最近运行'}</span>
            {(showingItem || sourcePanelOpen) && (
              <button
                className={`collection-icon-button ${!showingItem ? 'collection-mobile-only' : ''}`}
                aria-label={showingItem ? '关闭结果详情' : '返回收集结果'}
                onClick={() => filter({ item: undefined, panel: undefined })}
              >
                <ArrowLeft size={14} className="collection-mobile-only" />
                <span className="collection-mobile-only">返回列表</span>
                <X size={16} className="collection-desktop-only" />
              </button>
            )}
          </div>
          {announcement && (
            <div className="collection-feedback" role="status">
              <CheckCheck size={14} /> {announcement}
            </div>
          )}
          {mutation.isError && <Notice error={mutation.error} />}
          {showingItem ? (
            <>
              {detail.isPending && (
                <div className="collection-loading" role="status">
                  <LoaderCircle className="spin" size={16} /> 正在读取详情…
                </div>
              )}
              {detail.isError && (
                <Notice error={detail.error} retry={() => void detail.refetch()} />
              )}
              {selected && (
                <>
                  <div className="collection-detail-header">
                    <div className="collection-detail-meta">
                      <span className="collection-kind">
                        {selected.proposed_action
                          ? '待确认'
                          : actions[selected.action] || selected.action}
                      </span>
                      <span>
                        {selected.archived_at ? '已归档' : selected.read_at ? '已读' : '未读'}
                      </span>
                    </div>
                    <h2>{selected.title || '收集记录'}</h2>
                    <p className="collection-muted">
                      {sourceName(selectedSource)} ·{' '}
                      {stamp(selected.occurred_at || selected.updated_at)}
                    </p>
                    <div className="collection-actions">
                      <button
                        className="button"
                        disabled={!writable || mutation.isPending}
                        onClick={() =>
                          change(
                            'collect.item.read',
                            { item_id: selected.id, read: selected.read_at === 0 },
                            selected.read_at ? '已标为未读' : '已标为已读',
                          )
                        }
                      >
                        <CheckCheck size={14} />
                        {selected.read_at ? '标为未读' : '标为已读'}
                      </button>
                      <button
                        className="button"
                        disabled={!writable || mutation.isPending}
                        onClick={() =>
                          change(
                            'collect.item.archive',
                            { item_id: selected.id, archived: selected.archived_at === 0 },
                            selected.archived_at ? '结果已恢复' : '结果已归档',
                          )
                        }
                      >
                        <Archive size={14} />
                        {selected.archived_at ? '恢复' : '归档'}
                      </button>
                    </div>
                    {!writable && <p className="collection-readonly">只读预览</p>}
                  </div>
                  <div className="collection-detail-scroll" ref={detailRef}>
                    {selected.summary && (
                      <section className="collection-detail-section">
                        <h3>结论</h3>
                        <Markdown text={selected.summary} />
                      </section>
                    )}
                    {selected.reason && (
                      <section className="collection-detail-section">
                        <h3>判断依据</h3>
                        <Markdown text={selected.reason} />
                      </section>
                    )}
                    {selected.error && (
                      <div className="collection-record-error">
                        <strong>处理未完成</strong>
                        <p>{selected.error}</p>
                        {selected.retry_stopped && (
                          <span>自动重试已停止（{selected.attempts} 次）。</span>
                        )}
                      </div>
                    )}
                    {selected.todo_id && (
                      <p className="collection-linked">
                        <Link to={`/tasks/${encodeURIComponent(selected.todo_id)}`}>
                          查看任务 {selected.todo_id} <ChevronRight size={13} />
                        </Link>
                        <span>
                          {selected.todo_archived
                            ? '任务已归档'
                            : selected.todo_status === 'done'
                              ? '任务已完成'
                              : ''}
                        </span>
                      </p>
                    )}
                    {selected.knowledge_document_id && (
                      <p className="collection-muted">
                        已保存知识文档：{selected.knowledge_document_id}
                        {selected.knowledge_collection && ` · ${selected.knowledge_collection}`}
                      </p>
                    )}
                    <section className="collection-detail-section">
                      <h3>原始上下文</h3>
                      {selected.raw_context ? (
                        <Markdown text={selected.raw_context} />
                      ) : (
                        <p className="collection-muted">这条记录没有保存原始上下文。</p>
                      )}
                    </section>
                  </div>
                </>
              )}
            </>
          ) : source ? (
            <div className="collection-detail-scroll" ref={detailRef}>
              <SourcePanel
                source={source}
                run={latestRun(source.id)}
                writable={writable}
                busy={mutation.isPending}
                change={change}
              />
            </div>
          ) : (
            <div className="collection-detail-scroll" ref={detailRef}>
              <p className="collection-muted">
                选择结果阅读完整上下文，或选择来源查看配置与消息历史。
              </p>
              {data?.runs.length === 0 && (
                <div className="collection-empty">
                  <Circle size={24} />
                  <p>尚无采集运行记录</p>
                </div>
              )}
              {data?.runs.slice(0, 10).map((run) => (
                <div className="collection-run" key={run.id}>
                  <strong>
                    {sourceName(data.sources.find((value) => value.id === run.source_id))}
                  </strong>
                  <RunStatus run={run} />
                </div>
              ))}
            </div>
          )}
        </aside>
      </div>
    </section>
  )
}

function RunStatus({ run }: { run?: CollectionRun }) {
  if (!run) return <p className="collection-muted">尚无采集运行记录。</p>
  return (
    <div className="collection-run-status">
      <span className={run.status === 'failed' ? 'collection-failure' : ''}>
        {runLabels[run.status] || run.status}
      </span>
      <time>{stamp(run.finished_at || run.started_at)}</time>
      <p>
        读取 {run.fetched_count} 条 · 分析 {run.analyzed_count} 条
        {run.failed_count > 0 && ` · 失败 ${run.failed_count} 条`}
      </p>
      {run.error && <p className="collection-failure">{run.error}</p>}
      {run.status === 'running' && <p>运行记录尚未结束，无法据此确认外部进程仍在运行。</p>}
    </div>
  )
}

function SourcePanel({
  source,
  run,
  writable,
  busy,
  change,
}: {
  source: CollectionSource
  run?: CollectionRun
  writable: boolean
  busy: boolean
  change: (method: string, input: unknown, message: string) => void
}) {
  return (
    <>
      <h2>{sourceName(source)}</h2>
      <p className="collection-muted">
        {source.connector} · {source.kind}
      </p>
      <div className="collection-source-settings">
        <label>
          <span>
            <strong>启用采集</strong>
            <small>允许外部采集服务处理此来源</small>
          </span>
          <input
            type="checkbox"
            checked={source.enabled}
            disabled={!writable || busy}
            onChange={(event) =>
              change(
                'collect.source.enabled',
                { source_id: source.id, enabled: event.target.checked },
                event.target.checked ? '来源已启用' : '来源已停用',
              )
            }
          />
        </label>
        <label>
          <span>
            <strong>
              <BellOff size={13} /> 静音通知
            </strong>
            <small>保留采集结果与未读状态</small>
          </span>
          <input
            type="checkbox"
            checked={source.muted}
            disabled={!writable || busy}
            onChange={(event) =>
              change(
                'collect.source.muted',
                { source_id: source.id, muted: event.target.checked },
                event.target.checked ? '已静音此来源' : '已恢复此来源通知',
              )
            }
          />
        </label>
      </div>
      {!writable && <p className="collection-muted">当前连接只读，来源设置不可更改。</p>}
      <dl className="collection-source-facts">
        <dt>策略</dt>
        <dd>{source.strategy === 'observe' ? '观察与总结' : '任务跟进'}</dd>
        <dt>决策单位</dt>
        <dd>{source.decision_unit === 'message' ? '逐条消息' : '对话窗口'}</dd>
        <dt>采集间隔</dt>
        <dd>{source.interval_minutes} 分钟</dd>
        <dt>项目</dt>
        <dd>{source.project || '未指定'}</dd>
        <dt>优先级</dt>
        <dd>{source.priority}</dd>
      </dl>
      {source.instruction && (
        <section className="collection-detail-section">
          <h3>关注指令</h3>
          <Markdown text={source.instruction} />
        </section>
      )}
      {source.exclude_pattern && (
        <section className="collection-detail-section">
          <h3>排除规则</h3>
          <code>{source.exclude_pattern}</code>
        </section>
      )}
      <section className="collection-detail-section">
        <h3>最近运行</h3>
        <RunStatus run={run} />
      </section>
    </>
  )
}
