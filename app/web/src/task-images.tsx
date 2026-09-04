import { forwardRef, useImperativeHandle, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ImagePlus, LoaderCircle } from 'lucide-react'
import { ApiError, errorText, uploadTaskImage } from './api'
import { imageUploadError, taskImageLimit, taskImageTypes } from './image-uploads'
import type { Todo } from './types'

export type TaskImageUploadHandle = { add: (files: File[]) => void }
type Props = {
  todo: Todo
  etag: string
  onUploaded?: (result: { todo: Todo; etag: string }) => void
  onBusyChange?: (busy: boolean) => void
  onConflict?: () => void
}
export const TaskImageUpload = forwardRef<TaskImageUploadHandle, Props>(function TaskImageUpload(
  { todo, etag, onUploaded, onBusyChange, onConflict },
  ref,
) {
  const input = useRef<HTMLInputElement>(null)
  const active = useRef(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [result, setResult] = useState('')
  const [dragging, setDragging] = useState(false)
  const query = useQueryClient()
  const upload = async (files: File[]) => {
    if (active.current || files.length === 0) return
    const firstError = files.map(imageUploadError).find(Boolean)
    if (firstError) {
      setError(firstError)
      return
    }
    if ((todo.images?.length ?? 0) + files.length > taskImageLimit) {
      setError('每个任务最多保存 10 张图片，请减少本次选择的数量。')
      return
    }
    active.current = true
    setBusy(true)
    onBusyChange?.(true)
    setError('')
    setResult('')
    let nextETag = etag
    let uploaded = 0
    try {
      for (const file of files) {
        setResult(`正在上传 ${file.name || '粘贴的图片'}…`)
        const next = await uploadTaskImage<{ todo: Todo; etag: string }>(todo.id, file, nextETag)
        nextETag = next.etag
        uploaded++
        onUploaded?.(next)
        query.setQueryData<{ todo: Todo; etag: string }>(['todo', todo.id], (previous) =>
          previous ? { ...previous, ...next } : previous,
        )
      }
      setResult(`已添加 ${uploaded} 张图片。`)
    } catch (failure) {
      setError(errorText(failure))
      setResult(uploaded ? `已添加 ${uploaded} 张图片，其余未上传。` : '')
      if (failure instanceof ApiError && failure.status === 409) onConflict?.()
    } finally {
      active.current = false
      setBusy(false)
      onBusyChange?.(false)
      void query.invalidateQueries({ queryKey: ['todo', todo.id] })
      if (input.current) input.current.value = ''
    }
  }
  useImperativeHandle(ref, () => ({
    add: (files) => {
      void upload(files)
    },
  }))
  return (
    <div className="task-image-uploader">
      <input
        ref={input}
        type="file"
        hidden
        multiple
        accept={taskImageTypes.join(',')}
        disabled={busy}
        onChange={(event) => {
          void upload(Array.from(event.target.files ?? []))
        }}
      />
      <div
        className={`task-image-dropzone${dragging ? ' dragging' : ''}`}
        role="button"
        tabIndex={busy ? -1 : 0}
        aria-label="添加任务图片，可选择、拖入或粘贴"
        aria-disabled={busy}
        onClick={() => {
          if (!busy) input.current?.click()
        }}
        onKeyDown={(event) => {
          if (!busy && (event.key === 'Enter' || event.key === ' ')) {
            event.preventDefault()
            input.current?.click()
          }
        }}
        onDragOver={(event) => {
          if (event.dataTransfer.types.includes('Files')) {
            event.preventDefault()
            event.stopPropagation()
            setDragging(true)
          }
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault()
          event.stopPropagation()
          setDragging(false)
          void upload(Array.from(event.dataTransfer.files))
        }}
        onPaste={(event) => {
          const files = Array.from(event.clipboardData.files)
          if (files.length) {
            event.preventDefault()
            event.stopPropagation()
            void upload(files)
          }
        }}
      >
        {busy ? <LoaderCircle size={18} className="spin" /> : <ImagePlus size={18} />}
        <span>
          <strong>{busy ? '正在添加图片' : '添加图片'}</strong>
          <small>选择文件、拖入，或点击此区域后粘贴 · PNG / JPEG / GIF · 每张 ≤ 10 MB</small>
        </span>
      </div>
      {result && (
        <p className="task-upload-status" role="status">
          {result}
        </p>
      )}
      {error && (
        <p className="notice" role="alert">
          {error}
        </p>
      )}
    </div>
  )
})
