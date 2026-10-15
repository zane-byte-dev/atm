import { useRef, useState } from 'react'
import { Alert, Button, Empty, Input, Modal } from 'antd'
import { ArrowDown, ArrowUp, GripVertical, Pencil, Trash2 } from 'lucide-react'
import { useQueryClient } from '@tanstack/react-query'
import { call, errorText } from '../../api'
import { Select } from '../../components/select'
import { Notice } from '../../components/notice'
import type { KnowledgeCollection } from './knowledge-types'
import type { CollectionOrder } from './use-collection-order'
import './knowledge-collection-manager.css'

type Action = { kind: 'edit' | 'delete'; item: KnowledgeCollection }
export function KnowledgeCollectionManager({
  order,
  writable,
  onClose,
  onDeleted,
}: {
  order: CollectionOrder
  writable: boolean
  onClose: () => void
  onDeleted: (id: string, target: string) => void
}) {
  const client = useQueryClient()
  const [action, setAction] = useState<Action | null>(null)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [target, setTarget] = useState('')
  const [pending, setPending] = useState(false)
  const running = useRef(false)
  const [error, setError] = useState('')
  const [success, setSuccess] = useState('')
  const begin = (kind: Action['kind'], item: KnowledgeCollection) => {
    setAction({ kind, item })
    setName(item.name || item.id)
    setDescription(item.description || '')
    setTarget('')
    setError('')
    setSuccess('')
  }
  const actionLabel =
    action?.kind === 'edit'
      ? '保存'
      : action && action.item.document_count > 0
        ? '移动文档并删除集合'
        : '确认删除空集合'
  const save = async () => {
    if (!action || !writable || running.current) return
    running.current = true
    setPending(true)
    setError('')
    try {
      if (action.kind === 'edit') {
        await call('knowledge.collection.update', {
          id: action.item.id,
          name: name.trim(),
          description,
        })
        setSuccess('集合信息已保存。')
      } else {
        await call('knowledge.collection.delete', {
          id: action.item.id,
          move_to: target || undefined,
          confirmed: true,
        })
        onDeleted(action.item.id, target)
        setSuccess(target ? '文档已移动，集合已删除。' : '空集合已删除。')
      }
      setAction(null)
      await client.invalidateQueries({ queryKey: ['knowledge'] })
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      running.current = false
      setPending(false)
    }
  }
  return (
    <Modal
      open
      title="集合管理"
      width={720}
      footer={null}
      onCancel={() => {
        if (!running.current) onClose()
      }}
      closable={!pending}
      mask={{ closable: false }}
      keyboard={!pending}
    >
      <div className="knowledge-collection-manager">
        <p className="collection-manager-hint">
          拖动手柄或使用上下移动按钮调整顺序。排序保存在此浏览器。
        </p>
        {order.error && <Notice error={order.error} />}
        {success && <Alert type="success" title={success} showIcon />}
        {!order.rows.length && <Empty description="暂无集合，可在知识侧栏新建集合。" />}
        <div className="collection-manager-list">
          {order.rows.map((item, index) => (
            <div
              key={item.id}
              className="collection-manager-row"
              data-collection-id={item.id}
              {...(!action && !pending ? order.dropProps(item.id) : {})}
            >
              <Button
                type="text"
                className="collection-drag-handle"
                aria-label={`拖动${item.name || item.id}`}
                title="拖动排序"
                disabled={!!action || pending}
                {...(!action && !pending ? order.handleProps(item.id) : {})}
              >
                <GripVertical size={16} />
              </Button>
              <div className="collection-manager-summary">
                <strong>{item.name || item.id}</strong>
                <small>
                  {item.id} · {item.document_count} 篇文档
                </small>
                {item.description && <p>{item.description}</p>}
              </div>
              <div className="collection-manager-actions">
                <Button
                  type="text"
                  aria-label={`上移${item.name || item.id}`}
                  title="上移"
                  disabled={index === 0 || !!action || pending}
                  onClick={() => order.move(item.id, order.rows[index - 1].id)}
                >
                  <ArrowUp size={15} />
                </Button>
                <Button
                  type="text"
                  aria-label={`下移${item.name || item.id}`}
                  title="下移"
                  disabled={index === order.rows.length - 1 || !!action || pending}
                  onClick={() => order.move(item.id, order.rows[index + 1].id)}
                >
                  <ArrowDown size={15} />
                </Button>
                {writable && (
                  <>
                    <Button
                      type="text"
                      aria-label={`编辑${item.name || item.id}`}
                      title="重命名与说明"
                      disabled={pending || !!action}
                      onClick={() => begin('edit', item)}
                    >
                      <Pencil size={15} />
                    </Button>
                    <Button
                      type="text"
                      danger
                      aria-label={`删除${item.name || item.id}`}
                      title="删除集合"
                      disabled={pending || !!action}
                      onClick={() => begin('delete', item)}
                    >
                      <Trash2 size={15} />
                    </Button>
                  </>
                )}
              </div>
            </div>
          ))}
        </div>
        {action && (
          <form
            className="collection-manager-editor"
            onSubmit={(event) => {
              event.preventDefault()
              void save()
            }}
          >
            <h3>
              {action.kind === 'edit' ? '编辑集合' : '删除集合'} ·{' '}
              {action.item.name || action.item.id}
            </h3>
            {action.kind === 'edit' ? (
              <>
                <label>
                  集合名称
                  <Input
                    aria-label="集合名称"
                    value={name}
                    disabled={pending}
                    onChange={(event) => setName(event.target.value)}
                  />
                </label>
                <label>
                  说明
                  <Input.TextArea
                    aria-label="集合说明"
                    value={description}
                    disabled={pending}
                    rows={3}
                    onChange={(event) => setDescription(event.target.value)}
                  />
                </label>
                <small>重命名只修改显示名称，集合 ID 和文档链接保持不变。</small>
              </>
            ) : (
              <>
                <p>
                  {action.item.document_count > 0
                    ? `此集合包含 ${action.item.document_count} 篇文档（含已归档）。请选择接收集合，文档移动后再删除原集合。`
                    : '确认删除这个空集合？'}
                </p>
                {action.item.document_count > 0 && (
                  <label>
                    文档移至
                    <Select
                      aria-label="接收集合"
                      value={target}
                      disabled={pending}
                      onChange={(event) => setTarget(event.target.value)}
                    >
                      <option value="">选择接收集合</option>
                      {order.rows
                        .filter((item) => item.id !== action.item.id)
                        .map((item) => (
                          <option value={item.id} key={item.id}>
                            {item.name || item.id}
                          </option>
                        ))}
                    </Select>
                  </label>
                )}
                {action.item.document_count > 0 && order.rows.length < 2 && (
                  <Alert type="info" title="请先新建一个集合来接收文档。" />
                )}
              </>
            )}
            {error && <Alert type="error" title={error} showIcon />}
            <div className="collection-manager-footer">
              <Button
                disabled={pending}
                onClick={() => {
                  setAction(null)
                  setError('')
                }}
              >
                取消
              </Button>
              <Button
                type="primary"
                danger={action.kind === 'delete'}
                htmlType="submit"
                loading={pending}
                aria-label={actionLabel}
                aria-busy={pending}
                disabled={
                  pending ||
                  (action.kind === 'edit'
                    ? !name.trim()
                    : action.item.document_count > 0 && !target)
                }
              >
                {actionLabel}
              </Button>
            </div>
          </form>
        )}
        {!writable && <p className="collection-manager-hint">当前为只读模式，可以调整本地顺序。</p>}
      </div>
    </Modal>
  )
}
