import { Button, Checkbox } from 'antd'
import type { Todo } from '../../types'
import type { SessionStatus } from '../agents/activity-types'
import { waitingReason, taskActivity, taskActivityLabel } from './task-attention'

export function TaskPriority({ value }: { value: string }) {
  return (
    <span className={`table-priority table-priority-${value.toLowerCase()}`}>{value || '—'}</span>
  )
}

export function TaskCard({
  todo,
  properties,
  selected,
  tile,
  activityStatus,
  onSelect,
  onOpen,
}: {
  todo: Todo
  properties: string[]
  selected: boolean
  tile: boolean
  activityStatus?: SessionStatus
  onSelect: () => void
  onOpen: () => void
}) {
  const waiting = waitingReason(todo)
  const activity = taskActivity(todo, activityStatus)
  const activityText = taskActivityLabel(activity)
  const meta = (field: string) =>
    field === 'tags'
      ? todo.tags?.join(' · ')
      : field === 'created'
        ? todo.created?.slice(0, 10)
        : String(todo[field as keyof Todo] || '')
  return (
    <div
      className={`workbench-row ${tile ? 'workbench-row--tile' : ''} ${selected ? 'is-selected' : ''}`}
    >
      <Checkbox
        className="task-row-select"
        aria-label={`选择 ${todo.id}`}
        checked={selected}
        onChange={onSelect}
      />
      <Button className="workbench-card" data-task-id={todo.id} onClick={onOpen}>
        {properties.includes('id') && <span className="task-row-id">{todo.id}</span>}
        <span className="task-row-content">
          <strong title={todo.title}>{todo.title}</strong>
          {(waiting || activityText) && (
            <span className="task-row-attention">
              {waiting && (
                <span className="task-wait-label" title={waiting}>
                  等待：{waiting}
                </span>
              )}
              {activityText && (
                <span
                  title={
                    activity.lastAt
                      ? `最近观察 ${new Date(activity.lastAt).toLocaleString('zh-CN')}`
                      : '任务关联不表示会话正在工作'
                  }
                >
                  {activityText}
                  {activity.lastAt &&
                    ` · ${new Date(activity.lastAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`}
                </span>
              )}
            </span>
          )}
        </span>
        {properties.some((field) => field !== 'id') && (
          <span className="workbench-card-meta">
            {properties
              .filter((field) => field !== 'id')
              .map((field) => (
                <span key={field} data-field={field} title={meta(field) || '未设置'}>
                  {tile && field === 'priority' ? (
                    <TaskPriority value={todo.priority} />
                  ) : (
                    meta(field) || '—'
                  )}
                </span>
              ))}
          </span>
        )}
      </Button>
    </div>
  )
}
