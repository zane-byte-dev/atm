import { Select } from '../components/select'
import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Copy, Download, FolderInput, Link, Pencil, RotateCcw, Trash2 } from 'lucide-react'
import { call, errorText } from '../api'
import type { KnowledgeCollection, KnowledgeDocument } from './knowledge-types'

export function KnowledgeActions({
  document: doc,
  collections,
  writable,
  unsaved,
  onRename,
  onCopy,
  onSaved,
  onBusy,
}: {
  document: KnowledgeDocument
  collections: KnowledgeCollection[]
  writable: boolean
  unsaved: boolean
  onRename: () => void
  onCopy: () => void
  onSaved: (doc: KnowledgeDocument) => Promise<void>
  onBusy: (value: boolean) => void
}) {
  const [moving, setMoving] = useState(false)
  const [target, setTarget] = useState('')
  const [message, setMessage] = useState('')
  const mutation = useMutation({
    mutationFn: (patch: { status?: string; collection?: string }) =>
      call<KnowledgeDocument>('knowledge.document.update', {
        document_id: doc.metadata.id,
        etag: doc.etag,
        content: doc.content || '',
        ...patch,
      }),
    onSuccess: async (result) => {
      setMoving(false)
      setMessage(
        result.metadata.status === 'archived'
          ? '文档已归档，可在“文档管理”的“已归档”中恢复。'
          : '已更新',
      )
      await onSaved(result)
    },
  })
  useEffect(() => {
    onBusy(mutation.isPending)
    return () => onBusy(false)
  }, [mutation.isPending, onBusy])
  const disabled = unsaved || mutation.isPending
  const editable = writable && doc.editable && doc.metadata.status !== 'archived'
  return (
    <div className="knowledge-page-actions">
      {editable && (
        <button type="button" disabled={disabled} onClick={onRename}>
          <Pencil size={15} />
          重命名
        </button>
      )}
      <button
        type="button"
        onClick={async () => {
          const url = new URL(window.location.href)
          url.search = new URLSearchParams({
            collection: doc.collection,
            document: doc.metadata.id,
          }).toString()
          url.hash = ''
          try {
            await navigator.clipboard.writeText(url.toString())
            setMessage('已复制文档链接')
          } catch {
            setMessage('无法访问剪贴板，请复制地址栏链接。')
          }
        }}
      >
        <Link size={15} />
        复制链接
      </button>
      {writable && (
        <button type="button" disabled={disabled} onClick={onCopy}>
          <Copy size={15} />
          创建副本
        </button>
      )}
      {writable && (
        <button
          type="button"
          disabled={disabled}
          onClick={() => {
            mutation.reset()
            setMoving(!moving)
          }}
        >
          <FolderInput size={15} />
          移动到…
        </button>
      )}
      {moving && (
        <form
          className="knowledge-move-form"
          onSubmit={(event) => {
            event.preventDefault()
            if (target) mutation.mutate({ collection: target })
          }}
        >
          <Select
            aria-label="目标集合"
            value={target}
            disabled={disabled}
            onChange={(event) => setTarget(event.target.value)}
          >
            <option value="">选择目标集合</option>
            {collections
              .filter((collection) => collection.id !== doc.collection)
              .map((collection) => (
                <option key={collection.id} value={collection.id}>
                  {collection.name || collection.id}
                </option>
              ))}
          </Select>
          <button type="submit" disabled={disabled || !target}>
            移动
          </button>
        </form>
      )}
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          const url = URL.createObjectURL(
            new Blob([doc.content || ''], { type: 'text/markdown;charset=utf-8' }),
          )
          const link = window.document.createElement('a')
          link.href = url
          link.download = `${doc.metadata.title.replace(/[\\/:*?"<>|]/g, '-').slice(0, 120) || 'document'}.md`
          link.click()
          window.setTimeout(() => URL.revokeObjectURL(url), 1000)
        }}
      >
        <Download size={15} />
        导出 Markdown
      </button>
      {writable &&
        (doc.metadata.status === 'archived' ? (
          <button
            type="button"
            disabled={disabled}
            onClick={() => mutation.mutate({ status: 'active' })}
          >
            <RotateCcw size={15} />
            恢复文档
          </button>
        ) : (
          <button
            type="button"
            className="knowledge-action-danger"
            disabled={disabled}
            onClick={() => mutation.mutate({ status: 'archived' })}
          >
            <Trash2 size={15} />
            删除（可恢复归档）
          </button>
        ))}
      {unsaved && <p>请等待当前修改保存后再操作。</p>}
      {message && <p role="status">{message}</p>}
      {mutation.error && (
        <p role="alert">
          {errorText(mutation.error)}
          {String(errorText(mutation.error)).includes('changed') ? ' 请刷新文档后重试。' : ''}
        </p>
      )}
    </div>
  )
}
