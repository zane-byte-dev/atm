import type { Todo } from '../../types'
import type { SessionStatus } from '../agents/activity-types'

export function waitingReason(todo: Todo): string {
  if (todo.status !== 'in_progress') return ''
  return todo.wake_condition || (todo.review_at ? `复查时间 ${todo.review_at}` : '')
}

// Binding is a relationship, not proof of activity. Match explicit identities only.
export function taskActivity(todo: Todo, status?: SessionStatus) {
  const bindings = (status?.bindings || []).filter(
    (row) =>
      row.state === 'bound' && row.binding.todo_id === todo.id && todo.status === 'in_progress',
  )
  const ids = new Set(bindings.map((row) => row.binding.session_id))
  const observed = (status?.agent_hooks ? status.presence?.sessions || [] : []).filter((row) =>
    [row.id, row.session_id, row.resume_id].some((id) => id && ids.has(id)),
  )
  const active = observed.filter((row) => row.state === 'active' || row.state === 'busy')
  const attention = observed.filter((row) => !!row.attention)
  const lastAt = observed
    .map((row) => row.updated_at)
    .sort()
    .at(-1)
  return { bound: bindings.length, active: active.length, attention: attention.length, lastAt }
}

export function taskActivityLabel(value: ReturnType<typeof taskActivity>): string {
  if (value.attention) return `${value.attention} 个 Agent 待处理`
  if (value.active) return `${value.active} 个 Agent 活动中`
  if (value.bound) return `${value.bound} 个会话已关联`
  return ''
}
