import { DateInput } from '../../components/date-input'
import { Alert, Modal, Input as AntInput, Input as AntdInput } from 'antd'
import { Button as AntButton } from 'antd'
import { Select } from '../../components/select'
import { useState } from 'react'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CircleCheck, Clock3, Link2, Plus, Trash2 } from 'lucide-react'
import { ApiError, call, callScopedTodoMutation } from '../../api'
import type { TodoScopedMutationRequest } from '../../api-contracts'
import { Notice } from '../../components/notice'
import type { TodoDetail } from '../../types'

type PlanItem = { step: string; status: string }
function useTaskAction(todoID: string) {
  const query = useQueryClient()
  return useMutation({
    retry: false,
    mutationKey: ['todo-write', todoID],
    mutationFn: (request: TodoScopedMutationRequest) => callScopedTodoMutation(todoID, request),
    onSuccess: async () => {
      await Promise.all([
        query.invalidateQueries({ queryKey: ['todos'] }),
        query.invalidateQueries({ queryKey: ['todo', todoID] }),
        query.invalidateQueries({ queryKey: ['doc', todoID] }),
        query.invalidateQueries({ queryKey: ['dependencies', todoID] }),
      ])
    },
  })
}

export function TaskProgressForm({ data }: { data: TodoDetail }) {
  const [message, setMessage] = useState('')
  const action = useTaskAction(data.todo.id)
  const length = Array.from(message).length
  return (
    <form
      className="task-operation-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (!message.trim() || length > 400 || action.isPending) return
        action.mutate(
          {
            method: 'todo.progress.append',
            input: { expected_etag: data.etag, message: message.trim() },
          },
          { onSuccess: () => setMessage('') },
        )
      }}
    >
      <label className="field-label" htmlFor="task-progress-message">
        记录一次进展
      </label>
      <AntdInput.TextArea
        id="task-progress-message"
        rows={3}
        value={message}
        disabled={action.isPending}
        onChange={(event) => setMessage(event.target.value.replace(/[\r\n]+/g, ' '))}
        placeholder="完成了什么、验证结果，以及下一步。"
      />
      <div className="task-form-actions">
        <span className={length > 400 ? 'task-field-error' : ''}>{length} / 400 字 · 单段记录</span>
        <AntButton
          htmlType="submit"
          className="button primary"
          disabled={!message.trim() || length > 400 || action.isPending}
          loading={action.isPending}
        >
          {!action.isPending && <Plus size={14} />}
          添加进展
        </AntButton>
      </div>
      {action.error && <Notice error={action.error} />}
    </form>
  )
}

export function TaskPlanEditor({
  data,
  onClose,
  onSaved = onClose,
}: {
  data: TodoDetail
  onClose: () => void
  onSaved?: () => void
}) {
  const [baseline, setBaseline] = useState(data)
  const [items, setItems] = useState<PlanItem[]>(
    () => data.latest_plan?.items.map((item) => ({ ...item })) ?? [{ step: '', status: 'pending' }],
  )
  const [explanation, setExplanation] = useState(data.latest_plan?.explanation ?? '')
  const [latest, setLatest] = useState<TodoDetail>()
  const [loadError, setLoadError] = useState<unknown>()
  const [loading, setLoading] = useState(false)
  const action = useTaskAction(data.todo.id)
  const conflict = action.error instanceof ApiError && action.error.status === 409
  const multipleActiveSteps = items.filter((item) => item.status === 'in_progress').length > 1
  const loadLatest = async () => {
    setLoading(true)
    setLoadError(undefined)
    try {
      setLatest(await call('todo.show', { todo_id: data.todo.id }))
    } catch (error) {
      setLoadError(error)
    } finally {
      setLoading(false)
    }
  }
  return (
    <form
      className="task-operation-form"
      aria-label="编辑执行计划"
      onSubmit={(event) => {
        event.preventDefault()
        if (
          action.isPending ||
          conflict ||
          multipleActiveSteps ||
          items.length === 0 ||
          items.some((item) => !item.step.trim())
        )
          return
        action.mutate(
          {
            method: 'todo.plan.set',
            input: {
              expected_etag: baseline.etag,
              base_revision: baseline.latest_plan?.revision ?? 0,
              explanation,
              items: items.map((item) => ({ ...item, step: item.step.trim() })),
            },
          },
          { onSuccess: onSaved },
        )
      }}
    >
      <label className="field-label" htmlFor="task-plan-explanation">
        计划说明
      </label>
      <AntdInput.TextArea
        id="task-plan-explanation"
        rows={2}
        value={explanation}
        onChange={(event) => setExplanation(event.target.value)}
        disabled={action.isPending}
        placeholder="这次计划解决什么问题？"
      />
      <div className="task-plan-rows">
        {items.map((item, index) => (
          <div className="task-plan-edit-row" key={index}>
            <span>{index + 1}</span>
            <AntInput
              aria-label={`步骤 ${index + 1}`}
              value={item.step}
              required
              disabled={action.isPending}
              onChange={(event) =>
                setItems(
                  items.map((entry, entryIndex) =>
                    entryIndex === index ? { ...entry, step: event.target.value } : entry,
                  ),
                )
              }
              placeholder="具体可验证的步骤"
            />
            <Select
              aria-label={`步骤 ${index + 1} 状态`}
              value={item.status}
              disabled={action.isPending}
              onChange={(event) =>
                setItems(
                  items.map((entry, entryIndex) =>
                    entryIndex === index ? { ...entry, status: event.target.value } : entry,
                  ),
                )
              }
            >
              <option value="pending">待开始</option>
              <option value="in_progress">进行中</option>
              <option value="completed">已完成</option>
            </Select>
            <AntButton
              htmlType="button"
              className="icon-button"
              aria-label={`删除步骤 ${index + 1}`}
              disabled={items.length === 1 || action.isPending}
              onClick={() => setItems(items.filter((_, entryIndex) => entryIndex !== index))}
            >
              <Trash2 size={14} />
            </AntButton>
          </div>
        ))}
      </div>
      {multipleActiveSteps && (
        <Alert className="notice" type="error" title="计划同时只能有一个进行中的步骤。" />
      )}
      <AntButton
        htmlType="button"
        className="text-button"
        disabled={action.isPending || items.length >= 100}
        onClick={() => setItems([...items, { step: '', status: 'pending' }])}
      >
        <Plus size={14} />
        添加步骤
      </AntButton>
      {action.error && <Notice error={action.error} />}
      {conflict && (
        <div className="task-conflict-review">
          <p>当前计划草稿已保留。读取最新计划后，选择要继续编辑的内容。</p>
          <AntButton
            htmlType="button"
            className="button"
            disabled={loading}
            onClick={() => void loadLatest()}
          >
            {loading ? '正在读取…' : '读取最新计划'}
          </AntButton>
          {loadError !== undefined && <Notice error={loadError} />}
          {latest && (
            <>
              <strong>最新计划 · 版本 {latest.latest_plan?.revision ?? 0}</strong>
              <p>{latest.latest_plan?.explanation || '无计划说明'}</p>
              <ol>
                {latest.latest_plan?.items.map((item, index) => (
                  <li key={index}>
                    {item.step} · {item.status}
                  </li>
                ))}
              </ol>
              <div className="task-form-actions">
                <AntButton
                  htmlType="button"
                  className="button"
                  onClick={() => {
                    setItems(
                      latest.latest_plan?.items.map((item) => ({ ...item })) ?? [
                        { step: '', status: 'pending' },
                      ],
                    )
                    setExplanation(latest.latest_plan?.explanation ?? '')
                    setBaseline(latest)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  采用最新计划
                </AntButton>
                <AntButton
                  htmlType="button"
                  className="button"
                  onClick={() => {
                    setBaseline(latest)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  以当前草稿替换最新计划，继续检查
                </AntButton>
              </div>
            </>
          )}
        </div>
      )}
      <div className="task-form-actions">
        <AntButton
          htmlType="button"
          className="button subtle"
          onClick={onClose}
          disabled={action.isPending}
        >
          取消
        </AntButton>
        <AntButton
          htmlType="submit"
          className="button primary"
          disabled={
            action.isPending ||
            conflict ||
            multipleActiveSteps ||
            items.some((item) => !item.step.trim())
          }
          loading={action.isPending}
        >
          {!action.isPending && <Check size={14} />}
          保存计划
        </AntButton>
      </div>
    </form>
  )
}

export function TaskRelationships({ data, canWrite }: { data: TodoDetail; canWrite: boolean }) {
  const action = useTaskAction(data.todo.id)
  const dependencies = useQuery({
    queryKey: ['dependencies', data.todo.id, data.todo.depends_on],
    queryFn: ({ signal }) => call('todo.dependency.list', { todo_id: data.todo.id }, signal),
    enabled: (data.todo.depends_on?.length ?? 0) > 0,
  })
  if (!data.todo.depends_on?.length && !data.todo.wake_condition && !data.todo.review_at)
    return null
  return (
    <section className="task-relationships" aria-label="依赖与等待">
      <h3>
        <Link2 size={15} />
        依赖与等待
      </h3>
      {dependencies.error && (
        <Notice error={dependencies.error} retry={() => void dependencies.refetch()} />
      )}
      <div className="task-dependency-list">
        {dependencies.data?.dependencies.map((item) => (
          <div key={item.id} className="task-dependency-row">
            <a href={`/tasks/${item.id}`}>
              {item.met && <CircleCheck size={14} />}
              {item.id} · {item.title}
            </a>
            <span>{item.met ? '已满足' : '未完成'}</span>
            {canWrite && (
              <AntButton
                htmlType="button"
                className="icon-button"
                aria-label={`移除依赖 ${item.id}`}
                disabled={action.isPending}
                onClick={() =>
                  action.mutate({
                    method: 'todo.dependency.remove',
                    input: { expected_etag: data.etag, dependency_id: item.id },
                  })
                }
              >
                <Trash2 size={14} />
              </AntButton>
            )}
          </div>
        ))}
      </div>
      {data.todo.wake_condition && (
        <p className="task-wait-date">
          <Clock3 size={14} />
          {data.todo.wake_condition}
        </p>
      )}
      {data.todo.review_at && (
        <p className="task-wait-date">
          <Clock3 size={14} />
          复查日期：{data.todo.review_at}
        </p>
      )}
      {action.error && <Notice error={action.error} />}
    </section>
  )
}

function TaskWaitingEditor({
  data,
  onClose,
  onSaved = onClose,
}: {
  data: TodoDetail
  onClose: () => void
  onSaved?: () => void
}) {
  const [baseline, setBaseline] = useState(data.etag)
  const [latest, setLatest] = useState<TodoDetail>()
  const [loadError, setLoadError] = useState<unknown>()
  const [loading, setLoading] = useState(false)
  const [wake, setWake] = useState(data.todo.wake_condition ?? '')
  const [reviewAt, setReviewAt] = useState(data.todo.review_at ?? '')
  const action = useTaskAction(data.todo.id)
  const conflict = action.error instanceof ApiError && action.error.status === 409
  const loadLatest = async () => {
    setLoading(true)
    setLoadError(undefined)
    try {
      setLatest(await call('todo.show', { todo_id: data.todo.id }))
    } catch (error) {
      setLoadError(error)
    } finally {
      setLoading(false)
    }
  }
  return (
    <form
      className="task-operation-form"
      aria-label="修改等待条件"
      onSubmit={(event) => {
        event.preventDefault()
        if (action.isPending || conflict) return
        action.mutate(
          {
            method: 'todo.wait.update',
            input: { expected_etag: baseline, wake_condition: wake.trim(), review_at: reviewAt },
          },
          { onSuccess: onSaved },
        )
      }}
    >
      <label className="field-label" htmlFor="task-wake-condition">
        可观察的等待条件
      </label>
      <AntInput
        id="task-wake-condition"
        value={wake}
        onChange={(event) => setWake(event.target.value)}
        disabled={action.isPending}
        placeholder="例如：评审通过，或依赖任务完成"
      />
      <label className="field-label" htmlFor="task-review-at">
        复查日期
      </label>
      <DateInput
        id="task-review-at"
        value={reviewAt}
        onChange={setReviewAt}
        disabled={action.isPending}
      />
      {action.error && <Notice error={action.error} />}
      {conflict && (
        <div className="task-conflict-review">
          <p>当前输入已保留。读取最新等待条件后，再选择要保存的内容。</p>
          <AntButton
            className="button"
            htmlType="button"
            disabled={loading}
            onClick={() => void loadLatest()}
          >
            {loading ? '正在读取…' : '读取最新等待条件'}
          </AntButton>
          {loadError !== undefined && <Notice error={loadError} />}
          {latest && (
            <>
              <p>
                最新条件：{latest.todo.wake_condition || '无'}
                <br />
                最新复查日期：{latest.todo.review_at || '无'}
              </p>
              <div className="task-form-actions">
                <AntButton
                  className="button"
                  htmlType="button"
                  onClick={() => {
                    setWake(latest.todo.wake_condition ?? '')
                    setReviewAt(latest.todo.review_at ?? '')
                    setBaseline(latest.etag)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  采用最新条件
                </AntButton>
                <AntButton
                  className="button"
                  htmlType="button"
                  onClick={() => {
                    setBaseline(latest.etag)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  保留当前输入，继续检查
                </AntButton>
              </div>
            </>
          )}
        </div>
      )}
      <div className="task-form-actions">
        <AntButton
          htmlType="button"
          className="button subtle"
          onClick={onClose}
          disabled={action.isPending}
        >
          取消
        </AntButton>
        <AntButton
          htmlType="submit"
          className="button primary"
          disabled={action.isPending || conflict || (!wake.trim() && !reviewAt)}
        >
          保存等待条件
        </AntButton>
      </div>
    </form>
  )
}

export type TaskOperation = 'dependency' | 'waiting' | 'wake' | 'plan'

export function TaskOperationDialog({
  data,
  operation,
  onClose,
  onSaved,
}: {
  data: TodoDetail
  operation: TaskOperation
  onClose: () => void
  onSaved: () => void
}) {
  const busy = useIsMutating({ mutationKey: ['todo-write', data.todo.id] }) > 0
  const titles = {
    dependency: '添加依赖',
    waiting: data.todo.wake_condition || data.todo.review_at ? '修改等待条件' : '设置等待条件',
    wake: '解除等待',
    plan: data.latest_plan?.items.length ? '编辑执行计划' : '创建执行计划',
  }
  return (
    <Modal
      open
      centered
      title={titles[operation]}
      footer={null}
      width={operation === 'plan' ? 680 : 440}
      className="task-operation-modal"
      mask={{ closable: false }}
      keyboard={!busy}
      closable={!busy}
      onCancel={() => {
        if (!busy) onClose()
      }}
    >
      {operation === 'plan' ? (
        <TaskPlanEditor data={data} onClose={onClose} onSaved={onSaved} />
      ) : operation === 'waiting' ? (
        <TaskWaitingEditor data={data} onClose={onClose} onSaved={onSaved} />
      ) : (
        <TaskRelationshipEditor
          data={data}
          operation={operation}
          onClose={onClose}
          onSaved={onSaved}
        />
      )}
    </Modal>
  )
}

function TaskRelationshipEditor({
  data,
  operation,
  onClose,
  onSaved,
}: {
  data: TodoDetail
  operation: 'dependency' | 'wake'
  onClose: () => void
  onSaved: () => void
}) {
  const [value, setValue] = useState('')
  const action = useTaskAction(data.todo.id)
  const dependency = operation === 'dependency'
  const valid = dependency
    ? /^t\d+$/.test(value.trim()) && value.trim() !== data.todo.id
    : !!value.trim()
  return (
    <form
      className="task-operation-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (!valid || action.isPending) return
        action.mutate(
          dependency
            ? {
                method: 'todo.dependency.add',
                input: { expected_etag: data.etag, dependency_id: value.trim() },
              }
            : {
                method: 'todo.wake',
                input: { expected_etag: data.etag, reason: value.trim() },
              },
          { onSuccess: onSaved },
        )
      }}
    >
      <label className="field-label" htmlFor="task-relationship-value">
        {dependency ? '依赖任务编号' : '解除等待的原因'}
      </label>
      <AntInput
        autoFocus
        id="task-relationship-value"
        value={value}
        placeholder={dependency ? '例如 t123' : '条件已满足的依据'}
        disabled={action.isPending}
        onChange={(event) => setValue(event.target.value)}
      />
      {action.error && <Notice error={action.error} />}
      <div className="task-form-actions">
        <AntButton onClick={onClose} disabled={action.isPending}>
          取消
        </AntButton>
        <AntButton
          type="primary"
          htmlType="submit"
          disabled={!valid || action.isPending}
          loading={action.isPending}
        >
          {dependency ? '添加依赖' : '解除等待'}
        </AntButton>
      </div>
    </form>
  )
}
