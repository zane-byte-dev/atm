import { useEffect, useRef, useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  ListTodo,
  Filter,
  Layers,
  SlidersHorizontal,
  Plus,
  List,
  Columns3,
  Table2,
  Waves,
  X,
  Check,
} from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { call } from './api'
import type { SessionStatus } from './workspaces/activity-types'
import { waitingReason, taskActivity, taskActivityLabel } from './task-attention'
import { TaskBulkActions } from './task-bulk-actions'
import { WorkspaceSearchButton } from './global-search'
import { progressiveTasks } from './task-progressive'
import { TaskDataTable } from './task-data-table'
import type { Todo } from './types'
import { readTaskLayout, updateTaskLayout } from './task-navigation'
import { fields, statuses, filterTasks, visibleFields } from './task-view-model'
import { TaskFilterMenu } from './task-filter-menu'
import { TaskDisplayMenu } from './task-display-menu'
import { WorkspaceTitle } from './workspace-title'
import { TaskViews } from './task-views'
import './task-workbench.css'
const modes = [
  ['kanban', '看板', Columns3],
  ['list', '列表', List],
  ['table', '表格', Table2],
  ['swimlane', '泳道', Waves],
] as const

export function TaskWorkbench({
  items,
  params,
  onChange,
  onOpen,
  onCreate,
  writable,
  loading,
  error,
  onRetry,
}: {
  items: Todo[]
  params: URLSearchParams
  onChange: (params: URLSearchParams) => void
  onOpen: (id: string) => void
  onCreate: () => void
  writable: boolean
  loading: boolean
  error?: string
  onRetry: () => void
}) {
  const layout = readTaskLayout(params)
  const effectiveParams = new URLSearchParams(params)
  if (!params.has('focus') && !params.has('status') && !params.has('view'))
    effectiveParams.set('focus', 'unfinished')
  const shown = filterTasks(items, effectiveParams)
  const focus = effectiveParams.get('focus') || 'all'
  const presence = useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: 15000,
    refetchIntervalInBackground: false,
  })
  const [selected, setSelected] = useState<string[]>([])
  useEffect(() => setSelected([]), [params.toString()])
  const selectedItems = shown.filter((item) => selected.includes(item.id))
  const toggleSelected = (id: string) =>
    setSelected((previous) =>
      previous.includes(id) ? previous.filter((value) => value !== id) : [...previous, id],
    )
  const properties = visibleFields(params)
  const [menu, setMenu] = useState('')
  const toolbar = useRef<HTMLDivElement>(null)
  const [completionLimits, setCompletionLimits] = useState<Record<string, number>>({})
  useEffect(() => setCompletionLimits({}), [params.toString()])
  const [collapsed, setCollapsed] = useState<string[]>(['已完成'])
  useEffect(() => {
    if ((params.get('status') || '').split(',').includes('done'))
      setCollapsed((previous) => previous.filter((group) => group !== '已完成'))
  }, [params.get('status')])
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (!toolbar.current?.contains(event.target as Node)) setMenu('')
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenu('')
    }
    document.addEventListener('pointerdown', close)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('pointerdown', close)
      document.removeEventListener('keydown', escape)
    }
  }, [])
  const patch = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    value ? next.set(key, value) : next.delete(key)
    if (key === 'status') {
      next.set('focus', 'all')
      next.delete('view')
    }
    onChange(next)
  }
  const filterKeys = ['status', 'priority', 'project', 'creator', 'tag', 'from', 'to', 'q']
  const activeFilters = filterKeys.filter((key) => params.get(key) && params.get(key) !== 'all')
  const mode = modes.find((value) => value[0] === layout)!
  const ModeIcon = mode[2]
  const openMenu = (value: string) => setMenu(menu === value ? '' : value)
  const meta = (todo: Todo, field: string) =>
    field === 'tags'
      ? todo.tags?.join(' · ')
      : field === 'created'
        ? todo.created?.slice(0, 10)
        : String(todo[field as keyof Todo] || '')
  const card = (todo: Todo) => {
    const waiting = waitingReason(todo)
    const activity = taskActivity(todo, presence.isError ? undefined : presence.data)
    const activityText = taskActivityLabel(activity)
    return (
      <div
        key={todo.id}
        className={`workbench-row ${selected.includes(todo.id) ? 'is-selected' : ''}`}
      >
        <input
          className="task-row-select"
          type="checkbox"
          aria-label={`选择 ${todo.id}`}
          checked={selected.includes(todo.id)}
          onChange={() => toggleSelected(todo.id)}
        />
        <button className="workbench-card" data-task-id={todo.id} onClick={() => onOpen(todo.id)}>
          <span
            className={`task-state state-${todo.status} ${waiting ? 'state-waiting' : ''}`}
            aria-label={waiting ? '等待中' : statuses.find((s) => s[0] === todo.status)?.[1]}
          />
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
          <span className="workbench-card-meta">
            {properties
              .filter((field) => field !== 'id')
              .map((field) => (
                <span key={field} data-field={field} title={meta(todo, field) || '未设置'}>
                  {meta(todo, field) || '—'}
                </span>
              ))}
          </span>
        </button>
      </div>
    )
  }
  const cards = (rows: Todo[], key: string) => {
    const limit = completionLimits[key] || 5
    const visible = progressiveTasks(rows, limit)
    return (
      <>
        {visible.map(card)}
        {visible.length < rows.length && (
          <button
            className="workbench-load-more"
            onClick={() => setCompletionLimits((current) => ({ ...current, [key]: limit + 5 }))}
          >
            查看更多已完成 · 还有 {rows.length - visible.length} 项
          </button>
        )}
      </>
    )
  }
  const groupBy = params.get('group') || 'status'
  const groups: [string, Todo[]][] =
    groupBy === 'none'
      ? [['全部任务', shown]]
      : groupBy === 'project'
        ? [...new Set(shown.map((item) => item.project || '无项目'))].map((project) => [
            project,
            shown.filter((item) => (item.project || '无项目') === project),
          ])
        : statuses.map(([status, label]) => [label, shown.filter((item) => item.status === status)])
  return (
    <section className={`task-workbench density-${params.get('density') || 'compact'}`}>
      <header className="workbench-heading">
        <WorkspaceTitle icon={ListTodo}>
          任务 <small>{shown.length}</small>
        </WorkspaceTitle>
        {writable && (
          <button className="button" onClick={onCreate}>
            <Plus size={15} />
            新建任务
          </button>
        )}
      </header>
      <div className="workbench-toolbar" ref={toolbar}>
        <TaskViews
          params={effectiveParams}
          items={items}
          onChange={onChange}
          onOpenMenu={() => setMenu('')}
        />
        <div className="workbench-tools">
          <WorkspaceSearchButton />
          {presence.data?.agent_hooks && (
            <Link className="workbench-live-link" to="/agents">
              {presence.data.presence?.active_count || 0} 个 Agent 活动中
            </Link>
          )}
          <button aria-expanded={menu === 'filter'} onClick={() => openMenu('filter')}>
            <Filter size={15} />
            筛选{activeFilters.length ? ` ${activeFilters.length}` : ''}
          </button>
          {layout === 'table' && (
            <>
              <button aria-expanded={menu === 'group'} onClick={() => openMenu('group')}>
                <Layers size={15} />
                分组
              </button>
              <button aria-expanded={menu === 'columns'} onClick={() => openMenu('columns')}>
                <Columns3 size={15} />列
              </button>
            </>
          )}
          <button aria-expanded={menu === 'display'} onClick={() => openMenu('display')}>
            <SlidersHorizontal size={15} />
            显示
          </button>
          <button
            aria-label="切换显示模式"
            aria-expanded={menu === 'layout'}
            onClick={() => openMenu('layout')}
          >
            <ModeIcon size={15} />
            {mode[1]}
          </button>
        </div>
        {menu && (
          <div className={`workbench-popover popover-${menu}`}>
            {menu === 'layout' && (
              <>
                {modes.map(([value, label, Icon]) => (
                  <button
                    key={value}
                    onClick={() => {
                      onChange(updateTaskLayout(params, value))
                      setMenu('')
                    }}
                  >
                    <Icon size={16} />
                    {label}
                    {layout === value && <Check size={15} />}
                  </button>
                ))}
              </>
            )}
            {menu === 'filter' && (
              <TaskFilterMenu params={effectiveParams} items={items} onChange={onChange} />
            )}
            {menu === 'group' && (
              <>
                {[
                  ['none', '不分组'],
                  ['status', '状态'],
                  ['project', '项目'],
                ].map(([key, label]) => (
                  <button
                    key={key}
                    onClick={() => {
                      patch('tableGroup', key)
                      setMenu('')
                    }}
                  >
                    {label}
                    {(params.get('tableGroup') || 'none') === key && <Check size={14} />}
                  </button>
                ))}
              </>
            )}
            {menu === 'columns' && (
              <fieldset>
                <legend>显示列</legend>
                {fields.map(([value, label]) => (
                  <label key={value}>
                    <input
                      type="checkbox"
                      checked={properties.includes(value)}
                      onChange={() => {
                        const next = new URLSearchParams(params)
                        next.set(
                          'fields',
                          (properties.includes(value)
                            ? properties.filter((v) => v !== value)
                            : [...properties, value]
                          ).join(','),
                        )
                        onChange(next)
                      }}
                    />
                    {value === 'id' ? '任务编号' : label}
                  </label>
                ))}
              </fieldset>
            )}
            {menu === 'display' && <TaskDisplayMenu params={params} onChange={onChange} />}
          </div>
        )}
      </div>
      {!!activeFilters.length && (
        <div className="workbench-filter-chips">
          {activeFilters.map((key) => (
            <button key={key} onClick={() => patch(key, '')}>
              {
                (
                  {
                    status: '状态',
                    priority: '优先级',
                    project: '项目',
                    creator: '创建者',
                    tag: '标签',
                    from: '创建于',
                    to: '截止于',
                    q: '关键词',
                  } as Record<string, string>
                )[key]
              }
              :{' '}
              {key === 'status'
                ? params
                    .get(key)!
                    .split(',')
                    .map((value) => statuses.find((s) => s[0] === value)?.[1] || '已归档')
                    .join('、')
                : params.get(key)}
              <X size={12} />
            </button>
          ))}
        </div>
      )}
      <TaskBulkActions
        selected={selectedItems}
        writable={writable}
        onClear={() => setSelected([])}
      />
      {layout !== 'table' && shown.length > 0 && (
        <label className="task-select-all">
          <input
            type="checkbox"
            aria-label="选择全部筛选结果"
            checked={!!selectedItems.length && selectedItems.length === shown.length}
            onChange={(e) => setSelected(e.target.checked ? shown.map((item) => item.id) : [])}
          />
          选择全部 {shown.length} 项
        </label>
      )}
      <div className={`workbench-content ${layout === 'table' ? 'table-content' : ''}`}>
        {loading ? (
          <p role="status" className="workbench-empty">
            正在读取任务…
          </p>
        ) : error ? (
          <div role="alert" className="workbench-empty">
            {error}
            <button onClick={onRetry}>重试</button>
          </div>
        ) : !shown.length ? (
          <div className="workbench-empty">
            <List size={28} />
            <h3>没有匹配的任务</h3>
            <p>
              {activeFilters.length
                ? '调整筛选条件，或清除筛选。'
                : focus === 'review'
                  ? '目前没有等待验收的任务。'
                  : focus === 'waiting'
                    ? '目前没有等待条件或复查时间的进行中任务。'
                    : '当前视图没有任务。'}
            </p>
            <button
              className="button"
              onClick={() => {
                const next = new URLSearchParams()
                next.set('focus', 'all')
                onChange(next)
              }}
            >
              查看全部任务
            </button>
          </div>
        ) : (
          <>
            {layout === 'list' &&
              groups
                .filter(([, rows]) => rows.length)
                .map(([label, rows]) => (
                  <section className="workbench-group" key={label}>
                    <button
                      className="workbench-group-title"
                      aria-expanded={!collapsed.includes(label)}
                      onClick={() =>
                        setCollapsed(
                          collapsed.includes(label)
                            ? collapsed.filter((value) => value !== label)
                            : [...collapsed, label],
                        )
                      }
                    >
                      {collapsed.includes(label) ? (
                        <ChevronRight size={13} />
                      ) : (
                        <ChevronDown size={13} />
                      )}
                      {groupBy === 'status' && (
                        <span
                          className={`task-state state-${statuses.find(([, name]) => name === label)?.[0] || 'open'}`}
                          aria-hidden="true"
                        />
                      )}
                      {label}
                      <small>{rows.length}</small>
                    </button>
                    {!collapsed.includes(label) && cards(rows, 'list:' + label)}
                  </section>
                ))}
            {layout === 'table' && (
              <TaskDataTable
                items={shown}
                properties={properties}
                params={params}
                onChange={onChange}
                onOpen={onOpen}
                onColumns={() => setMenu('columns')}
                selected={selected}
                onSelectionChange={setSelected}
                activityStatus={presence.isError ? undefined : presence.data}
              />
            )}
            {layout === 'kanban' && (
              <div className="workbench-board">
                {statuses.map(([status, label]) => (
                  <section key={status}>
                    <h2>
                      {label}
                      <small>{shown.filter((todo) => todo.status === status).length}</small>
                    </h2>
                    {cards(
                      shown.filter((todo) => todo.status === status),
                      'board:' + status,
                    )}
                  </section>
                ))}
              </div>
            )}
            {layout === 'swimlane' && (
              <table className="workbench-swimlane">
                <thead>
                  <tr>
                    <th>项目</th>
                    {statuses.map(([status, label]) => (
                      <th key={status}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {[...new Set(shown.map((todo) => todo.project || '无项目'))].map((project) => (
                    <tr key={project}>
                      <th>
                        {project}
                        <small>
                          {shown.filter((todo) => (todo.project || '无项目') === project).length}
                        </small>
                      </th>
                      {statuses.map(([status]) => (
                        <td key={status}>
                          {cards(
                            shown.filter(
                              (todo) =>
                                (todo.project || '无项目') === project && todo.status === status,
                            ),
                            'swim:' + project + ':' + status,
                          )}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>
    </section>
  )
}
