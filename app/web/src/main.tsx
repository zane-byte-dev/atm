import React, { useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import {
  BrowserRouter,
  Link,
  useNavigate,
  useLocation,
  useParams,
  useSearchParams,
  Routes,
  Route,
  Navigate,
} from 'react-router'
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
  useQueries,
  useQueryClient,
} from '@tanstack/react-query'
import {
  Archive,
  ArrowLeft,
  ArrowUpRight,
  Check,
  CheckCheck,
  Circle,
  CircleCheck,
  CircleDot,
  Clock3,
  FileText,
  Folder,
  Inbox,
  LayoutGrid,
  List as ListIcon,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Search,
  X,
} from 'lucide-react'
import { ApiError, bootstrap, call, errorText, reconnect } from './api'
import { Editor, Markdown, Notice } from './editor'
import { AppShell, workspaces } from './workspace-shell'
import { useLiveUpdates } from './live-updates'
import { TaskPlanEditor, TaskProgressForm, TaskRelationships } from './task-operations'
import { TaskImageUpload } from './task-images'
import { TaskRefine } from './task-refine'
import {
  consumeNewTaskRequest,
  readTaskLayout,
  updateTaskLayout,
  type TaskLayout,
} from './task-navigation'
import type { Bootstrap, Todo, TodoDetail, TodoList, OperationWarning } from './types'
import './style.css'
import './themes.css'
import './theme'

const AgentsWorkspace = React.lazy(() =>
  import('./workspaces/activity').then((module) => ({ default: module.AgentsWorkspace })),
)
const UsageWorkspace = React.lazy(() =>
  import('./workspaces/activity').then((module) => ({ default: module.UsageWorkspace })),
)
const KnowledgeWorkspace = React.lazy(() =>
  import('./workspaces/knowledge').then((module) => ({ default: module.KnowledgeWorkspace })),
)
const CollectionWorkspace = React.lazy(() =>
  import('./workspaces/collection').then((module) => ({ default: module.CollectionWorkspace })),
)
const AIDayWorkspace = React.lazy(() =>
  import('./workspaces/aiday-settings').then((module) => ({ default: module.AIDayWorkspace })),
)
const SettingsWorkspace = React.lazy(() =>
  import('./workspaces/aiday-settings').then((module) => ({ default: module.SettingsWorkspace })),
)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      retry: (count, err) => count < 1 && (!(err instanceof ApiError) || err.status >= 500),
    },
    mutations: { retry: false },
  },
})

const statuses = { open: '待开始', in_progress: '工作中', review: '待验收', done: '已完成' }
const navItems = [
  { key: '', label: '全部任务', icon: LayoutGrid },
  { key: 'in_progress', label: '工作中', icon: CircleDot },
  { key: 'review', label: '待验收', icon: CheckCheck },
  { key: 'open', label: '待开始', icon: Inbox },
  { key: 'done', label: '已完成', icon: CircleCheck },
  { key: 'archived', label: '归档', icon: Archive },
]
const boardColumns = [
  { key: 'open', label: '待开始' },
  { key: 'in_progress', label: '工作中' },
  { key: 'review', label: '待验收' },
  { key: 'done', label: '已完成' },
] as const

function App() {
  const [boot, setBoot] = useState<Bootstrap>()
  useLiveUpdates(boot)
  const [failure, setFailure] = useState<unknown>()
  useEffect(() => {
    bootstrap().then(setBoot).catch(setFailure)
    const expire = () => setFailure(new ApiError(401, 'unauthenticated', '连接已过期'))
    window.addEventListener('atm:session-expired', expire)
    return () => window.removeEventListener('atm:session-expired', expire)
  }, [])
  if (failure)
    return (
      <div className="connection-screen">
        <img src="/mark.svg" alt="ATM" />
        <h1>连接你的工作台</h1>
        <p>{errorText(failure)}</p>
        <code>atm serve --open</code>
        <button
          className="button primary"
          onClick={() => {
            setFailure(undefined)
            setBoot(undefined)
            void queryClient
              .cancelQueries()
              .then(() => {
                queryClient.clear()
                return reconnect()
              })
              .then(setBoot)
              .catch(setFailure)
          }}
        >
          <RefreshCw size={15} />
          重新连接
        </button>
        <span>任务和数据仍保存在这台 Mac 上。</span>
      </div>
    )
  if (!boot)
    return (
      <div className="connection-screen">
        <img src="/mark.svg" alt="ATM" />
        <LoaderCircle className="spin" size={22} />
        <p>正在打开工作台…</p>
      </div>
    )
  return (
    <Routes>
      <Route path="/tasks/:id?" element={<Workspace boot={boot} />} />
      <Route
        path="/collection"
        element={
          <ModulePage boot={boot} path="/collection">
            <CollectionWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route
        path="/agents/:id?"
        element={
          <ModulePage boot={boot} path="/agents">
            <AgentsWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route
        path="/knowledge"
        element={
          <ModulePage boot={boot} path="/knowledge">
            <KnowledgeWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route
        path="/usage"
        element={
          <ModulePage boot={boot} path="/usage">
            <UsageWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route
        path="/ai-day"
        element={
          <ModulePage boot={boot} path="/ai-day">
            <AIDayWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route
        path="/settings"
        element={
          <ModulePage boot={boot} path="/settings">
            <SettingsWorkspace boot={boot} />
          </ModulePage>
        }
      />
      <Route path="*" element={<Navigate to="/tasks" replace />} />
    </Routes>
  )
}

function ModulePage({
  boot,
  path,
  children,
}: {
  boot: Bootstrap
  path: (typeof workspaces)[number]['path']
  children: React.ReactNode
}) {
  return (
    <AppShell boot={boot}>
      <div className="module-host" key={path}>
        <WorkspaceErrorBoundary key={path}>
          <React.Suspense fallback={<Loading text="正在打开工作区…" />}>{children}</React.Suspense>
        </WorkspaceErrorBoundary>
      </div>
    </AppShell>
  )
}

class WorkspaceErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error?: Error }
> {
  state: { error?: Error } = {}
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error)
      return (
        <div className="workspace-render-error" role="alert">
          <h2>这个工作区暂时无法显示</h2>
          <p>{errorText(this.state.error)}</p>
          <button className="button" onClick={() => window.location.reload()}>
            <RefreshCw size={15} />
            重新加载
          </button>
        </div>
      )
    return this.props.children
  }
}

function Workspace({ boot }: { boot: Bootstrap }) {
  const { id } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const layout = readTaskLayout(params)
  const status = params.get('status') ?? ''
  const project = params.get('project') ?? ''
  const search = params.get('q') ?? ''
  const setSearch = (value: string) =>
    setParams(
      (current) => {
        const next = new URLSearchParams(current)
        if (value) next.set('q', value)
        else next.delete('q')
        return next
      },
      { replace: true },
    )
  const [debounced, setDebounced] = useState(search)
  const [page, setPage] = useState(0)
  const [newTask, setNewTask] = useState(false)
  const [editing, setEditing] = useState(false)
  const searchInput = useRef<HTMLInputElement>(null)
  const canWrite = boot.capabilities?.todo_write === true

  useEffect(() => {
    const timeout = setTimeout(() => {
      setDebounced(search)
      setPage(0)
    }, 220)
    return () => clearTimeout(timeout)
  }, [search])
  useEffect(() => {
    setEditing(false)
  }, [id])
  useEffect(() => {
    const request = consumeNewTaskRequest(params, canWrite)
    if (!request) return
    if (request.open) setNewTask(true)
    setParams(request.params, { replace: true })
  }, [canWrite, params, setParams])
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        searchInput.current?.focus()
      }
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])
  const list = useQuery({
    queryKey: ['todos', layout, layout === 'kanban' ? '' : status, project, debounced, page],
    queryFn: ({ signal }) =>
      call<TodoList>(
        'todo.list',
        {
          status: layout === 'kanban' ? '' : status,
          project,
          query: debounced,
          limit: layout === 'kanban' ? 200 : 60,
          offset: layout === 'kanban' ? 0 : page * 60,
        },
        signal,
      ),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  })
  const needsLaneQueries =
    layout === 'kanban' && !!list.data && list.data.total > list.data.items.length
  const laneQueries = useQueries({
    queries: boardColumns.map(({ key }) => ({
      queryKey: ['todos', 'kanban-lane', key, project, debounced],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        call<TodoList>(
          'todo.list',
          { status: key, project, query: debounced, limit: 200, offset: 0 },
          signal,
        ),
      enabled: needsLaneQueries,
      refetchInterval: 5000,
      refetchIntervalInBackground: false,
    })),
  })
  const detail = useQuery({
    queryKey: ['todo', id],
    enabled: !!id,
    queryFn: ({ signal }) => call<TodoDetail>('todo.show', { todo_id: id }, signal),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  })
  const chooseFilter = (nextStatus: string, nextProject = '') => {
    setPage(0)
    setEditing(false)
    const next = new URLSearchParams()
    if (layout === 'kanban') next.set('layout', 'kanban')
    if (layout === 'list' && nextStatus) next.set('status', nextStatus)
    if (nextProject) next.set('project', nextProject)
    if (search) next.set('q', search)
    setParams(next)
  }
  const openTask = (todoID: string) =>
    navigate(`/tasks/${todoID}${params.size ? `?${params}` : ''}`)
  const chooseLayout = (nextLayout: TaskLayout) => {
    setPage(0)
    setEditing(false)
    setParams((current) => updateTaskLayout(current, nextLayout), { replace: true })
  }
  const activeNav = navItems.find((item) => item.key === status) ?? navItems[0]
  const counts = list.data?.counts ?? {}
  const projects = list.data?.projects ?? []
  const boardItems = needsLaneQueries
    ? boardColumns.flatMap(
        ({ key }, index) =>
          laneQueries[index].data?.items ??
          list.data?.items.filter((todo) => todo.status === key) ??
          [],
      )
    : (list.data?.items ?? [])
  const boardError = list.error ?? laneQueries.find((query) => query.error)?.error
  const boardSupplementing = needsLaneQueries && laneQueries.some((query) => query.isPending)
  const refreshTasks = () => {
    void list.refetch()
    if (needsLaneQueries) laneQueries.forEach((query) => void query.refetch())
  }
  const hasSelection = !!id

  return (
    <AppShell boot={boot} className={`${hasSelection ? 'has-selection' : ''} task-view-${layout}`}>
      <div className="content-columns">
        <section
          className={`task-column ${layout === 'kanban' ? 'board-mode' : ''}`}
          aria-label={layout === 'kanban' ? '任务工作板' : '任务列表'}
        >
          <div className="list-heading">
            <h1>
              工作板
              <span>{list.data?.total ?? '—'}</span>
            </h1>
            <div className="list-heading-actions">
              <TaskLayoutSwitch layout={layout} onChange={chooseLayout} />
              <button
                className="icon-button"
                title="刷新任务"
                aria-label="刷新任务"
                onClick={refreshTasks}
              >
                <RefreshCw
                  size={16}
                  className={
                    list.isFetching || laneQueries.some((query) => query.isFetching) ? 'spin' : ''
                  }
                />
              </button>
              {canWrite && (
                <button className="button primary new-task" onClick={() => setNewTask(true)}>
                  <Plus size={15} />
                  新建
                </button>
              )}
            </div>
          </div>
          <div className="search-box">
            <Search size={16} />
            <input
              ref={searchInput}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索任务…"
              aria-label="搜索任务"
            />
            {search ? (
              <button className="clear-search" aria-label="清除搜索" onClick={() => setSearch('')}>
                <X size={13} />
              </button>
            ) : (
              <kbd>⌘ K</kbd>
            )}
          </div>
          <div className="task-filter-row">
            {layout === 'list' && (
              <select
                className="task-status-select"
                aria-label="筛选任务状态"
                value={status}
                onChange={(event) => chooseFilter(event.target.value, project)}
              >
                {navItems.map(({ key, label }) => (
                  <option value={key} key={key}>
                    {label} · {counts[key || 'all'] ?? '—'}
                  </option>
                ))}
              </select>
            )}
            {projects.length > 0 && (
              <select
                className="project-filter"
                aria-label="筛选项目"
                value={project}
                onChange={(event) => chooseFilter(status, event.target.value)}
              >
                <option value="">全部项目</option>
                {projects.map((name) => (
                  <option value={name} key={name}>
                    {name}
                  </option>
                ))}
              </select>
            )}
          </div>
          {layout === 'list' ? (
            <>
              <div className="list-subheading">
                <span>{search ? '搜索结果' : project || activeNav.label}</span>
                <span className="list-total">
                  {list.data ? `${list.data.items.length} 项` : ''}
                </span>
              </div>
              <div className="task-list">
                {list.isPending && <Loading text="正在读取任务…" />}
                {list.isError && <Notice error={list.error} retry={() => void list.refetch()} />}
                {list.data?.items.map((todo) => (
                  <TaskListRow
                    key={todo.id}
                    todo={todo}
                    active={todo.id === id}
                    onOpen={openTask}
                  />
                ))}
                {list.data?.items.length === 0 && (
                  <TaskEmpty
                    search={search}
                    canWrite={canWrite}
                    onCreate={() => setNewTask(true)}
                  />
                )}
              </div>
            </>
          ) : (
            <TaskBoard
              items={boardItems}
              counts={counts}
              activeID={id}
              search={search}
              loading={list.isPending}
              supplementing={boardSupplementing}
              error={boardError}
              total={list.data?.total ?? 0}
              onRetry={refreshTasks}
              onOpen={openTask}
              canWrite={canWrite}
              onCreate={() => setNewTask(true)}
            />
          )}
          {layout === 'list' && (page > 0 || (list.data?.total ?? 0) > (page + 1) * 60) && (
            <div className="pagination">
              <button disabled={!page} onClick={() => setPage((p) => p - 1)}>
                上一页
              </button>
              <span>第 {page + 1} 页</span>
              <button
                disabled={(list.data?.total ?? 0) <= (page + 1) * 60}
                onClick={() => setPage((p) => p + 1)}
              >
                下一页
              </button>
            </div>
          )}
        </section>
        {(layout === 'list' || id) && (
          <section className="detail-column" aria-label="任务详情">
            {!id ? (
              <div className="welcome">
                <div className="welcome-mark">
                  <FileText size={32} strokeWidth={1.2} />
                </div>
                <h2>选一件事，专注推进</h2>
                <p>
                  从左侧选择任务，查看详情与进展。
                  <br />
                  你的工作，随时都能接着往下做。
                </p>
                <div className="welcome-hint">
                  <kbd>⌘ K</kbd>快速查找任务
                </div>
              </div>
            ) : detail.isPending ? (
              <Loading text="正在打开任务…" />
            ) : detail.error ? (
              <Notice error={detail.error} retry={() => void detail.refetch()} />
            ) : (
              detail.data && (
                <>
                  <div className="detail-toolbar">
                    <button
                      className="icon-button back-list"
                      aria-label="返回任务列表"
                      onClick={() => navigate(`/tasks?${params}`)}
                    >
                      <ArrowLeft size={16} />
                    </button>
                    <span className="detail-id">
                      <FileText size={14} />
                      {id}
                    </span>
                    <div className="detail-actions">
                      {canWrite && !editing && !detail.data.todo.archived && (
                        <button className="button subtle" onClick={() => setEditing(true)}>
                          <Pencil size={14} />
                          编辑
                        </button>
                      )}
                      <button
                        className="icon-button"
                        title="刷新详情"
                        aria-label="刷新详情"
                        onClick={() => void detail.refetch()}
                      >
                        <RefreshCw size={14} className={detail.isFetching ? 'spin' : ''} />
                      </button>
                    </div>
                  </div>
                  {editing ? (
                    <Editor
                      key={id}
                      initial={detail.data}
                      onClose={() => setEditing(false)}
                      onSaved={(todoID) => {
                        setEditing(false)
                        openTask(todoID)
                      }}
                    />
                  ) : (
                    <TaskDetail
                      key={id}
                      data={detail.data}
                      canWrite={canWrite}
                      canRefine={canWrite && boot.capabilities?.runtime_jobs === true}
                    />
                  )}
                </>
              )
            )}
          </section>
        )}
      </div>
      {newTask && (
        <NewTaskDialog
          onClose={() => setNewTask(false)}
          onSaved={(todoID, warnings) => {
            setNewTask(false)
            setPage(0)
            navigate(`/tasks/${todoID}${params.size ? `?${params}` : ''}`, {
              state: { taskWarnings: warnings },
            })
          }}
          defaultProject={project}
        />
      )}
    </AppShell>
  )
}

function TaskLayoutSwitch({
  layout,
  onChange,
}: {
  layout: TaskLayout
  onChange: (layout: TaskLayout) => void
}) {
  return (
    <div className="task-layout-switch" role="group" aria-label="任务视图">
      <button
        type="button"
        title="列表视图"
        aria-label="列表视图"
        aria-pressed={layout === 'list'}
        onClick={() => onChange('list')}
      >
        <ListIcon size={15} />
      </button>
      <button
        type="button"
        title="看板视图"
        aria-label="看板视图"
        aria-pressed={layout === 'kanban'}
        onClick={() => onChange('kanban')}
      >
        <LayoutGrid size={14} />
      </button>
    </div>
  )
}

function TaskListRow({
  todo,
  active,
  onOpen,
}: {
  todo: Todo
  active: boolean
  onOpen: (todoID: string) => void
}) {
  return (
    <button
      className={`task-row ${active ? 'active' : ''}`}
      onClick={() => onOpen(todo.id)}
      aria-current={active ? 'true' : undefined}
    >
      <StatusIcon todo={todo} />
      <div className="task-row-body">
        <div className="task-row-title">{todo.title}</div>
        <div className="task-row-meta">
          <span className="task-id">{todo.id}</span>
          {todo.project && (
            <>
              <span className="meta-dot">·</span>
              <span className="truncate">{todo.project}</span>
            </>
          )}
          <span className={`priority priority-${todo.priority.toLowerCase()}`}>
            {todo.priority}
          </span>
        </div>
        {todo.wake_condition && (
          <div className="row-waiting">
            <Clock3 size={11} />
            {todo.wake_condition}
          </div>
        )}
      </div>
    </button>
  )
}

function TaskEmpty({
  search,
  canWrite,
  onCreate,
}: {
  search: string
  canWrite: boolean
  onCreate: () => void
}) {
  return (
    <div className="empty-list">
      <Inbox size={27} strokeWidth={1.3} />
      <h3>{search ? '没有找到相关任务' : '这里还没有任务'}</h3>
      <p>{search ? '试试其他关键词，或者切换项目。' : '新建一个任务，开始整理下一步。'}</p>
      {canWrite && !search && (
        <button className="button" onClick={onCreate}>
          <Plus size={14} />
          新建任务
        </button>
      )}
    </div>
  )
}

function TaskBoard({
  items,
  counts,
  activeID,
  search,
  loading,
  supplementing,
  error,
  total,
  onRetry,
  onOpen,
  canWrite,
  onCreate,
}: {
  items: Todo[]
  counts: Record<string, number>
  activeID?: string
  search: string
  loading: boolean
  supplementing: boolean
  error: unknown
  total: number
  onRetry: () => void
  onOpen: (todoID: string) => void
  canWrite: boolean
  onCreate: () => void
}) {
  if (loading) return <Loading text="正在读取工作板…" />
  if (error && items.length === 0 && total === 0) return <Notice error={error} retry={onRetry} />
  if (total === 0) return <TaskEmpty search={search} canWrite={canWrite} onCreate={onCreate} />
  return (
    <>
      {error && <Notice error={error} retry={onRetry} />}
      {total > items.length && (
        <div className="task-board-limit" role="status">
          {supplementing
            ? `正在补齐各列，已显示 ${items.length} / ${total} 项…`
            : `每列最多显示 200 项，当前共显示 ${items.length} / ${total} 项；可用项目或搜索缩小范围。`}
        </div>
      )}
      <div className="task-board">
        {boardColumns.map(({ key, label }) => {
          const laneItems = items.filter((todo) => todo.status === key)
          return (
            <section className={`task-lane lane-${key}`} key={key} aria-label={label}>
              <header className="task-lane-heading">
                <span className={`task-lane-dot status-${key}`} />
                <h2>{label}</h2>
                <span>{counts[key] ?? laneItems.length}</span>
              </header>
              <div className="task-lane-list">
                {laneItems.map((todo) => (
                  <button
                    key={todo.id}
                    className={`task-card ${todo.id === activeID ? 'active' : ''}`}
                    onClick={() => onOpen(todo.id)}
                    aria-current={todo.id === activeID ? 'true' : undefined}
                  >
                    <div className="task-card-heading">
                      <StatusIcon todo={todo} />
                      <span className="task-card-title">{todo.title}</span>
                    </div>
                    <div className="task-row-meta">
                      <span className="task-id">{todo.id}</span>
                      {todo.project && <span className="truncate">{todo.project}</span>}
                      <span className={`priority priority-${todo.priority.toLowerCase()}`}>
                        {todo.priority}
                      </span>
                    </div>
                    {todo.wake_condition && (
                      <div className="row-waiting">
                        <Clock3 size={11} />
                        {todo.wake_condition}
                      </div>
                    )}
                  </button>
                ))}
                {laneItems.length === 0 && <p className="task-lane-empty">暂无任务</p>}
              </div>
            </section>
          )
        })}
      </div>
    </>
  )
}

function StatusIcon({ todo }: { todo: Todo }) {
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

function TaskDetail({
  data,
  canWrite,
  canRefine,
}: {
  data: TodoDetail
  canWrite: boolean
  canRefine: boolean
}) {
  const todo = data.todo
  const location = useLocation()
  const [warnings, setWarnings] = useState<OperationWarning[]>(
    () => location.state?.taskWarnings ?? [],
  )
  const archived = todo.archived === true
  const query = useQueryClient()
  const [tab, setTab] = useState('description')
  const [editingPlan, setEditingPlan] = useState(false)
  useEffect(() => setTab('description'), [todo.id])
  const document = useQuery({
    queryKey: ['doc', todo.id],
    enabled: tab === 'progress',
    queryFn: ({ signal }) =>
      call<{ exists: boolean; content?: string }>('todo.doc', { todo_id: todo.id }, signal),
    refetchInterval: 5000,
    refetchIntervalInBackground: false,
  })
  const action = useMutation({
    mutationFn: ({ method, input }: { method: string; input: object }) => call(method, input),
    onSuccess: async () => {
      await Promise.all([
        query.invalidateQueries({ queryKey: ['todos'] }),
        query.invalidateQueries({ queryKey: ['todo', todo.id] }),
        query.invalidateQueries({ queryKey: ['doc', todo.id] }),
      ])
    },
  })
  const perform = (method: string, extra: object = {}) =>
    action.mutate({ method, input: { todo_id: todo.id, ...extra } })
  return (
    <div className="detail-scroll">
      <article className="task-detail">
        <div className="detail-status-line">
          <span className={`status-badge status-${todo.status}`}>
            <StatusIcon todo={todo} />
            {statuses[todo.status] ?? todo.status}
          </span>
          {archived && (
            <span className="project-badge">
              <Archive size={13} />
              已归档
            </span>
          )}
          {todo.project && (
            <span className="project-badge">
              <Folder size={13} />
              {todo.project}
            </span>
          )}
        </div>
        <h2 className="task-title">{todo.title}</h2>
        {warnings.length > 0 && (
          <div className="notice" role="status">
            <div>
              <strong>任务已创建</strong>
              {warnings.map((warning, index) => (
                <p key={index}>{warning.message}</p>
              ))}
            </div>
            <button type="button" onClick={() => setWarnings([])}>
              知道了
            </button>
          </div>
        )}
        <div className="properties">
          <span className="property-label">优先级</span>
          <span className={`priority priority-${todo.priority.toLowerCase()}`}>
            {todo.priority}
          </span>
          <span className="property-label">创建日期</span>
          <span>{todo.created}</span>
          {todo.creator && (
            <>
              <span className="property-label">创建者</span>
              <span>{todo.creator}</span>
            </>
          )}
        </div>
        {canRefine && !archived && <TaskRefine data={data} />}
        {todo.wake_condition && (
          <div className="waiting-banner">
            <Clock3 size={16} />
            <div>
              <strong>等待条件</strong>
              <p>{todo.wake_condition}</p>
            </div>
          </div>
        )}
        {todo.depends_on && todo.depends_on.length > 0 && (
          <div className="dependencies">
            <span>依赖任务</span>
            {todo.depends_on.map((dep) => (
              <Link key={dep} to={`/tasks/${dep}${location.search}`}>
                {dep}
              </Link>
            ))}
          </div>
        )}
        <div className="detail-tabs">
          <button
            className={tab === 'description' ? 'selected' : ''}
            onClick={() => setTab('description')}
          >
            任务说明
          </button>
          <button
            className={tab === 'progress' ? 'selected' : ''}
            onClick={() => setTab('progress')}
          >
            进展记录
          </button>
          {(data.latest_plan || (canWrite && !archived)) && (
            <button className={tab === 'plan' ? 'selected' : ''} onClick={() => setTab('plan')}>
              执行计划<span>{data.latest_plan?.items.length ?? 0}</span>
            </button>
          )}
        </div>
        {canWrite && !archived && tab === 'progress' && (
          <TaskProgressForm key={todo.id} data={data} />
        )}
        {canWrite &&
          !archived &&
          tab === 'plan' &&
          (editingPlan ? (
            <TaskPlanEditor key={todo.id} data={data} onClose={() => setEditingPlan(false)} />
          ) : (
            <button className="button task-inline-action" onClick={() => setEditingPlan(true)}>
              <Pencil size={14} />
              {data.latest_plan ? '编辑计划' : '创建计划'}
            </button>
          ))}
        <div className="detail-body">
          {tab === 'description' ? (
            <>
              <Markdown text={todo.description || '还没有任务说明。'} />
              {todo.links && todo.links.length > 0 && (
                <div className="related-links">
                  <span className="eyebrow">相关链接</span>
                  {todo.links
                    .filter((link) => /^https?:\/\//i.test(link.url))
                    .map((link) => (
                      <a key={link.url} href={link.url} target="_blank" rel="noreferrer noopener">
                        <ArrowUpRight size={15} />
                        <span>{link.title || link.url}</span>
                      </a>
                    ))}
                </div>
              )}
              {todo.closed_reason && (
                <div className="completion-note">
                  <Check size={16} />
                  <div>
                    <strong>完成记录</strong>
                    <p>{todo.closed_reason}</p>
                  </div>
                </div>
              )}
            </>
          ) : tab === 'progress' ? (
            document.isPending ? (
              <Loading text="读取进展记录…" />
            ) : document.error ? (
              <Notice error={document.error} retry={() => void document.refetch()} />
            ) : (
              <Markdown text={document.data?.content || '还没有进展记录。'} />
            )
          ) : (
            <div className="plan">
              {data.latest_plan?.explanation && (
                <p className="plan-explanation">{data.latest_plan.explanation}</p>
              )}
              {data.latest_plan?.items.map((item, index) => (
                <div className={`plan-item ${item.status}`} key={index}>
                  {item.status === 'completed' ? (
                    <CircleCheck size={18} />
                  ) : item.status === 'in_progress' ? (
                    <CircleDot size={18} />
                  ) : (
                    <Circle size={18} />
                  )}
                  <span>{item.step}</span>
                </div>
              ))}
            </div>
          )}
        </div>
        {tab === 'description' && todo.images && todo.images.length > 0 && (
          <div className="attachments">
            <span className="eyebrow">附件</span>
            <div className="attachment-grid">
              {todo.images
                .filter((item) => item.url?.startsWith('/api/v1/attachments/'))
                .map((item) => (
                  <a key={item.url} href={item.url} target="_blank" rel="noreferrer noopener">
                    <img src={item.url} alt={item.name} loading="lazy" />
                    <span>{item.name}</span>
                  </a>
                ))}
            </div>
          </div>
        )}
        {tab === 'description' && canWrite && !archived && (
          <TaskImageUpload todo={todo} etag={data.etag} />
        )}
        {tab === 'description' && (
          <TaskRelationships data={data} canWrite={canWrite && !archived} />
        )}
        {action.error && <Notice error={action.error} />}
        {canWrite && (
          <div className="lifecycle-actions">
            {archived ? (
              <button
                className="button"
                disabled={action.isPending}
                onClick={() => perform('todo.restore')}
              >
                <Archive size={14} />
                恢复任务
              </button>
            ) : (
              <>
                {todo.status === 'open' && (
                  <button
                    className="button primary"
                    disabled={action.isPending}
                    onClick={() => perform('todo.start')}
                  >
                    <CircleDot size={14} />
                    开始任务
                  </button>
                )}
                {todo.status !== 'done' && (
                  <button
                    className="button"
                    disabled={action.isPending}
                    onClick={() => perform('todo.done')}
                  >
                    <Check size={15} />
                    {todo.status === 'review' ? '验收完成' : '标记完成'}
                  </button>
                )}
                {todo.status === 'done' && (
                  <button
                    className="button"
                    disabled={action.isPending}
                    onClick={() =>
                      perform('todo.start', { reopen_reason: '在 Web 工作区恢复工作' })
                    }
                  >
                    <RefreshCw size={14} />
                    重新开始
                  </button>
                )}
                <button
                  className="button subtle archive-action"
                  disabled={action.isPending}
                  onClick={() => perform('todo.archive')}
                >
                  <Archive size={14} />
                  归档
                </button>
              </>
            )}
            {action.isPending && <LoaderCircle className="spin" size={15} />}
          </div>
        )}
      </article>
    </div>
  )
}

function NewTaskDialog({
  onClose,
  onSaved,
  defaultProject,
}: {
  onClose: () => void
  onSaved: (id: string, warnings?: OperationWarning[]) => void
  defaultProject: string
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    const element = dialog.current!
    element.showModal()
    return () => element.close()
  }, [])
  return (
    <dialog
      ref={dialog}
      className="modal"
      aria-labelledby="new-title"
      onCancel={(event) => {
        event.preventDefault()
        if (!busy) onClose()
      }}
      onClick={(event) => {
        if (event.target !== event.currentTarget || busy) return
        const bounds = event.currentTarget.getBoundingClientRect()
        if (
          event.clientX < bounds.left ||
          event.clientX > bounds.right ||
          event.clientY < bounds.top ||
          event.clientY > bounds.bottom
        )
          onClose()
      }}
    >
      <div className="modal-heading">
        <div>
          <span className="eyebrow">开始一件新工作</span>
          <h2 id="new-title">新建任务</h2>
        </div>
        <button className="icon-button" aria-label="关闭新建任务" disabled={busy} onClick={onClose}>
          <X size={18} />
        </button>
      </div>
      <Editor
        onClose={onClose}
        onSaved={onSaved}
        onBusyChange={setBusy}
        defaultProject={defaultProject}
      />
    </dialog>
  )
}

function Loading({ text }: { text: string }) {
  return (
    <div className="loading">
      <LoaderCircle size={19} className="spin" />
      <span>{text}</span>
    </div>
  )
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
