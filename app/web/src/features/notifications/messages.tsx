import { Button, Card, Empty, Popconfirm, Segmented, Tag, Tooltip } from 'antd'
import { CheckCheck, Trash2 } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams, Link } from 'react-router'
import { useWorkspaceMessages, useWorkspaceNotifications } from './workspace-notifications'
import { call, errorText } from '../../api'
import type { Bootstrap } from '../../types'
import type { RuntimeJob } from './runtime-job-model'
import { jobActive, jobsEnabled } from './runtime-job-model'
import { notifyRuntimeJob } from './runtime-job-notifications'
import { notificationStore } from './notification-store'
import {
  WorkspaceSplitLayout,
  WorkspaceNavigator,
  WorkspaceNavigatorHeading,
} from '../../components/workspace-navigator'
import { WorkspaceListItem } from '../../components/workspace-list-item'
import './messages.css'
const labels = { success: '已完成', error: '失败', info: '通知', warning: '提醒' }
const colors = {
  success: 'success',
  error: 'error',
  info: 'processing',
  warning: 'warning',
} as const
const time = (value: number) =>
  new Date(value).toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
function JobAction({ id, boot }: { id: string; boot: Bootstrap }) {
  const client = useQueryClient()
  const notification = useWorkspaceNotifications()
  const jobs = useQuery({
    queryKey: ['runtime-jobs'],
    queryFn: ({ signal }) => call<{ jobs: RuntimeJob[] }>('jobs.list', { limit: 30 }, signal),
    enabled: jobsEnabled(boot),
  })
  const job = jobs.data?.jobs.find((item) => item.id === id)
  const cancel = useMutation({
    mutationFn: () => call<RuntimeJob>('jobs.cancel', { job_id: id }),
    onSuccess: (result) => {
      notifyRuntimeJob(notification, result)
      void client.invalidateQueries({ queryKey: ['runtime-jobs'] })
    },
    onError: (error) =>
      notification.error({ title: '取消任务失败', description: errorText(error), duration: 0 }),
  })
  return jobsEnabled(boot) && job && jobActive(job) ? (
    <Button
      loading={cancel.isPending}
      disabled={job.cancel_requested}
      onClick={() => cancel.mutate()}
    >
      {job.cancel_requested ? '正在取消…' : '取消任务'}
    </Button>
  ) : null
}
export function MessagesWorkspace({ boot }: { boot: Bootstrap }) {
  const messages = useWorkspaceMessages()
  const [params, setParams] = useSearchParams()
  const filter = ['all', 'activity'].includes(params.get('filter') || '')
    ? params.get('filter')!
    : 'unread'
  const unread = filter === 'unread'
  const visible = messages.filter((item) =>
    filter === 'activity' ? item.activity : !unread || !item.readAt,
  )
  const selected = messages.find((item) => item.id === params.get('message'))
  return (
    <WorkspaceSplitLayout className="messages-workspace">
      <WorkspaceNavigator variant="list" className="messages-navigator">
        <WorkspaceNavigatorHeading
          title="消息"
          actions={
            <>
              <Tooltip title="全部标为已读">
                <Button
                  type="text"
                  aria-label="全部标为已读"
                  disabled={!messages.some((item) => !item.readAt)}
                  onClick={() => notificationStore.read()}
                  icon={<CheckCheck size={15} />}
                />
              </Tooltip>
              <Popconfirm
                title="清除所有消息记录？"
                onConfirm={() => {
                  notificationStore.clear()
                  setParams({})
                }}
                disabled={!messages.length}
              >
                <Tooltip title="清除消息">
                  <Button
                    type="text"
                    aria-label="清除消息"
                    disabled={!messages.length}
                    icon={<Trash2 size={15} />}
                  />
                </Tooltip>
              </Popconfirm>
            </>
          }
        />
        <div className="messages-filter">
          <Segmented
            block
            aria-label="消息筛选"
            value={filter}
            options={[
              { label: `全部 ${messages.length}`, value: 'all' },
              { label: `未读 ${messages.filter((item) => !item.readAt).length}`, value: 'unread' },
              { label: '运行记录', value: 'activity' },
            ]}
            onChange={(value) => setParams({ filter: String(value) })}
          />
        </div>
        <div className="messages-list">
          {visible.map((item) => (
            <WorkspaceListItem
              key={item.id}
              title={item.title}
              label={item.activity ? '后台运行' : labels[item.kind]}
              context={item.description || '操作状态更新'}
              timestamp={time(item.createdAt)}
              unread={!item.readAt}
              tone={item.kind === 'error' ? 'danger' : 'default'}
              selected={selected?.id === item.id}
              onClick={() => {
                notificationStore.read(item.id)
                setParams({ filter, message: item.id })
              }}
            />
          ))}
          {!visible.length && (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                unread ? '没有未读消息' : filter === 'activity' ? '暂无后台运行记录' : '暂无消息'
              }
            />
          )}
        </div>
      </WorkspaceNavigator>
      <section className="messages-detail" aria-label="消息详情">
        {selected ? (
          <Card
            title={selected.title}
            extra={<Tag color={colors[selected.kind]}>{labels[selected.kind]}</Tag>}
          >
            {unread && selected.readAt && (
              <p className="collection-muted">这条消息已读，详情保留供你查看。</p>
            )}
            <time>{new Date(selected.createdAt).toLocaleString('zh-CN')}</time>
            {selected.description && <p>{selected.description}</p>}
            {selected.id.startsWith('runtime-job-') && (
              <JobAction id={selected.id.slice('runtime-job-'.length)} boot={boot} />
            )}
            {selected.href && <Link to={selected.href}>查看运行状态</Link>}
          </Card>
        ) : (
          <Empty description="选择一条消息查看详情" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        )}
      </section>
    </WorkspaceSplitLayout>
  )
}
