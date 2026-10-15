import { Tags, Guidance } from './knowledge-details'
import { Disclosure } from '../../components/disclosure'
import {
  WorkspaceSplitLayout,
  WorkspaceNavigator,
  WorkspaceNavigatorHeading,
} from '../../components/workspace-navigator'
import { Button as AntButton } from 'antd'
import { Select } from '../../components/select'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Files, ArrowLeft, FileText, Plus, Sparkles, X } from 'lucide-react'
import { call } from '../../api'
import type { Bootstrap } from '../../types'
import { Notice } from '../../components/notice'
import type {
  KnowledgeDocument,
  KnowledgeQueryInput,
  MemoryEventResult,
  MemoryHit,
} from './knowledge-types'
import { KnowledgeManager } from './knowledge-manager'
import { useKnowledgeDocuments } from './knowledge-query'
import './knowledge.css'
import '../../styles/workspace-forms.css'
import { useCollectionOrder } from './use-collection-order'
import { KnowledgeCollectionManager } from './knowledge-collection-manager'
import { statusLabels, formatDate, scopeLabel } from './knowledge-format'
import { CollectionBranch } from './knowledge-tree'
import { Loading, Empty, DetailRequest } from './knowledge-feedback'
import { DocumentDetail, MemoryDetail } from './knowledge-details'
import { DocumentComposer, MemoryComposer, CollectionComposer } from './knowledge-composers'

type RoutePatch = Record<string, string | undefined>
export function KnowledgeWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const queryClient = useQueryClient()
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
  const [collectionsOpen, setCollectionsOpen] = useState(false)
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
  const collectionOrder = useCollectionOrder(catalog.data || [])
  const orderedCollections = collectionOrder.rows
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
    <WorkspaceSplitLayout
      as="section"
      className="knowledge-workspace knowledge-browser"
      aria-label="知识与共享记忆"
    >
      <WorkspaceNavigator
        as="aside"
        variant="tree"
        className="knowledge-collections"
        aria-label="知识导航"
      >
        <WorkspaceNavigatorHeading className="knowledge-navigation-heading" title="知识" />
        <nav aria-label="知识内容">
          <div className="knowledge-section-label">
            <span>知识集合</span>
            {writable && (
              <AntButton
                htmlType="button"
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
              </AntButton>
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
          {collectionOrder.error && <Notice error={collectionOrder.error} />}
          {orderedCollections.map((item) => (
            <CollectionBranch
              key={item.id}
              item={item}
              order={busy ? undefined : collectionOrder}
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
          <AntButton
            className="knowledge-collection-row"
            disabled={busy}
            onClick={() => setCollectionsOpen(true)}
          >
            <Files size={16} />
            <span>集合管理</span>
          </AntButton>
          <AntButton
            htmlType="button"
            className="knowledge-collection-row"
            disabled={busy}
            onClick={() => setManagerCollection('')}
          >
            <Files size={16} />
            <span>文档管理</span>
          </AntButton>
        </nav>
      </WorkspaceNavigator>
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
                <AntButton
                  htmlType="button"
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
                </AntButton>
              )}
            </div>
            <p>
              {memoryTab
                ? '跨任务复用的简短约定，清楚标记适用范围与来源。'
                : '按集合整理，让经验成为下次工作的起点。'}
            </p>
            {!memoryTab && activeCollection && (
              <Disclosure className="knowledge-collection-about" key={activeCollection.id}>
                <summary>集合说明</summary>
                <p>{activeCollection.description || '此集合暂无说明。'}</p>
                {activeCollection.role && (
                  <span className="knowledge-pill">{activeCollection.role}</span>
                )}
                <Tags values={activeCollection.topics} />
                <Guidance label="适合何时使用" items={activeCollection.use_when} />
                <Guidance label="不适用情况" items={activeCollection.avoid_when} />
                <Guidance label="使用说明" items={activeCollection.instructions} />
              </Disclosure>
            )}
            <div className="knowledge-list-filters">
              {text && (
                <AntButton
                  htmlType="button"
                  className="text-button"
                  disabled={busy}
                  onClick={() => route({ q: undefined })}
                >
                  清除关键词：{text}
                  <X size={14} />
                </AntButton>
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
                  <AntButton
                    key={item.id}
                    htmlType="button"
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
                  </AntButton>
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
                  <AntButton
                    key={item.document_id}
                    htmlType="button"
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
                  </AntButton>
                ))}
                <div className="knowledge-list-footnote">
                  {documents.total !== undefined && (
                    <span>
                      已加载 {documents.documents.length} / {documents.total} 篇文档
                    </span>
                  )}
                  {documents.hasNextPage && (
                    <AntButton
                      className="text-button"
                      disabled={busy || documents.isFetching}
                      onClick={() => void documents.fetchNextPage()}
                    >
                      {documents.isFetchingNextPage ? '正在加载…' : '加载更多文档'}
                    </AntButton>
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
            <AntButton
              htmlType="button"
              className="text-button knowledge-back"
              disabled={busy}
              onClick={() => route({ document: undefined, memory: undefined, compose: undefined })}
            >
              <ArrowLeft size={15} />
              {params.get('view') === 'search' || text || status ? '返回列表' : '返回导航'}
            </AntButton>
            {compose && (
              <AntButton
                htmlType="button"
                className="text-button"
                disabled={busy}
                onClick={closeEditor}
              >
                <X size={15} />
                关闭编辑
              </AntButton>
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
                  todoWritable={boot.capabilities?.todo_write === true}
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
      {collectionsOpen && (
        <KnowledgeCollectionManager
          order={collectionOrder}
          writable={writable}
          onClose={() => setCollectionsOpen(false)}
          onDeleted={(id, target) => {
            if (collection === id)
              route({ collection: target || undefined, document: undefined, compose: undefined })
          }}
        />
      )}
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
    </WorkspaceSplitLayout>
  )
}
