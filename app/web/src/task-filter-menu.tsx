import { useEffect, useRef, useState } from 'react'
import {
  Archive,
  CalendarDays,
  Check,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Circle,
  CircleDot,
  Clock3,
  Folder,
  Search,
  Signal,
  Tag,
  UserRound,
  X,
  type LucideIcon,
} from 'lucide-react'
import type { Todo } from './types'
import { matchingTasks, statuses } from './task-view-model'
import type { TaskFacet } from './task-query'
import './task-filter-menu.css'
const categories: { key: string; label: string; icon: LucideIcon }[] = [
  { key: 'status', label: '状态', icon: CircleDot },
  { key: 'priority', label: '优先级', icon: Signal },
  { key: 'date', label: '创建日期', icon: CalendarDays },
  { key: 'creator', label: '创建者', icon: UserRound },
  { key: 'project', label: '项目', icon: Folder },
  { key: 'tag', label: '标签', icon: Tag },
]
const statusIcons: Record<string, LucideIcon> = {
  open: Circle,
  in_progress: Clock3,
  review: CircleDot,
  done: CheckCircle2,
  archived: Archive,
}
export function TaskFilterMenu({
  params,
  items,
  onChange,
  facets,
}: {
  params: URLSearchParams
  facets?: Record<string, TaskFacet[]>
  items: Todo[]
  onChange: (params: URLSearchParams) => void
}) {
  const [category, setCategory] = useState('')
  const [query, setQuery] = useState('')
  const root = useRef<HTMLDivElement>(null)
  const child = useRef<HTMLDivElement>(null)
  const [side, setSide] = useState('left')
  useEffect(() => {
    if (root.current) setSide(root.current.getBoundingClientRect().left >= 290 ? 'left' : 'right')
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
  const values = (key: string) => (params.get(key) || '').split(',').filter(Boolean)
  const selected = (key: string, value: string) =>
    ['status', 'priority'].includes(key) ? values(key).includes(value) : params.get(key) === value
  const choose = (key: string, value: string) => {
    if (key === 'status' && value === 'archived') {
      patch(key, params.get(key) === 'archived' ? '' : 'archived')
      return
    }
    if (['status', 'priority'].includes(key)) {
      const list = values(key).filter((item) => !['all', 'archived'].includes(item))
      patch(
        key,
        (list.includes(value) ? list.filter((item) => item !== value) : [...list, value]).join(','),
      )
    } else patch(key, params.get(key) === value ? '' : value)
  }
  const count = (key: string, value: string) => {
    if (facets) return facets[key]?.find((item) => item.value === value)?.count || 0
    const next = new URLSearchParams(params)
    next.set(key, value)
    if (key === 'status') next.set('focus', 'all')
    return matchingTasks(items, next).length
  }
  const options =
    category === 'status'
      ? [...statuses, ['archived', '已归档']]
      : category === 'priority'
        ? [
            ['P0', 'P0 · 紧急'],
            ['P1', 'P1 · 优先'],
            ['P2', 'P2 · 正常'],
          ]
        : [
            ...new Set(
              [
                ...(facets?.[category]
                  ? facets[category].map((item) => item.value)
                  : items.flatMap((item) =>
                      category === 'tag'
                        ? item.tags || []
                        : [item[category as 'project' | 'creator'] || ''],
                    )),
                params.get(category) || '',
              ].filter(Boolean),
            ),
          ]
            .sort()
            .map((value) => [value, value])
  const open = (key: string, focus = false) => {
    setCategory(key)
    setQuery('')
    if (focus)
      requestAnimationFrame(() =>
        child.current?.querySelector<HTMLElement>('button,input')?.focus(),
      )
  }
  const navigate = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (
      event.key === 'ArrowDown' ||
      event.key === 'ArrowUp' ||
      event.key === 'Home' ||
      event.key === 'End'
    ) {
      if (event.target instanceof HTMLInputElement) return
      const buttons = [
        ...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'),
      ]
      const index = buttons.indexOf(event.target as HTMLButtonElement)
      event.preventDefault()
      buttons[
        event.key === 'Home'
          ? 0
          : event.key === 'End'
            ? buttons.length - 1
            : (index + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length
      ]?.focus()
    }
  }
  const active = categories.find((item) => item.key === category)
  return (
    <div ref={root} className={`task-filter-menu submenu-${side} ${category ? 'has-submenu' : ''}`}>
      <div
        className="task-filter-categories"
        role="menu"
        aria-label="任务筛选分类"
        onKeyDown={navigate}
      >
        {categories.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            role="menuitem"
            data-category={key}
            aria-haspopup="menu"
            aria-expanded={category === key}
            onPointerEnter={(event) => {
              if (event.pointerType === 'mouse') open(key)
            }}
            onClick={() => open(key, true)}
            onKeyDown={(event) => {
              if (event.key === 'ArrowRight') {
                event.preventDefault()
                open(key, true)
              }
            }}
          >
            <Icon size={16} />
            <span>{label}</span>
            {(key === 'date' ? params.get('from') || params.get('to') : params.get(key)) && (
              <span className="task-filter-active-dot" />
            )}
            <ChevronRight size={15} />
          </button>
        ))}
        <button
          role="menuitem"
          className="task-filter-clear"
          onClick={() => {
            const next = new URLSearchParams(params)
            ;['status', 'priority', 'project', 'creator', 'tag', 'from', 'to', 'q'].forEach((key) =>
              next.delete(key),
            )
            onChange(next)
          }}
        >
          <X size={15} />
          清除筛选
        </button>
      </div>
      {active && (
        <div
          ref={child}
          className="task-filter-submenu"
          onKeyDown={(event) => {
            if (event.key === 'ArrowLeft' || event.key === 'Escape') {
              if (event.target instanceof HTMLInputElement && event.key === 'ArrowLeft') return
              event.preventDefault()
              event.stopPropagation()
              setCategory('')
              root.current
                ?.querySelector<HTMLButtonElement>(`[data-category="${category}"]`)
                ?.focus()
            }
          }}
        >
          <header>
            <button
              type="button"
              className="task-filter-back"
              aria-label="返回筛选分类"
              onClick={() => setCategory('')}
            >
              <ChevronLeft size={15} />
            </button>
            <active.icon size={15} />
            <strong>{active.label}</strong>
            {category !== 'date' && (
              <button className="task-filter-reset" onClick={() => patch(category, '')}>
                清除
              </button>
            )}
          </header>
          {category === 'date' ? (
            <div className="task-filter-dates">
              <label>
                开始日期
                <input
                  type="date"
                  value={params.get('from') || ''}
                  max={params.get('to') || undefined}
                  onChange={(event) => patch('from', event.target.value)}
                />
              </label>
              <label>
                结束日期
                <input
                  type="date"
                  value={params.get('to') || ''}
                  min={params.get('from') || undefined}
                  onChange={(event) => patch('to', event.target.value)}
                />
              </label>
              <button
                className="text-button"
                onClick={() => {
                  const next = new URLSearchParams(params)
                  next.delete('from')
                  next.delete('to')
                  onChange(next)
                }}
              >
                清除日期
              </button>
            </div>
          ) : (
            <>
              {options.length > 7 && (
                <label className="task-filter-search">
                  <Search size={14} />
                  <input
                    aria-label={`搜索${active.label}筛选项`}
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    placeholder={`搜索${active.label}…`}
                  />
                </label>
              )}
              <div
                className="task-filter-options"
                role="menu"
                aria-label={`${active.label}筛选项`}
                onKeyDown={navigate}
              >
                {options
                  .filter(([, label]) =>
                    label.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
                  )
                  .map(([value, label]) => {
                    const Icon = category === 'status' ? statusIcons[value] : active.icon
                    return (
                      <button
                        key={value}
                        role="menuitemcheckbox"
                        aria-checked={selected(category, value)}
                        onClick={() => choose(category, value)}
                      >
                        <span className="task-filter-check">
                          {selected(category, value) && <Check size={13} />}
                        </span>
                        <Icon
                          size={16}
                          className={
                            category === 'status' ? `task-filter-state-${value}` : undefined
                          }
                        />
                        <span>{label}</span>
                        <small>{count(category, value)} 个任务</small>
                      </button>
                    )
                  })}
                {!options.filter(([, label]) =>
                  label.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
                ).length && <p>没有匹配选项</p>}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  )
}
