export type NewTaskRequest = {
  open: boolean
  params: URLSearchParams
}

export type TaskLayout = 'list' | 'kanban'

export function readTaskLayout(params: URLSearchParams): TaskLayout {
  return params.get('layout') === 'kanban' ? 'kanban' : 'list'
}

export function updateTaskLayout(current: URLSearchParams, layout: TaskLayout): URLSearchParams {
  const params = new URLSearchParams(current)
  if (layout === 'kanban') {
    params.set('layout', 'kanban')
    params.delete('status')
  } else {
    params.delete('layout')
  }
  return params
}

/**
 * Consumes the one-shot task creation signal used by native launchers.
 * The caller replaces the current history entry with `params`, so reloading
 * cannot reopen the composer and unrelated task filters stay intact.
 */
export function consumeNewTaskRequest(
  current: URLSearchParams,
  canWrite: boolean,
): NewTaskRequest | undefined {
  if (current.get('new') !== '1') return undefined
  const params = new URLSearchParams(current)
  params.delete('new')
  return { open: canWrite, params }
}
