import { useState } from 'react'
import { App, Button, Input, Modal, Select } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { BookOpen, Plus, Unlink } from 'lucide-react'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import type { Todo } from '../../types'
import type { KnowledgeDocument } from './knowledge-types'
import './task-knowledge-links.css'

export function knowledgeHref(id: string) {
  return `/knowledge?${new URLSearchParams({ document: id })}`
}
function documentID(url: string) {
  if (!url.startsWith('/knowledge?')) return ''
  return new URLSearchParams(url.slice(url.indexOf('?') + 1)).get('document') || ''
}
function useRefreshLinks() {
  const client = useQueryClient()
  return async (todoID?: string) => {
    await Promise.all([
      client.invalidateQueries({ queryKey: ['knowledge-tasks'] }),
      client.invalidateQueries({ queryKey: ['todos'] }),
      ...(todoID ? [client.invalidateQueries({ queryKey: ['todo', todoID] })] : []),
    ])
  }
}

export function TaskKnowledge({ todo, writable }: { todo: Todo; writable: boolean }) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [selected, setSelected] = useState<string>()
  const refresh = useRefreshLinks()
  const links = (todo.links || []).filter((link) => documentID(link.url))
  const results = useQuery({
    queryKey: ['knowledge-link-search', text],
    enabled: open,
    queryFn: ({ signal }) =>
      call('knowledge.query', { text, exclude_archived: true, limit: 20 }, signal),
  })
  const mutation = useMutation({
    mutationKey: ['todo-write', todo.id],
    mutationFn: ({ id, remove }: { id: string; remove?: boolean }) =>
      remove
        ? call('todo.knowledge.remove', { todo_id: todo.id, document_id: id })
        : call('todo.knowledge.add', { todo_id: todo.id, document_id: id }),
    onSuccess: async () => {
      await refresh(todo.id)
      setOpen(false)
      setSelected(undefined)
    },
  })
  return (
    <section className="work-knowledge-links" aria-label="关联知识">
      <div className="work-knowledge-heading">
        <strong>
          <BookOpen size={15} />
          关联知识
        </strong>
        {writable && (
          <Button
            size="small"
            onClick={() => {
              mutation.reset()
              setOpen(true)
            }}
          >
            <Plus size={14} />
            关联知识
          </Button>
        )}
      </div>
      {!links.length && (
        <p className="work-knowledge-hint">把背景资料和解决方案关联到任务，方便随时查阅。</p>
      )}
      {links.map((link) => (
        <div className="work-knowledge-row" key={link.url}>
          <Link to={knowledgeHref(documentID(link.url))}>{link.title || '查看知识文档'}</Link>
          {writable && (
            <Button
              type="text"
              size="small"
              aria-label={`取消关联 ${link.title || '知识文档'}`}
              disabled={mutation.isPending}
              onClick={() => mutation.mutate({ id: documentID(link.url), remove: true })}
            >
              <Unlink size={14} />
            </Button>
          )}
        </div>
      ))}
      {!open && mutation.error && <Notice error={mutation.error} />}
      <Modal
        title="关联知识"
        open={open}
        onCancel={() => {
          if (!mutation.isPending) setOpen(false)
        }}
        footer={null}
        destroyOnHidden
      >
        <form
          className="work-knowledge-form"
          onSubmit={(event) => {
            event.preventDefault()
            if (selected && !mutation.isPending) mutation.mutate({ id: selected })
          }}
        >
          <Select
            aria-label="选择知识文档"
            showSearch={{ filterOption: false, onSearch: setText }}
            value={selected}
            onChange={setSelected}
            placeholder="搜索或选择知识文档"
            loading={results.isFetching}
            disabled={mutation.isPending}
            options={(results.data?.documents || []).map((doc) => ({
              value: doc.document_id,
              label: doc.title,
              disabled: links.some((link) => documentID(link.url) === doc.document_id),
            }))}
          />
          {results.data?.has_more && (
            <p className="work-knowledge-hint">仅显示前 20 篇，请输入关键词缩小范围。</p>
          )}
          {results.error && <Notice error={results.error} retry={() => void results.refetch()} />}
          {mutation.error && <Notice error={mutation.error} />}
          <Button
            type="primary"
            htmlType="submit"
            loading={mutation.isPending}
            aria-label="保存关联"
            disabled={!selected || mutation.isPending}
          >
            保存关联
          </Button>
        </form>
      </Modal>
    </section>
  )
}

const statusLabels: Record<string, string> = {
  open: '待办',
  in_progress: '进行中',
  review: '待验收',
  done: '已完成',
}
export function KnowledgeTasks({
  document: doc,
  writable,
}: {
  document: KnowledgeDocument
  writable: boolean
}) {
  const [offset, setOffset] = useState(0)
  const [mode, setMode] = useState<'create' | 'link'>()
  const refresh = useRefreshLinks()
  const tasks = useQuery({
    queryKey: ['knowledge-tasks', doc.metadata.id, offset],
    queryFn: ({ signal }) =>
      call('knowledge.tasks', { document_id: doc.metadata.id, offset }, signal),
  })
  return (
    <section className="work-knowledge-links" aria-label="关联任务">
      <div className="work-knowledge-heading">
        <strong>关联任务</strong>
        {writable && (
          <div className="work-knowledge-actions">
            <Button size="small" onClick={() => setMode('link')}>
              关联已有任务
            </Button>
            <Button size="small" onClick={() => setMode('create')}>
              <Plus size={14} />
              创建任务
            </Button>
          </div>
        )}
      </div>
      {tasks.isPending && <p role="status">正在读取关联任务…</p>}
      {tasks.error && <Notice error={tasks.error} retry={() => void tasks.refetch()} />}
      {tasks.data?.items.length === 0 && (
        <p className="work-knowledge-hint">还没有关联任务。可以从这篇知识创建下一步行动。</p>
      )}
      {tasks.data?.items.map((task) => (
        <div className="work-knowledge-row" key={task.id}>
          <Link to={`/tasks/${encodeURIComponent(task.id)}`}>{task.title}</Link>
          <span className="work-knowledge-hint">
            {task.archived ? '已归档' : statusLabels[task.status] || task.status}
          </span>
        </div>
      ))}
      {(offset > 0 || tasks.data?.has_more) && (
        <div className="work-knowledge-actions">
          <Button
            disabled={!offset || tasks.isFetching}
            onClick={() => setOffset(Math.max(0, offset - 50))}
          >
            上一页
          </Button>
          <Button
            disabled={!tasks.data?.has_more || tasks.isFetching}
            onClick={() => setOffset(offset + 50)}
          >
            下一页
          </Button>
        </div>
      )}
      {mode && (
        <KnowledgeTaskDialog
          doc={doc}
          mode={mode}
          onClose={() => setMode(undefined)}
          onSaved={async (id) => {
            setOffset(0)
            await refresh(id)
            setMode(undefined)
          }}
        />
      )}
    </section>
  )
}

function KnowledgeTaskDialog({
  doc,
  mode,
  onClose,
  onSaved,
}: {
  doc: KnowledgeDocument
  mode: 'create' | 'link'
  onClose: () => void
  onSaved: (id: string) => Promise<void>
}) {
  const { message } = App.useApp()
  const [title, setTitle] = useState(`跟进：${doc.metadata.title}`)
  const [description, setDescription] = useState('')
  const [text, setText] = useState('')
  const [selected, setSelected] = useState<string>()
  const [key] = useState(() => crypto.randomUUID())
  const tasks = useQuery({
    queryKey: ['todos', 'knowledge-picker', text],
    enabled: mode === 'link',
    queryFn: ({ signal }) => call('todo.list', { workbench: true, query: text, limit: 20 }, signal),
  })
  const mutation = useMutation({
    mutationFn: async () => {
      if (mode === 'create') {
        const result = await call(
          'todo.create',
          {
            title: title.trim(),
            description,
            project: doc.metadata.projects?.[0] || '',
            knowledge_document_id: doc.metadata.id,
          },
          undefined,
          key,
        )
        for (const warning of result.warnings || []) void message.warning(warning.message)
        return result.todo.id
      }
      await call('todo.knowledge.add', { todo_id: selected!, document_id: doc.metadata.id })
      return selected!
    },
    onSuccess: onSaved,
  })
  return (
    <Modal
      open
      title={mode === 'create' ? '从知识创建任务' : '关联已有任务'}
      onCancel={() => {
        if (!mutation.isPending) onClose()
      }}
      footer={null}
      closable={!mutation.isPending}
      mask={{ closable: !mutation.isPending }}
      keyboard={!mutation.isPending}
    >
      <form
        className="work-knowledge-form"
        onSubmit={(event) => {
          event.preventDefault()
          if (!mutation.isPending) mutation.mutate()
        }}
      >
        <p className="work-knowledge-hint">关联知识：{doc.metadata.title}</p>
        {mode === 'create' ? (
          <>
            <label>
              任务标题
              <Input
                aria-label="任务标题"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                required
                disabled={mutation.isPending}
              />
            </label>
            <label>
              任务说明
              <Input.TextArea
                aria-label="任务说明"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={4}
                placeholder="写下下一步要做什么"
                disabled={mutation.isPending}
              />
            </label>
          </>
        ) : (
          <>
            <Select
              aria-label="选择已有任务"
              showSearch={{ filterOption: false, onSearch: setText }}
              value={selected}
              onChange={setSelected}
              placeholder="搜索或选择任务"
              loading={tasks.isFetching}
              disabled={mutation.isPending}
              options={(tasks.data?.items || []).map((task) => ({
                value: task.id,
                label: `${task.title} · ${statusLabels[task.status] || task.status}`,
              }))}
            />
            {tasks.data?.has_more && (
              <p className="work-knowledge-hint">仅显示前 20 条，请输入关键词缩小范围。</p>
            )}
            {tasks.error && <Notice error={tasks.error} retry={() => void tasks.refetch()} />}
          </>
        )}
        {mutation.error && <Notice error={mutation.error} />}
        <Button
          type="primary"
          htmlType="submit"
          loading={mutation.isPending}
          aria-label={mode === 'create' ? '创建并关联' : '保存关联'}
          disabled={mutation.isPending || (mode === 'create' ? !title.trim() : !selected)}
        >
          {mode === 'create' ? '创建并关联' : '保存关联'}
        </Button>
      </form>
    </Modal>
  )
}
