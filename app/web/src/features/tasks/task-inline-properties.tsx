import { taskStatusLabels as statusLabels } from './task-status'
import { Select } from 'antd'
import { useIsMutating, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { call, errorText } from '../../api'
import type { TodoDetail } from '../../types'

export function TaskInlineProperty({
  data,
  field,
  label,
  writable,
}: {
  data: TodoDetail
  field: 'priority' | 'project' | 'status'
  label: string
  writable: boolean
}) {
  const client = useQueryClient()
  const mutationKey = ['todo-write', data.todo.id]
  const busy = useIsMutating({ mutationKey }) > 0
  const projects = useQuery({
    queryKey: ['todos', 'projects'],
    queryFn: () => call('todo.list', { limit: 1, offset: 0 }),
    staleTime: 60_000,
    enabled: writable && field === 'project',
  })
  const mutation = useMutation({
    mutationKey,
    mutationFn: (value: string) => {
      if (field === 'status') {
        const todo_id = data.todo.id
        if (value === 'open')
          return call('todo.return_to_open', { todo_id, expected_etag: data.etag })
        if (value === 'review')
          return call('todo.submit', { todo_id, reason: '用户在 Web 工作区设为待验收' })
        if (value === 'done') return call('todo.done', { todo_id })
        return call('todo.start', { todo_id, reopen_reason: '用户在 Web 工作区切换为进行中' })
      }
      return call('todo.update', {
        todo_id: data.todo.id,
        expected_etag: data.etag,
        ...(field === 'priority' ? { priority: value } : { project: value }),
      })
    },
    onSuccess: async (result) => {
      client.setQueryData<TodoDetail>(['todo', data.todo.id], (current) =>
        current ? { ...current, todo: result.todo, etag: result.etag } : current,
      )
      await Promise.all([
        client.invalidateQueries({ queryKey: ['todo', data.todo.id] }),
        client.invalidateQueries({ queryKey: ['todos'] }),
        client.invalidateQueries({ queryKey: ['global-search'] }),
        client.invalidateQueries({ queryKey: ['doc', data.todo.id] }),
        client.invalidateQueries({ queryKey: ['activity-status'] }),
      ])
    },
    onError: () => client.invalidateQueries({ queryKey: ['todo', data.todo.id] }),
  })
  const value = data.todo[field] || ''
  if (!writable)
    return <span>{field === 'status' ? statusLabels[data.todo.status] : value || '未分配'}</span>
  const options =
    field === 'status'
      ? Object.entries(statusLabels).map(([value, label]) => ({
          value,
          label,
          disabled:
            value !== data.todo.status &&
            ((value === 'open' && data.todo.status !== 'in_progress') ||
              (value === 'review' && data.todo.status !== 'in_progress')),
          title:
            (value === 'open' || value === 'review') &&
            value !== data.todo.status &&
            data.todo.status !== 'in_progress'
              ? '请先切换为进行中'
              : label,
        }))
      : field === 'priority'
        ? ['P0', 'P1', 'P2'].map((value) => ({ value, label: value }))
        : [...new Set(['', value, ...(projects.data?.projects ?? [])])].map((value) => ({
            value,
            label: value || '未分配',
          }))
  return (
    <div className="inline-property-select">
      <Select
        aria-label={label}
        value={mutation.isPending ? mutation.variables : value}
        options={options}
        disabled={busy}
        loading={mutation.isPending || projects.isFetching}
        showSearch={field === 'project'}
        optionFilterProp="label"
        notFoundContent="没有匹配的项目"
        status={mutation.isError ? 'error' : undefined}
        onChange={(next) => {
          if (next !== value && !busy) mutation.mutate(next)
        }}
      />
      {mutation.error && <small role="alert">{errorText(mutation.error)}</small>}
      {projects.error && field === 'project' && (
        <small role="alert">项目列表加载失败，请稍后重新打开任务。</small>
      )}
    </div>
  )
}
