import { useEffect, useMemo, useRef, useState, type RefObject } from 'react'
import { ArrowDown, ArrowUp, Download, Plus, ChevronDown, ChevronRight } from 'lucide-react'
import type { SessionStatus } from './workspaces/activity-types'
import { waitingReason, taskActivity, taskActivityLabel } from './task-attention'
import type { Todo } from './types'
import { fields, statuses, groupTasks } from './task-view-model'
import type { RemoteTaskWorkbench } from './task-query'
import { TaskPagedGroup, TaskPageControls } from './task-paged-group'
import { errorText } from './api'
import { progressiveTasks } from './task-progressive'
import { taskTableCSV } from './task-table-export'

export function TaskDataTable({
  items,
  properties,
  params,
  onChange,
  onOpen,
  onColumns,
  selected,
  onSelectionChange,
  activityStatus,
  remote,
  total = items.length,
  selectionCount,
  isSelected: selectedState,
  onToggleSelected,
  onSelectAll,
  onClearSelection,
  onExportRows,
}: {
  items: Todo[]
  remote?: RemoteTaskWorkbench
  total?: number
  selectionCount?: number
  isSelected?: (id: string) => boolean
  onToggleSelected?: (id: string) => void
  onSelectAll?: (checked: boolean) => void
  onClearSelection?: () => void
  onExportRows?: () => Promise<Todo[]>
  selected: string[]
  onSelectionChange: (ids: string[]) => void
  activityStatus?: SessionStatus
  properties: string[]
  params: URLSearchParams
  onChange: (params: URLSearchParams) => void
  onOpen: (id: string) => void
  onColumns: () => void
}) {
  const [completionLimits, setCompletionLimits] = useState<Record<string, number>>({})
  useEffect(() => setCompletionLimits({}), [params.toString()])
  const setSelected = onSelectionChange
  const [collapsed, setCollapsed] = useState<string[]>([])
  const allRef = useRef<HTMLInputElement>(null)
  const selectedSet = useMemo(() => new Set(selected), [selected])
  const isSelected = selectedState || ((id: string) => selectedSet.has(id))
  const visibleSelected = items.filter((item) => isSelected(item.id))
  const selectedCount = selectionCount ?? visibleSelected.length
  const toggleSelected =
    onToggleSelected ||
    ((id: string) =>
      setSelected(selectedSet.has(id) ? selected.filter((item) => item !== id) : [...selected, id]))
  const selectAll =
    onSelectAll || ((checked: boolean) => setSelected(checked ? items.map((item) => item.id) : []))
  const clearSelection = onClearSelection || (() => setSelected([]))
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState('')
  const groupedItems = useMemo(() => groupTasks(items), [items])
  useEffect(() => {
    if (allRef.current) allRef.current.indeterminate = selectedCount > 0 && selectedCount < total
  }, [selectedCount, total])
  const columns = fields.filter(([key]) => key !== 'id' && properties.includes(key))
  const grouping = params.get('tableGroup') || 'none'
  const groups: [string, Todo[]][] =
    grouping === 'status'
      ? statuses.map(([key, label]) => [label, groupedItems.byStatus.get(key) || []])
      : grouping === 'project'
        ? [...groupedItems.byProject].map(([project, rows]) => [project || '无项目', rows])
        : [['', items]]
  const sort = (key: string) => {
    const next = new URLSearchParams(params)
    next.set('sort', key)
    next.set(
      'direction',
      params.get('sort') === key && params.get('direction') !== 'desc' ? 'desc' : 'asc',
    )
    onChange(next)
  }
  const heading = (key: string, label: string) => (
    <button className="table-sort" onClick={() => sort(key)}>
      {label}
      {(params.get('sort') || 'auto') === key &&
        (params.get('direction') === 'desc' ? <ArrowDown size={12} /> : <ArrowUp size={12} />)}
    </button>
  )
  const exportRows = async () => {
    if (exporting) return
    setExporting(true)
    setExportError('')
    try {
      const data = onExportRows
        ? await onExportRows()
        : visibleSelected.length
          ? visibleSelected
          : items
      const csv = taskTableCSV(data, [
        { key: 'id', label: '编号' },
        { key: 'title', label: '任务' },
        { key: 'status', label: '状态' },
        ...columns.map(([key, label]) => ({ key, label })),
      ])
      const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }))
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = 'ATM-任务.csv'
      anchor.click()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (error) {
      setExportError(errorText(error))
    } finally {
      setExporting(false)
    }
  }
  const renderRow = (todo: Todo) => {
    const waiting = waitingReason(todo)
    const activity = taskActivityLabel(taskActivity(todo, activityStatus))
    return (
      <tr
        key={todo.id}
        className={isSelected(todo.id) ? 'selected' : ''}
        onClick={() => onOpen(todo.id)}
      >
        <td onClick={(event) => event.stopPropagation()}>
          <input
            type="checkbox"
            aria-label={`选择 ${todo.id}`}
            checked={isSelected(todo.id)}
            onChange={() => toggleSelected(todo.id)}
          />
        </td>
        <td>
          <button
            className="table-task-link"
            data-task-id={todo.id}
            onClick={(event) => {
              event.stopPropagation()
              onOpen(todo.id)
            }}
          >
            {properties.includes('id') && (
              <span className="table-task-id">{todo.id.toUpperCase()}</span>
            )}
            <strong title={todo.title}>{todo.title}</strong>
          </button>
          {(waiting || activity) && (
            <small className="table-task-attention" title={waiting}>
              {waiting || activity}
            </small>
          )}
        </td>
        <td>
          <span className="table-status">
            <span className={`task-state state-${todo.status} ${waiting ? 'state-waiting' : ''}`} />
            {waiting ? '等待中' : statuses.find((s) => s[0] === todo.status)?.[1] || todo.status}
          </span>
        </td>
        {columns.map(([key]) => (
          <td key={key}>
            {key === 'priority' ? (
              <span className={`table-priority table-priority-${todo.priority.toLowerCase()}`}>
                {todo.priority || '—'}
              </span>
            ) : key === 'tags' ? (
              <span className="table-tags">
                {todo.tags?.length ? (
                  todo.tags.map((tag) => <span key={tag}>{tag}</span>)
                ) : (
                  <span className="table-empty">—</span>
                )}
              </span>
            ) : (
              <span className={todo[key] ? '' : 'table-empty'}>
                {key === 'created' ? todo.created?.slice(0, 10) || '—' : String(todo[key] || '—')}
              </span>
            )}
          </td>
        ))}
        <td />
      </tr>
    )
  }
  return (
    <div className="task-data-table">
      <div className="table-utility">
        <span>{selectedCount ? `已选择 ${selectedCount} 项` : `${total} 项任务`}</span>
        {selectedCount > 0 && <button onClick={clearSelection}>取消选择</button>}
        <button className="table-export" disabled={exporting} onClick={() => void exportRows()}>
          <Download size={14} />
          {exporting ? '正在导出…' : selectedCount ? '导出所选' : '导出 CSV'}
        </button>
      </div>
      {exportError && <p role="alert">{exportError}</p>}
      <div className="table-scroll">
        <table className="workbench-table">
          <colgroup>
            <col style={{ width: 44 }} />
            <col style={{ width: 380 }} />
            <col style={{ width: 140 }} />
            {columns.map(([key]) => (
              <col key={key} style={{ width: key === 'tags' ? 200 : 150 }} />
            ))}
            <col style={{ width: 44 }} />
          </colgroup>
          <thead>
            <tr>
              <th>
                <input
                  ref={allRef}
                  type="checkbox"
                  aria-label="选择全部筛选结果"
                  checked={total > 0 && selectedCount === total}
                  onChange={(event) => selectAll(event.target.checked)}
                />
              </th>
              <th
                aria-sort={
                  params.get('sort') === 'title'
                    ? params.get('direction') === 'desc'
                      ? 'descending'
                      : 'ascending'
                    : 'none'
                }
              >
                {heading('title', '任务')}
              </th>
              <th>状态</th>
              {columns.map(([key, label]) => (
                <th
                  key={key}
                  aria-sort={
                    ['priority', 'created'].includes(key) && (params.get('sort') || 'auto') === key
                      ? params.get('direction') === 'desc'
                        ? 'descending'
                        : 'ascending'
                      : undefined
                  }
                >
                  {['priority', 'created'].includes(key) ? heading(key, label) : label}
                </th>
              ))}
              <th>
                <button className="table-add-column" aria-label="设置表格列" onClick={onColumns}>
                  <Plus size={16} />
                </button>
              </th>
            </tr>
          </thead>
          {remote
            ? remote.metadata.groups.map((group) => {
                const label =
                  remote.input.group_by === 'status'
                    ? statuses.find(([status]) => status === group.status)?.[1] || group.status
                    : remote.input.group_by === 'project'
                      ? group.project || '无项目'
                      : ''
                const folded = !!label && collapsed.includes(group.key)
                return (
                  <TaskPagedGroup key={group.key} remote={remote} group={group} enabled={!folded}>
                    {(page, ref) => (
                      <tbody ref={ref as RefObject<HTMLTableSectionElement | null>}>
                        {label && (
                          <tr className="table-group">
                            <th colSpan={columns.length + 4}>
                              <button
                                aria-expanded={!folded}
                                onClick={() =>
                                  setCollapsed((previous) =>
                                    folded
                                      ? previous.filter((name) => name !== group.key)
                                      : [...previous, group.key],
                                  )
                                }
                              >
                                {folded ? <ChevronRight size={14} /> : <ChevronDown size={14} />}
                                {label}
                                <small>{group.total}</small>
                              </button>
                            </th>
                          </tr>
                        )}
                        {!folded && (
                          <>
                            {page.rows.map(renderRow)}
                            <tr className="table-more">
                              <td colSpan={columns.length + 4}>
                                <TaskPageControls page={page} />
                              </td>
                            </tr>
                          </>
                        )}
                      </tbody>
                    )}
                  </TaskPagedGroup>
                )
              })
            : groups
                .filter(([, rows]) => rows.length)
                .map(([label, rows]) => (
                  <tbody key={label}>
                    {label && (
                      <tr className="table-group">
                        <th colSpan={columns.length + 4}>
                          <button
                            aria-expanded={!collapsed.includes(label)}
                            onClick={() =>
                              setCollapsed(
                                collapsed.includes(label)
                                  ? collapsed.filter((v) => v !== label)
                                  : [...collapsed, label],
                              )
                            }
                          >
                            {collapsed.includes(label) ? (
                              <ChevronRight size={14} />
                            ) : (
                              <ChevronDown size={14} />
                            )}{' '}
                            {label}
                            <small>{rows.length}</small>
                          </button>
                        </th>
                      </tr>
                    )}
                    {!collapsed.includes(label) &&
                      progressiveTasks(rows, completionLimits[label] || 5).map(renderRow)}
                    {!collapsed.includes(label) &&
                      rows.filter((todo) => todo.status === 'done').length >
                        (completionLimits[label] || 5) && (
                        <tr className="table-more">
                          <td colSpan={columns.length + 4}>
                            <button
                              className="workbench-load-more"
                              onClick={() =>
                                setCompletionLimits((current) => ({
                                  ...current,
                                  [label]: (current[label] || 5) + 5,
                                }))
                              }
                            >
                              查看更多已完成 · 还有{' '}
                              {rows.filter((todo) => todo.status === 'done').length -
                                (completionLimits[label] || 5)}{' '}
                              项
                            </button>
                          </td>
                        </tr>
                      )}
                  </tbody>
                ))}
        </table>
      </div>
    </div>
  )
}
