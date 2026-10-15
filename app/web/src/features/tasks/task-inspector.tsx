import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowUpRight } from 'lucide-react'
import { call } from '../../api'
import { useLiveConnection } from '../../live-connection'
import type { TodoDetail } from '../../types'
import { TaskInlineProperty } from './task-inline-properties'
import { taskActivity, taskActivityLabel } from './task-attention'
import type { SessionStatus } from '../agents/activity-types'
import { taskStatusLabels as statuses } from './task-status'
import { StatusIcon } from './task-status-icon'
import { agentName } from '../../format'
const agentLabel = (value?: string) => (value ? agentName(value) : 'Agent')
const epochDateTime = (value?: number) =>
  value
    ? new Date(value * 1000).toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
    : '—'
export function TaskInspector({ data, canWrite }: { data: TodoDetail; canWrite: boolean }) {
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
              {canWrite ? (
                <TaskInlineProperty data={data} field="status" label="状态" writable />
              ) : (
                <span className={`status-badge status-${todo.status}`}>
                  <StatusIcon todo={todo} />
                  {statuses[todo.status]}
                </span>
              )}
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
