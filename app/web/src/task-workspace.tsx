import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useLocation, useParams, useSearchParams } from 'react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Maximize2,
  Minimize2,
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
  LoaderCircle,
  Pencil,
  RefreshCw,
  X,
} from 'lucide-react'
import { call, errorText, callScopedTodoMutation } from './api'
import type { TodoScopedMutationRequest } from './api-contracts'
import { Editor } from './editor'
import { Markdown, Notice } from './markdown'
import { AppShell } from './workspace-shell'
import { TaskPlanEditor, TaskProgressForm, TaskRelationships } from './task-operations'
import { TaskWorkbench } from './task-workbench'
import { useLiveConnection } from './live-connection'
import { taskWorkbenchInput } from './task-query'
import { taskProgress, latestTaskProgress } from './task-progress'
import { consumeNewTaskRequest, readTaskLayout } from './task-navigation'
import type { Bootstrap, Todo, TodoDetail, OperationWarning } from './types'
import { TaskInlineProperty } from './task-inline-properties'
import { taskActivity, taskActivityLabel } from './task-attention'
import type { SessionStatus } from './workspaces/activity-types'

const statuses = { open: '待开始', in_progress: '进行中', review: '待验收', done: '已完成' }
const agentLabels: Record<string, string> = {
  codex: 'Codex',
  claude: 'Claude',
  cursor: 'Cursor',
  grokbuild: 'Grok Build',
  antigravity: 'Antigravity',
}
const agentLabel = (value?: string) => (value ? agentLabels[value] || value : 'Agent')
const epochDateTime = (value?: number) =>
  value
    ? new Date(value * 1000).toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '—'
export function TaskWorkspace({ boot }: { boot: Bootstrap }) {
  const live = useLiveConnection()
  const { id } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const layout = readTaskLayout(params)
  const project = params.get('project') ?? ''
  const [newTask, setNewTask] = useState(false)
  const [editing, setEditing] = useState(false)
  const [expandedDetail, setExpandedDetail] = useState(false)
  const canWrite = boot.capabilities?.todo_write === true
  const input = taskWorkbenchInput(params)
  useEffect(() => {
    setEditing(false)
  }, [id])
  useEffect(() => {
    const request = consumeNewTaskRequest(params, canWrite)
    if (!request) return
    if (request.open) setNewTask(true)
    setParams(request.params, { replace: true })
  }, [canWrite, params, setParams])
  const list = useQuery({
    queryKey: ['todos', 'workbench', input],
    queryFn: ({ signal }) => call('todo.list', { ...input, metadata_only: true }, signal),
    refetchInterval: live ? 60000 : 15000,
    refetchIntervalInBackground: false,
  })
  const detail = useQuery({
    queryKey: ['todo', id],
    enabled: !!id,
    queryFn: ({ signal }) => call('todo.show', { todo_id: id! }, signal),
    refetchInterval: live ? 60000 : 5000,
    refetchIntervalInBackground: false,
  })
  const openTask = (todoID: string) =>
    navigate(`/tasks/${todoID}${params.size ? `?${params}` : ''}`)
  const modalRef = useRef<HTMLElement>(null)
  useEffect(() => {
    if (!id) return
    modalRef.current?.focus()
    const trap = (event: KeyboardEvent) => {
      if (event.key !== 'Tab' || document.querySelector('dialog[open]')) return
      const modal = modalRef.current
      const controls = Array.from(
        modal?.querySelectorAll<HTMLElement>(
          'button:not(:disabled), a[href], input:not(:disabled), textarea:not(:disabled), select:not(:disabled), summary, [tabindex="0"]',
        ) || [],
      ).filter((el) => el.getClientRects().length)
      if (!controls.length) {
        event.preventDefault()
        return
      }
      const first = controls[0],
        last = controls[controls.length - 1]
      if (
        event.shiftKey &&
        (document.activeElement === first || document.activeElement === modal)
      ) {
        event.preventDefault()
        last.focus()
      } else if (
        !event.shiftKey &&
        (document.activeElement === last || !modal?.contains(document.activeElement))
      ) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', trap)
    return () => document.removeEventListener('keydown', trap)
  }, [id])
  const hasSelection = !!id
  const taskListHref = `/tasks${params.size ? `?${params}` : ''}`
  const closeTask = () => {
    const focusedTaskID = id
    navigate(taskListHref)
    if (focusedTaskID) {
      requestAnimationFrame(() => {
        document
          .querySelector<HTMLButtonElement>(`[data-task-id="${CSS.escape(focusedTaskID)}"]`)
          ?.focus()
      })
    }
  }

  useEffect(() => {
    if (!id || editing) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (
        event.key !== 'Escape' ||
        event.defaultPrevented ||
        document.querySelector('dialog[open]')
      )
        return
      event.preventDefault()
      closeTask()
    }
    document.addEventListener('keydown', closeOnEscape)
    return () => document.removeEventListener('keydown', closeOnEscape)
  }, [editing, id, layout, taskListHref])

  return (
    <AppShell
      boot={boot}
      className={`${hasSelection ? 'has-selection' : ''} task-view-kanban task-mode-${layout}`}
    >
      <div className="content-columns">
        <TaskWorkbench
          items={[]}
          remote={
            list.data
              ? { input, metadata: list.data, refetchInterval: live ? 60000 : 15000 }
              : undefined
          }
          params={params}
          onChange={(next) => setParams(next)}
          onOpen={openTask}
          onCreate={() => setNewTask(true)}
          writable={canWrite}
          loading={list.isPending}
          error={list.error ? errorText(list.error) : undefined}
          onRetry={() => void list.refetch()}
        />
        {id && (
          <button
            type="button"
            className="kanban-detail-scrim"
            tabIndex={-1}
            aria-label="关闭任务详情"
            onClick={closeTask}
          />
        )}
        {id && (
          <section
            ref={modalRef}
            tabIndex={-1}
            className={`detail-column ${expandedDetail ? 'detail-expanded' : ''}`}
            aria-label="任务详情"
            role="dialog"
            aria-modal="true"
            aria-labelledby={id && detail.data ? `task-title-${id}` : undefined}
          >
            {detail.isPending ? (
              <Loading text="正在打开任务…" />
            ) : detail.error ? (
              <Notice error={detail.error} retry={() => void detail.refetch()} />
            ) : (
              detail.data && (
                <>
                  <div className="detail-toolbar">
                    <button
                      className={`icon-button back-list ${layout === 'kanban' ? 'kanban-back-button' : ''}`}
                      title={layout === 'kanban' ? '返回看板' : '返回任务列表'}
                      aria-label={layout === 'kanban' ? '返回看板' : '返回任务列表'}
                      onClick={closeTask}
                    >
                      <ArrowLeft size={16} />
                      {layout === 'kanban' && <span>返回看板</span>}
                    </button>
                    <span className="detail-id">
                      <FileText size={14} />
                      {id}
                    </span>
                    {id && (
                      <div className="detail-actions">
                        <button
                          className="icon-button"
                          aria-label={expandedDetail ? '收起阅读' : '展开阅读'}
                          title={expandedDetail ? '收起阅读' : '展开阅读'}
                          onClick={() => setExpandedDetail(!expandedDetail)}
                        >
                          {expandedDetail ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
                        </button>
                        <button
                          className="icon-button kanban-detail-close"
                          title="关闭任务详情"
                          aria-label="关闭任务详情"
                          onClick={closeTask}
                        >
                          <X size={16} />
                        </button>
                      </div>
                    )}
                  </div>
                  {editing ? (
                    <Editor
                      key={id}
                      initial={detail.data}
                      canRefine={canWrite && boot.capabilities?.runtime_jobs === true}
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
                      onEdit={() => setEditing(true)}
                      onClose={closeTask}
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

type TaskLifecycleAction = 'start' | 'done' | 'reopen' | 'archive' | 'restore'

function TaskDetail({
  data,
  canWrite,
  canRefine,
  onEdit,
  onClose,
}: {
  data: TodoDetail
  canWrite: boolean
  canRefine: boolean
  onEdit: () => void
  onClose: () => void
}) {
  const todo = data.todo
  const live = useLiveConnection()
  const location = useLocation()
  const [warnings, setWarnings] = useState<OperationWarning[]>(
    () => location.state?.taskWarnings ?? [],
  )
  const archived = todo.archived === true
  const query = useQueryClient()
  const [tab, setTab] = useState('description')
  const [editingPlan, setEditingPlan] = useState(false)
  const [editingDescription, setEditingDescription] = useState(false)
  const [lifecycleAction, setLifecycleAction] = useState<TaskLifecycleAction>()
  useEffect(() => {
    setTab('description')
    setEditingDescription(false)
    setLifecycleAction(undefined)
  }, [todo.id])
  const document = useQuery({
    queryKey: ['doc', todo.id],
    enabled: true,
    queryFn: ({ signal }) => call('todo.doc', { todo_id: todo.id }, signal),
    refetchInterval: live ? 60000 : 5000,
    refetchIntervalInBackground: false,
  })
  const action = useMutation({
    mutationFn: (request: TodoScopedMutationRequest) => callScopedTodoMutation(todo.id, request),
    onSuccess: async () => {
      setLifecycleAction(undefined)
      onClose()
      await Promise.all([
        query.invalidateQueries({ queryKey: ['todos'] }),
        query.invalidateQueries({ queryKey: ['todo', todo.id] }),
        query.invalidateQueries({ queryKey: ['doc', todo.id] }),
      ])
    },
  })
  const latestProgress = latestTaskProgress(document.data?.content || '')
  const nextStep =
    data.latest_plan?.items.find((item) => item.status === 'in_progress') ||
    data.latest_plan?.items.find((item) => item.status === 'pending')
  const perform = (request: TodoScopedMutationRequest) => action.mutate(request)
  return (
    <>
      <div className="detail-scroll">
        <article className="task-detail">
          <header className="task-detail-header">
            <h2 className="task-title" id={`task-title-${todo.id}`}>
              {todo.title}
            </h2>
          </header>
          <div className="task-detail-content">
            <div className="task-body-toolbar">
              <div className="detail-status-line">
                <span className="task-context">
                  {todo.project && (
                    <>
                      <Folder size={13} />
                      <span>{todo.project}</span>
                      <span aria-hidden="true">›</span>
                    </>
                  )}
                  <span className="task-context-id">{todo.id}</span>
                </span>
                {archived && (
                  <span className="project-badge">
                    <Archive size={13} />
                    已归档
                  </span>
                )}
              </div>
              {canWrite && (
                <div className="task-primary-actions" aria-label="任务主要操作">
                  {!archived && (
                    <button className="button" onClick={onEdit}>
                      <Pencil size={14} />
                      编辑
                    </button>
                  )}
                  {archived ? (
                    <button
                      className="button"
                      disabled={action.isPending}
                      onClick={() => setLifecycleAction('restore')}
                    >
                      <Archive size={14} />
                      恢复任务
                    </button>
                  ) : (
                    <>
                      {todo.status === 'open' && (
                        <button
                          className="button"
                          disabled={action.isPending}
                          onClick={() => setLifecycleAction('start')}
                        >
                          <CircleDot size={14} />
                          开始任务
                        </button>
                      )}
                      {todo.status !== 'done' && (
                        <button
                          className="button"
                          disabled={action.isPending}
                          onClick={() => setLifecycleAction('done')}
                        >
                          <Check size={15} />
                          {todo.status === 'review' ? '验收完成' : '标记完成'}
                        </button>
                      )}
                      {todo.status === 'done' && (
                        <button
                          className="button"
                          disabled={action.isPending}
                          onClick={() => setLifecycleAction('reopen')}
                        >
                          <RefreshCw size={14} />
                          重新开始
                        </button>
                      )}
                      <button
                        type="button"
                        className="button"
                        disabled={action.isPending}
                        onClick={() => setLifecycleAction('archive')}
                      >
                        <Archive size={15} />
                        归档
                      </button>
                    </>
                  )}
                  {action.isPending && <LoaderCircle className="spin" size={15} />}
                </div>
              )}
            </div>
            {action.error && <Notice error={action.error} />}
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
            {(latestProgress || (todo.status !== 'done' && nextStep?.step) || document.error) && (
              <section className="task-current-summary" aria-label="任务当前进展">
                {document.error && (
                  <Notice error={document.error} retry={() => void document.refetch()} />
                )}
                {latestProgress && (
                  <>
                    <div>
                      <strong>最新进展</strong>
                      <button className="button" onClick={() => setTab('progress')}>
                        查看记录
                      </button>
                    </div>
                    <p className="latest-progress-text">{latestProgress}</p>
                  </>
                )}
                {todo.status !== 'done' && nextStep?.step && (
                  <div className="task-next-step">
                    <strong>下一步</strong>
                    <span>{nextStep.step}</span>
                  </div>
                )}
              </section>
            )}
            <div className="detail-tabs">
              <button
                className={tab === 'description' ? 'selected' : ''}
                onClick={() => setTab('description')}
              >
                任务说明
              </button>
              {(latestProgress || (canWrite && !archived) || tab === 'progress') && (
                <button
                  className={tab === 'progress' ? 'selected' : ''}
                  onClick={() => setTab('progress')}
                >
                  {latestProgress ? '进展记录' : '记录进展'}
                </button>
              )}
              {(!!data.latest_plan?.items.length || tab === 'plan') && (
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
            <div className="task-reading-surface">
              <div className="detail-body">
                {tab === 'description' ? (
                  <>
                    {editingDescription ? (
                      <Editor
                        key={todo.id}
                        initial={data}
                        canRefine={canRefine}
                        descriptionOnly
                        onClose={() => setEditingDescription(false)}
                        onSaved={() => setEditingDescription(false)}
                      />
                    ) : (
                      <div className={canWrite && !archived ? 'task-editable-description' : ''}>
                        {canWrite && !archived && (
                          <button
                            className="button task-edit-description"
                            onClick={() => setEditingDescription(true)}
                          >
                            <Pencil size={14} />
                            编辑说明
                          </button>
                        )}
                        <div
                          onClick={(event) => {
                            if (
                              canWrite &&
                              !archived &&
                              !(event.target as Element).closest('a, button, input') &&
                              !window.getSelection()?.toString()
                            )
                              setEditingDescription(true)
                          }}
                        >
                          {todo.description ? (
                            <Markdown text={todo.description} />
                          ) : canWrite && !archived ? (
                            <button className="button" onClick={() => setEditingDescription(true)}>
                              填写任务说明
                            </button>
                          ) : null}
                        </div>
                      </div>
                    )}
                    {todo.links && todo.links.length > 0 && (
                      <div className="related-links">
                        <span className="eyebrow">相关链接</span>
                        {todo.links
                          .filter((link) => /^https?:\/\//i.test(link.url))
                          .map((link) => (
                            <a
                              key={link.url}
                              href={link.url}
                              target="_blank"
                              rel="noreferrer noopener"
                            >
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
                    <>
                      <Markdown
                        text={taskProgress(document.data?.content ?? '') || '还没有进展记录。'}
                      />
                      {document.data?.content && (
                        <details className="task-full-document">
                          <summary>查看完整任务文档</summary>
                          <Markdown text={document.data.content} />
                        </details>
                      )}
                    </>
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
              {tab === 'description' &&
                todo.images?.some(
                  (item) =>
                    item.url?.startsWith('/api/v1/attachments/') &&
                    !todo.description?.includes(item.url),
                ) && (
                  <div className="attachments">
                    <span className="eyebrow">附件</span>
                    <div className="attachment-grid">
                      {todo.images
                        .filter(
                          (item) =>
                            item.url?.startsWith('/api/v1/attachments/') &&
                            !todo.description?.includes(item.url),
                        )
                        .map((item) => (
                          <a
                            key={item.url}
                            href={item.url}
                            target="_blank"
                            rel="noreferrer noopener"
                          >
                            <img src={item.url} alt={item.name} loading="lazy" />
                            <span>{item.name}</span>
                          </a>
                        ))}
                    </div>
                  </div>
                )}
              {tab === 'description' && canWrite && !archived && (
                <div className="task-description-tools" aria-label="任务说明工具" key={todo.id}>
                  <TaskRelationships data={data} canWrite />
                  {!data.latest_plan?.items.length && (
                    <button
                      className="button"
                      onClick={() => {
                        setTab('plan')
                        setEditingPlan(true)
                      }}
                    >
                      <Pencil size={14} />
                      创建计划
                    </button>
                  )}
                </div>
              )}
              {tab === 'description' && (!canWrite || archived) && (
                <TaskRelationships data={data} canWrite={false} />
              )}
            </div>
          </div>
        </article>
      </div>
      <TaskInspector data={data} canWrite={canWrite && !archived} />
      {lifecycleAction && (
        <TaskLifecycleDialog
          action={lifecycleAction}
          data={data}
          pending={action.isPending}
          error={action.error}
          onClose={() => setLifecycleAction(undefined)}
          onConfirm={(reason) => {
            if (lifecycleAction === 'start') perform({ method: 'todo.start', input: {} })
            if (lifecycleAction === 'done')
              perform({ method: 'todo.done', input: reason ? { reason } : {} })
            if (lifecycleAction === 'reopen')
              perform({ method: 'todo.start', input: { reopen_reason: '在 Web 工作区恢复工作' } })
            if (lifecycleAction === 'archive') perform({ method: 'todo.archive', input: {} })
            if (lifecycleAction === 'restore') perform({ method: 'todo.restore', input: {} })
          }}
        />
      )}
    </>
  )
}

function TaskInspector({ data, canWrite }: { data: TodoDetail; canWrite: boolean }) {
  const live = useLiveConnection()
  const status = useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: live ? 60000 : 15000,
    refetchIntervalInBackground: false,
  })
  const activity = taskActivity(data.todo, status.isError ? undefined : status.data)
  const todo = data.todo
  const sessions = data.sessions ?? []
  const activeBindings = (data.bindings ?? []).filter((binding) => binding.unbound_at == null)
  const summary = data.summary
  return (
    <aside className="task-inspector" aria-label="任务检查器">
      <section>
        <h3>属性</h3>
        <dl className="task-inspector-fields">
          <div>
            <dt>状态</dt>
            <dd>
              <span className={`status-badge status-${todo.status}`}>
                <StatusIcon todo={todo} />
                {statuses[todo.status]}
              </span>
            </dd>
          </div>
          <div>
            <dt>优先级</dt>
            <dd>
              <TaskInlineProperty data={data} field="priority" label="优先级" writable={canWrite} />
            </dd>
          </div>
          <div>
            <dt>项目</dt>
            <dd>
              <TaskInlineProperty data={data} field="project" label="项目" writable={canWrite} />
            </dd>
          </div>
          <div>
            <dt>创建时间</dt>
            <dd>{todo.created}</dd>
          </div>
          {todo.creator && (
            <div>
              <dt>创建者</dt>
              <dd>{todo.creator}</dd>
            </div>
          )}
          {todo.review_at && (
            <div>
              <dt>复查时间</dt>
              <dd>{todo.review_at}</dd>
            </div>
          )}
        </dl>
      </section>
      {(sessions.length > 0 || activeBindings.length > 0) && (
        <section>
          <div className="task-inspector-section-heading">
            <h3>Agent 会话</h3>
            <span>{sessions.length}</span>
          </div>
          <p className="task-activity-note">
            {status.isError
              ? '暂时无法读取活动状态'
              : taskActivityLabel(activity) || '暂未观察到关联会话活动'}
            {activity.lastAt && (
              <span> · 最近观察 {new Date(activity.lastAt).toLocaleString('zh-CN')}</span>
            )}
          </p>
          {summary && (
            <div className="task-inspector-metrics" aria-label="任务执行汇总">
              <span>
                <b>{summary.queries}</b>轮对话
              </span>
              <span>
                <b>{summary.tool_calls}</b>次工具
              </span>
              <span>
                <b>
                  $
                  {summary.cost_usd.toFixed(
                    summary.cost_usd > 0 && summary.cost_usd < 0.01 ? 4 : 2,
                  )}
                </b>
                费用
              </span>
            </div>
          )}
          {sessions.length ? (
            <div className="task-inspector-sessions">
              {sessions.map((session) => {
                const target = session.indexed_id || session.session_id
                const active = session.unbound_at == null
                return (
                  <article key={session.session_id}>
                    <div className="task-inspector-session-line">
                      <span className={`activity-avatar ${session.agent}`}>
                        {agentLabel(session.agent).slice(0, 1)}
                      </span>
                      <div>
                        <strong>{agentLabel(session.agent)}</strong>
                        <span>
                          {active ? '当前关联' : '历史关联'} · {epochDateTime(session.last_at)}
                        </span>
                      </div>
                      {session.indexed ? (
                        <Link
                          className="icon-button"
                          to={`/agents?session=${encodeURIComponent(target)}`}
                          title="查看会话"
                          aria-label={`查看 ${agentLabel(session.agent)} 会话`}
                        >
                          <ArrowUpRight size={14} />
                        </Link>
                      ) : null}
                    </div>
                    <div className="task-inspector-session-stats">
                      <span>{session.queries} 轮</span>
                      <span>{session.tool_calls} 工具调用</span>
                      {session.cost_usd > 0 && <span>${session.cost_usd.toFixed(3)}</span>}
                    </div>
                  </article>
                )
              })}
            </div>
          ) : null}
          {activeBindings.length > 0 && (
            <p className="task-inspector-note">当前有 {activeBindings.length} 个有效绑定。</p>
          )}
        </section>
      )}
    </aside>
  )
}

function TaskLifecycleDialog({
  action,
  data,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  action: TaskLifecycleAction
  data: TodoDetail
  pending: boolean
  error: unknown
  onClose: () => void
  onConfirm: (reason: string) => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [reason, setReason] = useState('')
  const todo = data.todo
  const activeBindings = (data.bindings ?? []).filter(
    (binding) => binding.unbound_at == null,
  ).length
  const details = {
    start: {
      title: '开始这项任务？',
      confirm: '确认开始',
      nextStatus: '待开始 → 工作中',
      consequences: ['任务进入当前工作集', '不会自动启动或派发任何 Agent'],
    },
    done: {
      title: todo.status === 'review' ? '验收这项任务？' : '标记为已完成？',
      confirm: todo.status === 'review' ? '确认验收' : '确认完成',
      nextStatus: `${statuses[todo.status]} → 已完成`,
      consequences: [
        activeBindings
          ? `将解除 ${activeBindings} 个当前 Session 绑定`
          : '没有需要解除的 Session 绑定',
        '会话和历史记录会继续保留',
      ],
    },
    reopen: {
      title: '重新开始这项任务？',
      confirm: '确认重新开始',
      nextStatus: '已完成 → 工作中',
      consequences: ['保留原有完成记录与会话证据', '不会自动恢复或启动旧 Agent'],
    },
    archive: {
      title: '归档这项任务？',
      confirm: '确认归档',
      nextStatus: '移出当前工作集',
      consequences: [
        activeBindings
          ? `将解除 ${activeBindings} 个当前 Session 绑定`
          : '没有需要解除的 Session 绑定',
        '归档可恢复，不会删除任务、附件或会话',
      ],
    },
    restore: {
      title: '恢复这项任务？',
      confirm: '确认恢复',
      nextStatus: '返回归档前的生命周期状态',
      consequences: ['任务重新出现在工作区', '不会自动启动或派发任何 Agent'],
    },
  }[action]

  useEffect(() => {
    const element = dialog.current!
    element.showModal()
    return () => {
      if (element.open) element.close()
    }
  }, [])

  return (
    <dialog
      ref={dialog}
      className="task-lifecycle-dialog"
      aria-labelledby="task-lifecycle-title"
      onCancel={(event) => {
        event.preventDefault()
        if (!pending) onClose()
      }}
    >
      <header>
        <div>
          <span className="eyebrow">操作预览 · {todo.id}</span>
          <h2 id="task-lifecycle-title">{details.title}</h2>
        </div>
        <button
          className="icon-button"
          aria-label="关闭操作预览"
          disabled={pending}
          onClick={onClose}
        >
          <X size={17} />
        </button>
      </header>
      <div className="task-lifecycle-body">
        <div className="task-lifecycle-status">
          <span>状态变化</span>
          <strong>{details.nextStatus}</strong>
        </div>
        <ul>
          {details.consequences.map((item) => (
            <li key={item}>
              <Check size={14} />
              <span>{item}</span>
            </li>
          ))}
        </ul>
        {action === 'done' && (
          <label className="task-lifecycle-reason">
            <span className="task-lifecycle-reason-label">
              验收说明 <small>可选</small>
            </span>
            <textarea
              value={reason}
              maxLength={360}
              placeholder="记录判断依据，便于以后恢复上下文…"
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
        )}
        {error ? <Notice error={error} /> : null}
      </div>
      <footer>
        <button className="button" disabled={pending} onClick={onClose}>
          取消
        </button>
        <button
          className={`button primary ${action === 'archive' ? 'danger' : ''}`}
          disabled={pending}
          onClick={() => onConfirm(reason.trim())}
        >
          {pending ? <LoaderCircle className="spin" size={14} /> : null}
          {details.confirm}
        </button>
      </footer>
    </dialog>
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
      className="modal task-create-modal"
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
