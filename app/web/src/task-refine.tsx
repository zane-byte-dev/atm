import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, LoaderCircle, Sparkles, X } from 'lucide-react'
import { ApiError, call } from './api'
import { Notice } from './editor'
import type { TodoDetail } from './types'

type RefineRequest = { kind: 'todo.refine'; todo_id: string; expected_etag: string; hint?: string }
type RefineJob = {
  id: string
  kind: string
  todo_id?: string
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'interrupted'
  phase?: string
  cancel_requested?: boolean
  error?: { code: string; message: string }
  result?: {
    todo_id: string
    etag: string
    changed: boolean
    committed?: boolean
    children?: { id: string; title: string }[]
    summary?: string
  }
}
const active = (job?: RefineJob) => job?.status === 'queued' || job?.status === 'running'
const statuses = {
  queued: '等待整理',
  running: '正在整理',
  succeeded: '整理完成',
  failed: '整理未完成',
  canceled: '已取消整理',
  interrupted: '整理已中断',
}

export function TaskRefine({ data }: { data: TodoDetail }) {
  const query = useQueryClient()
  const [hint, setHint] = useState('')
  const attempt = useRef<{ input: RefineRequest; key: string } | undefined>(undefined)
  const refreshed = useRef(new Set<string>())
  const jobs = useQuery({
    queryKey: ['runtime-jobs'],
    queryFn: ({ signal }) => call<{ jobs: RefineJob[] }>('jobs.list', { limit: 30 }, signal),
    refetchInterval: (query) => (query.state.data?.jobs.some((job) => active(job)) ? 1500 : 15000),
    refetchIntervalInBackground: false,
  })
  const run = useMutation({
    mutationFn: (request: { input: RefineRequest; key: string }) =>
      call<RefineJob>('jobs.run', request.input, undefined, request.key),
    retry: false,
    onSuccess: () => {
      attempt.current = undefined
      void query.invalidateQueries({ queryKey: ['runtime-jobs'] })
    },
    onError: () => {
      void query.invalidateQueries({ queryKey: ['runtime-jobs'] })
    },
  })
  const matching =
    jobs.data?.jobs.filter((job) => job.kind === 'todo.refine' && job.todo_id === data.todo.id) ??
    []
  const submitted = run.data
    ? (matching.find((job) => job.id === run.data.id) ?? run.data)
    : undefined
  const target =
    matching.find((job) => active(job)) ??
    (active(submitted) ? submitted : (matching[0] ?? submitted))
  const current = useQuery({
    queryKey: ['runtime-job', target?.id],
    queryFn: ({ signal }) => call<RefineJob>('jobs.show', { job_id: target!.id }, signal),
    enabled: !!target?.id,
    refetchInterval: (query) => (active(query.state.data) ? 1500 : false),
    refetchIntervalInBackground: false,
  })
  const job = current.data ?? target
  const cancel = useMutation({
    mutationFn: (id: string) => call<RefineJob>('jobs.cancel', { job_id: id }),
    retry: false,
    onSuccess: (result) => {
      query.setQueryData(['runtime-job', result.id], result)
      void query.invalidateQueries({ queryKey: ['runtime-jobs'] })
    },
  })
  useEffect(() => {
    if (!job || active(job) || refreshed.current.has(job.id)) return
    refreshed.current.add(job.id)
    void query.invalidateQueries({ queryKey: ['todo', data.todo.id] })
    void query.invalidateQueries({ queryKey: ['todos'] })
    void query.invalidateQueries({ queryKey: ['doc', data.todo.id] })
  }, [job, query, data.todo.id])
  const busy = run.isPending || active(job)
  const jobLabel =
    job?.result?.committed && job.status !== 'succeeded' && !active(job)
      ? '任务已保存，文档更新待重试'
      : job?.status === 'succeeded' && job.result?.changed === false && !job.result.children?.length
        ? '内容已清晰，无需调整'
        : job
          ? statuses[job.status]
          : ''
  const uncertain =
    !!run.error &&
    (!(run.error instanceof ApiError) ||
      run.error.status >= 500 ||
      run.error.code === 'invalid_response')
  const submit = () => {
    if (busy) return
    // A lost HTTP response is retried with precisely the same input and key.
    // A known rejection allows a new deliberate request after editing the hint.
    if (!attempt.current || !uncertain)
      attempt.current = {
        key: crypto.randomUUID(),
        input: {
          kind: 'todo.refine',
          todo_id: data.todo.id,
          expected_etag: data.etag,
          ...(hint.trim() ? { hint: hint.trim() } : {}),
        },
      }
    cancel.reset()
    run.mutate(attempt.current)
  }
  return (
    <section className="task-refine" aria-label="AI 整理任务">
      <div className="task-refine-heading">
        <button
          className="button"
          type="button"
          disabled={busy || jobs.isPending || jobs.isError}
          onClick={submit}
        >
          {busy ? <LoaderCircle size={14} className="spin" /> : <Sparkles size={14} />}
          {uncertain ? '重试整理请求' : 'AI 整理'}
        </button>
        <span>整理标题与说明，必要时拆分为最多 5 个子任务。</span>
      </div>
      <details className="task-refine-options">
        <summary>补充整理要求</summary>
        <label className="field-label" htmlFor="task-refine-hint">
          希望怎样整理（可选）
        </label>
        <textarea
          id="task-refine-hint"
          rows={2}
          maxLength={500}
          value={hint}
          disabled={busy || !!uncertain}
          onChange={(event) => setHint(event.target.value)}
          placeholder="例如：保留技术背景，按可独立验收的步骤拆分。"
        />
      </details>
      {jobs.error && <Notice error={jobs.error} retry={() => void jobs.refetch()} />}
      {run.error && <Notice error={run.error} />}
      {current.error && <Notice error={current.error} retry={() => void current.refetch()} />}
      {cancel.error && <Notice error={cancel.error} />}
      {job && (
        <div className={`task-refine-status ${job.status}`} role="status">
          {active(job) ? (
            <LoaderCircle size={15} className="spin" />
          ) : job.status === 'succeeded' ? (
            <Check size={15} />
          ) : (
            <Sparkles size={15} />
          )}
          <div>
            <strong>{jobLabel}</strong>
            {active(job) && <p>{job.cancel_requested ? '正在取消…' : job.phase}</p>}
            {job.error?.message && <p>{job.error.message}</p>}
            {job.result?.summary && job.result.summary !== jobLabel && <p>{job.result.summary}</p>}
            {job.result?.children && job.result.children.length > 0 && (
              <div className="task-refine-children">
                {job.result.children
                  .filter((child) => /^t\d+$/.test(child.id))
                  .map((child) => (
                    <a href={`/tasks/${child.id}`} key={child.id}>
                      {child.id} · {child.title}
                    </a>
                  ))}
              </div>
            )}
          </div>
          {active(job) && (
            <button
              className="text-button"
              type="button"
              disabled={cancel.isPending || job.cancel_requested}
              onClick={() => cancel.mutate(job.id)}
              aria-label="取消 AI 整理"
            >
              <X size={14} />
              取消
            </button>
          )}
        </div>
      )}
    </section>
  )
}
