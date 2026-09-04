import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, LoaderCircle, Play, X } from 'lucide-react'
import { call } from '../api'
import { Notice } from '../editor'
import type { Bootstrap } from '../types'
import './workspace-forms.css'

export type JobKind =
  | 'session.sync'
  | 'collect.run'
  | 'collect.reprocess'
  | 'day.rebuild'
  | 'quota.refresh'
  | 'todo.refine'
export type JobInput = {
  kind: JobKind
  agent?: string
  source_id?: string
  item_id?: string
  todo_id?: string
  expected_etag?: string
  day?: string
  from?: string
  to?: string
}
export type RuntimeJob = {
  id: string
  todo_id?: string
  kind: JobKind
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'interrupted'
  phase: string
  created_at: string | number
  started_at?: string | number
  finished_at?: string | number
  cancel_requested?: boolean
  error?: { code: string; message: string }
}
const names: Record<JobKind, string> = {
  'session.sync': '同步会话',
  'collect.run': '执行采集',
  'collect.reprocess': '重新处理',
  'day.rebuild': '生成 AI Day',
  'quota.refresh': '刷新实时额度',
  'todo.refine': 'AI 整理任务',
}
const statuses: Record<RuntimeJob['status'], string> = {
  queued: '排队中',
  running: '运行中',
  succeeded: '已完成',
  failed: '未完成',
  canceled: '已取消',
  interrupted: '服务重启后中断',
}
const active = (job: RuntimeJob) => job.status === 'running' || job.status === 'queued'
export const jobsEnabled = (boot: Bootstrap) =>
  boot.capabilities?.workspace_write === true && boot.capabilities?.runtime_jobs === true

/** Starting a job is always an explicit button click. Polling only reads its saved state. */
export function RuntimeJobs({
  boot,
  actions = [],
  kinds,
  history = false,
}: {
  boot: Bootstrap
  actions?: { label?: string; input: JobInput; disabled?: boolean; title?: string }[]
  kinds: JobKind[]
  history?: boolean
}) {
  const enabled = jobsEnabled(boot)
  const client = useQueryClient()
  const previous = useRef(new Map<string, string>())
  const pendingOperations = useRef(new Map<string, { input: JobInput; key: string }>())
  const jobs = useQuery({
    queryKey: ['runtime-jobs'],
    queryFn: ({ signal }) => call<{ jobs: RuntimeJob[] }>('jobs.list', { limit: 30 }, signal),
    enabled,
    refetchInterval: (query) => (query.state.data?.jobs.some(active) ? 1500 : 15000),
    refetchIntervalInBackground: false,
  })
  const run = useMutation({
    mutationFn: (operation: { input: JobInput; key: string }) =>
      call<RuntimeJob>('jobs.run', operation.input, undefined, operation.key),
    retry: false,
    onSuccess: (job, operation) => {
      pendingOperations.current.delete(JSON.stringify(operation.input))
      void client.invalidateQueries({ queryKey: ['runtime-jobs'] })
      if (!active(job))
        void client.invalidateQueries({
          predicate: (query) => query.queryKey[0] !== 'runtime-jobs',
        })
    },
  })
  const cancel = useMutation({
    mutationFn: (job_id: string) => call<RuntimeJob>('jobs.cancel', { job_id }),
    retry: false,
    onSuccess: () => client.invalidateQueries({ queryKey: ['runtime-jobs'] }),
  })
  useEffect(() => {
    let finished = false
    for (const job of jobs.data?.jobs || []) {
      const last = previous.current.get(job.id)
      if (last && last !== job.status && !active(job)) finished = true
      previous.current.set(job.id, job.status)
    }
    if (finished)
      void client.invalidateQueries({ predicate: (query) => query.queryKey[0] !== 'runtime-jobs' })
  }, [jobs.data, client])
  if (!enabled) return null
  const matching = (jobs.data?.jobs || []).filter((job) => kinds.includes(job.kind))
  const visible = history
    ? matching.slice(0, 15)
    : matching.filter((job) => active(job) || job.id === run.data?.id).slice(0, 3)
  return (
    <div className={`workspace-jobs ${history ? 'workspace-jobs-history' : ''}`}>
      {!!actions.length && (
        <div className="workspace-actions">
          {actions.map((action, index) => (
            <button
              key={index}
              type="button"
              className="button"
              title={action.title}
              disabled={
                action.disabled ||
                run.isPending ||
                matching.some((job) => job.kind === action.input.kind && active(job))
              }
              onClick={() => {
                cancel.reset()
                const signature = JSON.stringify(action.input)
                let operation = pendingOperations.current.get(signature)
                if (!operation) {
                  operation = { input: { ...action.input }, key: crypto.randomUUID() }
                  pendingOperations.current.set(signature, operation)
                }
                run.mutate(operation)
              }}
            >
              <Play size={13} />
              {action.label || names[action.input.kind]}
            </button>
          ))}
        </div>
      )}
      {run.error && (
        <Notice
          error={run.error}
          retry={() => {
            if (run.variables) run.mutate(run.variables)
          }}
        />
      )}
      {cancel.error && <Notice error={cancel.error} />}
      {history && jobs.error && <Notice error={jobs.error} retry={() => void jobs.refetch()} />}
      {visible.map((job) => (
        <div className={`workspace-job ${job.status}`} key={job.id} role="status">
          {active(job) ? (
            <LoaderCircle size={14} className="spin" />
          ) : job.status === 'succeeded' ? (
            <Check size={14} />
          ) : (
            <span className="workspace-job-dot" />
          )}
          <div>
            <strong>{names[job.kind]}</strong>
            <span>{job.cancel_requested && active(job) ? '正在取消…' : statuses[job.status]}</span>
            {active(job) && job.phase && <small>{job.phase}</small>}
            {job.error?.message && <p>{job.error.message}</p>}
          </div>
          {active(job) && (
            <button
              type="button"
              className="text-button"
              disabled={job.cancel_requested || cancel.isPending}
              aria-label={`取消${names[job.kind]}`}
              onClick={() => cancel.mutate(job.id)}
            >
              <X size={14} />
              取消
            </button>
          )}
        </div>
      ))}
      {history && !jobs.isPending && !jobs.error && !matching.length && (
        <p className="workspace-form-hint">尚无手动运行记录。</p>
      )}
    </div>
  )
}
