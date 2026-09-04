import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, FileUp } from 'lucide-react'
import { call } from '../api'
import { useNativePreferences } from './native-preferences-react'
import {
  nativePreferenceKeys,
  nativePreferenceLabels,
  parseNativePreferenceFile,
  resetNativePreferences,
  saveNativePreferences,
  type NativePreferenceFile,
  type NativePreferences,
} from './native-preferences'
import type { KnowledgeCollection } from './knowledge-types'
import type { CollectionOverview } from './collection-types'
import './workspace-forms.css'

export function NativePreferencesImport() {
  const preferences = useNativePreferences()
  const [preview, setPreview] = useState<NativePreferenceFile>()
  const [selected, setSelected] = useState<(keyof NativePreferences)[]>([])
  const [fileName, setFileName] = useState('')
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [confirmReset, setConfirmReset] = useState(false)
  const readVersion = useRef(0)
  const catalog = useQuery({
    queryKey: ['knowledge', 'catalog'],
    queryFn: ({ signal }) => call<KnowledgeCollection[]>('knowledge.catalog', {}, signal),
    enabled: !!preview?.preferences.knowledge_collection_order,
  })
  const collection = useQuery({
    queryKey: ['collection', 'overview'],
    queryFn: ({ signal }) => call<CollectionOverview>('collect.overview', {}, signal),
    enabled: !!preview?.preferences.collection_source_order,
  })
  async function choose(file?: File) {
    const version = ++readVersion.current
    setPreview(undefined)
    setError('')
    setSaved(false)
    if (!file) return
    try {
      if (file.size > 2 * 1024 * 1024) throw new Error('偏好文件不能大于 2 MB。')
      const next = parseNativePreferenceFile(await file.text())
      if (version !== readVersion.current) return
      setFileName(file.name)
      setPreview(next)
      setSelected(nativePreferenceKeys.filter((key) => next.preferences[key] !== undefined))
    } catch (error) {
      if (version === readVersion.current)
        setError(error instanceof Error ? error.message : '无法读取偏好文件。')
    }
  }
  function apply() {
    if (!preview) return
    try {
      saveNativePreferences(
        Object.fromEntries(selected.map((key) => [key, preview.preferences[key]])),
      )
      setPreview(undefined)
      setSaved(true)
      setError('')
    } catch {
      setError('浏览器没有允许保存偏好。本次没有应用更改，请允许本地存储后重试。')
    }
  }
  function describe(field: keyof NativePreferences) {
    const value = preview?.preferences[field]
    if (!Array.isArray(value)) return value || '全部'
    const labels = new Map(
      field === 'knowledge_collection_order'
        ? catalog.data?.map((item) => [item.id, item.name || item.id])
        : collection.data?.sources.map((item) => [item.id, item.name || item.external_id]),
    )
    return `${value.length} 项${
      value.length
        ? `：${value
            .slice(0, 8)
            .map((id) => labels.get(id) || id)
            .join(' → ')}${value.length > 8 ? '…' : ''}`
        : '，使用默认顺序'
    }`
  }
  return (
    <section
      className="as-card workspace-form native-preferences-import"
      aria-label="迁移旧版界面偏好"
    >
      <div className="as-section-caption">
        <h3>迁移旧版界面偏好</h3>
        <FileUp size={16} />
      </div>
      <p className="workspace-form-hint">
        导入旧版 ATM 导出的 JSON
        文件，可恢复知识集合、采集来源顺序与用量筛选。文件只在此浏览器读取，应用前可预览。
      </p>
      <label>
        选择界面偏好文件
        <input
          type="file"
          accept=".json,application/json"
          onChange={(event) => void choose(event.target.files?.[0])}
        />
      </label>
      {preview && (
        <div className="workspace-import-preview">
          <strong>{fileName}</strong>
          <p className="workspace-form-hint">
            选择要恢复的项目。这些项目会替换当前浏览器对应的设置。
          </p>
          {nativePreferenceKeys
            .filter((key) => preview.preferences[key] !== undefined)
            .map((key) => (
              <label className="workspace-check" key={key}>
                <span>
                  {nativePreferenceLabels[key]}
                  <small>{describe(key)}</small>
                </span>
                <input
                  type="checkbox"
                  checked={selected.includes(key)}
                  onChange={(event) =>
                    setSelected((previous) =>
                      event.target.checked
                        ? [...previous, key]
                        : previous.filter((item) => item !== key),
                    )
                  }
                />
              </label>
            ))}
          {!selected.length && <p className="workspace-form-hint">文件中没有可恢复的已选偏好。</p>}
          <div className="workspace-actions">
            <button
              type="button"
              className="button primary"
              disabled={!selected.length}
              onClick={apply}
            >
              <Check size={14} />
              确认恢复所选偏好
            </button>
            <button type="button" className="text-button" onClick={() => setPreview(undefined)}>
              取消
            </button>
          </div>
        </div>
      )}
      {saved && (
        <p role="status" className="as-save-status">
          <Check size={14} />
          偏好已恢复，打开对应页面即可使用。
        </p>
      )}
      {error && (
        <p role="alert" className="as-inline-error">
          {error}
        </p>
      )}
      <p className="workspace-form-hint">
        仅影响当前浏览器。窗口尺寸、面板折叠等原生布局采用网页布局；通知与语音偏好由各自的独立 App
        迁移。
      </p>
      {!!Object.keys(preferences).length && !confirmReset && (
        <div>
          <button type="button" className="text-button" onClick={() => setConfirmReset(true)}>
            恢复默认顺序和筛选
          </button>
        </div>
      )}
      {confirmReset && (
        <div className="workspace-form-confirm">
          <p>清除当前浏览器保存的排序和用量筛选？主题、任务与文档不受影响。</p>
          <div className="workspace-actions">
            <button
              type="button"
              className="button"
              onClick={() => {
                try {
                  resetNativePreferences()
                  setConfirmReset(false)
                  setSaved(false)
                  setError('')
                } catch {
                  setError('浏览器没有允许清除本地偏好，请重试。')
                }
              }}
            >
              确认恢复默认
            </button>
            <button type="button" className="text-button" onClick={() => setConfirmReset(false)}>
              取消
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
