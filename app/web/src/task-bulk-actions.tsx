import { Select } from './components/select'
import { useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { call, errorText, callTodoMutation } from './api'
import { applyTaskBatch, undoTaskBatch, type BatchOperation, type BatchResult } from './task-batch'
import type { TodoDetail } from './types'

export function TaskBulkActions({
  selected,
  selectionCount = selected.length,
  selectionScopeLabel,
  loadSelected,
  writable,
  onClear,
  onRemaining,
  onBusyChange,
}: {
  selected: { id: string }[]
  selectionCount?: number
  selectionScopeLabel?: string
  loadSelected?: () => Promise<{ id: string }[]>
  writable: boolean
  onClear: () => void
  onRemaining: (ids: string[]) => void
  onBusyChange: (busy: boolean) => void
}) {
  const [busy, setBusy] = useState(false)
  const [projectOpen, setProjectOpen] = useState(false)
  const [project, setProject] = useState('')
  const [archive, setArchive] = useState<TodoDetail[]>()
  const [results, setResults] = useState<BatchResult[]>([])
  const [undo, setUndo] = useState<BatchResult[]>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const lock = useRef(false)
  const dialog = useRef<HTMLDialogElement>(null)
  const client = useQueryClient()
  useEffect(() => {
    if (archive) dialog.current?.showModal()
    else dialog.current?.close()
  }, [archive])
  const refresh = () =>
    Promise.all(
      ['todos', 'todo', 'global-search', 'activity-status'].map((key) =>
        client.invalidateQueries({ queryKey: [key] }),
      ),
    )
  const run = async (operation: () => Promise<void>) => {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    onBusyChange(true)
    setError('')
    try {
      await operation()
    } catch (error) {
      setError(errorText(error))
    } finally {
      lock.current = false
      setBusy(false)
      onBusyChange(false)
    }
  }
  const save = async (snapshots: TodoDetail[], operation: BatchOperation, value: string) => {
    const changed = await applyTaskBatch(snapshots, operation, value, callTodoMutation, () => {})
    setResults(changed)
    setUndo(changed.filter((result) => result.ok && result.undo))
    setMessage(
      `已保存 ${changed.filter((result) => result.ok).length} 项${changed.some((result) => !result.ok) ? '，未完成项已保留选择' : ''}`,
    )
    setArchive(undefined)
    await refresh()
    onRemaining(changed.filter((result) => !result.ok).map((result) => result.id))
  }
  const choose = (operation: BatchOperation, value = '') =>
    void run(async () => {
      if (!selectionCount || selectionCount > 100) return
      const targets = loadSelected ? await loadSelected() : selected
      if (!targets.length || targets.length > 100) throw new Error('请重新选择最多 100 项任务。')
      const snapshots = await Promise.all(
        targets.map(({ id }) => call('todo.show', { todo_id: id })),
      )
      if (snapshots.some((row) => row.todo.archived))
        throw new Error('选中任务中包含已归档任务，请先恢复。')
      if (operation === 'archive') setArchive(snapshots)
      else await save(snapshots, operation, value)
    })
  return (
    <>
      {(!!selectionCount || !!message || !!error || busy) && (
        <div className="task-bulk-bar" role="region" aria-label="任务批量操作" aria-busy={busy}>
          {!!selectionCount && (
            <>
              <strong>
                已选择{selectionScopeLabel ? ` ${selectionScopeLabel}` : ''} {selectionCount} 项
              </strong>
              <button onClick={onClear} disabled={busy}>
                取消选择
              </button>
              {writable && (
                <>
                  <Select
                    aria-label="批量优先级"
                    value=""
                    disabled={busy || selectionCount > 100}
                    onChange={(event) => {
                      if (event.target.value) choose('priority', event.target.value)
                    }}
                  >
                    <option value="" disabled>
                      优先级
                    </option>
                    {['P0', 'P1', 'P2'].map((value) => (
                      <option key={value}>{value}</option>
                    ))}
                  </Select>
                  <button disabled={busy} onClick={() => setProjectOpen(!projectOpen)}>
                    项目
                  </button>
                  {projectOpen && (
                    <form
                      onSubmit={(event) => {
                        event.preventDefault()
                        choose('project', project)
                      }}
                    >
                      <input
                        aria-label="批量项目"
                        value={project}
                        disabled={busy}
                        maxLength={200}
                        placeholder="项目名，留空取消项目"
                        onChange={(event) => setProject(event.target.value)}
                      />
                      <button disabled={busy || selectionCount > 100}>保存</button>
                    </form>
                  )}
                  <button disabled={busy || selectionCount > 100} onClick={() => choose('archive')}>
                    归档
                  </button>
                  {selectionCount > 100 && <span>每次最多处理 100 项</span>}
                </>
              )}
            </>
          )}
          {busy && <span role="status">正在保存…</span>}
          {!busy && message && <span role="status">{message}</span>}
          {error && <span role="alert">{error}</span>}
          {!busy &&
            results
              .filter((result) => !result.ok)
              .map((result) => (
                <span key={result.id} role="alert">
                  {result.id}：{result.error}
                </span>
              ))}
          {!!undo.length && (
            <button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  const reverted = await undoTaskBatch(undo, callTodoMutation)
                  setResults(reverted)
                  setUndo(reverted.filter((result) => !result.ok))
                  setMessage(
                    `已撤销 ${reverted.filter((result) => result.ok).length} 项${reverted.some((result) => !result.ok) ? '，其余任务可能已被修改，请核对' : ''}`,
                  )
                  await refresh()
                })
              }
            >
              撤销
            </button>
          )}
          {!busy && (message || error) && (
            <button
              aria-label="关闭批量操作结果"
              onClick={() => {
                setMessage('')
                setError('')
                setResults([])
                setUndo([])
              }}
            >
              关闭
            </button>
          )}
        </div>
      )}
      <dialog
        ref={dialog}
        className="task-batch-dialog"
        aria-label="确认归档任务"
        onCancel={(event) => {
          if (busy) event.preventDefault()
          else setArchive(undefined)
        }}
      >
        <h2>归档 {archive?.length} 项任务？</h2>
        <p>将移出工作集并解除任务关联，可在已归档中恢复。</p>
        <div className="task-batch-preview">
          {archive?.map(({ todo }) => (
            <div key={todo.id}>
              {todo.id} · {todo.title}
            </div>
          ))}
        </div>
        {error && <p role="alert">{error}</p>}
        <footer>
          <button className="button" disabled={busy} onClick={() => setArchive(undefined)}>
            取消
          </button>
          <button
            className="button"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                if (archive) await save(archive, 'archive', '')
              })
            }
          >
            确认归档
          </button>
        </footer>
      </dialog>
    </>
  )
}
