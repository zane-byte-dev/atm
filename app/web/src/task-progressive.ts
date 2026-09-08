import type { Todo } from './types'
export function progressiveTasks(rows: Todo[], limit = 5) {
  let completed = 0
  return rows.filter((todo) => todo.status !== 'done' || completed++ < limit)
}
