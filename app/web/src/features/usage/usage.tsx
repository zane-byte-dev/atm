import { Disclosure } from '../../components/disclosure'
import {
  WorkspaceSplitLayout,
  WorkspaceNavigator,
  WorkspaceNavigatorHeading,
  WorkspaceNavigatorItem,
} from '../../components/workspace-navigator'
import { Button as AntButton, Empty as AntEmpty, Segmented, Tooltip } from 'antd'
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { Award, ChartNoAxesCombined, Clock3, RefreshCw } from 'lucide-react'
import { Select } from '../../components/select'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import { QueryLoading } from '../../components/query-loading'
import type { Bootstrap } from '../../types'
import { BadgeCalendar } from './badge-calendar'
import { RuntimeJobs } from '../notifications/runtime-jobs'
import { useNativePreferences } from '../../preferences/native-preferences-react'
import { saveNativePreferences } from '../../preferences/native-preferences'
import { filterUsage, nativeClientAgent, usageProjectName } from './usage-filters'
import { agentName, dateTime, int } from '../../format'
import { EmptyActivity } from '../agents/activity-shared'
import { useActivityStatus } from '../agents/activity-query'
import { useForeground } from '../../use-foreground'
import type {
  CachedQuota,
  DayStats,
  MetricFields,
  UsageRange,
  UsageSnapshot,
} from '../agents/activity-types'
import './usage.css'

const ranges = [
  ['today', '今天'],
  ['yesterday', '昨天'],
  ['this_week', '本周'],
  ['last_week', '上周'],
  ['this_month', '本月'],
  ['last_7_days', '最近 7 天'],
  ['last_30_days', '最近 30 天'],
] as const
const compact = (value: number) =>
  new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 }).format(value || 0)
const money = (value: number) =>
  new Intl.NumberFormat('en', {
    style: 'currency',
    currency: 'USD',
    maximumFractionDigits: value > 0 && value < 0.01 ? 4 : 2,
  }).format(value || 0)
const percent = (value: number) => `${(value || 0).toFixed(1)}%`

function UsageEmpty({ detail }: { detail: string }) {
  return (
    <AntEmpty
      className="usage-empty-state"
      image={<ChartNoAxesCombined size={30} />}
      description={
        <>
          <strong>暂无用量数据</strong>
          <p>{detail}</p>
        </>
      }
    />
  )
}

type Metric = 'tokens' | 'cost' | 'requests'
type Breakdown = 'agent' | 'model' | 'project' | 'skill' | 'speed'
type UsageRow = { name: string; agent?: string } & Partial<MetricFields>
const metricValue = (row: Partial<MetricFields>, metric: Metric) =>
  metric === 'cost'
    ? row.cost_usd || 0
    : metric === 'requests'
      ? row.requests || 0
      : row.total_tokens || 0
const formatMetric = (value: number, metric: Metric) =>
  metric === 'cost' ? money(value) : compact(value)

function UsageViewTabs() {
  const [params, setParams] = useSearchParams()
  const view = params.get('view') || 'usage'
  return (
    <div className="usage-view-tabs" role="group" aria-label="统计视图">
      {(
        [
          ['usage', '用量', ChartNoAxesCombined],
          ['quota', '额度', Clock3],
          ['badges', '徽章日历', Award],
        ] as const
      ).map(([key, label, Icon]) => (
        <WorkspaceNavigatorItem
          key={key}
          selected={view === key}
          onClick={() =>
            setParams((current) => {
              const next = new URLSearchParams(current)
              next.set('view', key)
              return next
            })
          }
        >
          <Icon size={17} aria-hidden="true" />
          {label}
        </WorkspaceNavigatorItem>
      ))}
    </div>
  )
}
export function UsageWorkspace({ boot }: { boot: Bootstrap }) {
  const [params] = useSearchParams()
  const view =
    params.get('view') === 'badges' ? 'badges' : params.get('view') === 'quota' ? 'quota' : 'usage'
  return (
    <WorkspaceSplitLayout
      as="section"
      className={`activity-workspace activity-usage usage-view-${view}`}
      aria-label="用量和统计"
    >
      <WorkspaceNavigator className="usage-navigator" aria-label="统计分类">
        <WorkspaceNavigatorHeading title="统计" />
        <UsageViewTabs />
      </WorkspaceNavigator>
      <div className="usage-detail">
        {view === 'badges' ? (
          <section className="usage-page usage-badges-page">
            <header className="activity-heading">
              <h1>徽章日历</h1>
            </header>
            <BadgeCalendar />
          </section>
        ) : (
          <UsageMetrics boot={boot} />
        )}
      </div>
    </WorkspaceSplitLayout>
  )
}

function UsageMetrics({ boot }: { boot: Bootstrap }) {
  const [params, setParams] = useSearchParams()
  const nativePreferences = useNativePreferences()
  const [preferenceError, setPreferenceError] = useState('')
  const foreground = useForeground()
  const status = useActivityStatus(foreground)
  const view = params.get('view') === 'quota' ? 'quota' : 'usage'
  const rawRange = params.get('range') || 'today'
  const range = ranges.some(([key]) => key === rawRange) ? rawRange : 'today'
  const agent = params.has('agent')
    ? params.get('agent') || ''
    : nativeClientAgent(nativePreferences.usage_filter_client || '')
  const model = params.has('model')
    ? params.get('model') || ''
    : nativePreferences.usage_filter_model || ''
  const project = params.has('project')
    ? params.get('project') || ''
    : nativePreferences.usage_filter_project || ''
  const metric = (
    ['tokens', 'cost', 'requests'].includes(params.get('metric') || '')
      ? params.get('metric')
      : 'tokens'
  ) as Metric
  const requestedGroup = (
    ['agent', 'model', 'project', 'skill', 'speed'].includes(params.get('group') || '')
      ? params.get('group')
      : 'agent'
  ) as Breakdown
  const availableGroups = project
    ? ['agent', 'project']
    : model
      ? ['model', 'speed']
      : ['agent', 'model', 'project', 'skill', 'speed']
  const group = (
    availableGroups.includes(requestedGroup) ? requestedGroup : project ? 'project' : 'model'
  ) as Breakdown
  const update = (key: string, value: string) => {
    if (['agent', 'model', 'project'].includes(key)) {
      try {
        saveNativePreferences({
          [key === 'agent'
            ? 'usage_filter_client'
            : key === 'model'
              ? 'usage_filter_model'
              : 'usage_filter_project']: value,
        })
        setPreferenceError('')
      } catch {
        setPreferenceError('本次筛选已应用，但浏览器未允许记住此筛选。')
      }
    }
    setParams((current) => {
      const next = new URLSearchParams(current)
      if (value || ['agent', 'model', 'project'].includes(key)) next.set(key, value)
      else next.delete(key)
      return next
    })
  }
  const snapshot = useQuery({
    queryKey: ['activity-usage', range, agent],
    queryFn: ({ signal }) => call<UsageSnapshot>('usage.snapshot', { range, agent }, signal),
    refetchInterval: foreground ? 120000 : false,
    refetchIntervalInBackground: false,
  })
  const quota = useQuery({
    queryKey: ['activity-quota', agent],
    queryFn: ({ signal }) => call<CachedQuota>('quota.cached', { agent }, signal),
    refetchInterval: foreground ? 120000 : false,
    refetchIntervalInBackground: false,
  })
  const unfiltered = snapshot.data?.ranges[range]
  const scoped = snapshot.data ? filterUsage(snapshot.data, range, model, project) : undefined
  const data = scoped?.range
  const totals = useMemo(
    () =>
      (scoped?.summary || []).reduce(
        (sum, row) => ({
          tokens: sum.tokens + row.total_tokens,
          requests: sum.requests + row.requests,
          cost: sum.cost + row.cost_usd,
          cache: sum.cache + row.cache_read_tokens,
          input: sum.input + row.total_input_tokens,
          estimated: sum.estimated + row.estimated_cost_usd,
          sessions: sum.sessions + row.sessions,
        }),
        { tokens: 0, requests: 0, cost: 0, cache: 0, input: 0, estimated: 0, sessions: 0 },
      ),
    [scoped?.summary],
  )
  const days = scoped?.days || []
  return (
    <section className={`usage-page usage-view-${view}`}>
      <div className="activity-heading">
        <h1>{view === 'quota' ? '额度' : '用量'}</h1>
        <div className="usage-refresh">
          <span>
            更新于{' '}
            {dateTime(view === 'usage' ? snapshot.data?.generated_at : quota.data?.generated_at)}
          </span>
          <AntButton
            htmlType="button"
            className="icon-button"
            aria-label="刷新统计"
            disabled={snapshot.isFetching || quota.isFetching}
            loading={snapshot.isFetching || quota.isFetching}
            onClick={() => {
              void snapshot.refetch()
              void quota.refetch()
              void status.refetch()
            }}
          >
            {!(snapshot.isFetching || quota.isFetching) && <RefreshCw size={16} />}
          </AntButton>
        </div>
      </div>
      <div className="activity-usage-filters">
        {view === 'usage' && (
          <label className="usage-range-select">
            <Select
              value={range}
              aria-label="统计时间范围"
              onChange={(event) => update('range', event.target.value)}
            >
              {ranges.map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </Select>
          </label>
        )}
        <label>
          Agent
          <Select
            value={agent}
            aria-label="统计 Agent"
            onChange={(event) => update('agent', event.target.value)}
          >
            <option value="">全部 Agent</option>
            {status.data?.agents.map((row) => (
              <option key={row.agent} value={row.agent}>
                {agentName(row.agent)}
              </option>
            ))}
            {agent && !status.data?.agents.some((row) => row.agent === agent) && (
              <option value={agent}>{agentName(agent)}</option>
            )}
          </Select>
        </label>
        {view === 'usage' && (
          <>
            <label>
              模型
              <Select
                value={model}
                aria-label="统计模型"
                onChange={(event) => update('model', event.target.value)}
              >
                <option value="">全部模型</option>
                {[
                  ...new Set([
                    ...(unfiltered?.model_stats.map((row) => row.model) || []),
                    ...(model ? [model] : []),
                  ]),
                ]
                  .sort()
                  .map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
              </Select>
            </label>
            <label>
              项目
              <Select
                value={project}
                aria-label="统计项目"
                onChange={(event) => update('project', event.target.value)}
              >
                <option value="">全部项目</option>
                {[
                  ...new Set([
                    ...(unfiltered?.project_stats.map((row) => usageProjectName(row.project)) ||
                      []),
                    ...(project ? [project] : []),
                  ]),
                ]
                  .sort()
                  .map((value) => (
                    <option key={value} value={value}>
                      {value}
                    </option>
                  ))}
              </Select>
            </label>
          </>
        )}
        {(agent || model || project) && (
          <AntButton
            htmlType="button"
            className="text-button"
            onClick={() => {
              try {
                saveNativePreferences({
                  usage_filter_client: '',
                  usage_filter_model: '',
                  usage_filter_project: '',
                })
                setPreferenceError('')
              } catch {
                setPreferenceError('本次筛选已清空，但浏览器未允许记住更改。')
              }
              setParams((current) => {
                const next = new URLSearchParams(current)
                ;['agent', 'model', 'project'].forEach((key) => next.set(key, ''))
                return next
              })
            }}
          >
            清除筛选
          </AntButton>
        )}
      </div>
      {preferenceError && (
        <p className="activity-inline-note" role="status">
          {preferenceError}
        </p>
      )}
      {view === 'usage' && (
        <>
          {project && model && (
            <p className="activity-inline-note">
              已按项目“{project}”汇总与绘图。模型“{model}
              ”筛选会在清除项目后应用；现有记录不提供模型与项目的联合汇总。
            </p>
          )}
          <div className="activity-source-note">
            <Clock3 size={13} />
            {data ? `${data.start_date} — ${data.end_date}` : '按本地日历计算统计范围'}
            <span>· 索引更新于 {dateTime(status.data?.health.last_success_at)}</span>
          </div>
          {status.data?.missing_index ? (
            <UsageEmpty detail="同步会话后，Token 用量与费用会显示在这里。" />
          ) : snapshot.isError ? (
            <Notice error={snapshot.error} retry={() => void snapshot.refetch()} />
          ) : snapshot.isPending ? (
            <QueryLoading
              className="activity-loading"
              text="正在汇总用量…"
              onRetry={() => void snapshot.refetch()}
            />
          ) : !data ||
            (!totals.tokens &&
              !totals.requests &&
              !totals.cost &&
              !totals.sessions &&
              !days.length) ? (
            <UsageEmpty detail="当前范围内没有用量记录，试试其他时间范围或筛选条件。" />
          ) : (
            data && (
              <>
                {!model &&
                  !project &&
                  data.quality.active_sessions > data.quality.token_sessions && (
                    <p className="usage-coverage-notice" role="status">
                      当前 {int(data.quality.active_sessions)} 个活跃会话中，仅{' '}
                      {int(data.quality.token_sessions)} 个有 Token 记录（
                      {percent(data.quality.session_coverage_percent)}
                      ）。以下用量和费用仅涵盖已记录部分；无记录不代表零用量。
                    </p>
                  )}
                <div className="activity-metrics">
                  <MetricCard
                    label="已记录 Token"
                    value={compact(totals.tokens)}
                    note={`缓存读取 ${compact(totals.cache)}`}
                  />
                  <MetricCard
                    label="估算费用（USD）"
                    value={money(totals.cost)}
                    note={
                      totals.estimated > 0
                        ? `其中 ${money(totals.estimated)} 采用估算价格`
                        : '按记录与模型价格计算'
                    }
                  />
                  <MetricCard
                    label="模型请求"
                    value={int(totals.requests)}
                    note={
                      model || project
                        ? '所选范围的请求记录'
                        : `已记录请求的详情覆盖 ${percent(data.quality.request_coverage_percent)}`
                    }
                  />
                  <MetricCard
                    label="活跃会话"
                    value={
                      project
                        ? int(totals.sessions)
                        : model
                          ? '—'
                          : int(data.quality.active_sessions)
                    }
                    note={
                      project
                        ? '所选项目的会话记录'
                        : model
                          ? '模型明细不提供去重会话数'
                          : `${int(data.quality.token_sessions)} 个会话有 Token 记录`
                    }
                  />
                </div>
                <section className="activity-chart-card">
                  <div className="activity-panel-heading">
                    <div>
                      <h2>用量趋势</h2>
                      <p>{range === 'today' || range === 'yesterday' ? '每小时' : '每日'}记录</p>
                    </div>
                    <MetricPicker metric={metric} onChange={(value) => update('metric', value)} />
                  </div>
                  <UsageChart
                    rows={days}
                    metric={metric}
                    hourly={range === 'today' || range === 'yesterday'}
                    today={range === 'today'}
                  />
                  {!days.length && (
                    <EmptyActivity title="这个范围内没有用量记录">
                      试试更长的时间范围或其他 Agent。
                    </EmptyActivity>
                  )}
                </section>
                <section className="activity-breakdown-card">
                  <div className="activity-panel-heading">
                    <h2>用量明细</h2>
                    <div className="activity-segment" role="group" aria-label="用量分组">
                      {(
                        [
                          ['agent', 'Agent'],
                          ['model', '模型'],
                          ['project', '项目'],
                          ['skill', '技能'],
                          ['speed', '速度'],
                        ] as const
                      )
                        .filter(([key]) => availableGroups.includes(key))
                        .map(([key, label]) => (
                          <AntButton
                            htmlType="button"
                            key={key}
                            className={group === key ? 'selected' : ''}
                            onClick={() => update('group', key)}
                            aria-pressed={group === key}
                          >
                            {label}
                          </AntButton>
                        ))}
                    </div>
                  </div>
                  <UsageBreakdown range={data} group={group} metric={metric} />
                </section>
                <div className="activity-quality">
                  <strong>{model || project ? '当前 Agent 的完整记录覆盖' : '记录覆盖'}</strong>
                  <span>会话 Token 覆盖 {percent(data.quality.session_coverage_percent)}</span>
                  <span>请求详情覆盖 {percent(data.quality.request_coverage_percent)}</span>
                  <span>速度采样 {percent(data.quality.speed_sample_percent)}</span>
                  <Disclosure>
                    <summary>统计口径</summary>
                    <p>
                      费用来自已索引记录及配置的模型价格；未记录的请求和不可计量的会话不计入 Token
                      总量。
                    </p>
                  </Disclosure>
                </div>
              </>
            )
          )}
        </>
      )}
      {view === 'quota' && (
        <section className="activity-quota-section">
          <div className="activity-panel-heading">
            <div>
              <h2>额度观察</h2>
              <p>最近一次保存的额度快照</p>
            </div>
            <RuntimeJobs
              boot={boot}
              kinds={['quota.refresh']}
              actions={[{ input: { kind: 'quota.refresh', ...(agent ? { agent } : {}) } }]}
            />
          </div>
          {quota.isPending && (
            <QueryLoading
              className="activity-loading"
              text="正在读取额度记录…"
              onRetry={() => void quota.refetch()}
            />
          )}
          {quota.isError && <Notice error={quota.error} retry={() => void quota.refetch()} />}
          {quota.isSuccess && !quota.data.windows.length && (
            <div className="activity-inline-note">
              还没有保存的额度记录。后续同步采集到额度后，会在这里显示。
            </div>
          )}
          <div className="activity-quotas">
            {quota.data?.windows.map((window) => (
              <div
                className={`activity-quota-card ${window.stale || window.reset_elapsed ? 'stale' : ''}`}
                key={`${window.agent}-${window.window_minutes}`}
              >
                <div>
                  <strong>{agentName(window.agent)}</strong>
                  <span>
                    {window.window_minutes >= 1440
                      ? `${window.window_minutes / 1440} 天`
                      : `${window.window_minutes / 60} 小时`}
                    窗口
                  </span>
                </div>
                <p>
                  <b>{percent(window.used_percent)}</b> 已使用
                  {window.reset_elapsed && <span className="activity-tag">重置后待更新</span>}
                  {window.stale && !window.reset_elapsed && (
                    <span className="activity-tag">记录较旧</span>
                  )}
                </p>
                <div className="activity-meter">
                  <span style={{ width: `${Math.min(100, Math.max(0, window.used_percent))}%` }} />
                </div>
                <small>
                  记录于 {dateTime(window.observed_at)}
                  {window.resets_at > 0 &&
                    ` · 重置 ${dateTime(new Date(window.resets_at * 1000).toISOString())}`}
                </small>
              </div>
            ))}
          </div>
        </section>
      )}
    </section>
  )
}

function MetricCard({ label, value, note }: { label: string; value: string; note: string }) {
  return (
    <div className="activity-metric">
      <span>{label}</span>
      <strong>{value}</strong>
      <small>{note}</small>
    </div>
  )
}
function MetricPicker({
  metric,
  onChange,
}: {
  metric: Metric
  onChange: (metric: Metric) => void
}) {
  return (
    <Segmented
      aria-label="趋势指标"
      value={metric}
      options={[
        { label: 'Token', value: 'tokens' },
        { label: '费用', value: 'cost' },
        { label: '请求', value: 'requests' },
      ]}
      onChange={(value) => onChange(value as Metric)}
    />
  )
}

function UsageChart({
  rows,
  metric,
  hourly,
  today,
}: {
  rows: DayStats[]
  metric: Metric
  hourly: boolean
  today: boolean
}) {
  const [selected, setSelected] = useState<string>()
  if (!rows.length) return null
  const peak = Math.max(
    ...rows.map((row) => metricValue(row, metric)),
    metric === 'cost' ? 0.0001 : 1,
  )
  const magnitude = 10 ** Math.floor(Math.log10(peak / 4))
  const step = [1, 2, 5, 10].find((factor) => factor * magnitude >= peak / 4)! * magnitude
  const max = Math.max(metric === 'requests' ? 1 : 0, step) * 4
  const chosen = rows.find((row) => row.date === selected)
  const slots = hourly
    ? Array.from({ length: 24 }, (_, hour) => ({
        label: `${String(hour).padStart(2, '0')}时`,
        row: rows.find((row) => Number(row.date.slice(11, 13)) === hour),
        future: today && hour > new Date().getHours(),
      }))
    : rows.map((row) => ({ label: row.date.slice(5), row, future: false }))
  return (
    <>
      <div className="usage-chart-layout">
        <div className="usage-chart-axis" aria-hidden="true">
          {[1, 0.75, 0.5, 0.25, 0].map((ratio) => (
            <span key={ratio}>{formatMetric(max * ratio, metric)}</span>
          ))}
        </div>
        <div className="activity-chart" role="group" aria-label="用量趋势柱状图">
          {slots.map(({ row, label, future }, index) => (
            <Tooltip
              key={row?.date || label}
              title={
                row
                  ? `${row.date} · ${metric === 'cost' ? money(metricValue(row, metric)) : int(metricValue(row, metric))}`
                  : `${label} · ${future ? '尚未到达' : '无记录'}`
              }
            >
              <AntButton
                htmlType="button"
                className={`activity-bar-column ${row && selected === row.date ? 'selected' : ''} ${future ? 'usage-future' : ''}`}
                onClick={() => setSelected(row?.date)}
                aria-label={
                  row
                    ? `${row.date}：${formatMetric(metricValue(row, metric), metric)}`
                    : `${label}：${future ? '尚未到达' : '无记录'}`
                }
                aria-pressed={!!row && selected === row.date}
              >
                <span className="activity-bar-track">
                  <span
                    className="activity-bar"
                    style={{
                      height: row
                        ? `${Math.max((metricValue(row, metric) / max) * 100, metricValue(row, metric) > 0 ? 2 : 0)}%`
                        : '0%',
                    }}
                  />
                </span>
                <span className="activity-bar-label">
                  {slots.length < 12 ||
                  index % Math.ceil(slots.length / 8) === 0 ||
                  index === slots.length - 1
                    ? label
                    : ' '}
                </span>
              </AntButton>
            </Tooltip>
          ))}
        </div>
      </div>
      <div className="activity-chart-detail" aria-live="polite">
        {chosen &&
          `${chosen.date} · ${int(chosen.total_tokens)} Token · ${int(chosen.requests)} 次请求 · ${money(chosen.cost_usd)}`}
      </div>
    </>
  )
}

function UsageBreakdown({
  range,
  group,
  metric,
}: {
  range: UsageRange
  group: Breakdown
  metric: Metric
}) {
  if (group === 'skill')
    return range.skill_stats.length ? (
      <div className="activity-table-scroll">
        <table className="activity-table">
          <thead>
            <tr>
              <th>技能</th>
              <th>调用次数</th>
              <th>会话</th>
              <th>Agent 数</th>
            </tr>
          </thead>
          <tbody>
            {range.skill_stats.map((row) => (
              <tr key={row.skill}>
                <td>{row.skill}</td>
                <td>{int(row.calls)}</td>
                <td>{int(row.sessions)}</td>
                <td>{int(row.agents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    ) : (
      <EmptyActivity title="这个范围内没有技能调用记录" />
    )
  if (group === 'speed')
    return (
      <>
        <div className="activity-inline-note">
          生成速度只统计可测量的模型请求。未计时 {int(range.speed.untimed_requests)}{' '}
          次，超出有效测量窗口 {int(range.speed.out_of_window_requests)} 次。
        </div>
        {range.speed.models.length ? (
          <div className="activity-table-scroll">
            <table className="activity-table">
              <thead>
                <tr>
                  <th>模型</th>
                  <th>Agent</th>
                  <th>输出 Token / 秒</th>
                  <th>请求耗时中位数</th>
                  <th>有效样本 / 请求</th>
                </tr>
              </thead>
              <tbody>
                {range.speed.models.map((row) => (
                  <tr key={`${row.client}-${row.model}`}>
                    <td>{row.model}</td>
                    <td>{agentName(row.client)}</td>
                    <td>{row.tokens_per_second_weighted.toFixed(1)}</td>
                    <td>{row.duration_p50_seconds.toFixed(1)} 秒</td>
                    <td>
                      {int(row.sampled)} / {int(row.requests)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <EmptyActivity title="还没有有效的速度样本" />
        )}
        {!!range.speed.turns.length && (
          <div className="activity-table-scroll">
            <table className="activity-table">
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>对话轮数</th>
                  <th>等待中位数</th>
                  <th>等待 P90</th>
                  <th>每轮请求数</th>
                </tr>
              </thead>
              <tbody>
                {range.speed.turns.map((row) => (
                  <tr key={row.agent}>
                    <td>{agentName(row.agent)}</td>
                    <td>{int(row.turns)}</td>
                    <td>{row.wait_p50_seconds.toFixed(1)} 秒</td>
                    <td>{row.wait_p90_seconds.toFixed(1)} 秒</td>
                    <td>{row.requests_per_turn.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </>
    )
  let rows: UsageRow[]
  if (group === 'model')
    rows = range.model_stats.map((row) => ({ ...row, name: row.model, agent: row.client }))
  else if (group === 'project')
    rows = range.project_stats.map((row) => ({ ...row, name: row.project || '未分配项目' }))
  else {
    const grouped = new Map<string, UsageRow>()
    for (const row of range.project_stats) {
      const current = grouped.get(row.agent) || { name: agentName(row.agent), agent: row.agent }
      for (const key of [
        'sessions',
        'total_tokens',
        'total_input_tokens',
        'fresh_input_tokens',
        'output_tokens',
        'cache_read_tokens',
        'cache_create_tokens',
        'requests',
        'cost_usd',
        'estimated_cost_usd',
      ] as const)
        current[key] = (current[key] || 0) + row[key]
      grouped.set(row.agent, current)
    }
    rows = [...grouped.values()]
  }
  rows.sort((a, b) => metricValue(b, metric) - metricValue(a, metric))
  if (!rows.length) return <EmptyActivity title="这个范围内没有用量明细" />
  const total = rows.reduce((sum, row) => sum + metricValue(row, metric), 0)
  return (
    <div className="activity-table-scroll">
      <table
        className={`activity-table usage-numeric-table ${group !== 'agent' ? 'usage-has-agent' : ''}`}
      >
        <thead>
          <tr>
            <th>{group === 'model' ? '模型' : group === 'project' ? '项目' : 'Agent'}</th>
            {group !== 'agent' && <th>Agent</th>}
            <th>Token</th>
            <th>缓存读取</th>
            <th>请求</th>
            <th>费用（USD）</th>
            <th>{metric === 'tokens' ? 'Token' : metric === 'cost' ? '费用' : '请求'}占比</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={`${row.agent}-${row.name}`}>
              <td>
                <strong>{row.name}</strong>
              </td>
              {group !== 'agent' && <td>{agentName(row.agent || '')}</td>}
              <td
                title={
                  !row.total_tokens && !row.requests && !row.cost_usd
                    ? '没有可计量的用量记录'
                    : int(row.total_tokens || 0)
                }
              >
                {!row.total_tokens && !row.requests && !row.cost_usd
                  ? '无记录'
                  : compact(row.total_tokens || 0)}
              </td>
              <td>
                {!row.total_tokens && !row.requests && !row.cost_usd
                  ? '—'
                  : compact(row.cache_read_tokens || 0)}
              </td>
              <td>
                {!row.total_tokens && !row.requests && !row.cost_usd ? '—' : int(row.requests || 0)}
              </td>
              <td>
                {!row.total_tokens && !row.requests && !row.cost_usd
                  ? '—'
                  : money(row.cost_usd || 0)}
                {(row.estimated_cost_usd || 0) > 0 && (
                  <span
                    className="activity-estimated"
                    title={`估算部分 ${money(row.estimated_cost_usd || 0)}`}
                  >
                    *
                  </span>
                )}
              </td>
              <td>
                <div className="usage-share">
                  <div
                    className="activity-table-meter"
                    title={formatMetric(metricValue(row, metric), metric)}
                  >
                    <span
                      style={{
                        width: `${(total > 0 ? metricValue(row, metric) / total : 0) * 100}%`,
                      }}
                    />
                  </div>
                  <span>
                    {!row.total_tokens && !row.requests && !row.cost_usd
                      ? '—'
                      : percent(total > 0 ? (metricValue(row, metric) / total) * 100 : 0)}
                  </span>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
