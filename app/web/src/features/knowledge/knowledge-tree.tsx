import { Button as AntButton } from 'antd'
import { useEffect, useId, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { GripVertical, Files, BookOpen, ChevronRight, FileText, Plus, Sparkles } from 'lucide-react'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import type { KnowledgeCollection, MemoryHit } from './knowledge-types'
import { useKnowledgeDocuments } from './knowledge-query'
import { Loading } from './knowledge-feedback'
import type { CollectionOrder } from './use-collection-order'
export function CollectionBranch({
  item,
  order,
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
  order?: CollectionOrder
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
    <div className="knowledge-branch" {...order?.dropProps(item.id)}>
      <div className="knowledge-branch-heading">
        {order && (
          <AntButton
            type="text"
            className="collection-drag-handle"
            aria-label={`拖动${item.name || item.id}`}
            title="拖动排序，也可在集合管理中上下移动"
            {...order.handleProps(item.id)}
          >
            <GripVertical size={14} />
          </AntButton>
        )}
        <AntButton
          htmlType="button"
          className="knowledge-branch-toggle"
          aria-label={`${expanded ? '收起' : '展开'}${item.name || item.id}`}
          aria-expanded={expanded}
          aria-controls={childrenID}
          disabled={busy}
          onClick={() => setExpanded((value) => !value)}
        >
          <ChevronRight size={15} />
        </AntButton>
        <AntButton
          htmlType="button"
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
          <small className="knowledge-branch-count">
            {memoryCollection
              ? rows
                ? `${rows.length}${rows.length >= 200 ? '+' : ''}`
                : ''
              : item.document_count}
          </small>
        </AntButton>
        <div className="knowledge-branch-actions">
          {onManage && (
            <AntButton
              htmlType="button"
              className="knowledge-branch-manage"
              aria-label={`管理${item.name || item.id}的文档`}
              title="文档管理"
              disabled={busy}
              onClick={onManage}
            >
              <Files size={14} />
            </AntButton>
          )}
          {writable && (
            <AntButton
              htmlType="button"
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
            </AntButton>
          )}
        </div>
      </div>
      <div id={childrenID} className="knowledge-branch-documents" hidden={!expanded}>
        {expanded && (
          <>
            {pending && <Loading label={`读取${contentLabel}…`} retry={retry} />}
            {error && <Notice error={error} retry={retry} />}
            {rows?.map((doc) => (
              <AntButton
                htmlType="button"
                key={doc.document_id}
                className={`knowledge-collection-row ${active && documentID === doc.document_id ? 'selected' : ''}`}
                aria-current={active && documentID === doc.document_id ? 'page' : undefined}
                title={doc.title}
                disabled={busy}
                onClick={() => onOpen(doc.document_id)}
              >
                {memoryCollection ? <Sparkles size={16} /> : <FileText size={16} />}
                <span>{doc.title || `未命名${contentLabel}`}</span>
              </AntButton>
            ))}
            {rows?.length === 0 && <p className="knowledge-side-note">暂无{contentLabel}</p>}
            {!memoryCollection && documents.hasNextPage && (
              <AntButton
                className="text-button"
                disabled={busy || documents.isFetching}
                onClick={() => void documents.fetchNextPage()}
              >
                {documents.isFetchingNextPage ? '正在加载…' : '加载更多文档'}
              </AntButton>
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
