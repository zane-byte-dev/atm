import { Button as AntButton, Dropdown, Spin } from 'antd'
import { useWorkspaceNotifications } from './workspace-notifications'
import { useRef, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Play, RefreshCw, X } from 'lucide-react'
import { call, errorText } from '../../api'
import {
  jobNames as names,
  jobStatuses as statuses,
  jobActive as active,
  jobsEnabled,
} from './runtime-job-model'
import { notifyRuntimeJob } from './runtime-job-notifications'

import { Notice } from '../../components/notice'
import { domainsForJob, invalidateDomains } from '../../query-domains'
import { useLiveConnection } from '../../live-connection'
import type { Bootstrap } from '../../types'
import '../../styles/workspace-forms.css'

import type { JobKind, JobInput, RuntimeJob } from './runtime-job-model'
/** Starting a job is always an explicit button click. Polling only reads its saved state. */
export function RuntimeJobs({
  boot,
  actions = [],
  kinds,
  history = false,
  secondaryActions = false,
  extraAction,
}: {
  boot: Bootstrap
  actions?: { label?: string; input: JobInput; disabled?: boolean; title?: string }[]
  kinds: JobKind[]
  history?: boolean
  secondaryActions?: boolean
  extraAction?: ReactNode
}) {
  const notification = useWorkspaceNotifications()
  const enabled = jobsEnabled(boot)
  const live = useLiveConnection()
  const client = useQueryClient()
  const pendingOperations = useRef(new Map<string, { input: JobInput; key: string }>())
  const jobs = useQuery({
    queryKey: ['runtime-jobs'],
    queryFn: ({ signal }) => call<{ jobs: RuntimeJob[] }>('jobs.list', { limit: 30 }, signal),
    enabled,
    refetchInterval: (query) => (live ? 60000 : query.state.data?.jobs.some(active) ? 1500 : 15000),
    refetchIntervalInBackground: false,
  })
  const run = useMutation({
    onError: (error, operation) =>
      notification.error({
        title: `${names[operation.input.kind]} · 提交未确认`,
        description: errorText(error),
        duration: 0,
      }),
    mutationFn: (operation: { input: JobInput; key: string }) =>
      call<RuntimeJob>('jobs.run', operation.input, undefined, operation.key),
    retry: false,
    onSuccess: (job, operation) => {
      pendingOperations.current.delete(JSON.stringify(operation.input))
      notifyRuntimeJob(notification, job)
      void client.invalidateQueries({ queryKey: ['runtime-jobs'] })
      if (!active(job)) void invalidateDomains(client, domainsForJob(job.kind))
    },
  })
  const cancel = useMutation({
    onError: (error) =>
      notification.error({ title: '取消任务失败', description: errorText(error), duration: 0 }),
    mutationFn: (job_id: string) => call<RuntimeJob>('jobs.cancel', { job_id }),
    retry: false,
    onSuccess: (job) => {
      notifyRuntimeJob(notification, job)
      return client.invalidateQueries({ queryKey: ['runtime-jobs'] })
    },
  })
  if (!enabled) return extraAction ? <div className="workspace-actions">{extraAction}</div> : null
  const matching = (jobs.data?.jobs || []).filter((job) => kinds.includes(job.kind))
  const visible = history ? matching.slice(0, 15) : []
  const renderAction = (action: NonNullable<typeof actions>[number], index: number) => (
    <AntButton
      key={index}
      htmlType="button"
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
      {action.input.kind === 'session.sync' || action.input.kind === 'quota.refresh' ? (
        <RefreshCw size={13} />
      ) : (
        <Play size={13} />
      )}
      {action.label || names[action.input.kind]}
    </AntButton>
  )
  return (
    <div className={`workspace-jobs ${history ? 'workspace-jobs-history' : ''}`}>
      {(!!actions.length || extraAction) && (
        <div className="workspace-actions">
          {secondaryActions ? (
            <>
              {actions[0] && renderAction(actions[0], 0)}
              {actions.length > 1 && (
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: actions.slice(1).map((action, index) => ({
                      key: String(index),
                      label: action.label || names[action.input.kind],
                      disabled:
                        action.disabled ||
                        run.isPending ||
                        matching.some((job) => job.kind === action.input.kind && active(job)),
                      onClick: () => {
                        const signature = JSON.stringify(action.input)
                        let operation = pendingOperations.current.get(signature)
                        if (!operation) {
                          operation = { input: { ...action.input }, key: crypto.randomUUID() }
                          pendingOperations.current.set(signature, operation)
                        }
                        run.mutate(operation)
                      },
                    })),
                  }}
                >
                  <AntButton>更多</AntButton>
                </Dropdown>
              )}
            </>
          ) : (
            actions.map(renderAction)
          )}
          {extraAction}
        </div>
      )}
      {history && jobs.error && <Notice error={jobs.error} retry={() => void jobs.refetch()} />}
      {visible.map((job) => (
        <div className={`workspace-job ${job.status}`} key={job.id} role="status">
          {active(job) ? (
            <Spin size="small" />
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
            <AntButton
              htmlType="button"
              className="text-button"
              disabled={job.cancel_requested || cancel.isPending}
              aria-label={`取消${names[job.kind]}`}
              onClick={() => cancel.mutate(job.id)}
            >
              <X size={14} />
              取消
            </AntButton>
          )}
        </div>
      ))}
      {history && !jobs.isPending && !jobs.error && !matching.length && (
        <p className="workspace-form-hint">尚无手动运行记录。</p>
      )}
    </div>
  )
}
