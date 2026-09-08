import { Select } from './components/select'
import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { call, errorText, callTodoMutation } from './api'
import { applyTaskBatch, type BatchOperation, type BatchResult } from './task-batch'
import type { TodoDetail } from './types'

export function TaskBulkActions({
  selected,
  selectionCount = selected.length,
  selectionScopeLabel,
  loadSelected,
  writable,
  onClear,
}: {
  selected: { id: string }[]
  selectionCount?: number
  selectionScopeLabel?: string
  loadSelected?: () => Promise<{ id: string }[]>
  writable: boolean
  onClear: () => void
}) {
  const [open, setOpen] = useState(false)
  const [operation, setOperation] = useState<BatchOperation>('priority')
  const [value, setValue] = useState('P2')
  const [snapshots, setSnapshots] = useState<TodoDetail[]>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [results, setResults] = useState<BatchResult[]>()
  const dialog = useRef<HTMLDialogElement>(null)
  const lock = useRef(false)
  const client = useQueryClient()
  useEffect(() => {
    if (open) dialog.current?.showModal()
    else dialog.current?.close()
  }, [open])
  const preview = async () => {
    if (lock.current || !selectionCount || selectionCount > 100) return
    lock.current = true
    setBusy(true)
    setError('')
    setResults(undefined)
    setSnapshots(undefined)
    setOpen(true)
    try {
      const targets = loadSelected ? await loadSelected() : [...selected]
      if (!targets.length || targets.length > 100)
        throw new Error('筛选结果已变化，请重新选择最多 100 项任务。')
      const loaded: TodoDetail[] = []
      for (const todo of targets) loaded.push(await call('todo.show', { todo_id: todo.id }))
      if (loaded.some((row) => row.todo.archived))
        throw new Error('选中任务中包含已归档任务，请先恢复后再编辑。')
      setSnapshots(loaded)
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
      lock.current = false
    }
  }
  const apply = async () => {
    if (!snapshots || lock.current) return
    lock.current = true
    setBusy(true)
    setError('')
    setResults([])
    try {
      await applyTaskBatch(snapshots, operation, value, callTodoMutation, (result) =>
        setResults((previous) => [...(previous || []), result]),
      )
    } finally {
      await Promise.all([
        client.invalidateQueries({ queryKey: ['todos'] }),
        client.invalidateQueries({ queryKey: ['todo'] }),
        client.invalidateQueries({ queryKey: ['global-search'] }),
        client.invalidateQueries({ queryKey: ['activity-status'] }),
      ])
      lock.current = false
      setBusy(false)
    }
  }
  return (
    <>
      {!!selectionCount && (
        <div className="task-bulk-bar" role="region" aria-label="任务批量操作">
          <strong>
            已选择{selectionScopeLabel ? ` ${selectionScopeLabel}` : ''} {selectionCount} 项
          </strong>
          <button onClick={onClear} disabled={busy}>
            取消选择
          </button>
          {writable && (
            <>
              <Select
                aria-label="批量操作类型"
                value={operation}
                disabled={busy}
                onChange={(e) => {
                  const next = e.target.value as BatchOperation
                  setOperation(next)
                  setValue(next === 'priority' ? 'P2' : '')
                }}
              >
                <option value="priority">修改优先级</option>
                <option value="project">修改项目</option>
                <option value="archive">归档</option>
              </Select>
              {operation === 'priority' && (
                <Select
                  aria-label="批量优先级"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  disabled={busy}
                >
                  {['P0', 'P1', 'P2'].map((p) => (
                    <option key={p}>{p}</option>
                  ))}
                </Select>
              )}
              {operation === 'project' && (
                <input
                  aria-label="批量项目"
                  value={value}
                  onChange={(e) => setValue(e.target.value)}
                  maxLength={200}
                  placeholder="项目名，留空取消项目"
                  disabled={busy}
                />
              )}
              <button
                className="button"
                disabled={busy || selectionCount > 100}
                onClick={() => void preview()}
              >
                预览变更
              </button>
              {selectionCount > 100 && <span>每次最多处理 100 项，请缩小选择范围。</span>}
            </>
          )}
        </div>
      )}
      <dialog
        ref={dialog}
        className="task-batch-dialog"
        aria-label="批量变更预览"
        onCancel={(e) => {
          if (busy) e.preventDefault()
          else setOpen(false)
        }}
        onClose={() => setOpen(false)}
      >
        <h2>批量变更预览</h2>
        <p>
          {operation === 'archive'
            ? '归档后将移出当前工作集并解除任务关联，可在已归档中恢复。'
            : `将${snapshots?.length || selectionCount} 项任务的${operation === 'priority' ? '优先级' : '项目'}改为“${value.trim() || '未分配'}”。`}
        </p>
        {error && <p role="alert">{error}</p>}
        <div className="task-batch-preview">
          {(snapshots || []).map((row) => (
            <div key={row.todo.id}>
              <span>{row.todo.id}</span>
              <strong>{row.todo.title}</strong>
              <small>
                {operation !== 'archive' &&
                  `${row.todo[operation] || '未分配'} → ${value.trim() || '未分配'}`}
              </small>
            </div>
          ))}
        </div>
        {results && (
          <div className="task-batch-results" role="status">
            <p>
              已处理 {results.length} / {snapshots?.length} 项 · 成功{' '}
              {results.filter((r) => r.ok).length} 项 · 未完成 {results.filter((r) => !r.ok).length}{' '}
              项
            </p>
            {results
              .filter((r) => !r.ok)
              .map((r) => (
                <p key={r.id}>
                  {r.id} {r.title}：{r.error}
                </p>
              ))}
          </div>
        )}
        {busy && <p role="status">{snapshots ? '正在保存变更…' : '正在读取选中任务…'}</p>}
        <footer>
          <button
            className="button"
            disabled={busy}
            onClick={() => {
              setOpen(false)
              if (results) onClear()
            }}
          >
            {results ? '关闭结果' : '取消'}
          </button>
          {!results && (
            <button
              className="button primary"
              disabled={busy || !snapshots || !!error}
              onClick={() => void apply()}
            >
              确认{operation === 'archive' ? '归档' : '修改'} {snapshots?.length || 0} 项
            </button>
          )}
        </footer>
      </dialog>
    </>
  )
}
