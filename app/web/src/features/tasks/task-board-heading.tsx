import type { Todo } from '../../types'
import { taskStatusLabels } from './task-status'

export function TaskBoardHeading({ status, count }: { status: Todo['status']; count?: number }) {
  return (
    <span className="task-board-heading">
      <span className={`task-state state-${status}`} aria-hidden="true" />
      <span>{taskStatusLabels[status]}</span>
      {count !== undefined && <small className="task-board-count">{count}</small>}
    </span>
  )
}

export function TaskBoardEmpty() {
  return <div className="task-board-empty">暂无任务</div>
}
