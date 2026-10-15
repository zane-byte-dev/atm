import { useWorkspaceNotifications } from './workspace-notifications'
import { Link } from 'react-router'
import { useEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { call } from '../../api'
import { useLiveConnection } from '../../live-connection'
import { domainsForJob, invalidateDomains } from '../../query-domains'
import type { Bootstrap } from '../../types'
import type { RuntimeJob } from './runtime-job-model'

import { jobNames, jobStatuses, jobActive, jobsEnabled } from './runtime-job-model'
// A submitted response and the global observer can see the same state. Show it once.
const notified = new Map<string, string>()
const initiated = new Set<string>()
export function notifyRuntimeJob(
  notification: ReturnType<typeof useWorkspaceNotifications>,
  job: RuntimeJob,
  observed = false,
) {
  if (!observed) initiated.add(job.id)
  if (notified.get(job.id) === job.status) return
  notified.delete(job.id)
  notified.set(job.id, job.status)
  if (notified.size > 100) notified.delete(notified.keys().next().value!)
  const failed = job.status === 'failed' || job.status === 'interrupted'
  const activity = observed && !initiated.has(job.id) && !failed
  notification.open({
    silent: activity,
    activity,
    href: '/settings?section=runtime',
    key: `runtime-job-${job.id}`,
    type: failed ? 'error' : job.status === 'succeeded' ? 'success' : 'info',
    title: `${jobNames[job.kind]} · ${jobStatuses[job.status]}`,
    description:
      job.error?.message ||
      (jobActive(job) ? job.phase || '任务在后台执行，可继续使用工作台。' : undefined),
    duration: failed ? 0 : 4.5,
    actions: <Link to="/messages">查看消息</Link>,
  })
  if (!jobActive(job)) initiated.delete(job.id)
}

// This observer stays mounted when the user switches workspaces.
export function RuntimeJobNotifications({ boot }: { boot: Bootstrap }) {
  const notification = useWorkspaceNotifications()
  const client = useQueryClient()
  const live = useLiveConnection()
  const previous = useRef<Map<string, string> | null>(null)
  const jobs = useQuery({
    queryKey: ['runtime-jobs'],
    queryFn: ({ signal }) => call<{ jobs: RuntimeJob[] }>('jobs.list', { limit: 30 }, signal),
    enabled: jobsEnabled(boot),
    refetchInterval: (query) =>
      live ? 60000 : query.state.data?.jobs.some(jobActive) ? 1500 : 15000,
    refetchIntervalInBackground: false,
  })
  useEffect(() => {
    if (!jobs.data) return
    const current = new Map(jobs.data.jobs.map((job) => [job.id, job.status]))
    if (previous.current) {
      const changed = new Set<string>()
      for (const job of jobs.data.jobs) {
        if (previous.current.get(job.id) === job.status) continue
        notifyRuntimeJob(notification, job, true)
        if (!jobActive(job)) for (const domain of domainsForJob(job.kind)) changed.add(domain)
      }
      if (changed.size) void invalidateDomains(client, [...changed])
    }
    previous.current = current
  }, [jobs.data, notification, client])
  return null
}
