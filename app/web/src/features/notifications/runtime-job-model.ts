import type { Bootstrap } from '../../types'
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
export const jobNames: Record<JobKind, string> = {
  'session.sync': '同步会话',
  'collect.run': '执行采集',
  'collect.reprocess': '重新处理',
  'day.rebuild': '生成 AI Day',
  'quota.refresh': '刷新实时额度',
  'todo.refine': 'AI 整理任务',
}
export const jobStatuses: Record<RuntimeJob['status'], string> = {
  queued: '排队中',
  running: '运行中',
  succeeded: '已完成',
  failed: '未完成',
  canceled: '已取消',
  interrupted: '服务重启后中断',
}
export const jobActive = (job: RuntimeJob) => job.status === 'queued' || job.status === 'running'
export const jobsEnabled = (boot: Bootstrap) =>
  boot.capabilities?.workspace_write === true && boot.capabilities?.runtime_jobs === true
