import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import {
  Settings,
  ChevronRight,
  Clock3,
  Database,
  Layers3,
  Palette,
  Sparkles,
  UserRound,
} from 'lucide-react'
import { WorkspaceTitle } from '../workspace-title'
import { call } from '../api'
import { Notice } from '../editor'
import { QueryLoading } from '../query-loading'
import { AppearanceSettings } from '../appearance-settings'
import type { Bootstrap } from '../types'
import type { WorkspaceSettings as SettingsData } from './aiday-settings-types'
import './aiday-settings.css'
import { RuntimeJobs, jobsEnabled } from './runtime-jobs'
import { BusinessPreferences, ModelSettings } from './business-settings'
import { NativePreferencesImport } from './native-preferences-import'
const number = (value: number) => new Intl.NumberFormat('zh-CN').format(value)
function timestamp(value: string | number | null | undefined) {
  if (!value) return '尚无记录'
  const date = new Date(typeof value === 'number' ? value * 1000 : value)
  return Number.isNaN(date.valueOf()) ? '尚无记录' : date.toLocaleString('zh-CN', { hour12: false })
}
const syncNames: Record<string, string> = {
  never: '尚未记录同步',
  fresh: '索引已更新',
  stale: '索引等待更新',
  syncing: '正在同步',
  failed: '上次同步未完成',
  unavailable: '暂时无法读取索引',
}

function Loading({ text = '正在读取本机记录…', retry }: { text?: string; retry?: () => void }) {
  return <QueryLoading className="as-loading" text={text} onRetry={retry} />
}
function StatePill({
  enabled,
  on = '已开启',
  off = '已关闭',
}: {
  enabled: boolean
  on?: string
  off?: string
}) {
  return (
    <span className={`as-state-pill ${enabled ? 'on' : ''}`}>
      <span />
      {enabled ? on : off}
    </span>
  )
}
function SettingRow({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <div className="as-setting-row">
      <div>
        <strong>{title}</strong>
        {description && <p>{description}</p>}
      </div>
      <div className="as-setting-value">{children}</div>
    </div>
  )
}

export function SettingsWorkspace({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const requestedSection = params.get('section')
  const section =
    requestedSection && ['appearance', 'model', 'runtime'].includes(requestedSection)
      ? requestedSection
      : 'general'
  function setSection(value: string) {
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      if (value === 'general') next.delete('section')
      else next.set('section', value)
      return next
    })
  }
  const settings = useQuery({
    queryKey: ['settings.get'],
    queryFn: ({ signal }) => call<SettingsData>('settings.get', {}, signal),
    staleTime: 30_000,
  })
  const data = settings.data
  const writable = boot.capabilities?.workspace_write === true
  const sectionItems = [
    { value: 'general', title: '通用偏好', subtitle: '个人信息与当前行为', icon: UserRound },
    { value: 'appearance', title: '外观', subtitle: '主题与工作台配色', icon: Palette },
    { value: 'model', title: '模型与连接', subtitle: '服务、凭证与来源', icon: Sparkles },
    { value: 'runtime', title: '运行与同步', subtitle: '工作台状态与索引', icon: Database },
  ] as const
  const activeSection = sectionItems.find((item) => item.value === section) ?? sectionItems[0]
  return (
    <section className="as-workspace settings-workspace">
      <div className="as-settings-layout">
        <nav className="as-settings-nav" aria-label="设置分类">
          <div className="settings-navigator-heading">
            <WorkspaceTitle icon={Settings}>设置</WorkspaceTitle>
            <p>偏好与本机运行</p>
          </div>
          {sectionItems.map((item) => (
            <button
              key={item.value}
              type="button"
              className={section === item.value ? 'selected' : ''}
              onClick={() => setSection(item.value)}
            >
              <item.icon size={18} />
              <span>
                <strong>{item.title}</strong>
                <small>{item.subtitle}</small>
              </span>
              <ChevronRight size={13} />
            </button>
          ))}
          <div className="as-settings-version">
            <span className="as-source-dot" />
            ATM · {boot.version}
          </div>
        </nav>
        <section className="as-settings-detail" aria-label={`${activeSection.title}设置`}>
          <header className="settings-detail-header">
            <div className="settings-detail-identity">
              <activeSection.icon size={16} />
              <div>
                <h1>{activeSection.title}</h1>
                <p>{activeSection.subtitle}</p>
              </div>
            </div>
          </header>
          <div className="as-settings-content">
            <div className="settings-content-sheet">
              {section === 'appearance' ? (
                <>
                  <AppearanceSettings />
                  <NativePreferencesImport />
                </>
              ) : settings.isPending ? (
                <Loading retry={() => void settings.refetch()} />
              ) : settings.isError ? (
                <Notice error={settings.error} retry={() => void settings.refetch()} />
              ) : (
                data && (
                  <>
                    {section === 'general' && (
                      <BusinessPreferences data={data} writable={writable} />
                    )}
                    {section === 'model' && <ModelSettings data={data} writable={writable} />}
                    {section === 'runtime' && (
                      <>
                        <div className="as-section-title">
                          <div>
                            <h2>运行与同步</h2>
                            <p>当前工作台的能力，以及已有会话索引的更新情况。</p>
                          </div>
                        </div>
                        <div className="as-card">
                          <div className="as-section-caption">
                            <h3>手动运行</h3>
                            <span>按需执行与最近记录</span>
                          </div>
                          {!jobsEnabled(boot) && (
                            <p className="workspace-form-hint">当前连接未启用手动运行。</p>
                          )}
                          <RuntimeJobs
                            boot={boot}
                            history
                            kinds={[
                              'session.sync',
                              'collect.run',
                              'collect.reprocess',
                              'day.rebuild',
                              'quota.refresh',
                              'todo.refine',
                            ]}
                            actions={[
                              { input: { kind: 'session.sync' } },
                              { input: { kind: 'quota.refresh' } },
                            ]}
                          />
                        </div>
                        <div className="as-runtime-banner">
                          <span className="as-runtime-icon">
                            <Layers3 size={25} />
                          </span>
                          <div>
                            <span className="as-eyebrow">WORKSPACE</span>
                            <h3>浏览器工作台</h3>
                            <p>
                              {data.runtime.background_sync
                                ? '当前本地服务已启用后台同步。'
                                : '当前本地服务未启用后台同步。'}
                              各项能力见下方运行状态。
                            </p>
                          </div>
                          <span className="as-status">{data.runtime.version}</span>
                        </div>
                        <div className="as-card">
                          <div className="as-section-caption">
                            <h3>索引状态</h3>
                            <span
                              className={`as-state-pill ${data.sync.status === 'fresh' ? 'on' : ''}`}
                            >
                              <span />
                              {syncNames[data.sync.status] || data.sync.status}
                            </span>
                          </div>
                          <SettingRow title="最近成功同步">
                            {timestamp(data.sync.last_success_at)}
                          </SettingRow>
                          <SettingRow title="最近同步尝试">
                            {timestamp(data.sync.last_attempt_at)}
                          </SettingRow>
                          <SettingRow title="会话索引">
                            {number(data.sync.indexed_sessions)} 个
                          </SettingRow>
                          <SettingRow
                            title="保留的历史会话"
                            description="来源文件已不在当前同步范围的历史记录"
                          >
                            {number(data.sync.retained_sessions)} 个
                          </SettingRow>
                          <SettingRow title="上次同步文件">
                            {number(data.sync.last_synced_files)} 个
                          </SettingRow>
                          <SettingRow title="数据库版本">
                            {data.sync.indexed && data.sync.schema_version
                              ? `v${data.sync.schema_version}`
                              : '暂无可用索引'}
                          </SettingRow>
                          {data.sync.has_error && (
                            <p className="as-inline-error">
                              上次同步或索引读取未完成，请查看手动运行记录或重试同步。
                            </p>
                          )}
                        </div>
                        <div className="as-card">
                          <div className="as-section-caption">
                            <h3>此工作台的后台能力</h3>
                            <Clock3 size={15} />
                          </div>
                          <SettingRow title="后台同步">
                            <StatePill
                              enabled={data.runtime.background_sync}
                              on="运行中"
                              off="当前服务未启用"
                            />
                          </SettingRow>
                          <SettingRow title="自动采集">
                            <StatePill
                              enabled={data.runtime.collection}
                              on="运行中"
                              off="当前服务未启用"
                            />
                          </SettingRow>
                          <SettingRow title="模型执行">
                            <StatePill
                              enabled={data.runtime.models}
                              on="可用"
                              off="此工作台未启用"
                            />
                          </SettingRow>
                          <SettingRow title="Agent Hook">
                            <StatePill
                              enabled={data.runtime.agent_hooks}
                              on="运行中"
                              off="当前服务未启用"
                            />
                          </SettingRow>
                        </div>
                      </>
                    )}
                  </>
                )
              )}
            </div>
          </div>
        </section>
      </div>
    </section>
  )
}
