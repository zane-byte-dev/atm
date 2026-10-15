import {
  WorkspaceSplitLayout,
  WorkspaceNavigator,
  WorkspaceNavigatorHeading,
  WorkspaceNavigatorItem,
} from '../../components/workspace-navigator'
import { Alert, Button as AntButton } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { Database, Palette, Sparkles, UserRound, RefreshCw } from 'lucide-react'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import { QueryLoading } from '../../components/query-loading'
import { AppearanceSettings } from './appearance-settings'
import type { Bootstrap } from '../../types'
import type { WorkspaceSettings as SettingsData } from './aiday-settings-types'
import './aiday-settings.css'
import { RuntimeJobs } from '../notifications/runtime-jobs'
import { BusinessPreferences, ModelSettings } from './business-settings'
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
function RuntimeSettings({
  data,
  boot,
  refreshing,
  onRefresh,
}: {
  data: SettingsData
  boot: Bootstrap
  refreshing: boolean
  onRefresh: () => void
}) {
  const sources = (data.preferences.session_sync_agents ?? 'codex')
    .split(',')
    .filter((source) => source !== 'none')
  const primaryAgent = sources[0] || 'codex'
  const capabilities = [
    { title: '夜间同步', enabled: data.runtime.background_sync && sources.length > 0 },
    { title: '自动采集', enabled: data.runtime.collection },
    { title: '模型执行', enabled: data.runtime.models },
    { title: 'Agent Hook', enabled: data.runtime.agent_hooks },
  ]
  return (
    <div className="runtime-settings">
      <div className="runtime-intro">
        <p>
          会话每晚 23:00 后采集一次，次日 06:00 前可补采（{data.timezone}）。默认只自动采集
          Codex，辅助来源可在通用偏好中开启；需要最新记录时可手动同步。
        </p>
        <AntButton onClick={onRefresh} disabled={refreshing} loading={refreshing}>
          {!refreshing && <RefreshCw size={14} />}刷新状态
        </AntButton>
      </div>
      <section className="as-card" aria-label="会话同步状态">
        <div className="as-section-caption">
          <h3>索引状态</h3>
          <span className={`as-state-pill ${data.sync.status === 'fresh' ? 'on' : ''}`}>
            <span />
            {syncNames[data.sync.status] || data.sync.status}
          </span>
        </div>
        <dl className="runtime-metrics">
          <div>
            <dt>已索引会话</dt>
            <dd>{number(data.sync.indexed_sessions)}</dd>
          </div>
          <div>
            <dt title="来源文件已不在当前同步范围的历史记录">保留历史</dt>
            <dd>{number(data.sync.retained_sessions)}</dd>
          </div>
          <div>
            <dt>上次同步文件</dt>
            <dd>{number(data.sync.last_synced_files)}</dd>
          </div>
        </dl>
        <dl className="runtime-facts">
          <dt>最近成功同步</dt>
          <dd>{timestamp(data.sync.last_success_at)}</dd>
          <dt>最近同步尝试</dt>
          <dd>{timestamp(data.sync.last_attempt_at)}</dd>
          <dt>数据库版本</dt>
          <dd>
            {data.sync.indexed && data.sync.schema_version
              ? `v${data.sync.schema_version}`
              : '暂无可用索引'}
          </dd>
        </dl>
        {data.sync.has_error && (
          <Alert
            className="as-inline-error"
            type="warning"
            role="status"
            title="上次同步或索引读取未完成，请查看消息或重试同步。"
          />
        )}
        <RuntimeJobs
          boot={boot}
          kinds={['session.sync']}
          actions={[{ input: { kind: 'session.sync', agent: primaryAgent } }]}
        />
      </section>
      <section className="as-card" aria-label="后台能力">
        <div className="as-section-caption">
          <h3>后台能力</h3>
          <span>当前服务提供的功能</span>
        </div>
        <div className="runtime-capabilities">
          {capabilities.map((item) => (
            <div className="runtime-capability" key={item.title}>
              <span>{item.title}</span>
              <StatePill enabled={item.enabled} on="已启用" off="未启用" />
            </div>
          ))}
        </div>
      </section>
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
    refetchInterval: section === 'runtime' ? 30_000 : false,
    refetchIntervalInBackground: false,
  })
  const data = settings.data
  const writable = boot.capabilities?.workspace_write === true
  const sectionItems = [
    { value: 'general', title: '通用偏好', icon: UserRound },
    { value: 'appearance', title: '外观', icon: Palette },
    { value: 'model', title: '模型与连接', icon: Sparkles },
    { value: 'runtime', title: '运行与同步', icon: Database },
  ] as const
  const activeSection = sectionItems.find((item) => item.value === section) ?? sectionItems[0]
  return (
    <section className="as-workspace settings-workspace">
      <WorkspaceSplitLayout className="as-settings-layout">
        <WorkspaceNavigator className="as-settings-nav" aria-label="设置分类">
          <WorkspaceNavigatorHeading className="settings-navigator-heading" title="设置" />
          {sectionItems.map((item) => (
            <WorkspaceNavigatorItem
              key={item.value}
              selected={section === item.value}
              onClick={() => setSection(item.value)}
            >
              <item.icon size={18} />
              <span>{item.title}</span>
            </WorkspaceNavigatorItem>
          ))}
          <div className="as-settings-version">
            <span className="as-source-dot" />
            ATM · {boot.version}
          </div>
        </WorkspaceNavigator>
        <section className="as-settings-detail" aria-label={`${activeSection.title}设置`}>
          <header className="settings-detail-header">
            <div className="settings-detail-identity">
              <h1>{activeSection.title}</h1>
            </div>
          </header>
          <div className="as-settings-content">
            <div className="settings-content-sheet">
              {section === 'appearance' ? (
                <AppearanceSettings />
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
                      <RuntimeSettings
                        data={data}
                        boot={boot}
                        refreshing={settings.isFetching}
                        onRefresh={() => void settings.refetch()}
                      />
                    )}
                  </>
                )
              )}
            </div>
          </div>
        </section>
      </WorkspaceSplitLayout>
    </section>
  )
}
