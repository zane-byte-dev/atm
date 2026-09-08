import { Select } from '../components/select'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams, Link } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive,
  Filter,
  Ellipsis,
  ArrowLeft,
  ArrowRight,
  BellOff,
  CheckCheck,
  ChevronRight,
  Circle,
  History,
  Inbox,
  LoaderCircle,
  Pencil,
  Plus,
  Settings2,
  X,
} from 'lucide-react'
import { WorkspaceTitle } from '../workspace-title'
import { WorkspaceSearchButton } from '../global-search'
import { call, errorText } from '../api'
import type { LegacyApiMethod } from '../api-contracts'
import { Markdown, Notice } from '../markdown'
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
import { CollectionContext } from './collection-context.tsx'
import { RuntimeJobs } from './runtime-jobs'
import { SourceEditor } from './source-editor'
import { useNativePreferences } from './native-preferences-react'
import { nativeOrdered } from './native-preferences'

const states = [
  ['active', '收件箱'],
  ['archived', '归档'],
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
  const nativePreferences = useNativePreferences()
  const sourceID = params.get('source') || ''
  const itemID = params.get('item') || ''
  const query = params.get('q') || ''
  const unreadOnly = params.get('unread') === '1'
  const [menu, setMenu] = useState<'filter' | 'more' | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!menu) return
    const close = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) setMenu(null)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenu(null)
    }
    document.addEventListener('pointerdown', close)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('pointerdown', close)
      document.removeEventListener('keydown', escape)
    }
  }, [menu])
  const panel = params.get('panel') || ''
  const state = states.some(([id]) => id === params.get('state')) ? params.get('state')! : 'active'
  const offset = Math.max(0, Number.parseInt(params.get('offset') || '0', 10) || 0)
  const requestedView = params.get('view')
  const sourceEditor = panel === 'new-source' || (panel === 'edit-source' && !!sourceID)
  const view =
    requestedView === 'sources' || sourceEditor || panel === 'source' || panel === 'overview'
      ? 'sources'
      : requestedView === 'history'
        ? 'history'
        : 'items'
  const sourcePanelOpen =
    sourceEditor || (view === 'sources' && (!!sourceID || panel === 'overview'))
  const showingItem = !!itemID && view === 'items'
  const [announcement, setAnnouncement] = useState('')
  const listRef = useRef<HTMLDivElement>(null)
  const detailRef = useRef<HTMLDivElement>(null)
  const client = useQueryClient()
  const writable = boot.capabilities?.workspace_write === true
  const bulk = useMutation({
    mutationFn: async (action: 'read' | 'archive' | 'archive-read') => {
      // Collect IDs before writes change ordering or remove rows from the filter.
      const snapshot = new Map<string, CollectionItem>()
      let offset = 0
      while (true) {
        const page = await call<CollectionList>('collect.items', {
          source_id: sourceID,
          state: 'active',
          query,
          unread_only: unreadOnly,
          limit: 100,
          offset,
        })
        for (const item of page.items) snapshot.set(item.id, item)
        offset += page.items.length
        if (!page.items.length || offset >= page.total) break
      }
      const targets = [...snapshot.values()].filter((item) =>
        action === 'read' ? !item.read_at : action === 'archive-read' ? !!item.read_at : true,
      )
      let completed = 0
      for (const item of targets) {
        try {
          await call(action === 'read' ? 'collect.item.read' : 'collect.item.archive', {
            item_id: item.id,
            ...(action === 'read' ? { read: true } : { archived: true }),
          })
          completed++
          setAnnouncement(`正在处理 ${completed} / ${targets.length} 条`)
        } catch (error) {
          throw new Error(
            `已处理 ${completed} / ${targets.length} 条，其余未处理。${errorText(error)}`,
          )
        }
      }
      return { completed, action }
    },
    onSuccess: ({ completed, action }) =>
      setAnnouncement(
        completed
          ? `已${action === 'read' ? '标为已读' : '归档'} ${completed} 条`
          : '没有需要处理的条目',
      ),
    onSettled: () => client.invalidateQueries({ queryKey: ['collection'] }),
  })
  const overview = useQuery({
    queryKey: ['collection', 'overview'],
    queryFn: ({ signal }) => call<CollectionOverview>('collect.overview', {}, signal),
    refetchInterval: 30_000,
  })
  const items = useQuery({
    queryKey: ['collection', 'items', sourceID, state, query, unreadOnly, offset],
    queryFn: ({ signal }) =>
      call<CollectionList>(
        'collect.items',
        { source_id: sourceID, state, query, unread_only: unreadOnly, limit: 50, offset },
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
  const readAttempts = useRef(new Set<string>())
  const markRead = useMutation({
    retry: false,
    mutationFn: (id: string) =>
      call<{ item: CollectionItem }>('collect.item.read', { item_id: id, read: true }),
    onSuccess: async (result, id) => {
      client.setQueryData(['collection', 'detail', id], result)
      await client.invalidateQueries({ queryKey: ['collection'] })
    },
  })
  const readItem = detail.data?.item
  useEffect(() => {
    if (
      !writable ||
      !showingItem ||
      !readItem ||
      readItem.id !== itemID ||
      readItem.read_at !== 0 ||
      readAttempts.current.has(itemID)
    )
      return
    // Only opening a successfully loaded detail marks it read. Guard repeated
    // renders and StrictMode effects; failures remain visible for explicit retry.
    readAttempts.current.add(itemID)
    markRead.mutate(itemID)
  }, [writable, showingItem, itemID, readItem, markRead.mutate])
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
    mutationFn: ({
      method,
      input,
    }: {
      method: Extract<LegacyApiMethod, `collect.${string}`>
      input: unknown
      message: string
    }) => call(method, input),
    onSuccess: async (_, variables) => {
      setAnnouncement(variables.message)
      await client.invalidateQueries({ queryKey: ['collection'] })
    },
  })
  useEffect(() => {
    mutation.reset()
    if (detailRef.current) detailRef.current.scrollTop = 0
  }, [itemID, sourceID])
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0
  }, [sourceID, state, query, offset, view])

  function filter(changes: Record<string, string | undefined>) {
    if (['source', 'item', 'view', 'panel'].some((key) => key in changes)) setAnnouncement('')
    setParams((current) => {
      const next = new URLSearchParams(current)
      for (const [key, value] of Object.entries(changes)) {
        if (value) next.set(key, value)
        else next.delete(key)
      }
      return next
    })
  }
  function change(
    method: Extract<LegacyApiMethod, `collect.${string}`>,
    input: unknown,
    message: string,
  ) {
    setAnnouncement('')
    mutation.mutate({ method, input, message })
  }
  const data = overview.data
  const orderedSources = nativeOrdered(
    data?.sources || [],
    nativePreferences.collection_source_order,
  )
  const source = data?.sources.find((value) => value.id === sourceID)
  const selected = detail.data?.item
  const selectedSource = data?.sources.find((value) => value.id === selected?.source_id)
  const latestRun = (id: string) => data?.runs.find((run) => run.source_id === id)

  return (
    <section
      className={`collection-workspace ${view === 'items' && items.isSuccess && !items.data.items.length ? 'collection-is-empty' : ''} collection-view-${view} ${showingItem ? 'has-selection' : ''} ${sourcePanelOpen ? 'show-source-detail' : ''}`}
      aria-label="收件箱工作区"
    >
      <div className="collection-heading">
        <div className="collection-heading-main">
          <WorkspaceTitle icon={Inbox}>收件箱</WorkspaceTitle>
          <div className="collection-mode-switch" role="group" aria-label="收件箱视图">
            <button
              type="button"
              aria-pressed={view === 'items'}
              onClick={() =>
                filter({ view: undefined, item: undefined, panel: undefined, offset: undefined })
              }
            >
              <Inbox size={14} /> 收件箱
            </button>
            <button
              type="button"
              aria-pressed={view !== 'items'}
              onClick={() =>
                filter({ view: 'sources', item: undefined, panel: undefined, offset: undefined })
              }
            >
              <Settings2 size={14} /> 来源与历史
            </button>
          </div>
        </div>
        <div className="workspace-actions">
          {view !== 'items' && (
            <>
              <RuntimeJobs
                boot={boot}
                kinds={['collect.run']}
                actions={[
                  {
                    label: sourceID ? '采集当前来源' : '立即采集',
                    input: { kind: 'collect.run', ...(sourceID ? { source_id: sourceID } : {}) },
                  },
                ]}
              />
              {writable && (
                <button
                  type="button"
                  className="button"
                  onClick={() =>
                    filter({
                      view: 'sources',
                      source: undefined,
                      item: undefined,
                      panel: 'new-source',
                    })
                  }
                >
                  <Plus size={14} />
                  新增来源
                </button>
              )}
            </>
          )}
        </div>
      </div>
      {overview.isError && <Notice error={overview.error} retry={() => void overview.refetch()} />}
      {overview.isPending && (
        <div className="collection-loading" role="status">
          <LoaderCircle className="spin" size={16} /> 正在读取采集状态…
        </div>
      )}
      {data && view === 'sources' && (
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
              <span className="collection-background-note">
                {data.worker_owned ? '本地服务管理' : '本地记录'}
              </span>
              <ChevronRight size={13} />
            </summary>
            <p>
              {data.worker_owned ? '当前本地服务负责自动采集。' : '当前服务尚未接管自动采集。'}
              最近状态：{data.worker_status || '尚无记录'}。
            </p>
          </details>
        </>
      )}
      <div className="collection-layout">
        {view !== 'items' && (
          <aside className="collection-sources" aria-label="采集来源">
            <div className="collection-column-label">
              来源 <span>{data?.sources.length ?? '—'}</span>
            </div>
            {view === 'sources' && (
              <button
                className={`collection-source ${!sourceID ? 'selected' : ''}`}
                aria-pressed={!sourceID}
                onClick={() =>
                  filter({
                    source: undefined,
                    item: undefined,
                    offset: undefined,
                    view: 'sources',
                    panel: 'overview',
                  })
                }
              >
                <Settings2 size={16} />
                <span>
                  <strong>运行概览</strong>
                  <small>查看采集状态</small>
                </span>
              </button>
            )}
            {orderedSources.map((value) => (
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
        )}
        {view !== 'sources' && (
          <div className="collection-results">
            <div className="collection-results-heading">
              <h2>{view === 'history' ? '本地消息' : state === 'archived' ? '归档' : '收件箱'}</h2>
              <WorkspaceSearchButton />
              {view === 'items' && (
                <div className="inbox-header-actions" ref={menuRef}>
                  <button
                    className="inbox-icon-button"
                    aria-label="筛选收件箱"
                    aria-expanded={menu === 'filter'}
                    aria-pressed={unreadOnly || !!sourceID || state === 'archived'}
                    onClick={() => setMenu(menu === 'filter' ? null : 'filter')}
                  >
                    <Filter size={18} />
                  </button>
                  <button
                    className="inbox-icon-button"
                    aria-label="更多收件箱操作"
                    aria-expanded={menu === 'more'}
                    onClick={() => setMenu(menu === 'more' ? null : 'more')}
                  >
                    <Ellipsis size={19} />
                  </button>
                  {menu === 'filter' && (
                    <div className="inbox-popover" aria-label="收件箱筛选">
                      <label className="inbox-unread">
                        <input
                          type="checkbox"
                          checked={state === 'archived'}
                          onChange={(event) =>
                            filter({
                              state: event.target.checked ? 'archived' : undefined,
                              offset: undefined,
                              item: undefined,
                            })
                          }
                        />
                        查看归档
                      </label>
                      <label className="inbox-unread">
                        <input
                          type="checkbox"
                          checked={unreadOnly}
                          onChange={(event) =>
                            filter({
                              unread: event.target.checked ? '1' : undefined,
                              offset: undefined,
                            })
                          }
                        />
                        只看未读
                      </label>
                      <label className="inbox-source-label">
                        来源
                        <Select
                          aria-label="筛选来源"
                          value={sourceID}
                          onChange={(event) =>
                            filter({
                              source: event.target.value || undefined,
                              offset: undefined,
                              item: undefined,
                            })
                          }
                        >
                          <option value="">全部来源</option>
                          {orderedSources.map((value) => (
                            <option value={value.id} key={value.id}>
                              {sourceName(value)}
                            </option>
                          ))}
                        </Select>
                      </label>
                      {(unreadOnly || sourceID || query || state === 'archived') && (
                        <button
                          onClick={() =>
                            filter({
                              unread: undefined,
                              state: undefined,
                              source: undefined,
                              q: undefined,
                              offset: undefined,
                            })
                          }
                        >
                          清除筛选
                        </button>
                      )}
                    </div>
                  )}
                  {menu === 'more' && (
                    <div className="inbox-popover inbox-bulk-actions" aria-label="收件箱批量操作">
                      <small>当前筛选范围 · 所有分页</small>
                      {writable && state === 'active' ? (
                        <>
                          <button
                            disabled={bulk.isPending}
                            onClick={() => {
                              setMenu(null)
                              bulk.mutate('read')
                            }}
                          >
                            <CheckCheck size={17} />
                            全部标为已读
                          </button>
                          <hr />
                          <button
                            disabled={bulk.isPending}
                            onClick={() => {
                              setMenu(null)
                              bulk.mutate('archive')
                            }}
                          >
                            <Archive size={17} />
                            归档全部
                          </button>
                          <button
                            disabled={bulk.isPending}
                            onClick={() => {
                              setMenu(null)
                              bulk.mutate('archive-read')
                            }}
                          >
                            <CheckCheck size={17} />
                            归档全部已读
                          </button>
                        </>
                      ) : (
                        <small>{writable ? '归档中的条目可在详情中恢复' : '当前为只读模式'}</small>
                      )}
                      <hr />
                      <button
                        onClick={() => {
                          setMenu(null)
                          filter({ view: 'sources', item: undefined, offset: undefined })
                        }}
                      >
                        <Settings2 size={17} />
                        管理来源
                      </button>
                    </div>
                  )}
                </div>
              )}
            </div>

            {view === 'items' ? (
              <>
                {announcement && (
                  <div className="collection-feedback" role="status">
                    {announcement}
                  </div>
                )}
                {bulk.isError && <Notice error={bulk.error} />}
                {(unreadOnly || sourceID) && (
                  <div className="inbox-filter-summary">
                    {unreadOnly && '只看未读'}
                    {unreadOnly && sourceID && ' · '}
                    {sourceID && (source ? sourceName(source) : sourceID)}
                  </div>
                )}
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
                        {overview.isPending ? (
                          <p role="status">正在读取来源状态…</p>
                        ) : overview.isError ? (
                          <Notice error={overview.error} retry={() => void overview.refetch()} />
                        ) : (
                          <>
                            <h3>
                              {query || sourceID || unreadOnly || offset
                                ? '没有匹配的消息'
                                : !data?.sources.length
                                  ? '先添加一个消息来源'
                                  : !data?.enabled || !data?.summary.enabled_sources
                                    ? '消息采集尚未启用'
                                    : state === 'archived'
                                      ? '还没有归档消息'
                                      : '收件箱暂时为空'}
                            </h3>
                            <p>
                              {query || sourceID || unreadOnly || offset
                                ? '清除筛选后查看其他消息。'
                                : !data?.sources.length
                                  ? '连接来源后，采集结果会显示在这里。'
                                  : !data?.enabled || !data?.summary.enabled_sources
                                    ? '前往来源管理查看配置，并启用需要的来源。'
                                    : data?.summary.failed_today
                                      ? '今天有采集处理失败的记录，可查看来源状态。'
                                      : '新采集的结果会显示在这里；已处理的内容可在归档中查看。'}
                            </p>
                            <div className="empty-actions">
                              {query || sourceID || unreadOnly || offset ? (
                                <button
                                  className="button"
                                  onClick={() =>
                                    filter({
                                      q: undefined,
                                      source: undefined,
                                      unread: undefined,
                                      offset: undefined,
                                    })
                                  }
                                >
                                  清除筛选
                                </button>
                              ) : (
                                <button
                                  className="button"
                                  onClick={() => filter({ view: 'sources', panel: 'overview' })}
                                >
                                  管理来源
                                </button>
                              )}
                              {state === 'active' ? (
                                <button
                                  className="button"
                                  onClick={() => filter({ state: 'archived', offset: undefined })}
                                >
                                  查看归档
                                </button>
                              ) : (
                                <button
                                  className="button"
                                  onClick={() => filter({ state: 'active', offset: undefined })}
                                >
                                  返回收件箱
                                </button>
                              )}
                            </div>
                          </>
                        )}
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
            ) : !sourceID ? (
              <div className="collection-empty">
                <History size={28} />
                <h3>选择一个来源</h3>
                <p>本地消息按来源保存，请先从左侧或上方选择来源。</p>
              </div>
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
        )}
        {view !== 'history' && (
          <aside
            className="collection-detail"
            aria-label={itemID ? '收件箱详情' : '来源与运行状态'}
          >
            <div className="collection-detail-bar">
              <span>
                {sourceEditor
                  ? panel === 'new-source'
                    ? '新增来源'
                    : '编辑来源'
                  : showingItem
                    ? '结果详情'
                    : view === 'sources' && source
                      ? '来源设置'
                      : view === 'sources'
                        ? '运行概览'
                        : '收件箱详情'}
              </span>
              {(showingItem || sourcePanelOpen) && (
                <button
                  className={`collection-icon-button ${!showingItem ? 'collection-mobile-only' : ''}`}
                  aria-label={showingItem ? '关闭结果详情' : '返回来源列表'}
                  onClick={() =>
                    showingItem
                      ? filter({ item: undefined, panel: undefined })
                      : filter({ source: undefined, item: undefined, panel: undefined })
                  }
                >
                  <ArrowLeft size={14} className="collection-mobile-only" />
                  <span className="collection-mobile-only">返回列表</span>
                  <X size={16} className="collection-desktop-only" />
                </button>
              )}
            </div>
            {announcement && view !== 'items' && (
              <div className="collection-feedback" role="status">
                <CheckCheck size={14} /> {announcement}
              </div>
            )}
            {mutation.isError && <Notice error={mutation.error} />}
            {sourceEditor && writable ? (
              <div className="collection-detail-scroll" ref={detailRef}>
                {panel === 'edit-source' && !source ? (
                  <p className="collection-muted">
                    {overview.isPending ? '正在读取来源…' : '这个来源已不存在，请返回来源列表。'}
                  </p>
                ) : (
                  <SourceEditor
                    key={panel === 'edit-source' ? sourceID : 'new-source'}
                    source={panel === 'edit-source' ? source : undefined}
                    onClose={() => filter({ view: 'sources', panel: undefined })}
                    onSaved={async (savedSource) => {
                      await client.invalidateQueries({ queryKey: ['collection'] })
                      filter({
                        view: 'sources',
                        source: savedSource?.id,
                        item: undefined,
                        panel: undefined,
                      })
                      setAnnouncement(savedSource ? '来源已保存' : '来源已删除')
                    }}
                  />
                )}
              </div>
            ) : showingItem ? (
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
                    {markRead.isError && markRead.variables === selected.id && (
                      <Notice error={markRead.error} retry={() => markRead.mutate(selected.id)} />
                    )}
                    <div className="collection-detail-header">
                      <h2>{selected.title || '收集记录'}</h2>
                      <div className="collection-actions">
                        <RuntimeJobs
                          key={selected.id}
                          boot={boot}
                          kinds={['collect.reprocess']}
                          actions={[
                            {
                              label: '重新处理',
                              input: { kind: 'collect.reprocess', item_id: selected.id },
                            },
                          ]}
                        />
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
                      <p className="collection-muted">
                        {sourceName(selectedSource)} ·{' '}
                        {stamp(selected.occurred_at || selected.updated_at)}
                      </p>
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
                      <CollectionContext key={selected.id} text={selected.raw_context} />
                    </div>
                  </>
                )}
              </>
            ) : view === 'sources' && source ? (
              <div className="collection-detail-scroll" ref={detailRef}>
                <SourcePanel
                  source={source}
                  run={latestRun(source.id)}
                  writable={writable}
                  busy={mutation.isPending}
                  change={change}
                  onEdit={() => filter({ panel: 'edit-source' })}
                  onHistory={() =>
                    filter({
                      view: 'history',
                      item: undefined,
                      panel: undefined,
                      offset: undefined,
                    })
                  }
                />
              </div>
            ) : view === 'sources' ? (
              <div className="collection-detail-scroll" ref={detailRef}>
                <p className="collection-muted">选择来源查看配置与本地消息历史。</p>
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
            ) : (
              <div className="collection-detail-scroll" ref={detailRef}>
                <div className="collection-empty">
                  <Inbox size={28} />
                  {items.isPending ? (
                    <p role="status">正在读取消息…</p>
                  ) : items.isError ? (
                    <p>读取失败，请在列表重试。</p>
                  ) : (
                    <>
                      <h3>选择一条消息</h3>
                      <p>打开详情即标为已读，处理完的内容可以归档。</p>
                    </>
                  )}
                </div>
              </div>
            )}
          </aside>
        )}
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
  onEdit,
  onHistory,
}: {
  source: CollectionSource
  run?: CollectionRun
  writable: boolean
  busy: boolean
  change: (
    method: Extract<LegacyApiMethod, `collect.${string}`>,
    input: unknown,
    message: string,
  ) => void
  onEdit: () => void
  onHistory: () => void
}) {
  return (
    <>
      <h2>{sourceName(source)}</h2>
      <div className="collection-source-panel-actions">
        <button type="button" className="button" onClick={onHistory}>
          <History size={14} />
          本地消息
        </button>
        {writable && (
          <button type="button" className="button" onClick={onEdit} disabled={busy}>
            <Pencil size={14} />
            编辑来源
          </button>
        )}
      </div>
      <p className="collection-muted">
        {source.connector} · {source.kind}
      </p>
      <div className="collection-source-settings">
        <label>
          <span>
            <strong>启用采集</strong>
            <small>允许采集服务处理此来源</small>
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
