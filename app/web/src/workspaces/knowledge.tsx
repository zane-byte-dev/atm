import { Select } from '../components/select'
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Files,
  ArrowLeft,
  BookOpen,
  Check,
  ChevronRight,
  FileText,
  LoaderCircle,
  Pencil,
  Plus,
  Sparkles,
  X,
} from 'lucide-react'
import { ApiError, call, errorText } from '../api'
import type { Bootstrap } from '../types'
import { Markdown, Notice } from '../markdown'
import { QueryLoading } from '../query-loading'
import type {
  KnowledgeCollection,
  KnowledgeDocument,
  KnowledgeQueryInput,
  MemoryEventResult,
  MemoryHit,
} from './knowledge-types'
import { KnowledgeDetailPage } from './knowledge-detail-page'
import { WorkspaceTitle } from '../workspace-title'
import { KnowledgeManager } from './knowledge-manager'
import { useKnowledgeDocuments } from './knowledge-query'
import { KnowledgeActions } from './knowledge-actions'
import { InlineKnowledgeField, useInlineKnowledge } from './knowledge-inline'
import './knowledge.css'
import './workspace-forms.css'
import { useNativePreferences } from './native-preferences-react'
import { nativeOrdered } from './native-preferences'

type RoutePatch = Record<string, string | undefined>
const statusLabels: Record<string, string> = { active: '有效', draft: '草稿', archived: '已归档' }
const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }) : '—'
const scopeLabel = (scope: string) =>
  scope === 'global'
    ? '全局'
    : scope.startsWith('project:')
      ? `项目 · ${scope.slice(8)}`
      : scope.startsWith('session:')
        ? `会话 · ${scope.slice(8)}`
        : scope
const splitValues = (value: string) => [
  ...new Set(
    value
      .split(/[,，\n]/)
      .map((item) => item.trim())
      .filter(Boolean),
  ),
]

export function KnowledgeWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const queryClient = useQueryClient()
  const nativePreferences = useNativePreferences()
  const memoryTab = params.get('tab') === 'memory'
  const collection = params.get('collection') || ''
  const documentID = params.get('document') || ''
  const memoryID = params.get('memory') || ''
  const text = params.get('q') || ''
  const scope = params.get('scope') || ''
  const status = (params.get('status') || '') as NonNullable<KnowledgeQueryInput['status']>
  const compose = params.get('compose') || ''
  const writable = boot.capabilities?.workspace_write === true
  const listPositions = useRef(new Map<string, number>())
  const selectedItem = useRef('')
  const wasDetailOpen = useRef(false)
  const listKey = JSON.stringify([memoryTab, collection, text, scope, status])
  const [busy, setBusy] = useState(false)
  const [managerCollection, setManagerCollection] = useState<string | null>(null)
  const resultsRef = useRef<HTMLDivElement>(null)
  const detailRef = useRef<HTMLElement>(null)
  useEffect(() => {
    detailRef.current?.scrollTo({ top: 0 })
  }, [documentID, memoryID, compose])
  const route = (patch: RoutePatch) => {
    if (busy) return
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      for (const [key, value] of Object.entries(patch))
        value ? next.set(key, value) : next.delete(key)
      return next
    })
  }
  const catalog = useQuery({
    queryKey: ['knowledge', 'catalog'],
    queryFn: ({ signal }) => call('knowledge.catalog', {}, signal),
  })
  const documents = useKnowledgeDocuments(
    { collection, text, status, exclude_archived: !status },
    !memoryTab,
  )
  const memories = useQuery({
    queryKey: ['knowledge', 'memory', text, scope],
    queryFn: ({ signal }) =>
      call<{ hits: MemoryHit[] }>('memory.recall', { query: text, scope, limit: 200 }, signal),
    enabled: memoryTab,
  })
  const document = useQuery({
    queryKey: ['knowledge', 'document', documentID],
    queryFn: ({ signal }) => call('knowledge.document.get', { document_id: documentID }, signal),
    enabled: !memoryTab && !!documentID,
  })
  const memory = useQuery({
    queryKey: ['knowledge', 'memory-detail', memoryID],
    queryFn: ({ signal }) => call<MemoryHit>('memory.get', { memory_id: memoryID }, signal),
    enabled: memoryTab && !!memoryID,
  })
  const orderedCollections = nativeOrdered(
    catalog.data || [],
    nativePreferences.knowledge_collection_order,
  )
  const activeCollection = catalog.data?.find((item) => item.id === collection)
  const closeEditor = () => route({ compose: undefined })
  const onCreatedDocument = async (result: KnowledgeDocument) => {
    await queryClient.invalidateQueries({ queryKey: ['knowledge'] })
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('document', result.metadata.id)
      next.set('collection', result.collection)
      ;['compose', 'q', 'status'].forEach((key) => next.delete(key))
      return next
    })
  }
  const onSavedMemory = async (result: MemoryEventResult) => {
    await queryClient.invalidateQueries({ queryKey: ['knowledge'] })
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('memory', result.event.id)
      next.set('tab', 'memory')
      ;['compose', 'q'].forEach((key) => next.delete(key))
      return next
    })
  }
  const collectionLanding =
    !documentID &&
    !memoryID &&
    !compose &&
    !text &&
    !status &&
    !scope &&
    params.get('view') !== 'search'
  const detailOpen = !!compose || (memoryTab ? !!memoryID : !!documentID)
  const readingDetail =
    !compose && (memoryTab ? !!memoryID && !!memory.data : !!documentID && !!document.data)
  const collectionName = (id: string) => catalog.data?.find((item) => item.id === id)?.name || id
  const listTitle = memoryTab
    ? '共享记忆'
    : activeCollection?.name || statusLabels[status] || '知识文档'
  const openItem = (kind: 'document' | 'memory', id: string) => {
    selectedItem.current = id
    listPositions.current.set(listKey, resultsRef.current?.scrollTop || 0)
    route({ [kind]: id, compose: undefined })
  }
  useLayoutEffect(() => {
    if (!detailOpen && resultsRef.current) {
      resultsRef.current.scrollTop = listPositions.current.get(listKey) || 0
      if (wasDetailOpen.current) {
        const row = Array.from(
          resultsRef.current.querySelectorAll<HTMLButtonElement>('[data-knowledge-id]'),
        ).find((item) => item.dataset.knowledgeId === selectedItem.current)
        row?.focus({ preventScroll: true })
      }
    } else if (detailOpen && !wasDetailOpen.current && !compose) {
      detailRef.current?.focus({ preventScroll: true })
    }
    wasDetailOpen.current = detailOpen
  }, [detailOpen, listKey, compose])

  return (
    <section className="knowledge-workspace knowledge-browser" aria-label="知识与共享记忆">
      <aside className="knowledge-collections" aria-label="知识导航">
        <div className="knowledge-navigation-heading">
          <WorkspaceTitle icon={BookOpen}>知识</WorkspaceTitle>
        </div>
        <nav aria-label="知识内容">
          <div className="knowledge-section-label">
            <span>知识集合</span>
            {writable && (
              <button
                type="button"
                aria-label="新建集合"
                disabled={busy}
                onClick={() =>
                  route({
                    tab: undefined,
                    compose: 'collection',
                    document: undefined,
                    memory: undefined,
                  })
                }
              >
                <Plus size={15} />
              </button>
            )}
          </div>
          <CollectionBranch
            key="special:memory"
            item={{ id: 'shared-memory', name: '共享记忆', description: '', document_count: 0 }}
            memoryCollection
            active={memoryTab}
            documentID={memoryTab ? memoryID : ''}
            busy={busy}
            writable={writable}
            onOpen={(id) => {
              selectedItem.current = id
              route({
                tab: 'memory',
                memory: id,
                collection: undefined,
                document: undefined,
                compose: undefined,
                q: undefined,
                scope: undefined,
                status: undefined,
                view: undefined,
              })
            }}
            onCreate={() =>
              route({
                tab: 'memory',
                compose: 'memory',
                memory: undefined,
                collection: undefined,
                document: undefined,
                q: undefined,
                scope: undefined,
                status: undefined,
                view: undefined,
              })
            }
          />
          {catalog.isPending && <Loading label="读取集合…" retry={() => void catalog.refetch()} />}
          {catalog.error && <Notice error={catalog.error} retry={() => void catalog.refetch()} />}
          {orderedCollections.map((item) => (
            <CollectionBranch
              key={item.id}
              item={item}
              onManage={() => setManagerCollection(item.id)}
              active={!memoryTab && collection === item.id}
              documentID={!memoryTab ? documentID : ''}
              busy={busy}
              writable={writable}
              onOpen={(id) => {
                selectedItem.current = id
                route({
                  tab: undefined,
                  collection: item.id,
                  document: id,
                  memory: undefined,
                  compose: undefined,
                  q: undefined,
                  status: undefined,
                })
              }}
              onCreate={() =>
                route({
                  tab: undefined,
                  collection: item.id,
                  compose: 'document',
                  document: undefined,
                  memory: undefined,
                  q: undefined,
                  status: undefined,
                })
              }
            />
          ))}
          {catalog.data?.length === 0 && (
            <p className="knowledge-side-note">
              尚无知识集合。{writable ? '点击上方 + 创建第一个集合。' : ''}
            </p>
          )}
          <div className="knowledge-section-label">
            <span>管理</span>
          </div>
          <button
            type="button"
            className="knowledge-collection-row"
            disabled={busy}
            onClick={() => setManagerCollection('')}
          >
            <Files size={16} />
            <span>文档管理</span>
          </button>
        </nav>
      </aside>
      <div className="knowledge-main">
        <div className="knowledge-breadcrumb" hidden={collectionLanding || readingDetail}>
          <span>知识</span>
          <span aria-hidden="true">/</span>
          <span>{listTitle}</span>
        </div>
        <section
          className="knowledge-results"
          aria-label={memoryTab ? '共享记忆列表' : '文档列表'}
          hidden={detailOpen || collectionLanding}
        >
          <header className="knowledge-browse-header">
            <div className="knowledge-browse-title">
              <h2>{listTitle}</h2>
              {writable && (
                <button
                  type="button"
                  className="button primary"
                  disabled={busy || (!memoryTab && !catalog.data?.length)}
                  onClick={() =>
                    route({
                      compose: memoryTab ? 'memory' : 'document',
                      document: undefined,
                      memory: undefined,
                    })
                  }
                >
                  <Plus size={16} />
                  {memoryTab ? '新增记忆' : '新建文档'}
                </button>
              )}
            </div>
            <p>
              {memoryTab
                ? '跨任务复用的简短约定，清楚标记适用范围与来源。'
                : '按集合整理，让经验成为下次工作的起点。'}
            </p>
            {!memoryTab && activeCollection && (
              <details className="knowledge-collection-about" key={activeCollection.id}>
                <summary>集合说明</summary>
                <p>{activeCollection.description || '此集合暂无说明。'}</p>
                {activeCollection.role && (
                  <span className="knowledge-pill">{activeCollection.role}</span>
                )}
                <Tags values={activeCollection.topics} />
                <Guidance label="适合何时使用" items={activeCollection.use_when} />
                <Guidance label="不适用情况" items={activeCollection.avoid_when} />
                <Guidance label="使用说明" items={activeCollection.instructions} />
              </details>
            )}
            <div className="knowledge-list-filters">
              {text && (
                <button
                  type="button"
                  className="text-button"
                  disabled={busy}
                  onClick={() => route({ q: undefined })}
                >
                  清除关键词：{text}
                  <X size={14} />
                </button>
              )}
              {!memoryTab && (
                <Select
                  aria-label="文档状态"
                  value={status}
                  disabled={busy}
                  onChange={(event) =>
                    route({ status: event.target.value, document: undefined, compose: undefined })
                  }
                >
                  <option value="">全部状态</option>
                  <option value="active">有效</option>
                  <option value="draft">草稿</option>
                  <option value="archived">已归档</option>
                </Select>
              )}
            </div>
          </header>
          <div className="knowledge-list-heading">
            <span aria-live="polite">
              {memoryTab ? (memories.data?.hits.length ?? '—') : (documents.total ?? '—')}
              {memoryTab ? ' 条记忆' : ' 篇文档'}
            </span>
            <span>{text ? '按相关性排序' : memoryTab ? '记忆内容与来源' : '最近更新 ↓'}</span>
          </div>
          <div
            className="knowledge-results-scroll"
            ref={resultsRef}
            onScroll={(event) => {
              if (!detailOpen) listPositions.current.set(listKey, event.currentTarget.scrollTop)
            }}
          >
            {memoryTab ? (
              <>
                {memories.isPending && (
                  <Loading label="读取共享记忆…" retry={() => void memories.refetch()} />
                )}
                {memories.error && (
                  <Notice error={memories.error} retry={() => void memories.refetch()} />
                )}
                {memories.data?.hits.length === 0 && (
                  <Empty
                    icon="memory"
                    title={text || scope ? '没有匹配的记忆' : '还没有共享记忆'}
                    detail={
                      text || scope ? '试试其他关键词或切换范围。' : '记下项目约定与可复用的经验。'
                    }
                  />
                )}
                {(memories.data?.hits || []).map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    disabled={busy}
                    className="knowledge-result-row knowledge-memory-row"
                    data-knowledge-id={item.id}
                    onClick={() => openItem('memory', item.id)}
                  >
                    <Sparkles size={18} className="knowledge-row-icon" />
                    <div className="knowledge-row-content">
                      <h3>{item.content.trim().split(/\r?\n/)[0] || '未命名记忆'}</h3>
                      {item.content.trim().includes('\n') && (
                        <p className="knowledge-snippet">
                          {item.content.trim().split(/\r?\n/).slice(1).join(' ')}
                        </p>
                      )}
                      <div className="knowledge-result-meta">
                        <span className="knowledge-memory-scope">{scopeLabel(item.scope)}</span>
                        <span className="knowledge-memory-source">
                          来源：{item.metadata?.source || item.source || '未记录'}
                        </span>
                      </div>
                    </div>
                    <time className="knowledge-row-date">{formatDate(item.created_at)}</time>
                  </button>
                ))}
                {(memories.data?.hits.length || 0) >= 200 && (
                  <p className="knowledge-list-footnote">已显示前 200 条，请使用关键词缩小范围。</p>
                )}
              </>
            ) : (
              <>
                {documents.isPending && (
                  <Loading label="读取文档…" retry={() => void documents.refresh()} />
                )}
                {documents.error && (
                  <Notice error={documents.error} retry={() => void documents.refresh()} />
                )}
                {!documents.isPending && documents.documents.length === 0 && (
                  <Empty
                    icon="document"
                    title={text || status ? '没有匹配的文档' : '这里还没有文档'}
                    detail={
                      text || status
                        ? '试试其他关键词或状态。'
                        : writable
                          ? '选择集合后，新建一篇文档。'
                          : '当前集合没有可浏览的文档。'
                    }
                  />
                )}
                {documents.documents.map((item) => (
                  <button
                    key={item.document_id}
                    type="button"
                    disabled={busy}
                    className="knowledge-result-row"
                    data-knowledge-id={item.document_id}
                    onClick={() => openItem('document', item.document_id)}
                  >
                    <FileText size={18} className="knowledge-row-icon" />
                    <div className="knowledge-row-content">
                      <div className="knowledge-row-title">
                        <h3>{item.title}</h3>
                        {item.status !== 'active' && (
                          <span className={`knowledge-status ${item.status}`}>
                            {statusLabels[item.status] || item.status}
                          </span>
                        )}
                      </div>
                      {item.snippet && <p className="knowledge-snippet">{item.snippet}</p>}
                      <div className="knowledge-result-meta">
                        <span>{collectionName(item.collection)}</span>
                        {item.projects?.length ? <span>{item.projects.join(' · ')}</span> : null}
                      </div>
                    </div>
                    {item.updated_at && (
                      <time className="knowledge-row-date">{formatDate(item.updated_at)}</time>
                    )}
                  </button>
                ))}
                <div className="knowledge-list-footnote">
                  {documents.total !== undefined && (
                    <span>
                      已加载 {documents.documents.length} / {documents.total} 篇文档
                    </span>
                  )}
                  {documents.hasNextPage && (
                    <button
                      className="text-button"
                      disabled={busy || documents.isFetching}
                      onClick={() => void documents.fetchNextPage()}
                    >
                      {documents.isFetchingNextPage ? '正在加载…' : '加载更多文档'}
                    </button>
                  )}
                </div>
              </>
            )}
          </div>
        </section>
        {collectionLanding && (
          <Empty icon="document" title="选择文档或记忆" detail="展开左侧集合，选择内容开始阅读。" />
        )}
        <section
          className="knowledge-detail"
          ref={detailRef}
          tabIndex={-1}
          hidden={!detailOpen}
          aria-label={compose ? '编辑内容' : '所选内容'}
        >
          <div className="knowledge-detail-toolbar" hidden={readingDetail}>
            <button
              type="button"
              className="text-button knowledge-back"
              disabled={busy}
              onClick={() => route({ document: undefined, memory: undefined, compose: undefined })}
            >
              <ArrowLeft size={15} />
              {params.get('view') === 'search' || text || status ? '返回列表' : '返回导航'}
            </button>
            {compose && (
              <button type="button" className="text-button" disabled={busy} onClick={closeEditor}>
                <X size={15} />
                关闭编辑
              </button>
            )}
          </div>
          {compose && !writable ? (
            <Empty
              icon="document"
              title="当前连接只读"
              detail="可以浏览知识与记忆；写入能力开启后可在这里编辑。"
            />
          ) : compose === 'collection' ? (
            <CollectionComposer
              key="collection-create"
              onBusy={setBusy}
              onClose={closeEditor}
              onSaved={async (result) => {
                await queryClient.invalidateQueries({ queryKey: ['knowledge'] })
                setParams((previous) => {
                  const next = new URLSearchParams(previous)
                  next.set('collection', result.id)
                  ;['document', 'compose'].forEach((key) => next.delete(key))
                  return next
                })
              }}
            />
          ) : !memoryTab && (compose === 'document' || compose === 'copy' || compose === 'edit') ? (
            (compose === 'copy' || compose === 'edit') && !document.data ? (
              <DetailRequest
                pending={document.isPending}
                error={document.error}
                retry={() => void document.refetch()}
              />
            ) : (
              <DocumentComposer
                key={`document:${compose}:${collection}:${compose === 'copy' || compose === 'edit' ? documentID : ''}`}
                collections={orderedCollections}
                collection={collection}
                original={compose === 'copy' || compose === 'edit' ? document.data : undefined}
                editing={compose === 'edit'}
                onBusy={setBusy}
                onClose={closeEditor}
                onSaved={onCreatedDocument}
              />
            )
          ) : memoryTab && (compose === 'memory' || compose === 'supersede') ? (
            compose === 'supersede' && !memory.data ? (
              <DetailRequest
                pending={memory.isPending}
                error={memory.error}
                retry={() => void memory.refetch()}
              />
            ) : (
              <MemoryComposer
                key={`memory:${compose}:${compose === 'supersede' ? memoryID : scope}`}
                scope={scope}
                original={compose === 'supersede' ? memory.data : undefined}
                onBusy={setBusy}
                onClose={closeEditor}
                onSaved={onSavedMemory}
              />
            )
          ) : memoryTab ? (
            memoryID ? (
              <>
                <DetailRequest
                  pending={memory.isPending}
                  error={memory.error}
                  retry={() => void memory.refetch()}
                />
                {memory.data && (
                  <MemoryDetail
                    key={memory.data.id}
                    memory={memory.data}
                    writable={writable}
                    onEdit={() => route({ compose: 'supersede' })}
                  />
                )}
              </>
            ) : (
              <Empty icon="memory" title="选择一条记忆" detail="查看完整内容、作用范围与来源。" />
            )
          ) : documentID ? (
            <>
              <DetailRequest
                pending={document.isPending}
                error={document.error}
                retry={() => void document.refetch()}
              />
              {document.data && (
                <DocumentDetail
                  key={document.data.metadata.id}
                  collectionLabel={collectionName(document.data.collection)}
                  collections={orderedCollections}
                  onSaved={onCreatedDocument}
                  onBusy={setBusy}
                  busy={busy}
                  document={document.data}
                  writable={writable}
                  onCopy={() => route({ compose: 'copy' })}
                  onEdit={() => route({ compose: 'edit' })}
                />
              )}
            </>
          ) : (
            <Empty
              icon="document"
              title="选择一篇文档"
              detail="浏览集合，或通过关键词找到需要的经验。"
            />
          )}
        </section>
      </div>
      {managerCollection !== null && (
        <KnowledgeManager
          initialCollection={managerCollection}
          collections={orderedCollections}
          writable={writable}
          onClose={() => setManagerCollection(null)}
          onOpen={(doc) => {
            setManagerCollection(null)
            route({
              tab: undefined,
              collection: doc.collection,
              document: doc.document_id,
              memory: undefined,
              compose: undefined,
              q: undefined,
              status: undefined,
              scope: undefined,
              view: undefined,
            })
          }}
        />
      )}
    </section>
  )
}

function CollectionBranch({
  item,
  memoryCollection = false,
  onManage,
  active,
  documentID,
  busy,
  writable,
  onOpen,
  onCreate,
}: {
  item: KnowledgeCollection
  memoryCollection?: boolean
  onManage?: () => void
  active: boolean
  documentID: string
  busy: boolean
  writable: boolean
  onOpen: (id: string) => void
  onCreate: () => void
}) {
  const [expanded, setExpanded] = useState(active)
  const childrenID = useId()
  useEffect(() => {
    if (active) setExpanded(true)
  }, [active, documentID])
  const documents = useKnowledgeDocuments(
    { collection: item.id, exclude_archived: true },
    expanded && !memoryCollection,
  )
  const memories = useQuery({
    queryKey: ['knowledge', 'memory', '', ''],
    queryFn: ({ signal }) =>
      call<{ hits: MemoryHit[] }>('memory.recall', { query: '', scope: '', limit: 200 }, signal),
    enabled: expanded && memoryCollection,
  })
  const rows = memoryCollection
    ? memories.data?.hits.map((memory) => ({
        document_id: memory.id,
        title: memory.content.trim().split(/\r?\n/)[0] || '未命名记忆',
      }))
    : documents.documents
  const pending = memoryCollection ? memories.isPending : documents.isPending
  const error = memoryCollection ? memories.error : documents.error
  const retry = () => {
    if (memoryCollection) void memories.refetch()
    else void documents.refresh()
  }
  const contentLabel = memoryCollection ? '记忆' : '文档'
  return (
    <div className="knowledge-branch">
      <div className="knowledge-branch-heading">
        <button
          type="button"
          className="knowledge-branch-toggle"
          aria-label={`${expanded ? '收起' : '展开'}${item.name || item.id}`}
          aria-expanded={expanded}
          aria-controls={childrenID}
          disabled={busy}
          onClick={() => setExpanded((value) => !value)}
        >
          <ChevronRight size={15} />
        </button>
        <button
          type="button"
          className="knowledge-branch-title"
          title={item.name || item.id}
          aria-expanded={expanded}
          aria-controls={childrenID}
          disabled={busy}
          onClick={() => setExpanded((value) => !value)}
        >
          {memoryCollection && <Sparkles size={14} />}
          {!memoryCollection && <BookOpen size={14} />}
          <span>{item.name || item.id}</span>
          <small>
            {memoryCollection
              ? rows
                ? `${rows.length}${rows.length >= 200 ? '+' : ''}`
                : ''
              : item.document_count}
          </small>
        </button>
        {onManage && (
          <button
            type="button"
            className="knowledge-branch-manage"
            aria-label={`管理${item.name || item.id}的文档`}
            title="文档管理"
            disabled={busy}
            onClick={onManage}
          >
            <Files size={14} />
          </button>
        )}
        {writable && (
          <button
            type="button"
            className="knowledge-branch-add"
            aria-label={`在${item.name || item.id}中新建${contentLabel}`}
            title={`新建${contentLabel}`}
            disabled={busy}
            onClick={() => {
              setExpanded(true)
              onCreate()
            }}
          >
            <Plus size={15} />
          </button>
        )}
      </div>
      <div id={childrenID} className="knowledge-branch-documents" hidden={!expanded}>
        {expanded && (
          <>
            {pending && <Loading label={`读取${contentLabel}…`} retry={retry} />}
            {error && <Notice error={error} retry={retry} />}
            {rows?.map((doc) => (
              <button
                type="button"
                key={doc.document_id}
                className={`knowledge-collection-row ${active && documentID === doc.document_id ? 'selected' : ''}`}
                aria-current={active && documentID === doc.document_id ? 'page' : undefined}
                title={doc.title}
                disabled={busy}
                onClick={() => onOpen(doc.document_id)}
              >
                {memoryCollection ? <Sparkles size={16} /> : <FileText size={16} />}
                <span>{doc.title || `未命名${contentLabel}`}</span>
              </button>
            ))}
            {rows?.length === 0 && <p className="knowledge-side-note">暂无{contentLabel}</p>}
            {!memoryCollection && documents.hasNextPage && (
              <button
                className="text-button"
                disabled={busy || documents.isFetching}
                onClick={() => void documents.fetchNextPage()}
              >
                {documents.isFetchingNextPage ? '正在加载…' : '加载更多文档'}
              </button>
            )}
            {memoryCollection && (rows?.length || 0) >= 200 && (
              <p className="knowledge-side-note">已显示前 200 条记忆，请使用关键词缩小范围。</p>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function Loading({ label, retry }: { label: string; retry?: () => void }) {
  return <QueryLoading className="knowledge-loading" text={label} onRetry={retry} />
}
function Empty({
  icon,
  title,
  detail,
}: {
  icon: 'document' | 'memory'
  title: string
  detail: string
}) {
  const Icon = icon === 'memory' ? Sparkles : BookOpen
  return (
    <div className="knowledge-empty">
      <div>
        <Icon size={26} />
      </div>
      <h3>{title}</h3>
      <p>{detail}</p>
    </div>
  )
}
function DetailRequest({
  pending,
  error,
  retry,
}: {
  pending: boolean
  error: unknown
  retry: () => void
}) {
  return (
    <>
      {pending && <Loading label="读取完整内容…" retry={retry} />}
      {error ? <Notice error={error} retry={retry} /> : null}
    </>
  )
}
function Tags({ values }: { values?: string[] }) {
  return values?.length ? (
    <div className="knowledge-tags">
      {values.map((value) => (
        <span key={value}>{value}</span>
      ))}
    </div>
  ) : null
}
function Guidance({ label, items }: { label: string; items?: string[] }) {
  return items?.length ? (
    <details>
      <summary>{label}</summary>
      <ul>
        {items.map((item, index) => (
          <li key={index}>{item}</li>
        ))}
      </ul>
    </details>
  ) : null
}

function DocumentDetail({
  document,
  collections,
  onSaved,
  onBusy,
  busy,
  collectionLabel,
  writable,
  onCopy,
  onEdit,
}: {
  document: KnowledgeDocument
  collections: KnowledgeCollection[]
  onSaved: (doc: KnowledgeDocument) => Promise<void>
  onBusy: (value: boolean) => void
  busy: boolean
  collectionLabel: string
  writable: boolean
  onCopy: () => void
  onEdit: () => void
}) {
  const meta = document.metadata
  const editable = writable && document.editable && !busy && meta.status !== 'archived'
  const inline = useInlineKnowledge(document, editable)
  return (
    <KnowledgeDetailPage
      kind="文档"
      collectionLabel={collectionLabel}
      pathTitle={meta.title}
      date={meta.updatedAt}
      dateLabel="上次编辑"
      saveStatus={editable ? inline.status : undefined}
      properties={(closeMenu) => (
        <>
          <KnowledgeActions
            document={document}
            collections={collections}
            writable={writable}
            unsaved={inline.dirty || inline.pending}
            onCopy={() => {
              closeMenu()
              onCopy()
            }}
            onSaved={onSaved}
            onBusy={onBusy}
            onRename={() => {
              const field = window.document.querySelector<HTMLTextAreaElement>(
                'textarea[aria-label="文档标题"]',
              )
              field?.focus()
              field?.select()
              closeMenu()
            }}
          />
          <details className="knowledge-action-properties">
            <summary>文档属性与来源</summary>
            {editable && (
              <button
                type="button"
                className="text-button"
                disabled={inline.dirty || inline.pending}
                onClick={onEdit}
              >
                编辑属性
              </button>
            )}
            <p>
              {collectionLabel} · {statusLabels[meta.status] || meta.status}
            </p>
            <div className="knowledge-metadata">
              <span>
                项目<strong>{meta.projects?.join('、') || '未指定'}</strong>
              </span>
              <span>
                领域<strong>{meta.domains?.join('、') || '未指定'}</strong>
              </span>
              <span>
                创建者<strong>{meta.producer || '—'}</strong>
              </span>
            </div>
            <Tags values={meta.tags} />
            <dl>
              <dt>文档 ID</dt>
              <dd>{meta.id}</dd>
              <dt>创建时间</dt>
              <dd>{formatDate(meta.createdAt)}</dd>
              {meta.source && (
                <>
                  <dt>来源类型</dt>
                  <dd>{meta.source.type}</dd>
                  <dt>来源</dt>
                  <dd>{meta.source.uri}</dd>
                  {meta.source.importedAt && (
                    <>
                      <dt>导入时间</dt>
                      <dd>{formatDate(meta.source.importedAt)}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
          </details>
        </>
      )}
      title={
        <>
          {editable ? (
            <InlineKnowledgeField
              title
              value={inline.draft.title}
              onChange={(title) => inline.patch({ title })}
              onComposing={inline.setComposing}
            />
          ) : (
            <h2>{meta.title}</h2>
          )}
          {meta.status !== 'active' && (
            <span className={`knowledge-status ${meta.status}`}>
              {statusLabels[meta.status] || meta.status}
            </span>
          )}
        </>
      }
    >
      {meta.status === 'archived' && (
        <p className="knowledge-detail-note">文档已归档，可通过右上角菜单恢复。</p>
      )}
      {editable && inline.feedback}
      <div className="knowledge-document-body">
        {editable ? (
          <InlineKnowledgeField
            value={inline.draft.content}
            onChange={(content) => inline.patch({ content })}
            onComposing={inline.setComposing}
          />
        ) : (
          <Markdown text={document.content || '此文档没有正文。'} />
        )}
      </div>
      {writable && !document.editable && (
        <p className="knowledge-detail-note">
          此文档来自外部来源。可以创建副本进行编辑，保留来源原文。
        </p>
      )}
    </KnowledgeDetailPage>
  )
}

function MemoryDetail({
  memory,
  writable,
  onEdit,
}: {
  memory: MemoryHit
  writable: boolean
  onEdit: () => void
}) {
  const title = memory.content.trim().split(/\r?\n/)[0] || '未命名记忆'
  return (
    <KnowledgeDetailPage
      kind="记忆"
      collectionLabel="共享记忆"
      pathTitle={title}
      date={memory.created_at}
      dateLabel="创建于"
      title={<h2>{title}</h2>}
      properties={(closeMenu) => (
        <>
          {writable && (
            <div className="knowledge-page-actions">
              <button
                type="button"
                onClick={() => {
                  closeMenu()
                  onEdit()
                }}
              >
                <Pencil size={14} />
                修订记忆
              </button>
            </div>
          )}
          <details className="knowledge-action-properties">
            <summary>记忆属性与来源</summary>
            <div className="knowledge-metadata">
              <span>
                作用范围<strong>{scopeLabel(memory.scope)}</strong>
              </span>
            </div>
            <Tags values={memory.tags} />
            <dl>
              <dt>记忆 ID</dt>
              <dd>{memory.id}</dd>
              <dt>创建时间</dt>
              <dd>{formatDate(memory.created_at)}</dd>
              <dt>作用范围</dt>
              <dd>{memory.scope}</dd>
              <dt>来源</dt>
              <dd>{memory.metadata?.source || memory.source || '未记录'}</dd>
              {Object.entries(memory.metadata || {})
                .filter(([key]) => key !== 'source')
                .map(([key, value]) => (
                  <div key={key}>
                    <dt>{key}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
            </dl>
            <p className="knowledge-detail-note">
              修订会追加一条新记录并替代当前记忆，历史记录仍保留。
            </p>
          </details>
        </>
      )}
    >
      <div className="knowledge-document-body">
        <Markdown text={memory.content} />
      </div>
    </KnowledgeDetailPage>
  )
}

type ComposerProps<T> = {
  onBusy: (busy: boolean) => void
  onClose: () => void
  onSaved: (result: T) => Promise<void>
}
type ContentDraft = {
  title: string
  content: string
  collection: string
  tags: string
  domains: string
  projects: string
  scope: string
  id: string
  name: string
  description: string
  status: string
  etag: string
}
const emptyDraft: ContentDraft = {
  title: '',
  content: '',
  collection: '',
  tags: '',
  domains: '',
  projects: '',
  scope: 'global',
  id: '',
  name: '',
  description: '',
  status: 'active',
  etag: '',
}

// A document instance owns its own record. Recovering another tab's draft
// copies it, so saving here never destroys another editor's unsubmitted work.
const draftOwner = crypto.randomUUID()
type RecoverableDraft = { key: string; draft: ContentDraft; savedAt: number }
function parseDraft(raw: string): { draft: ContentDraft; savedAt: number } | undefined {
  try {
    const parsed = JSON.parse(raw)
    const value = parsed?.draft || parsed
    if (!value || typeof value.content !== 'string' || typeof value.title !== 'string') return
    const draft = { ...emptyDraft }
    for (const key of Object.keys(emptyDraft) as (keyof ContentDraft)[]) {
      if (typeof value[key] === 'string') draft[key] = value[key]
    }
    return {
      draft,
      savedAt:
        typeof parsed.savedAt === 'number' &&
        Number.isFinite(parsed.savedAt) &&
        parsed.savedAt > 0 &&
        parsed.savedAt < 8640000000000000
          ? parsed.savedAt
          : 0,
    }
  } catch {
    return
  }
}
function useKnowledgeDraft(key: string, initial: Partial<ContentDraft>) {
  const prefix = `atm.web.knowledge.draft.v2:${key}:`
  const storageKey = `${prefix}${draftOwner}`
  const [storageError, setStorageError] = useState(false)
  const [restored, setRestored] = useState(false)
  const completed = useRef(false)
  const persistenceBlocked = useRef(false)
  const [candidates, setCandidates] = useState<RecoverableDraft[]>(() => {
    const result: RecoverableDraft[] = []
    try {
      for (let index = 0; index < localStorage.length; index++) {
        const item = localStorage.key(index)
        if (item?.startsWith(prefix) && item !== storageKey) {
          const parsed = parseDraft(localStorage.getItem(item) || '')
          if (parsed) result.push({ key: item, ...parsed })
        }
      }
      const legacyKey = `atm.web.knowledge.draft.v1:${key.replace(/^copy:/, 'document:')}`
      const legacy = parseDraft(sessionStorage.getItem(legacyKey) || '')
      if (legacy) result.push({ key: `session:${legacyKey}`, ...legacy })
    } catch {
      /* The editable form remains available without storage. */
    }
    return result.sort((a, b) => b.savedAt - a.savedAt).slice(0, 10)
  })
  const [draft, setDraft] = useState<ContentDraft>(() => {
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw) {
        const parsed = parseDraft(raw)
        if (parsed) return parsed.draft
        persistenceBlocked.current = true
      }
    } catch {
      persistenceBlocked.current = true
    }
    return { ...emptyDraft, ...initial }
  })
  const draftRef = useRef(draft)
  draftRef.current = draft
  const lastStored = useRef('')
  const persist = (value: ContentDraft) => {
    if (completed.current || persistenceBlocked.current) {
      if (persistenceBlocked.current) setStorageError(true)
      return
    }
    try {
      const serialized = JSON.stringify({ draft: value, savedAt: Date.now() })
      localStorage.setItem(storageKey, serialized)
      lastStored.current = serialized
      setStorageError(false)
    } catch {
      setStorageError(true)
    }
  }
  useEffect(() => {
    persist(draft)
  }, [draft, storageKey])
  const patch = (value: Partial<ContentDraft>) => {
    const next = { ...draftRef.current, ...value }
    draftRef.current = next
    persist(next)
    setDraft(next)
  }
  const rebuildDraft = () => {
    persistenceBlocked.current = false
    persist(draftRef.current)
  }
  const recover = (candidate: RecoverableDraft) => {
    patch(candidate.draft)
    setCandidates([])
    setRestored(true)
  }
  const discard = (candidate: RecoverableDraft) => {
    try {
      if (candidate.key.startsWith('session:')) sessionStorage.removeItem(candidate.key.slice(8))
      else localStorage.removeItem(candidate.key)
      setCandidates((previous) => previous.filter((item) => item.key !== candidate.key))
    } catch {
      setStorageError(true)
    }
  }
  const clear = () => {
    completed.current = true
    try {
      if (localStorage.getItem(storageKey) === lastStored.current)
        localStorage.removeItem(storageKey)
    } catch {
      setStorageError(true)
    }
  }
  return { draft, patch, clear, rebuildDraft, storageError, restored, candidates, recover, discard }
}
function ComposerFeedback({
  restored,
  storageError,
  rebuildDraft,
  candidates,
  recover,
  discard,
}: {
  restored: boolean
  storageError: boolean
  rebuildDraft: () => void
  candidates: RecoverableDraft[]
  recover: (candidate: RecoverableDraft) => void
  discard: (candidate: RecoverableDraft) => void
}) {
  return (
    <>
      {restored && <p className="knowledge-draft-banner">已恢复之前的草稿，原草稿保留为备份。</p>}
      {!!candidates.length && (
        <details className="workspace-conflict">
          <summary>发现 {candidates.length} 份未提交草稿</summary>
          {candidates.map((candidate) => (
            <div key={candidate.key}>
              <p>
                {candidate.draft.title ||
                  candidate.draft.name ||
                  candidate.draft.content.slice(0, 80) ||
                  '未命名草稿'}
                {candidate.savedAt
                  ? ` · ${formatDate(new Date(candidate.savedAt).toISOString())}`
                  : ''}
              </p>
              <div className="workspace-actions">
                <button type="button" className="button" onClick={() => recover(candidate)}>
                  恢复这份草稿
                </button>
                <button type="button" className="text-button" onClick={() => discard(candidate)}>
                  删除这份草稿
                </button>
              </div>
            </div>
          ))}
        </details>
      )}
      {storageError && (
        <div className="notice" role="alert">
          <span>无法读取或保存浏览器草稿，离开页面前请复制当前内容。</span>
          <button type="button" onClick={rebuildDraft}>
            用当前内容重建草稿
          </button>
        </div>
      )}
    </>
  )
}

function ComposerFooter({
  pending,
  disabled,
  onClose,
  label,
  storageError,
}: {
  pending: boolean
  disabled: boolean
  onClose: () => void
  label: string
  storageError: boolean
}) {
  return (
    <div className="knowledge-composer-footer">
      <span>{storageError ? '草稿保存不可用' : '草稿保存在此浏览器，关闭后可恢复'}</span>
      <button type="button" className="button subtle" disabled={pending} onClick={onClose}>
        取消
      </button>
      <button type="submit" className="button primary" disabled={disabled || pending}>
        {pending ? <LoaderCircle size={15} className="spin" /> : <Check size={15} />}
        {pending ? '正在保存…' : label}
      </button>
    </div>
  )
}

function ContentField({
  content,
  onChange,
  disabled,
}: {
  content: string
  onChange: (content: string) => void
  disabled: boolean
}) {
  const [preview, setPreview] = useState(false)
  return (
    <div className="knowledge-content-field">
      <div>
        <label htmlFor="knowledge-content">正文</label>
        <button type="button" className="text-button" onClick={() => setPreview(!preview)}>
          {preview ? '继续编辑' : '预览 Markdown'}
        </button>
      </div>
      {preview ? (
        <div className="knowledge-editor-preview">
          <Markdown text={content || '正文预览'} />
        </div>
      ) : (
        <textarea
          id="knowledge-content"
          required
          value={content}
          onChange={(event) => onChange(event.target.value)}
          rows={13}
          maxLength={1000000}
          disabled={disabled}
          placeholder="写下背景、结论与可复用的经验。支持 Markdown。"
        />
      )}
    </div>
  )
}

function DocumentComposer({
  collections,
  collection,
  original,
  editing = false,
  ...props
}: {
  collections: KnowledgeCollection[]
  collection: string
  original?: KnowledgeDocument
  editing?: boolean
} & ComposerProps<KnowledgeDocument>) {
  const draftState = useKnowledgeDraft(
    `${editing ? 'edit' : original ? 'copy' : 'document'}:${original?.metadata.id || collection || 'new'}`,
    {
      title: original
        ? editing
          ? original.metadata.title
          : `${original.metadata.title}（副本）`
        : '',
      etag: editing ? original?.etag || '' : '',
      status: original?.metadata.status || 'active',
      content: original?.content || '',
      collection: original?.collection || collection || collections[0]?.id || '',
      tags: original?.metadata.tags?.join(', ') || '',
      domains: original?.metadata.domains?.join(', ') || '',
      projects: original?.metadata.projects?.join(', ') || '',
    },
  )
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () => {
      const content = {
        title: draft.title.trim(),
        content: draft.content,
        tags: splitValues(draft.tags),
        domains: splitValues(draft.domains),
        projects: splitValues(draft.projects),
      }
      if (editing) {
        if (!original) throw new Error('文档尚未读取，请刷新后重试。')
        return call('knowledge.document.update', {
          document_id: original.metadata.id,
          etag: draft.etag,
          status: draft.status,
          ...content,
        })
      }
      return call('knowledge.document.create', { collection: draft.collection, ...content })
    },
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  const latest = useMutation({
    mutationFn: () => call('knowledge.document.get', { document_id: original?.metadata.id || '' }),
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending && (!editing || (original?.editable && draft.etag)))
          mutation.mutate()
      }}
    >
      <h2>{editing ? '编辑知识文档' : original ? '创建文档副本' : '新建知识文档'}</h2>
      <p>
        {editing
          ? '保存正文与元数据；其他位置的修改会在保存时检查。'
          : '将正文与元数据保存到所选知识集合。'}
      </p>
      <ComposerFeedback {...draftState} />
      <label>
        标题
        <input
          value={draft.title}
          onChange={(event) => patch({ title: event.target.value })}
          required
          maxLength={500}
          disabled={mutation.isPending}
          autoFocus
          placeholder="这篇文档要讲什么？"
        />
      </label>
      <label>
        知识集合
        <Select
          value={draft.collection}
          required
          disabled={mutation.isPending || editing}
          onChange={(event) => patch({ collection: event.target.value })}
        >
          <option value="">选择一个集合</option>
          {collections.map((item) => (
            <option value={item.id} key={item.id}>
              {item.name || item.id}
            </option>
          ))}
        </Select>
      </label>
      {editing && (
        <label>
          状态
          <Select
            value={draft.status}
            disabled={mutation.isPending}
            onChange={(event) => patch({ status: event.target.value })}
          >
            <option value="active">有效</option>
            <option value="draft">草稿</option>
            <option value="archived">已归档</option>
          </Select>
        </label>
      )}
      <ContentField
        content={draft.content}
        onChange={(content) => patch({ content })}
        disabled={mutation.isPending}
      />
      <div className="knowledge-composer-meta">
        <label>
          标签
          <input
            value={draft.tags}
            onChange={(event) => patch({ tags: event.target.value })}
            disabled={mutation.isPending}
            placeholder="多个值用逗号分隔"
          />
        </label>
        <label>
          领域
          <input
            value={draft.domains}
            onChange={(event) => patch({ domains: event.target.value })}
            disabled={mutation.isPending}
            placeholder="如研发、产品"
          />
        </label>
        <label>
          关联项目
          <input
            value={draft.projects}
            onChange={(event) => patch({ projects: event.target.value })}
            disabled={mutation.isPending}
            placeholder="如 atm"
          />
        </label>
      </div>
      {editing && mutation.error instanceof ApiError && mutation.error.status === 409 ? (
        <div className="workspace-conflict" role="alert">
          <p>
            这篇文档已在其他地方修改。当前草稿已保留，请读取最新文档，将需要的内容合并到上方编辑区。
          </p>
          {!latest.data && (
            <button
              type="button"
              className="button"
              disabled={latest.isPending}
              onClick={() => latest.mutate()}
            >
              读取最新文档
            </button>
          )}
          {latest.error && <p>{errorText(latest.error)}</p>}
          {latest.data && (
            <>
              <details open>
                <summary>最新文档：{latest.data.metadata.title}</summary>
                <p>
                  状态：{statusLabels[latest.data.metadata.status] || latest.data.metadata.status} ·
                  标签：{latest.data.metadata.tags?.join('、') || '无'} · 领域：
                  {latest.data.metadata.domains?.join('、') || '无'} · 项目：
                  {latest.data.metadata.projects?.join('、') || '无'}
                </p>
                <Markdown text={latest.data.content || '无正文'} />
              </details>
              <button
                type="button"
                className="button"
                disabled={!latest.data.editable}
                onClick={() => {
                  patch({ etag: latest.data!.etag })
                  mutation.reset()
                  latest.reset()
                }}
              >
                已核对并合并，继续编辑
              </button>
              {!latest.data.editable && <p>最新文档由外部来源维护，请创建副本后编辑。</p>}
            </>
          )}
        </div>
      ) : mutation.error ? (
        <Notice error={mutation.error} />
      ) : null}
      {editing && !original?.editable && (
        <p className="knowledge-detail-note">此文档不支持原地修改，请返回文档后创建副本。</p>
      )}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={
          (editing && (!original?.editable || !draft.etag)) ||
          !draft.title.trim() ||
          !draft.content.trim() ||
          !collections.some((item) => item.id === draft.collection)
        }
        onClose={props.onClose}
        label={editing ? '保存文档' : '创建文档'}
      />
    </form>
  )
}

function MemoryComposer({
  scope,
  original,
  ...props
}: { scope: string; original?: MemoryHit } & ComposerProps<MemoryEventResult>) {
  const draftState = useKnowledgeDraft(`memory:${original?.id || scope || 'new'}`, {
    content: original?.content || '',
    tags: original?.tags?.join(', ') || '',
    scope: original?.scope || scope || 'global',
  })
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () =>
      call<MemoryEventResult>(original ? 'memory.supersede' : 'memory.create', {
        ...(original ? { target_id: original.id } : {}),
        scope: original?.scope || draft.scope.trim(),
        content: draft.content,
        tags: splitValues(draft.tags),
      }),
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  const stale = mutation.error instanceof ApiError && [404, 409].includes(mutation.error.status)
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending && !stale) mutation.mutate()
      }}
    >
      <h2>{original ? '修订共享记忆' : '新增共享记忆'}</h2>
      <p>
        {original
          ? '保存后，当前记忆会被这个完整的新版本替代。'
          : '适合保存长期有效的约定、偏好和事实。'}
      </p>
      <ComposerFeedback {...draftState} />
      <label>
        作用范围
        <input
          value={original?.scope || draft.scope}
          onChange={(event) => patch({ scope: event.target.value })}
          required
          disabled={mutation.isPending || !!original}
          placeholder="global、project:atm 或 session:…"
          maxLength={500}
        />
      </label>
      <p className="knowledge-field-help">
        global 用于所有工作；project:项目名 用于指定项目；session:会话ID 用于指定会话。
      </p>
      <ContentField
        content={draft.content}
        onChange={(content) => patch({ content })}
        disabled={mutation.isPending}
      />
      <label>
        标签
        <input
          value={draft.tags}
          onChange={(event) => patch({ tags: event.target.value })}
          disabled={mutation.isPending}
          placeholder="多个值用逗号分隔"
        />
      </label>
      {mutation.error &&
        (stale ? (
          <div className="notice" role="alert">
            这条记忆已被其他操作替代或移除。草稿已保留，请返回列表查看最新记忆后重新修订。
          </div>
        ) : (
          <div className="notice" role="alert">
            {errorText(mutation.error)}
          </div>
        ))}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={!draft.content.trim() || !draft.scope.trim() || stale}
        onClose={props.onClose}
        label={original ? '保存新版本' : '保存记忆'}
      />
    </form>
  )
}

function CollectionComposer(props: ComposerProps<KnowledgeCollection>) {
  const draftState = useKnowledgeDraft('collection:new', {})
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () =>
      call('knowledge.collection.create', {
        id: draft.id.trim(),
        name: draft.name.trim(),
        description: draft.description.trim(),
      }),
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending) mutation.mutate()
      }}
    >
      <h2>新建知识集合</h2>
      <p>按主题或用途组织文档。</p>
      <ComposerFeedback {...draftState} />
      <label>
        集合标识
        <input
          value={draft.id}
          onChange={(event) => patch({ id: event.target.value })}
          required
          maxLength={100}
          disabled={mutation.isPending}
          autoFocus
          placeholder="如 engineering"
        />
      </label>
      <p className="knowledge-field-help">
        使用简短名称，不能包含路径分隔符，也不能以点或下划线开头。
      </p>
      <label>
        显示名称
        <input
          value={draft.name}
          onChange={(event) => patch({ name: event.target.value })}
          disabled={mutation.isPending}
          maxLength={300}
          placeholder="如工程实践"
        />
      </label>
      <label>
        集合说明
        <textarea
          value={draft.description}
          onChange={(event) => patch({ description: event.target.value })}
          disabled={mutation.isPending}
          maxLength={8000}
          rows={5}
          placeholder="适合存放什么内容，何时查阅？"
        />
      </label>
      {mutation.error && (
        <Notice
          error={
            mutation.error instanceof ApiError && mutation.error.status === 409
              ? new Error('此集合标识已经存在，请使用其他标识。')
              : mutation.error
          }
        />
      )}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={!draft.id.trim()}
        onClose={props.onClose}
        label="创建集合"
      />
    </form>
  )
}
