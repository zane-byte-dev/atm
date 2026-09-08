import { Select } from './components/select'
import { useEffect, useRef, useState } from 'react'
import {
  Save,
  Layers,
  Plus,
  SlidersHorizontal,
  X,
  GripVertical,
  ArrowUp,
  ArrowDown,
  Trash2,
} from 'lucide-react'
import type { Todo } from './types'
import {
  fields,
  statuses,
  matchingTasks,
  parseViews,
  savedViewsKey,
  viewQuery,
  visibleFields,
  type SavedView,
} from './task-view-model'
import {
  builtinViews,
  orderViews,
  moveView,
  parseViewPreferences,
  viewPreferencesKey,
  type ViewPreferences,
} from './task-view-preferences'
import { readTaskLayout } from './task-navigation'
import './task-views.css'
import type { TaskWorkbenchMetadata } from './task-query'

const layouts = [
  ['list', '列表'],
  ['kanban', '看板'],
  ['table', '表格'],
  ['swimlane', '泳道'],
]
const sorts = [
  ['auto', '默认'],
  ['updated', '更新时间'],
  ['priority', '优先级'],
  ['created', '创建日期'],
  ['title', '标题'],
]
const filters = [
  ['status', '状态'],
  ['priority', '优先级'],
  ['project', '项目'],
  ['creator', '创建者'],
  ['tag', '标签'],
  ['from', '创建日期从'],
  ['to', '创建日期至'],
  ['q', '关键词'],
]
const read = (key: string) => {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}
type ViewEntry = { id: string; name: string; focus?: string; query?: string }

export function TaskViews({
  params,
  items,
  onChange,
  onOpenMenu,
  metadata,
}: {
  params: URLSearchParams
  metadata?: TaskWorkbenchMetadata
  items: Todo[]
  onChange: (params: URLSearchParams) => void
  onOpenMenu: () => void
}) {
  const [views, setViews] = useState(() => parseViews(read(savedViewsKey)))
  const [preferences, setPreferences] = useState(() =>
    parseViewPreferences(read(viewPreferencesKey)),
  )
  const [menu, setMenu] = useState(false)
  const [mode, setMode] = useState<'create' | 'manage' | null>(null)
  const [name, setName] = useState('')
  const [draft, setDraft] = useState(() => new URLSearchParams())
  const [filterRows, setFilterRows] = useState<string[]>([])
  const [error, setError] = useState('')
  const [dragged, setDragged] = useState('')
  const menuRoot = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const dialog = useRef<HTMLDialogElement>(null)
  const nameField = useRef<HTMLInputElement>(null)
  const active = views.find((view) => view.id === params.get('view'))
  const entries = orderViews<ViewEntry>([...builtinViews, ...views], preferences.order)
  const baseName =
    active?.name ||
    builtinViews.find((view) => view.focus === (params.get('focus') || 'all'))?.name ||
    '全部'
  useEffect(() => {
    if (mode) {
      dialog.current?.showModal()
      if (mode === 'create') nameField.current?.focus()
    } else if (dialog.current?.open) {
      dialog.current.close()
      trigger.current?.focus()
    }
  }, [mode])
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (!menuRoot.current?.contains(event.target as Node)) setMenu(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMenu(false)
    }
    const sync = (event: StorageEvent) => {
      if (event.key === savedViewsKey || event.key === null)
        setViews(parseViews(read(savedViewsKey)))
      if (event.key === viewPreferencesKey || event.key === null)
        setPreferences(parseViewPreferences(read(viewPreferencesKey)))
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', escape)
    window.addEventListener('storage', sync)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', escape)
      window.removeEventListener('storage', sync)
    }
  }, [])
  const persist = (next: SavedView[]) => {
    try {
      localStorage.setItem(savedViewsKey, JSON.stringify(next))
      setViews(next)
      setError('')
      return true
    } catch {
      setError('视图保存失败，浏览器存储不可用。请重试。')
      return false
    }
  }
  const savePreferences = (next: ViewPreferences) => {
    try {
      localStorage.setItem(viewPreferencesKey, JSON.stringify(next))
      setPreferences(next)
      setError('')
      return true
    } catch {
      setError('视图设置保存失败，浏览器存储不可用。请重试。')
      return false
    }
  }
  const patchDraft = (key: string, value: string) =>
    setDraft((previous) => {
      const next = new URLSearchParams(previous)
      next.set(key, value)
      return next
    })
  const open = (value: 'create' | 'manage') => {
    setMenu(false)
    setError('')
    setName('')
    setDraft(new URLSearchParams(viewQuery(params)))
    setFilterRows(filters.filter(([key]) => params.has(key)).map(([key]) => key))
    setMode(value)
  }
  const select = (entry: ViewEntry) => {
    const next = new URLSearchParams(entry.focus ? params : entry.query)
    if (entry.focus) {
      next.set('focus', entry.focus)
      next.delete('status')
      next.delete('view')
    } else next.set('view', entry.id)
    onChange(next)
  }
  const reorder = (source: string, target: string) =>
    savePreferences({
      ...preferences,
      order: moveView(
        entries.map((entry) => entry.id),
        source,
        target,
      ),
    })
  const toggleVisibility = (entry: ViewEntry) => {
    if (entry.id === 'builtin:all') return
    const hiding = !preferences.hidden.includes(entry.id)
    const hidden = hiding
      ? [...preferences.hidden, entry.id]
      : preferences.hidden.filter((id) => id !== entry.id)
    if (
      savePreferences({ ...preferences, hidden }) &&
      hiding &&
      (active?.id === entry.id || (!active && entry.focus === params.get('focus')))
    ) {
      select(builtinViews[3])
    }
  }
  const layout = readTaskLayout(draft)
  const groupKey = layout === 'table' ? 'tableGroup' : 'group'
  const group =
    layout === 'kanban'
      ? 'status'
      : layout === 'swimlane'
        ? 'project'
        : draft.get(groupKey) || (layout === 'table' ? 'none' : 'status')
  const groups = [
    ['none', '不分组'],
    ['status', '状态'],
    ['project', '项目'],
  ]
  const options = (key: string) =>
    key === 'status'
      ? [...statuses, ['archived', '已归档']]
      : key === 'priority'
        ? ['P0', 'P1', 'P2', 'P3'].map((value) => [value, value])
        : [
            ...new Set(
              metadata?.facets[key]
                ? metadata.facets[key].map((item) => item.value)
                : items
                    .flatMap((item) =>
                      key === 'tag' ? item.tags || [] : [item[key as 'creator' | 'project'] || ''],
                    )
                    .filter(Boolean),
            ),
          ]
            .sort()
            .map((value) => [value, value])
  return (
    <div className="workbench-views">
      {entries
        .filter((entry) => !preferences.hidden.includes(entry.id))
        .map((entry) => {
          const selected = active
            ? active.id === entry.id
            : !params.get('status') && entry.focus === (params.get('focus') || 'all')
          const countParams = new URLSearchParams(params)
          if (entry.focus) {
            countParams.set('focus', entry.focus)
            countParams.delete('status')
          }
          return (
            <button
              key={entry.id}
              className={selected ? 'selected' : ''}
              aria-pressed={selected}
              onClick={() => select(entry)}
            >
              {entry.name}
              {entry.focus && (
                <small>
                  {metadata
                    ? metadata.view_counts[entry.focus] || 0
                    : matchingTasks(items, countParams).length}
                </small>
              )}
            </button>
          )
        })}
      <div className="task-views-menu-anchor" ref={menuRoot}>
        <button
          ref={trigger}
          aria-label="新增或管理视图"
          aria-expanded={menu}
          aria-haspopup="true"
          onClick={() => {
            onOpenMenu()
            setMenu(!menu)
          }}
        >
          <Layers size={17} />
        </button>
        {menu && (
          <div className="task-views-menu">
            <button onClick={() => open('create')}>
              <Plus size={18} />
              新增视图
            </button>
            <button onClick={() => open('manage')}>
              <SlidersHorizontal size={18} />
              管理视图
            </button>
            {active && (
              <button
                onClick={() => {
                  if (
                    persist(
                      views.map((view) =>
                        view.id === active.id ? { ...view, query: viewQuery(params) } : view,
                      ),
                    )
                  )
                    setMenu(false)
                }}
              >
                <Save size={15} />
                更新当前视图
              </button>
            )}
          </div>
        )}
      </div>
      {!mode && error && <p role="alert">{error}</p>}
      <dialog
        ref={dialog}
        className={`task-view-dialog ${mode === 'manage' ? 'is-manage' : ''}`}
        aria-labelledby="task-view-dialog-title"
        onCancel={() => setMode(null)}
        onClick={(event) => {
          if (event.target === event.currentTarget) {
            const rect = event.currentTarget.getBoundingClientRect()
            if (
              event.clientX < rect.left ||
              event.clientX > rect.right ||
              event.clientY < rect.top ||
              event.clientY > rect.bottom
            )
              setMode(null)
          }
        }}
      >
        <header>
          <h2 id="task-view-dialog-title">
            {mode === 'manage' ? (
              <>
                <Layers size={22} />
                管理视图
              </>
            ) : (
              '保存为视图'
            )}
          </h2>
          <button
            className="view-icon-button"
            aria-label="关闭视图弹窗"
            onClick={() => setMode(null)}
          >
            <X size={22} />
          </button>
        </header>
        <p className="task-view-subtitle">
          {mode === 'manage' ? '排序与显隐只影响当前浏览器' : `基于「任务 · ${baseName}」创建`}
        </p>
        {error && (
          <p className="task-view-error" role="alert">
            {error}
          </p>
        )}
        {mode === 'create' && (
          <form
            onSubmit={(event) => {
              event.preventDefault()
              if (!name.trim()) return
              const view = { id: crypto.randomUUID(), name: name.trim(), query: viewQuery(draft) }
              if (persist([...views, view])) {
                const next = new URLSearchParams(view.query)
                next.set('view', view.id)
                onChange(next)
                setMode(null)
              }
            }}
          >
            <div className="task-view-form-body">
              <label className="task-view-field">
                <span>名称</span>
                <input
                  ref={nameField}
                  autoFocus
                  required
                  maxLength={60}
                  placeholder="例如：需要 Review"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                />
              </label>
              <p className="task-view-local-note">视图保存在当前浏览器</p>
              <label className="task-view-field">
                <span>任务范围</span>
                <Select
                  value={draft.get('focus') || 'all'}
                  onChange={(event) => patchDraft('focus', event.target.value)}
                >
                  {builtinViews.map((view) => (
                    <option key={view.focus} value={view.focus}>
                      {view.name}
                    </option>
                  ))}
                </Select>
              </label>
              <div className="task-view-field view-filter-field">
                <span>筛选</span>
                <div className="task-view-filters">
                  {filterRows.map((key) => (
                    <div className="task-view-filter" key={key}>
                      <span>{filters.find(([value]) => key === value)?.[1]}</span>
                      {['q', 'from', 'to'].includes(key) ? (
                        <input
                          aria-label={`视图筛选${key === 'q' ? '关键词' : key === 'from' ? '创建日期从' : '创建日期至'}`}
                          type={key === 'q' ? 'text' : 'date'}
                          value={draft.get(key) || ''}
                          onChange={(event) => patchDraft(key, event.target.value)}
                        />
                      ) : (
                        <Select
                          aria-label={`视图筛选${filters.find(([value]) => key === value)?.[1]}`}
                          value={draft.get(key) || ''}
                          onChange={(event) => patchDraft(key, event.target.value)}
                        >
                          <option value="">全部</option>
                          {draft.get(key) &&
                            !options(key).some(([value]) => value === draft.get(key)) && (
                              <option value={draft.get(key)!}>
                                {draft
                                  .get(key)!
                                  .split(',')
                                  .map(
                                    (value) =>
                                      options(key).find(([id]) => id === value)?.[1] || value,
                                  )
                                  .join('、')}
                              </option>
                            )}
                          {options(key).map(([value, label]) => (
                            <option key={value} value={value}>
                              {label}
                            </option>
                          ))}
                        </Select>
                      )}
                      <button
                        type="button"
                        className="view-icon-button"
                        aria-label={`移除${filters.find(([value]) => key === value)?.[1]}筛选`}
                        onClick={() => {
                          setFilterRows(filterRows.filter((value) => value !== key))
                          setDraft((previous) => {
                            const next = new URLSearchParams(previous)
                            next.delete(key)
                            return next
                          })
                        }}
                      >
                        <X size={14} />
                      </button>
                    </div>
                  ))}
                  {filterRows.length < filters.length && (
                    <label className="task-view-add-filter">
                      <Plus size={14} />
                      <Select
                        aria-label="添加筛选"
                        value=""
                        onChange={(event) => setFilterRows([...filterRows, event.target.value])}
                      >
                        <option value="" disabled>
                          添加筛选
                        </option>
                        {filters
                          .filter(([key]) => !filterRows.includes(key))
                          .map(([key, label]) => (
                            <option key={key} value={key}>
                              {label}
                            </option>
                          ))}
                      </Select>
                    </label>
                  )}
                </div>
              </div>
              <details className="task-view-defaults">
                <summary>
                  <strong>默认显示</strong>
                  <span>
                    {layouts.find(([value]) => value === layout)?.[1]} ·{' '}
                    {groups.find(([value]) => value === group)?.[1]} ·{' '}
                    {sorts.find(([value]) => value === (draft.get('sort') || 'auto'))?.[1]}
                  </span>
                </summary>
                <div className="task-view-default-fields">
                  <label className="task-view-field">
                    <span>布局</span>
                    <Select
                      value={layout}
                      onChange={(event) => patchDraft('layout', event.target.value)}
                    >
                      {layouts.map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </Select>
                  </label>
                  <label className="task-view-field">
                    <span>分组</span>
                    <Select
                      disabled={layout === 'kanban' || layout === 'swimlane'}
                      value={group}
                      onChange={(event) => patchDraft(groupKey, event.target.value)}
                    >
                      {groups.map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </Select>
                  </label>
                  <label className="task-view-field">
                    <span>排序</span>
                    <Select
                      value={draft.get('sort') || 'auto'}
                      onChange={(event) => patchDraft('sort', event.target.value)}
                    >
                      {sorts.map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </Select>
                  </label>
                  <label className="task-view-field">
                    <span>顺序</span>
                    <Select
                      value={draft.get('direction') || 'asc'}
                      onChange={(event) => patchDraft('direction', event.target.value)}
                    >
                      <option value="asc">升序</option>
                      <option value="desc">降序</option>
                    </Select>
                  </label>
                  <div className="task-view-field">
                    <span>{layout === 'table' ? '显示列' : '卡片属性'}</span>
                    <div className="task-view-property-chips">
                      {fields.map(([value, label]) => (
                        <button
                          key={value}
                          type="button"
                          aria-pressed={visibleFields(draft).includes(value)}
                          onClick={() =>
                            patchDraft(
                              'fields',
                              (visibleFields(draft).includes(value)
                                ? visibleFields(draft).filter((field) => field !== value)
                                : [...visibleFields(draft), value]
                              ).join(','),
                            )
                          }
                        >
                          {label}
                        </button>
                      ))}
                    </div>
                  </div>
                  <p className="task-view-default-note">
                    打开此视图时使用以上显示方式，之后可以调整。
                  </p>
                </div>
              </details>
            </div>
            <footer>
              <button type="button" onClick={() => setMode(null)}>
                取消
              </button>
              <button className="view-primary" type="submit" disabled={!name.trim()}>
                创建视图
              </button>
            </footer>
          </form>
        )}
        {mode === 'manage' && (
          <div className="task-view-manager">
            {entries.map((entry, index) => (
              <div
                className={`task-view-manager-row ${dragged === entry.id ? 'is-dragging' : ''}`}
                key={entry.id}
                onDragOver={(event) => event.preventDefault()}
                onDrop={(event) => {
                  event.preventDefault()
                  if (dragged) reorder(dragged, entry.id)
                  setDragged('')
                }}
              >
                <button
                  className="view-drag-handle"
                  draggable
                  aria-label={`拖动排序 ${entry.name}，或使用上下方向键`}
                  onDragStart={(event) => {
                    setDragged(entry.id)
                    event.dataTransfer.effectAllowed = 'move'
                    event.dataTransfer.setData('text/plain', entry.id)
                  }}
                  onDragEnd={() => setDragged('')}
                  onKeyDown={(event) => {
                    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
                      event.preventDefault()
                      const target = entries[index + (event.key === 'ArrowUp' ? -1 : 1)]
                      if (target) reorder(entry.id, target.id)
                    }
                  }}
                >
                  <GripVertical size={18} />
                </button>
                {entry.focus ? (
                  <span className="task-view-manager-name">
                    {entry.name}
                    <small>内置</small>
                  </span>
                ) : (
                  <input
                    className="task-view-manager-name"
                    key={`${entry.id}:${entry.name}`}
                    aria-label={`重命名 ${entry.name}`}
                    defaultValue={entry.name}
                    maxLength={60}
                    onBlur={(event) => {
                      const value = event.target.value.trim()
                      if (value && value !== entry.name) {
                        if (
                          !persist(
                            views.map((view) =>
                              view.id === entry.id ? { ...view, name: value } : view,
                            ),
                          )
                        )
                          event.target.value = entry.name
                      } else event.target.value = entry.name
                    }}
                  />
                )}
                <div className="task-view-row-actions">
                  <button
                    className="view-icon-button"
                    aria-label={`上移 ${entry.name}`}
                    disabled={index === 0}
                    onClick={() => reorder(entry.id, entries[index - 1].id)}
                  >
                    <ArrowUp size={14} />
                  </button>
                  <button
                    className="view-icon-button"
                    aria-label={`下移 ${entry.name}`}
                    disabled={index === entries.length - 1}
                    onClick={() => reorder(entry.id, entries[index + 1].id)}
                  >
                    <ArrowDown size={14} />
                  </button>
                  {!entry.focus && (
                    <button
                      className="view-icon-button"
                      aria-label={`删除视图 ${entry.name}`}
                      onClick={() => {
                        if (
                          persist(views.filter((view) => view.id !== entry.id)) &&
                          active?.id === entry.id
                        )
                          select(builtinViews[3])
                      }}
                    >
                      <Trash2 size={14} />
                    </button>
                  )}
                </div>
                <button
                  className="task-view-switch"
                  role="switch"
                  aria-label={`显示视图 ${entry.name}`}
                  aria-checked={!preferences.hidden.includes(entry.id)}
                  disabled={entry.id === 'builtin:all'}
                  title={entry.id === 'builtin:all' ? '全部视图始终显示' : undefined}
                  onClick={() => toggleVisibility(entry)}
                >
                  <span />
                </button>
              </div>
            ))}
          </div>
        )}
      </dialog>
    </div>
  )
}
