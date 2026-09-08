import { useEffect, useRef, useState } from 'react'
import { useNavigate, useLocation } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Search, Plus, ArrowUpRight } from 'lucide-react'
import { call } from './api'
import type { TodoList } from './types'
import type { CollectionList } from './workspaces/collection-types'
import type { SessionSearch } from './workspaces/activity-types'
import type { KnowledgeDocumentRow, MemoryHit } from './workspaces/knowledge-types'

type Result = { title: string; path: string; meta?: string }
type Group = { name: string; items: Result[]; failed?: boolean }
const url = (path: string, params: Record<string, string>) =>
  `${path}?${new URLSearchParams(params)}`

type SearchContext = { workspace: string; params: URLSearchParams }
export function WorkspaceSearchButton() {
  return (
    <button
      className="button workspace-search-button"
      title="搜索当前工作区"
      onClick={() => window.dispatchEvent(new Event('atm:open-search'))}
    >
      <Search size={15} />
      <span>搜索</span>
    </button>
  )
}
async function searchAll(
  query: string,
  signal: AbortSignal,
  context?: SearchContext,
): Promise<Group[]> {
  const params = context?.params || new URLSearchParams()
  const wants = (workspace: string) => !context || context.workspace === workspace
  const requests: [string, Promise<Result[]>][] = []
  const add = (workspace: string, name: string, request: () => Promise<Result[]>) => {
    if (wants(workspace)) requests.push([name, request()])
  }
  add('tasks', '任务', () =>
    call<TodoList>(
      'todo.list',
      {
        query,
        status: params.get('status') === 'archived' ? 'archived' : 'all',
        project: params.get('project') || '',
        limit: 20,
      },
      signal,
    ).then((data) =>
      (data.items || []).map((item) => ({
        title: item.title,
        path: `/tasks/${encodeURIComponent(item.id)}`,
        meta: item.id,
      })),
    ),
  )
  add('collection', '收件箱', () =>
    call<CollectionList>(
      'collect.items',
      {
        query,
        state: context ? params.get('state') || 'active' : 'all',
        source_id: params.get('source') || '',
        unread_only: params.get('unread') === '1',
        limit: 20,
      },
      signal,
    ).then((data) =>
      (data.items || []).map((item) => ({
        title: item.title || '未命名条目',
        path: url('/collection', {
          item: item.id,
          state: item.archived_at ? 'archived' : 'active',
        }),
        meta: item.project,
      })),
    ),
  )
  add('agents', '会话', () =>
    call<SessionSearch>(
      'session.search',
      {
        keyword: query,
        days: Number(params.get('days')) || (context ? 7 : 365),
        agent: params.get('agent') || '',
        project: params.get('project') || '',
        limit: 20,
      },
      signal,
    ).then((data) => [
      ...new Map(
        (data.matches || []).map((item) => [
          item.id,
          {
            title: item.content.slice(0, 160) || `${item.agent} · ${item.project || '未指定项目'}`,
            meta: `${item.agent} · ${item.short_id || item.id}`,
            path: url('/agents', {
              session: item.id,
              q: query,
              days: String(Number(params.get('days')) || (context ? 7 : 365)),
              ...(params.get('project') ? { project: params.get('project')! } : {}),
              ...(params.get('agent') ? { agent: params.get('agent')! } : {}),
            }),
          },
        ]),
      ).values(),
    ]),
  )
  if (!context || params.get('tab') !== 'memory')
    add('knowledge', '知识', () =>
      call<{ documents: KnowledgeDocumentRow[] }>(
        'knowledge.query',
        {
          text: query,
          collection: params.get('collection') || '',
          status: params.get('status') || '',
          limit: 20,
        },
        signal,
      ).then((data) =>
        (data.documents || []).map((item) => ({
          title: item.title,
          path: url('/knowledge', { document: item.document_id, collection: item.collection }),
          meta: item.collection,
        })),
      ),
    )
  if (!context || params.get('tab') === 'memory')
    add('knowledge', '共享记忆', () =>
      call<{ hits: MemoryHit[] }>(
        'memory.recall',
        {
          query,
          scope: params.get('scope') || '',
          limit: 20,
        },
        signal,
      ).then((data) =>
        (data.hits || []).map((item) => ({
          title: item.content.split('\n')[0] || '共享记忆',
          path: url('/knowledge', { tab: 'memory', memory: item.id }),
          meta: item.scope,
        })),
      ),
    )
  const results = await Promise.allSettled(requests.map(([, request]) => request))
  if (signal.aborted) throw new DOMException('Aborted', 'AbortError')
  return results.map((result, index) =>
    result.status === 'fulfilled'
      ? { name: requests[index][0], items: result.value }
      : { name: requests[index][0], items: [], failed: true },
  )
}

export function GlobalSearch({ writable }: { writable: boolean }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState(0)
  const dialog = useRef<HTMLDialogElement>(null)
  const input = useRef<HTMLInputElement>(null)
  const navigate = useNavigate()
  const location = useLocation()
  const workspace = location.pathname.split('/')[1]
  const names: Record<string, string> = {
    tasks: '工作板',
    collection: '收件箱',
    agents: 'Agent 会话',
    knowledge: '知识',
  }
  const canScope = !!names[workspace]
  const [scope, setScope] = useState<'current' | 'all'>('current')
  const params = new URLSearchParams(location.search)
  const context = scope === 'current' && canScope ? { workspace, params } : undefined
  const contextLabel = [
    names[workspace],
    params.get('project'),
    params.get('collection'),
    params.get('scope'),
  ]
    .filter(Boolean)
    .join(' · ')
  useEffect(() => {
    const show = () => {
      setScope('current')
      setOpen(true)
    }
    window.addEventListener('atm:open-search', show)
    return () => window.removeEventListener('atm:open-search', show)
  }, [])
  useEffect(() => {
    setScope('current')
    setSelected(0)
  }, [location.pathname, location.search])
  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setOpen((value) => !value)
      }
    }
    document.addEventListener('keydown', handle)
    return () => document.removeEventListener('keydown', handle)
  }, [])
  useEffect(() => {
    if (open) {
      dialog.current?.showModal()
      input.current?.focus()
    } else dialog.current?.close()
  }, [open])
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(text.trim())
      setSelected(0)
    }, 250)
    return () => clearTimeout(timer)
  }, [text])
  const search = useQuery({
    queryKey: ['global-search', query, context ? workspace : 'all', context ? location.search : ''],
    queryFn: ({ signal }) => searchAll(query, signal, context),
    enabled: open && !!query,
    retry: false,
  })
  const pending = text.trim() !== query || search.isFetching
  const groups: Group[] = !text.trim()
    ? [
        {
          name: '快捷入口',
          items: [
            ...(writable ? [{ title: '新建任务', path: '/tasks?new=1' }] : []),
            { title: '工作板', path: '/tasks' },
            { title: '收件箱', path: '/collection' },
            { title: '知识', path: '/knowledge' },
          ],
        },
      ]
    : pending
      ? []
      : search.data || []
  const rows = groups.flatMap((group) => group.items)
  const active = Math.min(selected, Math.max(0, rows.length - 1))
  const go = (result: Result) => {
    setOpen(false)
    navigate(result.path)
  }
  useEffect(() => {
    dialog.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
  }, [active])
  let index = 0
  return (
    <>
      <button
        className="nav-item sidebar-search"
        onClick={() => setOpen(true)}
        title="搜索 (⌘K / Ctrl+K)"
      >
        <Search size={18} />
        <span>搜索…</span>
        <kbd>{navigator.platform.includes('Mac') ? '⌘ K' : 'Ctrl K'}</kbd>
      </button>
      <dialog
        ref={dialog}
        className="global-search-dialog"
        aria-label="统一搜索"
        onCancel={() => setOpen(false)}
        onClose={() => setOpen(false)}
        onClick={(event) => {
          if (event.target === dialog.current) setOpen(false)
        }}
      >
        <div className="global-search-panel">
          <div className="global-search-input">
            <Search size={22} />
            <input
              ref={input}
              value={text}
              maxLength={200}
              placeholder="输入关键词搜索…"
              aria-label={context ? '搜索当前工作区' : '搜索全部内容'}
              role="combobox"
              aria-expanded="true"
              aria-controls="global-search-results"
              aria-activedescendant={rows.length ? `global-result-${active}` : undefined}
              onChange={(event) => setText(event.target.value)}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  event.preventDefault()
                  setSelected(
                    (active + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) %
                      (rows.length || 1),
                  )
                }
                if (event.key === 'Enter' && rows[active]) {
                  event.preventDefault()
                  go(rows[active])
                }
              }}
            />
            <button
              className="global-search-close"
              onClick={() => setOpen(false)}
              aria-label="关闭搜索"
            >
              Esc
            </button>
          </div>
          <div className="search-scope" role="group" aria-label="搜索范围">
            {canScope && (
              <button
                aria-pressed={scope === 'current'}
                onClick={() => {
                  setScope('current')
                  setSelected(0)
                }}
                title={contextLabel}
              >
                当前：{contextLabel}
              </button>
            )}
            <button
              aria-pressed={!context}
              onClick={() => {
                setScope('all')
                setSelected(0)
              }}
            >
              全部内容
            </button>
          </div>
          <div
            id="global-search-results"
            className="global-search-results"
            role="listbox"
            aria-label="搜索结果"
            aria-busy={pending}
          >
            {groups.map((group) => (
              <div key={group.name} role="group" aria-label={group.name}>
                {(group.items.length > 0 || group.failed) && (
                  <div className="global-search-group">
                    {group.name}
                    {group.failed && (
                      <span>
                        {' '}
                        · 暂时无法搜索 <button onClick={() => void search.refetch()}>重试</button>
                      </span>
                    )}
                  </div>
                )}
                {group.items.map((item) => {
                  const row = index++
                  return (
                    <button
                      key={item.path}
                      id={`global-result-${row}`}
                      role="option"
                      aria-selected={active === row}
                      className="global-search-result"
                      onMouseEnter={() => setSelected(row)}
                      onClick={() => go(item)}
                    >
                      {item.path === '/tasks?new=1' ? (
                        <Plus size={17} />
                      ) : (
                        <ArrowUpRight size={16} />
                      )}
                      <span>{item.title}</span>
                      {item.meta && <small>{item.meta}</small>}
                    </button>
                  )
                })}
              </div>
            ))}
            <div role="status" className="global-search-hint">
              {!text.trim()
                ? context
                  ? `在${contextLabel}中搜索；可切换到全部内容`
                  : '搜索任务、收件箱、会话和知识'
                : pending
                  ? '正在搜索…'
                  : !rows.length
                    ? '没有找到匹配结果'
                    : '↑ ↓ 选择 · Enter 打开'}
            </div>
          </div>
        </div>
      </dialog>
    </>
  )
}
