import { App, Button as AntButton, Tooltip } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Maximize2, Minimize2, ArrowLeft, FileText, X } from 'lucide-react'
import { call, errorText } from '../../api'
import { Notice } from '../../components/notice'
import { AppShell } from '../../workspace-shell'
import { TaskWorkbench } from './task-workbench'
import { useLiveConnection } from '../../live-connection'
import { taskWorkbenchInput } from './task-query'
import { consumeNewTaskRequest, readTaskLayout } from './task-navigation'
import type { Bootstrap } from '../../types'
import { TaskDetail } from './task-detail'
import { TaskEditorDialog } from './task-editor-dialog'
import { Loading } from '../../components/loading'
export function TaskWorkspace({ boot }: { boot: Bootstrap }) {
  const live = useLiveConnection()
  const { id } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const layout = readTaskLayout(params)
  const project = params.get('project') ?? ''
  const [newTask, setNewTask] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editingDirty, setEditingDirty] = useState(false)
  const [editingBusy, setEditingBusy] = useState(false)
  const { modal } = App.useApp()
  const leaveEditor = (after: () => void) => {
    if (editingBusy) return
    if (editing && editingDirty) {
      modal.confirm({
        keyboard: false,
        focusable: { autoFocusButton: 'cancel' },
        okButtonProps: { danger: true },
        title: '放弃未保存的修改？',
        content: '任务的标题、描述和属性尚未保存。',
        okText: '放弃修改',
        cancelText: '继续编辑',
        onOk: after,
      })
    } else after()
  }
  const stopEditing = () => leaveEditor(() => setEditing(false))
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
  const hasOverlay = () =>
    Array.from(
      document.querySelectorAll<HTMLElement>(
        'dialog[open], [role="dialog"][aria-modal="true"], .ant-dropdown:not(.ant-dropdown-hidden), .ant-select-dropdown:not(.ant-select-dropdown-hidden)',
      ),
    ).some((element) => element !== modalRef.current && element.getClientRects().length > 0)
  useEffect(() => {
    if (!id) return
    modalRef.current?.focus()
    const trap = (event: KeyboardEvent) => {
      if (event.key !== 'Tab' || hasOverlay()) return
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
  const closeTask = () =>
    leaveEditor(() => {
      const focusedTaskID = id
      navigate(taskListHref)
      if (focusedTaskID) {
        requestAnimationFrame(() => {
          document
            .querySelector<HTMLButtonElement>(`[data-task-id="${CSS.escape(focusedTaskID)}"]`)
            ?.focus()
        })
      }
    })

  useEffect(() => {
    if (!id) return
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented || hasOverlay()) return
      event.preventDefault()
      if (editing) stopEditing()
      else closeTask()
    }
    document.addEventListener('keydown', closeOnEscape)
    return () => document.removeEventListener('keydown', closeOnEscape)
  }, [editing, editingDirty, editingBusy, id, layout, taskListHref])

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
          <AntButton
            htmlType="button"
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
                    <Tooltip title={layout === 'kanban' ? '返回看板' : '返回任务列表'}>
                      <AntButton
                        htmlType="button"
                        className={`icon-button back-list ${layout === 'kanban' ? 'kanban-back-button' : ''}`}
                        aria-label={layout === 'kanban' ? '返回看板' : '返回任务列表'}
                        onClick={closeTask}
                      >
                        <ArrowLeft size={16} />
                        {layout === 'kanban' && <span>返回看板</span>}
                      </AntButton>
                    </Tooltip>
                    <span className="detail-id">
                      <FileText size={14} />
                      {id}
                    </span>
                    {id && (
                      <div className="detail-actions">
                        <Tooltip title={expandedDetail ? '收起阅读' : '展开阅读'}>
                          <AntButton
                            htmlType="button"
                            className="icon-button"
                            aria-label={expandedDetail ? '收起阅读' : '展开阅读'}
                            onClick={() => setExpandedDetail(!expandedDetail)}
                          >
                            {expandedDetail ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
                          </AntButton>
                        </Tooltip>
                        <Tooltip title="关闭任务详情">
                          <AntButton
                            htmlType="button"
                            className="icon-button kanban-detail-close"
                            aria-label="关闭任务详情"
                            onClick={closeTask}
                          >
                            <X size={16} />
                          </AntButton>
                        </Tooltip>
                      </div>
                    )}
                  </div>
                  <TaskDetail
                    key={id}
                    data={detail.data}
                    canWrite={canWrite}
                    onEdit={() => setEditing(true)}
                  />
                </>
              )
            )}
          </section>
        )}
      </div>
      {editing && detail.data && (
        <TaskEditorDialog
          key={id}
          initial={detail.data}
          defaultProject={detail.data.todo.project || ''}
          canRefine={canWrite && boot.capabilities?.runtime_jobs === true}
          onClose={stopEditing}
          onDirtyChange={setEditingDirty}
          onBusyChange={setEditingBusy}
          onSaved={() => {
            setEditing(false)
            setEditingDirty(false)
          }}
        />
      )}
      {newTask && (
        <TaskEditorDialog
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
