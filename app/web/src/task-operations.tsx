import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CircleCheck, Clock3, Link2, LoaderCircle, Plus, Trash2 } from 'lucide-react'
import { ApiError, call } from './api'
import { Notice } from './editor'
import type { TodoDetail } from './types'

type PlanItem = { step: string; status: string }
function useTaskAction(todoID: string) {
  const query = useQueryClient()
  return useMutation({
    retry: false,
    mutationFn: ({ method, input }: { method: string; input: object }) =>
      call(method, { todo_id: todoID, ...input }),
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
      <textarea
        id="task-progress-message"
        rows={3}
        value={message}
        disabled={action.isPending}
        onChange={(event) => setMessage(event.target.value.replace(/[\r\n]+/g, ' '))}
        placeholder="完成了什么、验证结果，以及下一步。"
      />
      <div className="task-form-actions">
        <span className={length > 400 ? 'task-field-error' : ''}>{length} / 400 字 · 单段记录</span>
        <button
          className="button primary"
          disabled={!message.trim() || length > 400 || action.isPending}
        >
          {action.isPending ? <LoaderCircle className="spin" size={14} /> : <Plus size={14} />}
          添加进展
        </button>
      </div>
      {action.error && <Notice error={action.error} />}
    </form>
  )
}

export function TaskPlanEditor({ data, onClose }: { data: TodoDetail; onClose: () => void }) {
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
      setLatest(await call<TodoDetail>('todo.show', { todo_id: data.todo.id }))
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
          { onSuccess: onClose },
        )
      }}
    >
      <label className="field-label" htmlFor="task-plan-explanation">
        计划说明
      </label>
      <textarea
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
            <input
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
            <select
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
            </select>
            <button
              type="button"
              className="icon-button"
              aria-label={`删除步骤 ${index + 1}`}
              disabled={items.length === 1 || action.isPending}
              onClick={() => setItems(items.filter((_, entryIndex) => entryIndex !== index))}
            >
              <Trash2 size={14} />
            </button>
          </div>
        ))}
      </div>
      {multipleActiveSteps && (
        <p className="notice" role="alert">
          计划同时只能有一个进行中的步骤。
        </p>
      )}
      <button
        type="button"
        className="text-button"
        disabled={action.isPending || items.length >= 100}
        onClick={() => setItems([...items, { step: '', status: 'pending' }])}
      >
        <Plus size={14} />
        添加步骤
      </button>
      {action.error && <Notice error={action.error} />}
      {conflict && (
        <div className="task-conflict-review">
          <p>当前计划草稿已保留。读取最新计划后，选择要继续编辑的内容。</p>
          <button
            type="button"
            className="button"
            disabled={loading}
            onClick={() => void loadLatest()}
          >
            {loading ? '正在读取…' : '读取最新计划'}
          </button>
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
                <button
                  type="button"
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
                </button>
                <button
                  type="button"
                  className="button"
                  onClick={() => {
                    setBaseline(latest)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  以当前草稿替换最新计划，继续检查
                </button>
              </div>
            </>
          )}
        </div>
      )}
      <div className="task-form-actions">
        <button
          type="button"
          className="button subtle"
          onClick={onClose}
          disabled={action.isPending}
        >
          取消
        </button>
        <button
          className="button primary"
          disabled={
            action.isPending ||
            conflict ||
            multipleActiveSteps ||
            items.some((item) => !item.step.trim())
          }
        >
          {action.isPending ? <LoaderCircle className="spin" size={14} /> : <Check size={14} />}
          保存计划
        </button>
      </div>
    </form>
  )
}

export function TaskRelationships({ data, canWrite }: { data: TodoDetail; canWrite: boolean }) {
  const [dependency, setDependency] = useState('')
  const [waiting, setWaiting] = useState(false)
  const [wakeReason, setWakeReason] = useState('')
  const action = useTaskAction(data.todo.id)
  const dependencies = useQuery({
    queryKey: ['dependencies', data.todo.id, data.todo.depends_on],
    queryFn: ({ signal }) =>
      call<{ dependencies: { id: string; title: string; status: string; met: boolean }[] }>(
        'todo.dependency.list',
        { todo_id: data.todo.id },
        signal,
      ),
    enabled: (data.todo.depends_on?.length ?? 0) > 0,
  })
  if (
    !canWrite &&
    !data.todo.depends_on?.length &&
    !data.todo.wake_condition &&
    !data.todo.review_at
  )
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
              <button
                type="button"
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
              </button>
            )}
          </div>
        ))}
      </div>
      {canWrite && (
        <form
          className="task-dependency-add"
          onSubmit={(event) => {
            event.preventDefault()
            if (!/^t\d+$/.test(dependency.trim()) || action.isPending) return
            action.mutate(
              {
                method: 'todo.dependency.add',
                input: { expected_etag: data.etag, dependency_id: dependency.trim() },
              },
              { onSuccess: () => setDependency('') },
            )
          }}
        >
          <input
            aria-label="依赖任务编号"
            placeholder="依赖任务编号，例如 t123"
            value={dependency}
            onChange={(event) => setDependency(event.target.value)}
            disabled={action.isPending}
          />
          <button
            className="button"
            disabled={
              !/^t\d+$/.test(dependency.trim()) ||
              dependency.trim() === data.todo.id ||
              action.isPending
            }
          >
            添加依赖
          </button>
        </form>
      )}
      {data.todo.review_at && (
        <p className="task-wait-date">
          <Clock3 size={14} />
          复查日期：{data.todo.review_at}
        </p>
      )}
      {canWrite &&
        (waiting ? (
          <TaskWaitingEditor key={data.todo.id} data={data} onClose={() => setWaiting(false)} />
        ) : (
          <button type="button" className="button" onClick={() => setWaiting(true)}>
            <Clock3 size={14} />
            {data.todo.wake_condition || data.todo.review_at ? '修改等待条件' : '设置等待条件'}
          </button>
        ))}
      {canWrite && (data.todo.wake_condition || data.todo.review_at) && (
        <form
          className="task-dependency-add"
          onSubmit={(event) => {
            event.preventDefault()
            if (!wakeReason.trim() || action.isPending) return
            action.mutate(
              {
                method: 'todo.wake',
                input: { expected_etag: data.etag, reason: wakeReason.trim() },
              },
              { onSuccess: () => setWakeReason('') },
            )
          }}
        >
          <input
            aria-label="解除等待的原因"
            placeholder="条件已满足的依据"
            value={wakeReason}
            onChange={(event) => setWakeReason(event.target.value)}
            disabled={action.isPending}
          />
          <button className="button" disabled={!wakeReason.trim() || action.isPending}>
            解除等待
          </button>
        </form>
      )}
      {action.error && <Notice error={action.error} />}
    </section>
  )
}

function TaskWaitingEditor({ data, onClose }: { data: TodoDetail; onClose: () => void }) {
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
      setLatest(await call<TodoDetail>('todo.show', { todo_id: data.todo.id }))
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
          { onSuccess: onClose },
        )
      }}
    >
      <label className="field-label" htmlFor="task-wake-condition">
        可观察的等待条件
      </label>
      <input
        id="task-wake-condition"
        value={wake}
        onChange={(event) => setWake(event.target.value)}
        disabled={action.isPending}
        placeholder="例如：评审通过，或依赖任务完成"
      />
      <label className="field-label" htmlFor="task-review-at">
        复查日期
      </label>
      <input
        id="task-review-at"
        type="date"
        value={reviewAt}
        onChange={(event) => setReviewAt(event.target.value)}
        disabled={action.isPending}
      />
      {action.error && <Notice error={action.error} />}
      {conflict && (
        <div className="task-conflict-review">
          <p>当前输入已保留。读取最新等待条件后，再选择要保存的内容。</p>
          <button
            className="button"
            type="button"
            disabled={loading}
            onClick={() => void loadLatest()}
          >
            {loading ? '正在读取…' : '读取最新等待条件'}
          </button>
          {loadError !== undefined && <Notice error={loadError} />}
          {latest && (
            <>
              <p>
                最新条件：{latest.todo.wake_condition || '无'}
                <br />
                最新复查日期：{latest.todo.review_at || '无'}
              </p>
              <div className="task-form-actions">
                <button
                  className="button"
                  type="button"
                  onClick={() => {
                    setWake(latest.todo.wake_condition ?? '')
                    setReviewAt(latest.todo.review_at ?? '')
                    setBaseline(latest.etag)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  采用最新条件
                </button>
                <button
                  className="button"
                  type="button"
                  onClick={() => {
                    setBaseline(latest.etag)
                    setLatest(undefined)
                    action.reset()
                  }}
                >
                  保留当前输入，继续检查
                </button>
              </div>
            </>
          )}
        </div>
      )}
      <div className="task-form-actions">
        <button
          type="button"
          className="button subtle"
          onClick={onClose}
          disabled={action.isPending}
        >
          取消
        </button>
        <button
          className="button primary"
          disabled={action.isPending || conflict || (!wake.trim() && !reviewAt)}
        >
          保存等待条件
        </button>
      </div>
    </form>
  )
}
