import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Check, LoaderCircle } from 'lucide-react'
import { call, errorText } from '../api'
import type { CollectionSource } from './collection-types'
import type { WorkspaceSettings } from './aiday-settings-types'
import type { KnowledgeCollection } from './knowledge-types'
import './workspace-forms.css'

type SourceDraft = Omit<CollectionSource, 'id' | 'muted'>
export function SourceEditor({
  source,
  onSaved,
  onClose,
}: {
  source?: CollectionSource
  onSaved: (source?: CollectionSource) => Promise<void>
  onClose: () => void
}) {
  const [draft, setDraft] = useState<SourceDraft>(() => ({
    connector: source?.connector || '',
    kind: source?.kind || 'group',
    external_id: source?.external_id || '',
    name: source?.name || '',
    project: source?.project || '',
    instruction: source?.instruction || '',
    exclude_pattern: source?.exclude_pattern || '',
    knowledge_collection: source?.knowledge_collection || '',
    strategy: source?.strategy || 'tasks',
    decision_unit: source?.decision_unit || 'window',
    interval_minutes: source?.interval_minutes || 0,
    priority: source?.priority || 'P2',
    enabled: source?.enabled ?? false,
  }))
  const [confirmDelete, setConfirmDelete] = useState(false)
  const settings = useQuery({
    queryKey: ['settings.get'],
    queryFn: ({ signal }) => call<WorkspaceSettings>('settings.get', {}, signal),
  })
  const catalog = useQuery({
    queryKey: ['knowledge', 'catalog'],
    queryFn: ({ signal }) => call<KnowledgeCollection[]>('knowledge.catalog', {}, signal),
  })
  const connectors = [
    ...new Set([
      ...(settings.data?.providers
        .filter((provider) => provider.kind === 'collection')
        .map((provider) => provider.name) || []),
      ...(source ? [source.connector] : []),
    ]),
  ]
  const save = useMutation({
    mutationFn: () =>
      call<{ source: CollectionSource }>('collect.source.save', {
        ...draft,
        external_id: draft.external_id.trim(),
      }),
    retry: false,
    onSuccess: (result) => onSaved(result.source),
  })
  const remove = useMutation({
    mutationFn: () => call('collect.source.delete', { source_id: source?.id, confirmed: true }),
    retry: false,
    onSuccess: () => onSaved(),
  })
  const busy = save.isPending || remove.isPending
  const patch = <K extends keyof SourceDraft>(key: K, value: SourceDraft[K]) => {
    setDraft((previous) => ({ ...previous, [key]: value }))
    save.reset()
  }
  return (
    <form
      className="workspace-form collection-source-editor"
      onSubmit={(event) => {
        event.preventDefault()
        if (!busy) save.mutate()
      }}
    >
      <h2>{source ? '编辑采集来源' : '新增采集来源'}</h2>
      <p className="workspace-form-hint">设置消息来源及处理方式，保存后不会立即执行采集。</p>
      <label>
        连接
        <select
          value={draft.connector}
          required
          disabled={busy || !!source}
          onChange={(event) => patch('connector', event.target.value)}
        >
          <option value="">选择已有连接</option>
          {connectors.map((connector) => (
            <option key={connector} value={connector}>
              {connector}
            </option>
          ))}
        </select>
      </label>
      {settings.error && (
        <p className="as-inline-error" role="alert">
          {errorText(settings.error)}
        </p>
      )}
      {!source && settings.data && !connectors.length && (
        <p className="workspace-form-hint">本机尚未登记采集连接，请先配置连接后刷新此页面。</p>
      )}
      <div className="workspace-form-grid">
        <label>
          来源类型
          <input
            value={draft.kind}
            required
            disabled={busy || !!source}
            maxLength={80}
            pattern="[a-z][a-z0-9_-]*"
            placeholder="如 group"
            onChange={(event) => patch('kind', event.target.value)}
          />
        </label>
        <label>
          外部会话 ID
          <input
            value={draft.external_id}
            required
            disabled={busy || !!source}
            maxLength={1000}
            onChange={(event) => patch('external_id', event.target.value)}
          />
        </label>
        <label>
          显示名称
          <input
            value={draft.name}
            disabled={busy}
            maxLength={500}
            onChange={(event) => patch('name', event.target.value)}
          />
        </label>
        <label>
          关联项目
          <input
            value={draft.project}
            disabled={busy}
            maxLength={200}
            onChange={(event) => patch('project', event.target.value)}
          />
        </label>
      </div>
      <label className="workspace-check">
        <span>
          启用自动采集<small>按采集间隔运行；可随时停用。</small>
        </span>
        <input
          type="checkbox"
          checked={draft.enabled}
          disabled={busy}
          onChange={(event) => patch('enabled', event.target.checked)}
        />
      </label>
      <label>
        处理方式
        <select
          value={draft.strategy}
          disabled={busy}
          onChange={(event) => patch('strategy', event.target.value)}
        >
          <option value="tasks">任务跟进</option>
          <option value="observe">观察与总结</option>
        </select>
      </label>
      <label>
        决策单位
        <select
          value={draft.decision_unit}
          disabled={busy}
          onChange={(event) => patch('decision_unit', event.target.value)}
        >
          <option value="window">对话窗口</option>
          <option value="message">逐条消息</option>
        </select>
      </label>
      <div className="workspace-form-grid">
        <label>
          采集间隔（分钟）
          <input
            type="number"
            required
            min={0}
            max={1440}
            value={draft.interval_minutes}
            disabled={busy}
            onChange={(event) => patch('interval_minutes', Number(event.target.value))}
          />
          <small className="workspace-form-hint">0 使用处理方式的默认间隔。</small>
        </label>
        <label>
          默认任务优先级
          <select
            value={draft.priority}
            disabled={busy}
            onChange={(event) => patch('priority', event.target.value)}
          >
            {['P0', 'P1', 'P2', 'P3'].map((value) => (
              <option key={value}>{value}</option>
            ))}
          </select>
        </label>
      </div>
      <label>
        关注指令
        <textarea
          value={draft.instruction}
          rows={5}
          maxLength={16000}
          disabled={busy}
          onChange={(event) => patch('instruction', event.target.value)}
          placeholder="希望关注的人、主题和需要发现的待办"
        />
      </label>
      <label>
        排除规则
        <input
          value={draft.exclude_pattern}
          maxLength={2000}
          disabled={busy}
          onChange={(event) => patch('exclude_pattern', event.target.value)}
          placeholder="用于跳过消息的正则表达式，可留空"
        />
      </label>
      <label>
        结论保存到知识集合
        <select
          value={draft.knowledge_collection}
          disabled={busy}
          onChange={(event) => patch('knowledge_collection', event.target.value)}
        >
          <option value="">不指定集合</option>
          {catalog.data?.map((item) => (
            <option key={item.id} value={item.id}>
              {item.name || item.id}
            </option>
          ))}
          {draft.knowledge_collection &&
            !catalog.data?.some((item) => item.id === draft.knowledge_collection) && (
              <option value={draft.knowledge_collection}>{draft.knowledge_collection}</option>
            )}
        </select>
      </label>
      {save.error && (
        <p className="as-inline-error" role="alert">
          {errorText(save.error)}
        </p>
      )}
      {remove.error && (
        <p className="as-inline-error" role="alert">
          {errorText(remove.error)}
        </p>
      )}
      {confirmDelete && (
        <div className="workspace-form-confirm">
          <p>
            删除“{source?.name || source?.external_id}”的采集配置？此操作会停止这个来源的后续采集。
          </p>
          <div className="workspace-actions">
            <button
              type="button"
              className="button"
              disabled={busy}
              onClick={() => remove.mutate()}
            >
              确认删除来源
            </button>
            <button
              type="button"
              className="text-button"
              disabled={busy}
              onClick={() => setConfirmDelete(false)}
            >
              保留来源
            </button>
          </div>
        </div>
      )}
      <div className="workspace-form-footer">
        {source && (
          <button
            type="button"
            className="text-button"
            disabled={busy}
            onClick={() => setConfirmDelete(true)}
          >
            删除来源
          </button>
        )}
        <button type="button" className="button" disabled={busy} onClick={onClose}>
          取消
        </button>
        <button
          type="submit"
          className="button primary"
          disabled={busy || !draft.connector || !draft.kind.trim() || !draft.external_id.trim()}
        >
          {busy ? <LoaderCircle size={14} className="spin" /> : <Check size={14} />}保存来源
        </button>
      </div>
    </form>
  )
}
