import { CheckCheck, Circle, CircleCheck, CircleDot, Clock3 } from 'lucide-react'
import type { Todo } from '../../types'

export function StatusIcon({ todo }: { todo: Todo }) {
  const Icon =
    todo.status === 'done'
      ? CircleCheck
      : todo.status === 'review'
        ? CheckCheck
        : todo.wake_condition
          ? Clock3
          : todo.status === 'in_progress'
            ? CircleDot
            : Circle
  return (
    <Icon
      size={17}
      strokeWidth={1.7}
      className={`status-icon status-${todo.status} ${todo.wake_condition ? 'waiting' : ''}`}
    />
  )
}
