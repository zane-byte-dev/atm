import type { CollectionSource } from './collection-types'
export const runLabels: Record<string, string> = {
  running: '账本记录运行中',
  succeeded: '最近采集成功',
  failed: '最近采集失败',
}
/** Wording follows `atm collect status` so the two surfaces cannot disagree. */
export const healthLabels: Record<string, string> = {
  ready: '正常',
  flaky: '偶发失败',
  auth_required: '登录已失效',
  permission_required: '权限不足',
  error: '持续失败',
  not_checked: '尚未运行',
}
export const healthTones: Record<string, string> = {
  ready: 'ok',
  flaky: 'warn',
  not_checked: 'warn',
}
/** A connector ATM stops retrying on its own is the one worth interrupting for. */
export const blockingStatuses = ['auth_required', 'permission_required', 'error']

export function rate(failed: number, runs: number) {
  if (!runs) return ''
  return `（${((failed / runs) * 100).toFixed(1)}%）`
}

export function total(value?: number) {
  return (value || 0).toLocaleString('zh-CN')
}

export function stamp(value?: number) {
  if (!value) return '尚无记录'
  return new Date(value * 1000).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function sourceName(source?: CollectionSource) {
  return source?.name || source?.external_id || '未知来源'
}
