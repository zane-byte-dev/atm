import { Select } from './components/select'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Pencil, Check, X } from 'lucide-react'
import { call } from './api'
import { Notice } from './markdown'
import type { TodoDetail } from './types'

export function TaskInlineProperty({
  data,
  field,
  label,
  writable,
}: {
  data: TodoDetail
  field: 'priority' | 'project'
  label: string
  writable: boolean
}) {
  const [draft, setDraft] = useState<{ value: string; etag: string }>()
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: () =>
      call('todo.update', {
        todo_id: data.todo.id,
        expected_etag: draft!.etag,
        ...(field === 'priority'
          ? { priority: draft!.value.trim() }
          : { project: draft!.value.trim() }),
      }),
    onSuccess: async () => {
      setDraft(undefined)
      await Promise.all([
        client.invalidateQueries({ queryKey: ['todo', data.todo.id] }),
        client.invalidateQueries({ queryKey: ['todos'] }),
        client.invalidateQueries({ queryKey: ['global-search'] }),
      ])
    },
  })
  const value = data.todo[field] || ''
  if (!writable) return <span>{value || '未分配'}</span>
  if (!draft)
    return (
      <button
        className="inline-property-trigger"
        aria-label={`编辑${label}`}
        title={`编辑${label}`}
        onClick={() => {
          mutation.reset()
          setDraft({ value, etag: data.etag })
        }}
      >
        {value || '未分配'}
        <Pencil size={12} />
      </button>
    )
  return (
    <form
      className="inline-property-form"
      onSubmit={(e) => {
        e.preventDefault()
        if (!mutation.isPending) mutation.mutate()
      }}
    >
      {field === 'priority' ? (
        <Select
          autoFocus
          aria-label={label}
          value={draft.value}
          disabled={mutation.isPending}
          onChange={(e) => setDraft({ ...draft, value: e.target.value })}
        >
          {['P0', 'P1', 'P2'].map((p) => (
            <option key={p}>{p}</option>
          ))}
        </Select>
      ) : (
        <input
          autoFocus
          aria-label={label}
          maxLength={200}
          value={draft.value}
          disabled={mutation.isPending}
          onChange={(e) => setDraft({ ...draft, value: e.target.value })}
        />
      )}
      <button
        title="保存属性"
        aria-label={`保存${label}`}
        disabled={mutation.isPending || draft.value === value}
      >
        <Check size={14} />
      </button>
      <button
        type="button"
        title="取消编辑"
        aria-label={`取消编辑${label}`}
        disabled={mutation.isPending}
        onClick={() => setDraft(undefined)}
      >
        <X size={14} />
      </button>
      {mutation.error && (
        <Notice
          error={mutation.error}
          retry={() => {
            mutation.reset()
            setDraft({ value, etag: data.etag })
          }}
        />
      )}
    </form>
  )
}
