import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  BookOpen,
  Check,
  Copy,
  FileText,
  Folder,
  Layers,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  Sparkles,
  X,
} from 'lucide-react'
import { ApiError, call, errorText } from '../api'
import type { Bootstrap } from '../types'
import { Markdown, Notice } from '../editor'
import type {
  KnowledgeCollection,
  KnowledgeDocument,
  KnowledgeDocumentRow,
  MemoryEventResult,
  MemoryHit,
} from './knowledge-types'
import './knowledge.css'

type RoutePatch = Record<string, string | undefined>
const statusLabels: Record<string, string> = { active: '有效', draft: '草稿', archived: '已归档' }
const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }) : '—'
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
  const memoryTab = params.get('tab') === 'memory'
  const collection = params.get('collection') || ''
  const documentID = params.get('document') || ''
  const memoryID = params.get('memory') || ''
  const text = params.get('q') || ''
  const scope = params.get('scope') || ''
  const status = params.get('status') || ''
  const compose = params.get('compose') || ''
  const writable = boot.capabilities?.workspace_write === true
  const [search, setSearch] = useState(text)
  const [scopeText, setScopeText] = useState(scope)
  const [busy, setBusy] = useState(false)
  const resultsRef = useRef<HTMLDivElement>(null)
  const detailRef = useRef<HTMLElement>(null)
  useEffect(() => setSearch(text), [text])
  useEffect(() => setScopeText(scope), [scope])
  useEffect(() => {
    resultsRef.current?.scrollTo({ top: 0 })
  }, [memoryTab, collection, text, scope, status])
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
    queryFn: ({ signal }) => call<KnowledgeCollection[]>('knowledge.catalog', {}, signal),
  })
  const documents = useQuery({
    queryKey: ['knowledge', 'query', collection, text, status],
    queryFn: ({ signal }) =>
      call<{ documents: KnowledgeDocumentRow[] }>(
        'knowledge.query',
        { collection, text, status, limit: 200 },
        signal,
      ),
    enabled: !memoryTab,
  })
  const memories = useQuery({
    queryKey: ['knowledge', 'memory', text, scope],
    queryFn: ({ signal }) =>
      call<{ hits: MemoryHit[] }>('memory.recall', { query: text, scope, limit: 200 }, signal),
    enabled: memoryTab,
  })
  const document = useQuery({
    queryKey: ['knowledge', 'document', documentID],
    queryFn: ({ signal }) =>
      call<KnowledgeDocument>('knowledge.document.get', { document_id: documentID }, signal),
    enabled: !memoryTab && !!documentID,
  })
  const memory = useQuery({
    queryKey: ['knowledge', 'memory-detail', memoryID],
    queryFn: ({ signal }) => call<MemoryHit>('memory.get', { memory_id: memoryID }, signal),
    enabled: memoryTab && !!memoryID,
  })
  const activeCollection = catalog.data?.find((item) => item.id === collection)
  const total = (catalog.data || []).reduce((sum, item) => sum + item.document_count, 0)
  const closeEditor = () => route({ compose: undefined })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['knowledge'] })
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
  const detailOpen = !!compose || (memoryTab ? !!memoryID : !!documentID)

  return (
    <section
      className={`knowledge-workspace ${detailOpen ? 'knowledge-detail-open' : ''}`}
      aria-label="知识与共享记忆"
    >
      <header className="knowledge-header">
        <div>
          <h1>知识与记忆</h1>
          <p>整理可复用的文档、经验与项目约定。</p>
        </div>
        <div className="knowledge-header-actions">
          <button
            type="button"
            className="button"
            onClick={refresh}
            disabled={busy}
            aria-label="刷新知识与记忆"
          >
            <RefreshCw size={15} />
            刷新
          </button>
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
      </header>
      <div className="knowledge-tabs" role="tablist" aria-label="知识工作区">
        <button
          type="button"
          role="tab"
          aria-selected={!memoryTab}
          disabled={busy}
          onClick={() => route({ tab: undefined, q: undefined, compose: undefined })}
        >
          <BookOpen size={16} />
          中心知识<span>{total}</span>
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={memoryTab}
          disabled={busy}
          onClick={() => route({ tab: 'memory', q: undefined, compose: undefined })}
        >
          <Sparkles size={16} />
          共享记忆
        </button>
      </div>
      <div className={`knowledge-columns ${memoryTab ? 'knowledge-memory-columns' : ''}`}>
        <aside className="knowledge-collections" aria-label={memoryTab ? '记忆范围' : '知识集合'}>
          <div className="knowledge-section-label">
            <span>{memoryTab ? '记忆范围' : '知识集合'}</span>
            {writable && !memoryTab && (
              <button
                type="button"
                disabled={busy}
                onClick={() => route({ compose: 'collection' })}
                title="新建集合"
                aria-label="新建集合"
              >
                <Plus size={16} />
              </button>
            )}
          </div>
          {memoryTab ? (
            <>
              <button
                type="button"
                className={`knowledge-collection-row ${!scope ? 'selected' : ''}`}
                disabled={busy}
                onClick={() => route({ scope: undefined, memory: undefined, compose: undefined })}
              >
                <Layers size={16} />
                <span>全部范围</span>
              </button>
              <button
                type="button"
                className={`knowledge-collection-row ${scope === 'global' ? 'selected' : ''}`}
                disabled={busy}
                onClick={() => route({ scope: 'global', memory: undefined, compose: undefined })}
              >
                <Sparkles size={16} />
                <span>全局记忆</span>
              </button>
              <form
                className="knowledge-scope-form"
                onSubmit={(event) => {
                  event.preventDefault()
                  route({ scope: scopeText.trim(), memory: undefined, compose: undefined })
                }}
              >
                <label htmlFor="knowledge-scope-filter">项目或会话范围</label>
                <input
                  id="knowledge-scope-filter"
                  value={scopeText}
                  onChange={(event) => setScopeText(event.target.value)}
                  placeholder="project:atm"
                  maxLength={500}
                  disabled={busy}
                />
                <button type="submit" className="button subtle" disabled={busy}>
                  应用范围
                </button>
              </form>
              <p className="knowledge-side-note">项目与会话检索会同时包含全局记忆。</p>
            </>
          ) : (
            <>
              <button
                type="button"
                className={`knowledge-collection-row ${!collection ? 'selected' : ''}`}
                disabled={busy}
                onClick={() =>
                  route({ collection: undefined, document: undefined, compose: undefined })
                }
              >
                <Layers size={16} />
                <span>全部文档</span>
                <small>{total}</small>
              </button>
              {catalog.isPending && <Loading label="读取集合…" />}
              {catalog.error && (
                <Notice error={catalog.error} retry={() => void catalog.refetch()} />
              )}
              {(catalog.data || []).map((item) => (
                <button
                  type="button"
                  key={item.id}
                  className={`knowledge-collection-row ${collection === item.id ? 'selected' : ''}`}
                  title={item.name || item.id}
                  disabled={busy}
                  onClick={() =>
                    route({ collection: item.id, document: undefined, compose: undefined })
                  }
                >
                  <Folder size={16} />
                  <span>{item.name || item.id}</span>
                  <small>{item.document_count}</small>
                </button>
              ))}
              {catalog.data?.length === 0 && (
                <p className="knowledge-side-note">
                  尚无知识集合。{writable ? '点击上方 + 创建第一个集合。' : ''}
                </p>
              )}
              {activeCollection && (
                <div className="knowledge-collection-about">
                  <h3>{activeCollection.name}</h3>
                  {activeCollection.role && (
                    <span className="knowledge-pill">{activeCollection.role}</span>
                  )}
                  <p>{activeCollection.description || '此集合暂无说明。'}</p>
                  <Tags values={activeCollection.topics} />
                  <Guidance label="适合何时使用" items={activeCollection.use_when} />
                  <Guidance label="不适用情况" items={activeCollection.avoid_when} />
                  <Guidance label="使用说明" items={activeCollection.instructions} />
                </div>
              )}
            </>
          )}
        </aside>
        <section className="knowledge-results" aria-label={memoryTab ? '共享记忆列表' : '文档列表'}>
          <form
            className="knowledge-search"
            onSubmit={(event) => {
              event.preventDefault()
              route({
                q: search.trim(),
                document: undefined,
                memory: undefined,
                compose: undefined,
              })
            }}
          >
            <Search size={16} />
            <input
              aria-label={memoryTab ? '搜索共享记忆' : '搜索知识文档'}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={memoryTab ? '搜索记忆内容…' : '搜索文档与正文…'}
              maxLength={2000}
              disabled={busy}
            />
            <button type="submit" disabled={busy}>
              搜索
            </button>
          </form>
          <div className="knowledge-list-heading">
            <span>
              {memoryTab ? '有效记忆' : activeCollection?.name || '全部文档'}{' '}
              <strong>
                {memoryTab
                  ? (memories.data?.hits.length ?? '—')
                  : (documents.data?.documents.length ?? '—')}
              </strong>
            </span>
            {!memoryTab && (
              <select
                aria-label="文档状态"
                value={status}
                disabled={busy}
                onChange={(event) =>
                  route({ status: event.target.value, document: undefined, compose: undefined })
                }
              >
                <option value="">所有状态</option>
                <option value="active">有效</option>
                <option value="draft">草稿</option>
                <option value="archived">已归档</option>
              </select>
            )}
          </div>
          <div className="knowledge-results-scroll" ref={resultsRef}>
            {memoryTab ? (
              <>
                {memories.isPending && <Loading label="读取共享记忆…" />}
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
                    className={`knowledge-result-row knowledge-memory-row ${memoryID === item.id ? 'selected' : ''}`}
                    aria-pressed={memoryID === item.id}
                    onClick={() => route({ memory: item.id, compose: undefined })}
                  >
                    <div className="knowledge-result-kicker">
                      <span>{item.scope}</span>
                      <time>{formatDate(item.created_at)}</time>
                    </div>
                    <p className="knowledge-memory-excerpt">{item.content}</p>
                    <Tags values={item.tags} />
                  </button>
                ))}
                {(memories.data?.hits.length || 0) >= 200 && (
                  <p className="knowledge-list-footnote">已显示前 200 条，请使用关键词缩小范围。</p>
                )}
              </>
            ) : (
              <>
                {documents.isPending && <Loading label="读取文档…" />}
                {documents.error && (
                  <Notice error={documents.error} retry={() => void documents.refetch()} />
                )}
                {documents.data?.documents.length === 0 && (
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
                {(documents.data?.documents || []).map((item) => (
                  <button
                    key={item.document_id}
                    type="button"
                    disabled={busy}
                    className={`knowledge-result-row ${documentID === item.document_id ? 'selected' : ''}`}
                    aria-pressed={documentID === item.document_id}
                    onClick={() => route({ document: item.document_id, compose: undefined })}
                  >
                    <div className="knowledge-result-kicker">
                      <span>
                        <FileText size={13} />
                        {item.collection}
                      </span>
                      <span className={`knowledge-status ${item.status}`}>
                        {statusLabels[item.status] || item.status}
                      </span>
                    </div>
                    <h3>{item.title}</h3>
                    {item.snippet ? (
                      <p className="knowledge-snippet">{item.snippet}</p>
                    ) : (
                      <Tags values={item.tags?.length ? item.tags : item.domains} />
                    )}
                    <div className="knowledge-result-meta">
                      <span>{item.projects?.join(' · ') || item.producer || '中心知识'}</span>
                      <time>{item.updated_at ? formatDate(item.updated_at) : '正文匹配'}</time>
                    </div>
                  </button>
                ))}
                {(documents.data?.documents.length || 0) >= 200 && (
                  <p className="knowledge-list-footnote">
                    已显示前 200 条，请选择集合或搜索关键词。
                  </p>
                )}
              </>
            )}
          </div>
        </section>
        <section
          className="knowledge-detail"
          ref={detailRef}
          aria-label={compose ? '编辑内容' : '所选内容'}
        >
          {detailOpen && (
            <div className="knowledge-detail-toolbar">
              <button
                type="button"
                className="text-button knowledge-back"
                disabled={busy}
                onClick={() =>
                  route({ document: undefined, memory: undefined, compose: undefined })
                }
              >
                <ArrowLeft size={15} />
                返回列表
              </button>
              {compose && (
                <button type="button" className="text-button" disabled={busy} onClick={closeEditor}>
                  <X size={15} />
                  关闭编辑
                </button>
              )}
            </div>
          )}
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
          ) : !memoryTab && (compose === 'document' || compose === 'copy') ? (
            compose === 'copy' && !document.data ? (
              <DetailRequest
                pending={document.isPending}
                error={document.error}
                retry={() => void document.refetch()}
              />
            ) : (
              <DocumentComposer
                key={`document:${compose}:${collection}:${compose === 'copy' ? documentID : ''}`}
                collections={catalog.data || []}
                collection={collection}
                original={compose === 'copy' ? document.data : undefined}
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
                  document={document.data}
                  writable={writable}
                  onCopy={() => route({ compose: 'copy' })}
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
    </section>
  )
}

function Loading({ label }: { label: string }) {
  return (
    <p className="knowledge-loading" role="status">
      <LoaderCircle size={17} className="spin" />
      {label}
    </p>
  )
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
      {pending && <Loading label="读取完整内容…" />}
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
  writable,
  onCopy,
}: {
  document: KnowledgeDocument
  writable: boolean
  onCopy: () => void
}) {
  const meta = document.metadata
  return (
    <article className="knowledge-document">
      <div className="knowledge-document-kicker">
        <span>{document.collection}</span>
        <span className={`knowledge-status ${meta.status}`}>
          {statusLabels[meta.status] || meta.status}
        </span>
      </div>
      <h2>{meta.title}</h2>
      <div className="knowledge-document-actions">
        <span>更新于 {formatDate(meta.updatedAt)}</span>
        {writable && (
          <button type="button" className="button subtle" onClick={onCopy}>
            <Copy size={14} />
            创建副本
          </button>
        )}
      </div>
      <Tags values={meta.tags} />
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
      <div className="knowledge-document-body">
        <Markdown text={document.content || '此文档没有正文。'} />
      </div>
      <details className="knowledge-provenance">
        <summary>文档信息</summary>
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
      {writable && (
        <p className="knowledge-detail-note">需要调整内容时，可创建副本；原文与来源文档会保留。</p>
      )}
    </article>
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
  return (
    <article className="knowledge-document">
      <div className="knowledge-document-kicker">
        <span>共享记忆</span>
        <span className="knowledge-pill">{memory.scope}</span>
      </div>
      <h2>一条可以复用的经验</h2>
      <div className="knowledge-document-actions">
        <span>{formatDate(memory.created_at)}</span>
        {writable && (
          <button type="button" className="button subtle" onClick={onEdit}>
            <Pencil size={14} />
            修订记忆
          </button>
        )}
      </div>
      <Tags values={memory.tags} />
      <div className="knowledge-document-body">
        <Markdown text={memory.content} />
      </div>
      <details className="knowledge-provenance">
        <summary>记忆信息</summary>
        <dl>
          <dt>记忆 ID</dt>
          <dd>{memory.id}</dd>
          <dt>作用范围</dt>
          <dd>{memory.scope}</dd>
          <dt>来源</dt>
          <dd>{memory.metadata?.source || memory.source}</dd>
          {Object.entries(memory.metadata || {})
            .filter(([key]) => key !== 'source')
            .map(([key, value]) => (
              <div key={key}>
                <dt>{key}</dt>
                <dd>{value}</dd>
              </div>
            ))}
        </dl>
      </details>
      <p className="knowledge-detail-note">修订会追加一条新记录并替代当前记忆，历史记录仍保留。</p>
    </article>
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
}

// Each label keeps an independent draft in this browser tab. Restore only the
// known string fields; malformed storage never becomes a request payload.
function useKnowledgeDraft(key: string, initial: Partial<ContentDraft>) {
  const storageKey = `atm.web.knowledge.draft.v1:${key}`
  const [storageError, setStorageError] = useState(false)
  const restored = useRef(false)
  const completed = useRef(false)
  const persistenceBlocked = useRef(false)
  const [draft, setDraft] = useState<ContentDraft>(() => {
    try {
      const raw = sessionStorage.getItem(storageKey)
      if (raw) {
        const parsed = JSON.parse(raw)
        if (parsed && Object.keys(emptyDraft).every((field) => typeof parsed[field] === 'string')) {
          restored.current = true
          return {
            ...emptyDraft,
            ...Object.fromEntries(Object.keys(emptyDraft).map((field) => [field, parsed[field]])),
          }
        }
        persistenceBlocked.current = true
      }
    } catch {
      // A damaged/unreadable draft must not be overwritten by the empty form.
      persistenceBlocked.current = true
    }
    return { ...emptyDraft, ...initial }
  })
  const draftRef = useRef(draft)
  draftRef.current = draft
  const persist = (value: ContentDraft) => {
    if (completed.current || persistenceBlocked.current) {
      if (persistenceBlocked.current) setStorageError(true)
      return
    }
    try {
      sessionStorage.setItem(storageKey, JSON.stringify(value))
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
    // The input event persists synchronously, including immediately before a
    // route change or pagehide; a queued React effect is not the only copy.
    persist(next)
    setDraft(next)
  }
  const rebuildDraft = () => {
    persistenceBlocked.current = false
    persist(draftRef.current)
  }
  const clear = () => {
    completed.current = true
    try {
      if (sessionStorage.getItem(storageKey) === JSON.stringify(draft))
        sessionStorage.removeItem(storageKey)
    } catch {
      setStorageError(true)
    }
  }
  return { draft, patch, clear, rebuildDraft, storageError, restored: restored.current }
}

function ComposerFeedback({
  restored,
  storageError,
  rebuildDraft,
}: {
  restored: boolean
  storageError: boolean
  rebuildDraft: () => void
}) {
  return (
    <>
      {restored && <p className="knowledge-draft-banner">已恢复此标签页未提交的草稿。</p>}
      {storageError && (
        <div className="notice" role="alert">
          <span>无法读取或保存此标签页的草稿，离开页面前请复制当前内容。</span>
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
      <span>{storageError ? '草稿保存不可用' : '草稿保存在此标签页'}</span>
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
  ...props
}: {
  collections: KnowledgeCollection[]
  collection: string
  original?: KnowledgeDocument
} & ComposerProps<KnowledgeDocument>) {
  const draftState = useKnowledgeDraft(`document:${original?.metadata.id || collection || 'new'}`, {
    title: original ? `${original.metadata.title}（副本）` : '',
    content: original?.content || '',
    collection: original?.collection || collection || collections[0]?.id || '',
    tags: original?.metadata.tags?.join(', ') || '',
    domains: original?.metadata.domains?.join(', ') || '',
    projects: original?.metadata.projects?.join(', ') || '',
  })
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () =>
      call<KnowledgeDocument>('knowledge.document.create', {
        title: draft.title.trim(),
        content: draft.content,
        collection: draft.collection,
        tags: splitValues(draft.tags),
        domains: splitValues(draft.domains),
        projects: splitValues(draft.projects),
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
      <h2>{original ? '创建文档副本' : '新建知识文档'}</h2>
      <p>将正文与元数据保存到所选知识集合。</p>
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
        <select
          value={draft.collection}
          required
          disabled={mutation.isPending}
          onChange={(event) => patch({ collection: event.target.value })}
        >
          <option value="">选择一个集合</option>
          {collections.map((item) => (
            <option value={item.id} key={item.id}>
              {item.name || item.id}
            </option>
          ))}
        </select>
      </label>
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
      {mutation.error && <Notice error={mutation.error} />}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={
          !draft.title.trim() ||
          !draft.content.trim() ||
          !collections.some((item) => item.id === draft.collection)
        }
        onClose={props.onClose}
        label="创建文档"
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
      call<KnowledgeCollection>('knowledge.collection.create', {
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
