import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, LoaderCircle } from 'lucide-react'
import { ApiError, call, errorText } from '../api'
import type { WorkspaceSettings } from './aiday-settings-types'
import './workspace-forms.css'

type BusinessFields = {
  owner_name: string
  grok_live_quota: boolean
  collection_enabled: boolean
  collection_interval_minutes: number
  collection_lookback_minutes: number
  collection_message_retention_days: number
  todo_refine_on_add: boolean
  todo_refine_prompt: string
  text_model_name: string
  text_model_source: string
  text_model_base_url: string
}
function fields(data: WorkspaceSettings): BusinessFields {
  return {
    owner_name: data.owner_name,
    ...data.preferences,
    todo_refine_prompt: data.preferences.todo_refine_prompt || '',
    text_model_name: data.model.name || '',
    text_model_source: data.model.source || '',
    text_model_base_url: data.model.base_url || '',
  }
}
function useBusinessForm(data: WorkspaceSettings) {
  const client = useQueryClient()
  const [base, setBase] = useState(data)
  const [draft, setDraft] = useState(() => fields(data))
  const [saved, setSaved] = useState(false)
  const baseFields = fields(base)
  const changes = Object.fromEntries(
    Object.entries(draft).filter(
      ([key, value]) => value !== baseFields[key as keyof BusinessFields],
    ),
  )
  const dirty = Object.keys(changes).length > 0
  useEffect(() => {
    if (!dirty) {
      setBase(data)
      setDraft(fields(data))
    }
  }, [data, dirty])
  const save = useMutation({
    mutationFn: () =>
      call<WorkspaceSettings>('settings.business.save', { revision: base.revision, ...changes }),
    retry: false,
    onSuccess: (result) => {
      client.setQueryData(['settings.get'], result)
      setBase(result)
      setDraft(fields(result))
      setSaved(true)
    },
  })
  const latest = useMutation({ mutationFn: () => call<WorkspaceSettings>('settings.get', {}) })
  const patch = <K extends keyof BusinessFields>(key: K, value: BusinessFields[K]) => {
    setDraft((previous) => ({ ...previous, [key]: value }))
    setSaved(false)
    save.reset()
    latest.reset()
  }
  const feedback = (
    <>
      {save.error instanceof ApiError && save.error.status === 409 ? (
        <div className="workspace-conflict" role="alert">
          <p>设置已在其他地方更新。你的输入已保留，请先查看最新值再合并保存。</p>
          {!latest.data && (
            <button
              type="button"
              className="button"
              disabled={latest.isPending}
              onClick={() => latest.mutate()}
            >
              查看最新设置
            </button>
          )}
          {latest.data && (
            <>
              <dl>
                {Object.keys(changes).map((key) => (
                  <div key={key}>
                    <dt>{fieldNames[key as keyof BusinessFields]}</dt>
                    <dd>{String(fields(latest.data!)[key as keyof BusinessFields]) || '未设置'}</dd>
                  </div>
                ))}
              </dl>
              <button
                type="button"
                className="button"
                onClick={() => {
                  const result = latest.data!
                  setBase(result)
                  setDraft({ ...fields(result), ...changes })
                  save.reset()
                  latest.reset()
                }}
              >
                保留以上字段的输入，合并其他最新设置
              </button>
            </>
          )}
          {latest.error && <p>{errorText(latest.error)}</p>}
        </div>
      ) : save.error ? (
        <p className="as-inline-error" role="alert">
          {errorText(save.error)}
        </p>
      ) : null}
      {saved && (
        <p className="as-save-status" role="status">
          <Check size={13} />
          设置已保存
        </p>
      )}
    </>
  )
  return { draft, patch, save, dirty, feedback }
}
const fieldNames: Record<keyof BusinessFields, string> = {
  owner_name: '称呼',
  grok_live_quota: 'Grok 实时额度',
  collection_enabled: '自动采集',
  collection_interval_minutes: '采集间隔',
  collection_lookback_minutes: '采集回看范围',
  collection_message_retention_days: '消息保留天数',
  todo_refine_on_add: '自动优化任务',
  todo_refine_prompt: '任务优化指令',
  text_model_name: '模型名称',
  text_model_source: '来源名称',
  text_model_base_url: '服务地址',
}
function SaveButton({ pending, disabled }: { pending: boolean; disabled: boolean }) {
  return (
    <button type="submit" className="button primary" disabled={disabled || pending}>
      {pending ? <LoaderCircle size={14} className="spin" /> : <Check size={14} />}保存设置
    </button>
  )
}
export function BusinessPreferences({
  data,
  writable,
}: {
  data: WorkspaceSettings
  writable: boolean
}) {
  const { draft, patch, save, dirty, feedback } = useBusinessForm(data)
  const disabled = !writable || save.isPending
  return (
    <form
      className="workspace-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (!disabled && dirty) save.mutate()
      }}
    >
      <div className="as-section-title">
        <div>
          <h2>通用偏好</h2>
          <p>个人信息、任务整理与自动采集。</p>
        </div>
      </div>
      <div className="as-card workspace-form">
        <h3>个人信息</h3>
        <label>
          如何称呼你
          <input
            value={draft.owner_name}
            maxLength={80}
            disabled={disabled}
            onChange={(event) => patch('owner_name', event.target.value)}
          />
        </label>
        <p className="workspace-form-hint">用于任务中的“我”。统计时区：{data.timezone}。</p>
      </div>
      <div className="as-card workspace-form">
        <h3>任务与自动整理</h3>
        <label className="workspace-check">
          <span>
            创建任务后自动优化<small>使用已配置的文本模型整理新任务。</small>
          </span>
          <input
            type="checkbox"
            checked={draft.todo_refine_on_add}
            disabled={disabled}
            onChange={(event) => patch('todo_refine_on_add', event.target.checked)}
          />
        </label>
        <label>
          任务优化指令
          <textarea
            rows={4}
            maxLength={32000}
            value={draft.todo_refine_prompt}
            disabled={disabled}
            onChange={(event) => patch('todo_refine_prompt', event.target.value)}
            placeholder="留空使用默认指令"
          />
        </label>
        <label className="workspace-check">
          <span>
            自动采集<small>按已启用来源的规则整理消息与发现待办。</small>
          </span>
          <input
            type="checkbox"
            checked={draft.collection_enabled}
            disabled={disabled}
            onChange={(event) => patch('collection_enabled', event.target.checked)}
          />
        </label>
        <div className="workspace-form-grid">
          <label>
            采集间隔（分钟）
            <input
              type="number"
              required
              min={1}
              max={1440}
              value={draft.collection_interval_minutes}
              disabled={disabled}
              onChange={(event) => patch('collection_interval_minutes', Number(event.target.value))}
            />
          </label>
          <label>
            采集回看范围（分钟）
            <input
              type="number"
              required
              min={1}
              max={10080}
              value={draft.collection_lookback_minutes}
              disabled={disabled}
              onChange={(event) => patch('collection_lookback_minutes', Number(event.target.value))}
            />
          </label>
          <label>
            消息保留天数
            <input
              type="number"
              required
              min={0}
              max={3650}
              value={draft.collection_message_retention_days}
              disabled={disabled}
              onChange={(event) =>
                patch('collection_message_retention_days', Number(event.target.value))
              }
            />
            <small className="workspace-form-hint">0 表示长期保留。</small>
          </label>
        </div>
        <label className="workspace-check">
          <span>
            Grok 实时额度<small>允许额度服务读取实时账单状态。</small>
          </span>
          <input
            type="checkbox"
            checked={draft.grok_live_quota}
            disabled={disabled}
            onChange={(event) => patch('grok_live_quota', event.target.checked)}
          />
        </label>
      </div>
      {feedback}
      <div className="workspace-form-footer">
        <span>{writable ? '保存后应用到本机服务。' : '当前连接仅可查看设置。'}</span>
        <SaveButton pending={save.isPending} disabled={!writable || !dirty} />
      </div>
    </form>
  )
}
export function ModelSettings({ data, writable }: { data: WorkspaceSettings; writable: boolean }) {
  const client = useQueryClient()
  const { draft, patch, save, dirty, feedback } = useBusinessForm(data)
  const [key, setKey] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const credentials = useMutation({
    mutationFn: (remove: boolean) =>
      call<{ configured: boolean }>(
        remove ? 'settings.credential.delete' : 'settings.credential.save',
        remove ? {} : { api_key: key },
      ),
    retry: false,
    onSuccess: () => {
      setKey('')
      setConfirmDelete(false)
      void client.invalidateQueries({ queryKey: ['settings.get'] })
    },
  })
  const disabled = !writable || save.isPending
  return (
    <>
      <div className="as-section-title">
        <div>
          <h2>模型与连接</h2>
          <p>配置用于任务整理与采集分析的文本服务。</p>
        </div>
      </div>
      <form
        className="as-card workspace-form"
        onSubmit={(event) => {
          event.preventDefault()
          if (!disabled && dirty) save.mutate()
        }}
      >
        <h3>文本模型</h3>
        <label>
          服务地址
          <input
            type="url"
            value={draft.text_model_base_url}
            disabled={disabled}
            maxLength={2048}
            onChange={(event) => patch('text_model_base_url', event.target.value)}
            placeholder="https://api.example.com/v1"
          />
        </label>
        <div className="workspace-form-grid">
          <label>
            模型名称
            <input
              value={draft.text_model_name}
              disabled={disabled}
              maxLength={200}
              onChange={(event) => patch('text_model_name', event.target.value)}
            />
          </label>
          <label>
            来源名称
            <input
              value={draft.text_model_source}
              disabled={disabled}
              maxLength={200}
              onChange={(event) => patch('text_model_source', event.target.value)}
            />
          </label>
        </div>
        {feedback}
        <div className="workspace-form-footer">
          <span>保存配置不会发起模型请求。</span>
          <SaveButton pending={save.isPending} disabled={!writable || !dirty} />
        </div>
      </form>
      <div className="as-card workspace-form">
        <h3>访问凭证</h3>
        <p className="workspace-form-hint">
          {data.model.credential_status === 'unavailable'
            ? '暂时无法读取凭证状态'
            : data.model.credential_configured
              ? '已配置凭证。输入新的 API Key 可替换。'
              : '尚未配置凭证。'}
          已有凭证不会回传到页面。
        </p>
        <form
          className="workspace-form"
          onSubmit={(event) => {
            event.preventDefault()
            if (writable && key.trim() && !credentials.isPending) credentials.mutate(false)
          }}
        >
          <label>
            新的 API Key
            <input
              type="password"
              value={key}
              autoComplete="new-password"
              spellCheck={false}
              maxLength={16384}
              disabled={!writable || credentials.isPending}
              onChange={(event) => {
                setKey(event.target.value)
                credentials.reset()
              }}
              placeholder="输入新凭证，留空保持不变"
            />
          </label>
          <div className="workspace-actions">
            <button
              type="submit"
              className="button"
              disabled={!writable || !key.trim() || credentials.isPending}
            >
              保存凭证
            </button>
            {data.model.credential_configured && (
              <button
                type="button"
                className="text-button"
                disabled={!writable || credentials.isPending}
                onClick={() => setConfirmDelete(true)}
              >
                移除凭证
              </button>
            )}
          </div>
        </form>
        {confirmDelete && (
          <div className="workspace-form-confirm">
            <p>移除凭证后，使用此模型的任务整理和采集分析将无法调用模型。</p>
            <div className="workspace-actions">
              <button
                type="button"
                className="button"
                disabled={credentials.isPending}
                onClick={() => credentials.mutate(true)}
              >
                确认移除凭证
              </button>
              <button
                type="button"
                className="text-button"
                disabled={credentials.isPending}
                onClick={() => setConfirmDelete(false)}
              >
                取消
              </button>
            </div>
          </div>
        )}
        {credentials.error && (
          <p className="as-inline-error" role="alert">
            {errorText(credentials.error)}
          </p>
        )}
        {credentials.isSuccess && (
          <p className="as-save-status" role="status">
            凭证设置已更新
          </p>
        )}
      </div>
      <div className="as-card">
        <div className="as-section-caption">
          <h3>外部连接</h3>
          <span>{data.providers.length} 项</span>
        </div>
        {data.providers.map((provider) => (
          <div className="as-setting-row" key={`${provider.kind}:${provider.name}`}>
            <div>
              <strong>{provider.name}</strong>
              <p>{provider.kind === 'quota' ? '额度来源' : '消息采集连接'}</p>
            </div>
            <span className="as-setting-value">{provider.enabled ? '已启用' : '已停用'}</span>
          </div>
        ))}
        <p className="workspace-form-hint">采集来源、项目与处理规则可在“收集 → 来源设置”中管理。</p>
      </div>
    </>
  )
}
