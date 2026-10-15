import { TaskCard } from './task-card'
import { TaskBoardHeading, TaskBoardEmpty } from './task-board-heading'
import { Button as AntButton, Checkbox, ConfigProvider } from 'antd'
import { useEffect, useMemo, useRef, useState, type RefObject } from 'react'
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
import { call, errorText } from '../../api'
import type { SessionStatus } from '../agents/activity-types'
import { updateTaskGroupSelection } from './task-selection'
import { TaskBulkActions } from './task-bulk-actions'
import { WorkspaceSearchButton } from '../../global-search'
import { progressiveTasks } from './task-progressive'
import { TaskDataTable } from './task-data-table'
import type { Todo } from '../../types'
import { readTaskLayout, updateTaskLayout } from './task-navigation'
import { fields, statuses, filterTasks, visibleFields, groupTasks } from './task-view-model'
import {
  collectTaskPages,
  selectedTaskRows,
  taskSelectionScope,
  type RemoteTaskWorkbench,
  type TaskGroup,
} from './task-query'
import { TaskPagedGroup, TaskPageControls } from './task-paged-group'
import { TaskFilterMenu } from './task-filter-menu'
import { TaskDisplayMenu } from './task-display-menu'
import { WorkspaceNavigatorHeading } from '../../components/workspace-navigator'
import { TaskViews } from './task-views'
import './task-workbench.css'
import './task-board.css'
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
  remote,
}: {
  items: Todo[]
  remote?: RemoteTaskWorkbench
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
  const queryString = params.toString()
  const shown = useMemo(
    () => (remote ? [] : filterTasks(items, effectiveParams)),
    [items, queryString, remote],
  )
  const groupedItems = useMemo(() => groupTasks(shown), [shown])
  const remoteGroupIndex = useMemo(
    () => new Map((remote?.metadata.groups || []).map((group) => [group.key, group])),
    [remote?.metadata.groups],
  )
  const remoteProjects = useMemo(() => {
    const projects = new Map<string, { total: number; statuses: Map<string, TaskGroup> }>()
    for (const group of remote?.metadata.groups || []) {
      const project = projects.get(group.project) || {
        total: 0,
        statuses: new Map<string, TaskGroup>(),
      }
      project.total += group.total
      project.statuses.set(group.status, group)
      projects.set(group.project, project)
    }
    return projects
  }, [remote?.metadata.groups])
  const total = remote?.metadata.matched_total ?? shown.length
  const focus = effectiveParams.get('focus') || 'all'
  const presence = useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: remote?.refetchInterval ?? 60000,
    refetchIntervalInBackground: false,
  })
  const [selected, setSelected] = useState<string[]>([])
  const [allSelected, setAllSelected] = useState(false)
  const [excluded, setExcluded] = useState<string[]>([])
  const [selectionNotice, setSelectionNotice] = useState('')
  const selectionEpoch = useRef(0)
  const groupSelectionLock = useRef(false)
  const batchBusy = useRef(false)
  const batchSelection = useRef({ scope: '', epoch: 0 })
  const previousMembership = useRef<string | undefined>(undefined)
  const selectionScope = taskSelectionScope(params)
  const currentScope = useRef(selectionScope)
  currentScope.current = selectionScope
  useEffect(() => {
    setSelected([])
    setAllSelected(false)
    setExcluded([])
    setSelectionNotice('')
    previousMembership.current = undefined
  }, [selectionScope])
  const knownRows = useRef(new Map<string, Todo>())
  for (const row of shown) knownRows.current.set(row.id, row)
  const selectionVersion = `${selectionScope}:${remote?.metadata.selection_revision || ''}`
  const currentSelectionVersion = useRef(selectionVersion)
  currentSelectionVersion.current = selectionVersion
  const [selectingGroup, setSelectingGroup] = useState(false)
  const selectedSet = useMemo(() => new Set(selected), [selected])
  const excludedSet = useMemo(() => new Set(excluded), [excluded])
  const isSelected = (id: string) => (allSelected ? !excludedSet.has(id) : selectedSet.has(id))
  const selectionCount = allSelected ? Math.max(0, total - excluded.length) : selected.length
  const clearSelection = () => {
    selectionEpoch.current += 1
    setSelected([])
    setAllSelected(false)
    setExcluded([])
  }
  useEffect(() => {
    const revision = remote?.metadata.selection_revision
    if (revision === undefined) return
    if (previousMembership.current !== undefined && previousMembership.current !== revision) {
      if (!batchBusy.current && (allSelected || selected.length))
        setSelectionNotice('筛选结果已变化，已清除之前的选择。')
      if (!batchBusy.current) clearSelection()
    }
    previousMembership.current = revision
  }, [remote?.metadata.selection_revision])
  const selectAll = (checked: boolean) => {
    clearSelection()
    if (checked) setAllSelected(true)
  }
  const loadSelected = async (): Promise<{ id: string }[]> => {
    if (!allSelected) return selected.map((id) => ({ id }))
    if (!remote) return shown.filter((row) => !excludedSet.has(row.id))
    const rows = await collectTaskPages(async (offset) => {
      const page = await call('todo.list', {
        ...remote.input,
        group_by: 'none',
        limit: 200,
        offset,
      })
      if (page.total - excluded.length > 100)
        throw new Error('筛选结果已变化，请重新选择最多 100 项任务。')
      return page
    })
    return selectedTaskRows(rows, excludedSet, 100)
  }
  const loadRows = async (): Promise<Todo[]> =>
    remote
      ? collectTaskPages((offset) =>
          call('todo.list', { ...remote.input, group_by: 'none', limit: 200, offset }),
        )
      : shown
  const exportRows = async (): Promise<Todo[]> => {
    if (!selectionCount) return loadRows()
    if (allSelected) return (await loadRows()).filter((row) => !excludedSet.has(row.id))
    if (!remote) return shown.filter((row) => selectedSet.has(row.id))
    const rows: Todo[] = []
    for (const id of selected) rows.push((await call('todo.show', { todo_id: id })).todo)
    return rows
  }
  const toggleSelected = (id: string) => {
    if (allSelected)
      setExcluded((previous) =>
        previous.includes(id) ? previous.filter((value) => value !== id) : [...previous, id],
      )
    else
      setSelected((previous) =>
        previous.includes(id) ? previous.filter((value) => value !== id) : [...previous, id],
      )
  }
  const groupCheckbox = (label: string, rows: Todo[], group?: TaskGroup) => {
    for (const row of rows) knownRows.current.set(row.id, row)
    const belongs = (id: string) => {
      const row = knownRows.current.get(id)
      if (!group || !remote) return rows.some((item) => item.id === id)
      if (!row) return false
      return (
        (remote.input.group_by !== 'status' || row.status === group.status) &&
        (remote.input.group_by !== 'project' || (row.project || '') === group.project)
      )
    }
    const count = group?.total ?? rows.length
    const checkedCount = allSelected
      ? count - excluded.filter(belongs).length
      : selected.filter(belongs).length
    return (
      <Checkbox
        className="task-row-select"
        aria-label={`选择${label}分组全部 ${count} 项`}
        checked={count > 0 && checkedCount === count}
        indeterminate={checkedCount > 0 && checkedCount < count}
        disabled={!count || selectingGroup}
        aria-busy={selectingGroup}
        onChange={async (event) => {
          if (groupSelectionLock.current) return
          groupSelectionLock.current = true
          const epoch = selectionEpoch.current
          const checked = event.target.checked
          const version = selectionVersion
          setSelectingGroup(true)
          try {
            const targets =
              remote && group
                ? await collectTaskPages((offset) =>
                    call('todo.list', {
                      ...remote.input,
                      group_key: group.key,
                      limit: 200,
                      offset,
                    }),
                  )
                : rows
            if (currentSelectionVersion.current !== version || selectionEpoch.current !== epoch)
              return
            for (const row of targets) knownRows.current.set(row.id, row)
            const ids = new Set(targets.map((row) => row.id))
            if (allSelected)
              setExcluded((previous) => updateTaskGroupSelection(previous, ids, checked, true))
            else setSelected((previous) => updateTaskGroupSelection(previous, ids, checked, false))
          } catch (error) {
            if (currentSelectionVersion.current === version) setSelectionNotice(errorText(error))
          } finally {
            groupSelectionLock.current = false
            setSelectingGroup(false)
          }
        }}
      />
    )
  }
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
      const target = event.target as Element
      if (
        !toolbar.current?.contains(target) &&
        !target.closest('.ant-select-dropdown, .ant-picker-dropdown')
      )
        setMenu('')
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setMenu('')
        if (
          !event.defaultPrevented &&
          !document.querySelector('dialog[open], [popover]:popover-open')
        )
          clearSelection()
      }
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
  const card = (todo: Todo) => {
    knownRows.current.set(todo.id, todo)
    return (
      <TaskCard
        key={todo.id}
        todo={todo}
        properties={properties}
        selected={isSelected(todo.id)}
        tile={layout !== 'list'}
        activityStatus={presence.isError ? undefined : presence.data}
        onSelect={() => toggleSelected(todo.id)}
        onOpen={() => onOpen(todo.id)}
      />
    )
  }
  const cards = (rows: Todo[], key: string) => {
    const limit = completionLimits[key] || 5
    const visible = progressiveTasks(rows, limit)
    return (
      <>
        {visible.map(card)}
        {visible.length < rows.length && (
          <AntButton
            htmlType="button"
            className="workbench-load-more"
            onClick={() => setCompletionLimits((current) => ({ ...current, [key]: limit + 5 }))}
          >
            查看更多已完成 · 还有 {rows.length - visible.length} 项
          </AntButton>
        )}
      </>
    )
  }
  const groupBy = params.get('group') || 'status'
  const groups: [string, Todo[]][] =
    groupBy === 'none'
      ? [['全部任务', shown]]
      : groupBy === 'project'
        ? [...groupedItems.byProject].map(([project, rows]) => [project || '无项目', rows])
        : statuses.map(([status, label]) => [label, groupedItems.byStatus.get(status) || []])
  return (
    <ConfigProvider wave={{ disabled: true }}>
      <section
        className={`task-workbench ${selectionCount ? 'has-bulk-selection' : ''} density-${params.get('density') || 'compact'}`}
      >
        <WorkspaceNavigatorHeading
          className="workbench-heading"
          icon={ListTodo}
          title={
            <>
              任务 <small>{total}</small>
            </>
          }
          actions={
            writable && (
              <AntButton className="button" onClick={onCreate}>
                <Plus size={15} />
                新建任务
              </AntButton>
            )
          }
        />
        <div className="workbench-toolbar" ref={toolbar}>
          <TaskViews
            params={effectiveParams}
            items={items}
            onChange={onChange}
            onOpenMenu={() => setMenu('')}
            metadata={remote?.metadata}
          />
          <div className="workbench-tools">
            <WorkspaceSearchButton />
            {presence.data?.agent_hooks && (
              <Link className="workbench-live-link" to="/agents">
                {presence.data.presence?.active_count || 0} 个 Agent 活动中
              </Link>
            )}
            <AntButton
              htmlType="button"
              aria-expanded={menu === 'filter'}
              onClick={() => openMenu('filter')}
            >
              <Filter size={15} />
              筛选{activeFilters.length ? ` ${activeFilters.length}` : ''}
            </AntButton>
            {layout === 'table' && (
              <>
                <AntButton
                  htmlType="button"
                  aria-expanded={menu === 'group'}
                  onClick={() => openMenu('group')}
                >
                  <Layers size={15} />
                  分组
                </AntButton>
                <AntButton
                  htmlType="button"
                  aria-expanded={menu === 'columns'}
                  onClick={() => openMenu('columns')}
                >
                  <Columns3 size={15} />列
                </AntButton>
              </>
            )}
            <AntButton
              htmlType="button"
              aria-expanded={menu === 'display'}
              onClick={() => openMenu('display')}
            >
              <SlidersHorizontal size={15} />
              显示
            </AntButton>
            <AntButton
              htmlType="button"
              aria-label="切换显示模式"
              aria-expanded={menu === 'layout'}
              onClick={() => openMenu('layout')}
            >
              <ModeIcon size={15} />
              {mode[1]}
            </AntButton>
          </div>
          {menu && (
            <div className={`workbench-popover popover-${menu}`}>
              {menu === 'layout' && (
                <>
                  {modes.map(([value, label, Icon]) => (
                    <AntButton
                      key={value}
                      htmlType="button"
                      onClick={() => {
                        onChange(updateTaskLayout(params, value))
                        setMenu('')
                      }}
                    >
                      <Icon size={16} />
                      {label}
                      {layout === value && <Check size={15} />}
                    </AntButton>
                  ))}
                </>
              )}
              {menu === 'filter' && (
                <TaskFilterMenu
                  params={effectiveParams}
                  items={items}
                  onChange={onChange}
                  facets={remote?.metadata.facets}
                />
              )}
              {menu === 'group' && (
                <>
                  {[
                    ['none', '不分组'],
                    ['status', '状态'],
                    ['project', '项目'],
                  ].map(([key, label]) => (
                    <AntButton
                      key={key}
                      htmlType="button"
                      onClick={() => {
                        patch('tableGroup', key)
                        setMenu('')
                      }}
                    >
                      {label}
                      {(params.get('tableGroup') || 'none') === key && <Check size={14} />}
                    </AntButton>
                  ))}
                </>
              )}
              {menu === 'columns' && (
                <fieldset>
                  <legend>显示列</legend>
                  {fields.map(([value, label]) => (
                    <Checkbox
                      key={value}
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
                    >
                      {value === 'id' ? '任务编号' : label}
                    </Checkbox>
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
              <AntButton key={key} htmlType="button" onClick={() => patch(key, '')}>
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
              </AntButton>
            ))}
          </div>
        )}
        <TaskBulkActions
          selected={selected.map((id) => ({ id }))}
          selectionCount={selectionCount}
          selectionScopeLabel={allSelected ? '全部筛选结果' : undefined}
          loadSelected={loadSelected}
          writable={writable}
          onClear={clearSelection}
          onBusyChange={(busy) => {
            batchBusy.current = busy
            if (busy)
              batchSelection.current = { scope: selectionScope, epoch: selectionEpoch.current }
          }}
          onRemaining={(ids) => {
            if (
              batchSelection.current.scope !== currentScope.current ||
              batchSelection.current.epoch !== selectionEpoch.current
            )
              return
            clearSelection()
            previousMembership.current = undefined
            setSelected(ids)
          }}
        />
        {selectionNotice && <p role="status">{selectionNotice}</p>}
        <div
          className={`workbench-content layout-${layout} ${layout === 'table' ? 'table-content' : ''}`}
          tabIndex={layout === 'kanban' || layout === 'swimlane' ? 0 : undefined}
          aria-label={
            layout === 'kanban' ? '任务看板' : layout === 'swimlane' ? '项目泳道' : undefined
          }
        >
          {loading ? (
            <p role="status" className="workbench-empty">
              正在读取任务…
            </p>
          ) : error ? (
            <div role="alert" className="workbench-empty">
              {error}
              <AntButton htmlType="button" onClick={onRetry}>
                重试
              </AntButton>
            </div>
          ) : !total ? (
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
              <AntButton
                className="button"
                onClick={() => {
                  const next = new URLSearchParams()
                  next.set('focus', 'all')
                  onChange(next)
                }}
              >
                查看全部任务
              </AntButton>
            </div>
          ) : remote ? (
            <>
              {layout === 'table' ? (
                <TaskDataTable
                  items={[]}
                  remote={remote}
                  properties={properties}
                  params={params}
                  onChange={onChange}
                  onOpen={onOpen}
                  onColumns={() => setMenu('columns')}
                  selected={selected}
                  onSelectionChange={setSelected}
                  activityStatus={presence.isError ? undefined : presence.data}
                  selectionCount={selectionCount}
                  total={total}
                  isSelected={isSelected}
                  onToggleSelected={toggleSelected}
                  onSelectAll={selectAll}
                  onClearSelection={clearSelection}
                  onExportRows={exportRows}
                />
              ) : layout === 'swimlane' ? (
                <table className="workbench-swimlane">
                  <thead>
                    <tr>
                      <th>项目</th>
                      {statuses.map(([status]) => (
                        <th key={status} scope="col">
                          <TaskBoardHeading status={status} />
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {[...remoteProjects].map(([project, projectGroups]) => (
                      <tr key={project}>
                        <th scope="row" className="task-project-heading">
                          <span title={project || '无项目'}>{project || '无项目'}</span>
                          <small>{projectGroups.total} 项任务</small>
                        </th>
                        {statuses.map(([status]) => {
                          const group = projectGroups.statuses.get(status)
                          return group ? (
                            <TaskPagedGroup key={status} remote={remote} group={group}>
                              {(page, ref) => (
                                <td ref={ref as RefObject<HTMLTableCellElement | null>}>
                                  {page.rows.map(card)}
                                  <TaskPageControls page={page} />
                                </td>
                              )}
                            </TaskPagedGroup>
                          ) : (
                            <td key={status}>
                              <TaskBoardEmpty />
                            </td>
                          )
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                <div className={layout === 'kanban' ? 'workbench-board' : undefined}>
                  {(layout === 'kanban'
                    ? statuses.map(
                        ([status]) =>
                          remoteGroupIndex.get(JSON.stringify(['', status])) || {
                            key: JSON.stringify(['', status]),
                            project: '',
                            status,
                            total: 0,
                            active_total: 0,
                            done_total: 0,
                          },
                      )
                    : remote.metadata.groups
                  ).map((group) => {
                    const label =
                      remote.input.group_by === 'status'
                        ? statuses.find(([status]) => status === group.status)?.[1] || group.status
                        : remote.input.group_by === 'project'
                          ? group.project || '无项目'
                          : '全部任务'
                    const collapseKey = remote.input.group_by === 'status' ? label : group.key
                    const folded = layout === 'list' && collapsed.includes(collapseKey)
                    return (
                      <TaskPagedGroup
                        key={group.key}
                        remote={remote}
                        group={group}
                        enabled={!folded}
                      >
                        {(page, ref) => (
                          <section
                            className={layout === 'list' ? 'workbench-group' : 'task-board-column'}
                            ref={ref as RefObject<HTMLElement | null>}
                          >
                            {layout === 'kanban' ? (
                              <h2>
                                <TaskBoardHeading
                                  status={group.status as Todo['status']}
                                  count={group.total}
                                />
                              </h2>
                            ) : (
                              <div className="workbench-group-header">
                                {groupCheckbox(label, page.rows, group)}
                                <AntButton
                                  className="workbench-group-title"
                                  aria-expanded={!folded}
                                  onClick={() =>
                                    setCollapsed((previous) =>
                                      folded
                                        ? previous.filter((name) => name !== collapseKey)
                                        : [...previous, collapseKey],
                                    )
                                  }
                                >
                                  {folded ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
                                  {remote.input.group_by === 'status' && (
                                    <span
                                      className={`task-state state-${group.status}`}
                                      aria-hidden="true"
                                    />
                                  )}
                                  {label}
                                  <small>{group.total}</small>
                                </AntButton>
                              </div>
                            )}
                            {!folded && (
                              <>
                                {page.rows.map(card)}
                                {layout === 'kanban' && group.total === 0 && <TaskBoardEmpty />}
                                <TaskPageControls page={page} />
                              </>
                            )}
                          </section>
                        )}
                      </TaskPagedGroup>
                    )
                  })}
                </div>
              )}
            </>
          ) : (
            <>
              {layout === 'list' &&
                groups
                  .filter(([, rows]) => rows.length)
                  .map(([label, rows]) => (
                    <section className="workbench-group" key={label}>
                      <div className="workbench-group-header">
                        {groupCheckbox(label, rows)}
                        <AntButton
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
                        </AntButton>
                      </div>
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
                  selectionCount={selectionCount}
                  total={total}
                  isSelected={isSelected}
                  onToggleSelected={toggleSelected}
                  onSelectAll={selectAll}
                  onClearSelection={clearSelection}
                  onExportRows={exportRows}
                />
              )}
              {layout === 'kanban' && (
                <div className="workbench-board">
                  {statuses.map(([status]) => (
                    <section key={status} className="task-board-column">
                      <h2>
                        <TaskBoardHeading
                          status={status}
                          count={(groupedItems.byStatus.get(status) || []).length}
                        />
                      </h2>
                      {!groupedItems.byStatus.get(status)?.length && <TaskBoardEmpty />}
                      {cards(groupedItems.byStatus.get(status) || [], 'board:' + status)}
                    </section>
                  ))}
                </div>
              )}
              {layout === 'swimlane' && (
                <table className="workbench-swimlane">
                  <thead>
                    <tr>
                      <th>项目</th>
                      {statuses.map(([status]) => (
                        <th key={status} scope="col">
                          <TaskBoardHeading status={status} />
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {[...groupedItems.byProject.keys()].map((project) => (
                      <tr key={project}>
                        <th scope="row" className="task-project-heading">
                          <span title={project || '无项目'}>{project || '无项目'}</span>
                          <small>{(groupedItems.byProject.get(project) || []).length} 项任务</small>
                        </th>
                        {statuses.map(([status]) => (
                          <td key={status}>
                            {!groupedItems.byProjectStatus.get(JSON.stringify([project, status]))
                              ?.length && <TaskBoardEmpty />}
                            {cards(
                              groupedItems.byProjectStatus.get(JSON.stringify([project, status])) ||
                                [],
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
    </ConfigProvider>
  )
}
