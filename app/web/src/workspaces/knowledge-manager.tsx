import { Select } from '../components/select'
import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Archive, Files, FolderInput, RotateCcw, X } from 'lucide-react'
import { call, errorText } from '../api'
import type {
  KnowledgeCollection,
  KnowledgeDocumentRow,
  KnowledgeQueryInput,
} from './knowledge-types'
import {
  updateDocumentBatch,
  type DocumentBatchResult,
  type DocumentPatch,
} from './knowledge-batch'
import './knowledge-manager.css'
import { useKnowledgeDocuments } from './knowledge-query'
const statuses = [
  ['', '全部'],
  ['active', '有效'],
  ['draft', '草稿'],
  ['archived', '已归档'],
]

export function KnowledgeManager({
  initialCollection,
  collections,
  writable,
  onClose,
  onOpen,
}: {
  initialCollection: string
  collections: KnowledgeCollection[]
  writable: boolean
  onClose: () => void
  onOpen: (doc: KnowledgeDocumentRow) => void
}) {
  const client = useQueryClient()
  const dialog = useRef<HTMLDialogElement>(null)
  const selectAll = useRef<HTMLInputElement>(null)
  const [collection, setCollection] = useState(initialCollection)
  const [status, setStatus] = useState<NonNullable<KnowledgeQueryInput['status']>>('')
  const [selected, setSelected] = useState<string[]>([])
  const [target, setTarget] = useState('')
  const [pending, setPending] = useState(false)
  const running = useRef(false)
  const [progress, setProgress] = useState(0)
  const [batchTotal, setBatchTotal] = useState(0)
  const [results, setResults] = useState<DocumentBatchResult[]>([])
  const [refreshError, setRefreshError] = useState('')
  const query = useKnowledgeDocuments({ collection, status })
  const rows = query.documents
  const picked = rows.filter((row) => selected.includes(row.document_id))
  const allSelected = rows.length > 0 && picked.length === rows.length
  useEffect(() => {
    dialog.current?.showModal()
  }, [])
  useEffect(() => {
    if (selectAll.current) selectAll.current.indeterminate = picked.length > 0 && !allSelected
  }, [picked.length, allSelected])
  const changeFilter = (key: 'collection' | 'status', value: string) => {
    if (running.current) return
    if (key === 'collection') setCollection(value)
    else setStatus(value as NonNullable<KnowledgeQueryInput['status']>)
    setSelected([])
    setResults([])
  }
  const execute = async (patch: DocumentPatch) => {
    if (!writable || running.current || !picked.length || query.isError) return
    running.current = true
    setPending(true)
    setProgress(0)
    setBatchTotal(picked.length)
    setResults([])
    setRefreshError('')
    try {
      const result = await updateDocumentBatch(
        picked,
        patch,
        {
          get: (id) => call('knowledge.document.get', { document_id: id }),
          update: (doc, change) =>
            call('knowledge.document.update', {
              document_id: doc.metadata.id,
              etag: doc.etag,
              content: doc.content || '',
              ...change,
            }),
        },
        setProgress,
      )
      setResults(result)
      setSelected(result.filter((item) => item.outcome === 'failed').map((item) => item.id))
      try {
        await client.invalidateQueries({ queryKey: ['knowledge'] }, { throwOnError: true })
      } catch {
        setRefreshError('操作结果已记录，但列表刷新失败，请重新加载。')
      }
    } finally {
      running.current = false
      setPending(false)
    }
  }
  const close = () => {
    if (!running.current) onClose()
  }
  const failed = results.filter((item) => item.outcome === 'failed')
  return (
    <dialog
      ref={dialog}
      className="knowledge-manager"
      aria-labelledby="knowledge-manager-title"
      onCancel={(event) => {
        event.preventDefault()
        close()
      }}
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          const rect = event.currentTarget.getBoundingClientRect()
          if (
            event.clientX < rect.left ||
            event.clientX > rect.right ||
            event.clientY < rect.top ||
            event.clientY > rect.bottom
          )
            close()
        }
      }}
    >
      <header className="knowledge-manager-header">
        <div>
          <h2 id="knowledge-manager-title">
            <Files size={22} />
            文档管理
          </h2>
          <p>按集合和状态查看文档，选择后批量管理。</p>
        </div>
        <button
          className="text-button"
          disabled={pending}
          aria-label="关闭文档管理"
          onClick={close}
        >
          <X size={20} />
        </button>
      </header>
      <div className="knowledge-manager-filters">
        <label>
          集合
          <Select
            aria-label="管理文档集合"
            disabled={pending}
            value={collection}
            onChange={(event) => changeFilter('collection', event.target.value)}
          >
            <option value="">全部集合</option>
            {collections.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name || item.id}
              </option>
            ))}
          </Select>
        </label>
        <div className="knowledge-manager-statuses" aria-label="文档状态">
          {statuses.map(([value, label]) => (
            <button
              key={value}
              disabled={pending}
              aria-pressed={status === value}
              onClick={() => changeFilter('status', value)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>
      {writable && (
        <div className="knowledge-manager-batch">
          <span aria-live="polite">已选 {picked.length} 篇</span>
          <button
            className="button subtle"
            disabled={pending || query.isError || !picked.some((row) => row.status !== 'archived')}
            onClick={() => void execute({ status: 'archived' })}
          >
            <Archive size={14} />
            归档
          </button>
          <button
            className="button subtle"
            disabled={pending || query.isError || !picked.some((row) => row.status !== 'active')}
            onClick={() => void execute({ status: 'active' })}
          >
            <RotateCcw size={14} />
            {status === 'archived' ? '恢复为有效' : '设为有效'}
          </button>
          <Select
            aria-label="批量移动目标集合"
            disabled={pending}
            value={target}
            onChange={(event) => setTarget(event.target.value)}
          >
            <option value="">移动到集合…</option>
            {collections.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name || item.id}
              </option>
            ))}
          </Select>
          <button
            className="button subtle"
            disabled={
              pending ||
              query.isError ||
              !target ||
              !picked.some((row) => row.collection !== target)
            }
            onClick={() => void execute({ collection: target })}
          >
            <FolderInput size={14} />
            移动
          </button>
          {picked.length > 0 && (
            <button className="text-button" disabled={pending} onClick={() => setSelected([])}>
              取消选择
            </button>
          )}
        </div>
      )}
      {pending && (
        <p className="knowledge-manager-feedback" role="status">
          正在处理 {progress} / {batchTotal} 篇文档…
        </p>
      )}
      {results.length > 0 && (
        <div className="knowledge-manager-feedback" role="status">
          已更新 {results.filter((item) => item.outcome === 'updated').length} 篇，未变化{' '}
          {results.filter((item) => item.outcome === 'skipped').length} 篇，失败 {failed.length}{' '}
          篇。
          {failed.length > 0 && (
            <details open>
              <summary>失败详情（已保留选择，可重试）</summary>
              <ul>
                {failed.map((item) => (
                  <li key={item.id}>
                    {item.title || item.id}：{item.error}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
      {refreshError && (
        <p className="knowledge-manager-feedback" role="alert">
          {refreshError}
        </p>
      )}
      {query.isError && (
        <p className="knowledge-manager-feedback" role="alert">
          {errorText(query.error)}{' '}
          <button className="text-button" disabled={pending} onClick={() => void query.refresh()}>
            重新加载
          </button>
        </p>
      )}
      <div className="knowledge-manager-table-wrap" aria-busy={pending || query.isPending}>
        <table>
          <thead>
            <tr>
              {writable && (
                <th className="knowledge-manager-check">
                  <input
                    ref={selectAll}
                    type="checkbox"
                    aria-label="选择当前已加载文档"
                    checked={allSelected}
                    disabled={pending || query.isError || !rows.length}
                    onChange={() =>
                      setSelected(allSelected ? [] : rows.map((row) => row.document_id))
                    }
                  />
                </th>
              )}
              <th>文档</th>
              <th>集合</th>
              <th>状态</th>
              <th>更新时间</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.document_id} data-selected={selected.includes(row.document_id)}>
                {writable && (
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`选择文档 ${row.title || row.document_id}`}
                      disabled={pending || query.isError}
                      checked={selected.includes(row.document_id)}
                      onChange={() =>
                        setSelected((previous) =>
                          previous.includes(row.document_id)
                            ? previous.filter((id) => id !== row.document_id)
                            : [...previous, row.document_id],
                        )
                      }
                    />
                  </td>
                )}
                <td>
                  <button
                    className="knowledge-manager-document"
                    disabled={pending}
                    onClick={() => onOpen(row)}
                  >
                    {row.title || '未命名文档'}
                  </button>
                </td>
                <td>
                  {collections.find((item) => item.id === row.collection)?.name || row.collection}
                </td>
                <td>
                  <span className={`knowledge-status ${row.status}`}>
                    {statuses.find(([value]) => value === row.status)?.[1] || row.status}
                  </span>
                </td>
                <td>
                  {row.updated_at ? new Date(row.updated_at).toLocaleDateString('zh-CN') : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {query.isPending && (
          <p className="knowledge-manager-empty" role="status">
            正在读取文档…
          </p>
        )}
        {!query.isPending && !query.isError && !rows.length && (
          <p className="knowledge-manager-empty">当前条件下没有文档。</p>
        )}
      </div>
      <footer>
        <span>
          已加载 {rows.length} / {query.total ?? '—'} 篇文档（批量选择仅限已加载文档）
          {query.hasNextPage && (
            <button
              className="text-button"
              disabled={pending || query.isFetching}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage ? '正在加载…' : '加载更多文档'}
            </button>
          )}
        </span>
        <span>{writable ? '归档可恢复，移动保留正文与来源。' : '当前为只读模式。'}</span>
      </footer>
    </dialog>
  )
}
