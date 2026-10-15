import { Loading } from '../../components/loading'
import { App, Alert, Button as AntButton, Dropdown, Spin } from 'antd'
import { useEffect, useState } from 'react'
import { useLocation } from 'react-router'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Modal } from 'antd'
import {
  MoreHorizontal,
  Link2,
  Archive,
  ArrowUpRight,
  Check,
  Circle,
  CircleCheck,
  CircleDot,
  Clock3,
  FileText,
  Folder,
  Pencil,
  RefreshCw,
} from 'lucide-react'
import { call, callScopedTodoMutation } from '../../api'
import type { TodoScopedMutationRequest } from '../../api-contracts'
import { Markdown } from '../../components/markdown'
import { Notice } from '../../components/notice'
import {
  TaskOperationDialog,
  type TaskOperation,
  TaskProgressForm,
  TaskRelationships,
} from './task-operations'
import { useLiveConnection } from '../../live-connection'
import { taskProgress, latestTaskProgress } from './task-progress'
import type { TodoDetail, OperationWarning } from '../../types'
import { TaskKnowledge } from '../knowledge/task-knowledge-links'
import { TaskInspector } from './task-inspector'
export function TaskDetail({
  data,
  canWrite,
  onEdit,
}: {
  data: TodoDetail
  canWrite: boolean
  onEdit: () => void
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
  const [showFullDocument, setShowFullDocument] = useState(false)
  const [operation, setOperation] = useState<TaskOperation>()
  const { message } = App.useApp()
  useEffect(() => {
    setTab('description')
    setShowFullDocument(false)
    setOperation(undefined)
  }, [todo.id])
  const document = useQuery({
    queryKey: ['doc', todo.id],
    enabled: true,
    queryFn: ({ signal }) => call('todo.doc', { todo_id: todo.id }, signal),
    refetchInterval: live ? 60000 : 5000,
    refetchIntervalInBackground: false,
  })
  const busy = useIsMutating({ mutationKey: ['todo-write', todo.id] }) > 0
  const action = useMutation({
    mutationKey: ['todo-write', todo.id],
    mutationFn: (request: TodoScopedMutationRequest) => callScopedTodoMutation(todo.id, request),
    onSuccess: async (result, request) => {
      query.setQueryData<TodoDetail>(['todo', todo.id], (current) =>
        current ? { ...current, todo: result.todo, etag: result.etag } : current,
      )
      const feedback = {
        'todo.start':
          request.input && 'reopen_reason' in request.input ? '任务已重新开始' : '任务已开始',
        'todo.done': '任务已完成',
        'todo.archive': '任务已归档，可在当前详情中恢复',
        'todo.restore': '任务已恢复',
      }
      void message.success({
        content: feedback[request.method as keyof typeof feedback] || '任务已更新',
        duration: 2,
      })
      await Promise.all([
        query.invalidateQueries({ queryKey: ['todos'] }),
        query.invalidateQueries({ queryKey: ['todo', todo.id] }),
        query.invalidateQueries({ queryKey: ['doc', todo.id] }),
      ])
    },
  })
  const latestProgress = latestTaskProgress(document.data?.content || '')
  const [progressCollapsed, setProgressCollapsed] = useState(false)
  useEffect(() => setProgressCollapsed(false), [latestProgress])
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
              <div className="task-primary-actions" aria-label="任务主要操作">
                {canWrite && !archived && (
                  <AntButton className="button" disabled={busy} onClick={onEdit}>
                    <Pencil size={14} />
                    编辑
                  </AntButton>
                )}
                {canWrite &&
                  (archived ? (
                    <AntButton
                      className="button primary"
                      disabled={busy}
                      type="primary"
                      onClick={() => perform({ method: 'todo.restore', input: {} })}
                    >
                      <Archive size={14} />
                      恢复任务
                    </AntButton>
                  ) : (
                    <>
                      {todo.status === 'open' && (
                        <AntButton
                          className="button primary"
                          disabled={busy}
                          type="primary"
                          onClick={() => perform({ method: 'todo.start', input: {} })}
                        >
                          <CircleDot size={14} />
                          开始任务
                        </AntButton>
                      )}
                      {todo.status !== 'done' && (
                        <AntButton
                          className={todo.status === 'open' ? 'button' : 'button primary'}
                          disabled={busy}
                          type={todo.status === 'open' ? 'default' : 'primary'}
                          onClick={() => perform({ method: 'todo.done', input: {} })}
                        >
                          <Check size={15} />
                          {todo.status === 'review' ? '验收完成' : '标记完成'}
                        </AntButton>
                      )}
                      {todo.status === 'done' && (
                        <AntButton
                          className="button primary"
                          disabled={busy}
                          type="primary"
                          onClick={() =>
                            perform({
                              method: 'todo.start',
                              input: { reopen_reason: '在 Web 工作区恢复工作' },
                            })
                          }
                        >
                          <RefreshCw size={14} />
                          重新开始
                        </AntButton>
                      )}
                    </>
                  ))}
                <Dropdown
                  trigger={['click']}
                  menu={{
                    items: [
                      {
                        key: 'document',
                        label: '查看完整任务文档',
                        icon: <FileText size={15} />,
                        onClick: () => setShowFullDocument(true),
                      },
                      ...(canWrite && !archived
                        ? [
                            { type: 'divider' as const },
                            {
                              key: 'dependency',
                              label: '添加依赖',
                              icon: <Link2 size={15} />,
                              onClick: () => setOperation('dependency'),
                            },
                            {
                              key: 'waiting',
                              label:
                                todo.wake_condition || todo.review_at
                                  ? '修改等待条件'
                                  : '设置等待条件',
                              icon: <Clock3 size={15} />,
                              onClick: () => setOperation('waiting'),
                            },
                            ...(todo.wake_condition || todo.review_at
                              ? [
                                  {
                                    key: 'wake',
                                    label: '解除等待',
                                    icon: <Check size={15} />,
                                    onClick: () => setOperation('wake'),
                                  },
                                ]
                              : []),
                            {
                              key: 'plan',
                              label: data.latest_plan?.items.length ? '编辑计划' : '创建计划',
                              icon: <Pencil size={15} />,
                              onClick: () => setOperation('plan'),
                            },
                            { type: 'divider' as const },
                            {
                              key: 'archive',
                              label: '归档任务',
                              icon: <Archive size={15} />,
                              onClick: () => perform({ method: 'todo.archive', input: {} }),
                            },
                          ]
                        : []),
                    ],
                  }}
                >
                  <AntButton
                    className="button"
                    aria-label="更多任务操作"
                    disabled={busy}
                    icon={<MoreHorizontal size={16} />}
                  />
                </Dropdown>
                {action.isPending && <Spin size="small" />}
              </div>
            </div>
            {action.error && <Notice error={action.error} />}
            {warnings.length > 0 && (
              <Alert
                className="notice"
                type="warning"
                role="status"
                title="任务已创建"
                description={warnings.map((warning, index) => (
                  <p key={index}>{warning.message}</p>
                ))}
                action={
                  <AntButton htmlType="button" onClick={() => setWarnings([])}>
                    知道了
                  </AntButton>
                }
              />
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
                    </div>
                    <p
                      id={`latest-progress-${todo.id}`}
                      className={`latest-progress-text${progressCollapsed ? ' is-collapsed' : ''}`}
                    >
                      {latestProgress}
                    </p>
                    {latestProgress.length > 240 && (
                      <AntButton
                        type="text"
                        aria-expanded={!progressCollapsed}
                        aria-controls={`latest-progress-${todo.id}`}
                        onClick={() => setProgressCollapsed(!progressCollapsed)}
                      >
                        {progressCollapsed ? '展开完整进展' : '收起进展'}
                      </AntButton>
                    )}
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
              <AntButton
                className={tab === 'description' ? 'selected' : ''}
                onClick={() => setTab('description')}
              >
                任务说明
              </AntButton>
              {(latestProgress || (canWrite && !archived) || tab === 'progress') && (
                <AntButton
                  className={tab === 'progress' ? 'selected' : ''}
                  onClick={() => setTab('progress')}
                >
                  {latestProgress ? '进展记录' : '记录进展'}
                </AntButton>
              )}
              {(!!data.latest_plan?.items.length || tab === 'plan') && (
                <AntButton
                  className={tab === 'plan' ? 'selected' : ''}
                  onClick={() => setTab('plan')}
                >
                  执行计划
                  <span className="detail-tab-count">{data.latest_plan?.items.length ?? 0}</span>
                </AntButton>
              )}
            </div>
            {canWrite && !archived && tab === 'progress' && (
              <TaskProgressForm key={todo.id} data={data} />
            )}
            {canWrite && !archived && tab === 'plan' && (
              <AntButton className="button task-inline-action" onClick={() => setOperation('plan')}>
                <Pencil size={14} />
                编辑计划
              </AntButton>
            )}
            <div className="task-reading-surface">
              <div className="detail-body">
                {tab === 'description' ? (
                  <>
                    {todo.description ? (
                      <Markdown text={todo.description} />
                    ) : canWrite && !archived ? (
                      <AntButton className="button" disabled={busy} onClick={onEdit}>
                        填写任务说明
                      </AntButton>
                    ) : null}
                    {todo.links?.some((link) => /^https?:\/\//i.test(link.url)) && (
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
              {tab === 'description' && (
                <>
                  <TaskKnowledge key={todo.id} todo={todo} writable={canWrite && !archived} />
                  <TaskRelationships data={data} canWrite={canWrite && !archived} />
                </>
              )}
            </div>
          </div>
        </article>
      </div>
      <TaskInspector data={data} canWrite={canWrite && !archived} />
      {showFullDocument && (
        <Modal
          open
          centered
          title={`完整任务文档 · ${todo.id}`}
          width={860}
          footer={null}
          className="task-document-modal"
          onCancel={() => setShowFullDocument(false)}
        >
          <div
            className="task-document-reader"
            role="region"
            aria-label="完整任务文档正文"
            tabIndex={0}
          >
            {document.error && (
              <Notice error={document.error} retry={() => void document.refetch()} />
            )}
            {document.isPending ? (
              <Loading text="读取任务文档…" />
            ) : (
              <Markdown text={document.data?.content || '暂无任务文档。'} />
            )}
          </div>
        </Modal>
      )}
      {operation && (
        <TaskOperationDialog
          data={data}
          operation={operation}
          onClose={() => setOperation(undefined)}
          onSaved={() => {
            if (operation === 'plan') setTab('plan')
            setOperation(undefined)
            void message.success({ content: '已保存', duration: 2 })
          }}
        />
      )}
    </>
  )
}
