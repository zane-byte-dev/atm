import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import {
  ArrowRight,
  CalendarDays,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Clock3,
  Database,
  Fingerprint,
  Layers3,
  List,
  LoaderCircle,
  Palette,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Star,
  UserRound,
  Zap,
} from 'lucide-react'
import { call, errorText } from '../api'
import { Notice } from '../editor'
import { AppearanceSettings } from '../appearance-settings'
import type { Bootstrap } from '../types'
import type {
  DayBadge,
  DayEvidence,
  DayLedger,
  DayPrivacy,
  DayResult,
  DaySnapshot,
  WorkspaceSettings as SettingsData,
} from './aiday-settings-types'
import './aiday-settings.css'

const number = (value: number) => new Intl.NumberFormat('zh-CN').format(value)
const compact = (value: number) =>
  new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 }).format(value)
const duration = (seconds: number) =>
  seconds >= 3600 ? `${(seconds / 3600).toFixed(1)} 小时` : `${Math.round(seconds / 60)} 分钟`
function timestamp(value: string | number | null | undefined) {
  if (!value) return '尚无记录'
  const date = new Date(typeof value === 'number' ? value * 1000 : value)
  return Number.isNaN(date.valueOf()) ? '尚无记录' : date.toLocaleString('zh-CN', { hour12: false })
}
function localDay(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
function rangeFor(days: number, to = localDay()) {
  const date = new Date(`${to}T12:00:00`)
  date.setDate(date.getDate() - days + 1)
  return { from: localDay(date), to }
}
const metricNames: Record<string, string> = {
  sessions: '会话数',
  session_count: '会话数',
  turns: '交互轮次',
  turn_count: '交互轮次',
  tool_calls: '工具调用',
  source_count: 'AI 来源',
  work_tokens: '工作 Token',
  total_tokens: 'Token',
  input_tokens: '输入 Token',
  output_tokens: '输出 Token',
  cache_create_tokens: '缓存创建 Token',
  cache_read_tokens: '缓存读取 Token',
  event_count: '协作事件',
  code_events: '代码事件',
  visual_events: '视觉事件',
  quality_loops: '质检循环',
  refinements: '细化',
  detail_turns: '细节追问',
  modality_count: '任务模态',
  corrections: '纠正',
  acceptances: '直接确认',
  consecutive_days: '连续使用',
  modality_share: '模态占比',
  loop_share: '质检占比',
  detail_share: '追问占比',
  correction_share: '纠正占比',
  acceptance_share: '确认占比',
  active_seconds: '活跃时间',
  foreground_seconds: '前台时间',
  background_seconds: '后台时间',
  background_share: '后台占比',
  turns_per_session: '每会话轮次',
  generation_seconds: '生成时间',
  streak: '连续天数',
  streak_days: '连续天数',
  correction: '纠正',
  retry: '重试',
  refinement: '细化',
  question: '提问',
  directive: '指令',
  acceptance: '采纳',
  brainstorm: '创意讨论',
  explanation: '解释',
  code: '代码',
  general: '通用',
  image: '图像',
  video: '视频',
  audio: '音频',
  visual: '视觉创作',
  writing: '写作',
  research: '研究',
}
const tagNames: Record<string, string> = {
  grid: '结构与执行',
  orbit: '协同与节奏',
  crystal: '深度协作',
  lens: '洞察与质检',
  prism: '多模态创作',
  growth: '成长徽章',
  instant: '单日成就',
  streak: '连续里程碑',
  'multi-agent': '多 Agent 协作',
  orchestration: '任务编排',
  collaboration: '共同创作',
  'deep-work': '深度工作',
  'high-load': '高强度协作',
  throughput: '高效产出',
  'low-load': '轻量协作',
  recovery: '休整节奏',
  steady: '稳定同行',
}
const sourceNames: Record<string, string> = {
  codex: 'Codex',
  claude: 'Claude',
  pi: 'Pi',
  qoder: 'Qoder',
  qodercli: 'Qoder CLI',
  qoderwork: 'QoderWork',
  copilot: 'GitHub Copilot',
  grokbuild: 'Grok Build',
  grok: 'Grok',
  antigravity: 'Antigravity',
  cursor: 'Cursor',
  atm: 'ATM',
  unknown: '未识别来源',
}
const sourceName = (source: string) => sourceNames[source] || '其他来源'
const executionNames: Record<string, string> = {
  interactive: '交互执行',
  agentic: '自主执行',
  autonomous: '自主执行',
  background: '后台执行',
  foreground: '前台执行',
}
const unitNames: Record<string, string> = {
  events: '次',
  calls: '次',
  turns: '轮',
  sessions: '个会话',
  agents: '个来源',
  sources: '个来源',
  types: '种',
  days: '天',
  seconds: '秒',
  minutes: '分钟',
  hours: '小时',
  tokens: 'Token',
}
function evidenceValue(evidence: DayEvidence) {
  const value = Number.isInteger(evidence.value)
    ? number(evidence.value)
    : evidence.value.toFixed(2)
  if (evidence.unit === '%') return `${value}%`
  const unit = unitNames[evidence.unit || '']
  return unit ? `${value} ${unit}` : value
}
function comparisonLabel(comparison: string, baselineDays: number) {
  const percentile = /^recent_p(\d{1,3})$/.exec(comparison)
  if (percentile && Number(percentile[1]) <= 100)
    return `近 ${baselineDays || 30} 个基线日的第 ${Number(percentile[1])} 百分位`
  return /\p{Script=Han}/u.test(comparison) ? comparison : '已与近期基线对比'
}
const eventNames: Record<string, string> = {
  turn: '对话轮次',
  user_turn: '用户轮次',
  assistant_turn: 'AI 轮次',
  tool: '工具调用',
  tool_call: '工具调用',
  model_call: '模型请求',
  usage: '用量',
  generation: '生成',
  session: '会话',
}
const syncNames: Record<string, string> = {
  never: '尚未记录同步',
  fresh: '索引已更新',
  stale: '索引等待更新',
  syncing: '正在同步',
  failed: '上次同步未完成',
  unavailable: '暂时无法读取索引',
}

function Empty({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="as-empty">
      <CalendarDays size={30} strokeWidth={1.3} />
      <h3>{title}</h3>
      <p>{children}</p>
    </div>
  )
}
function Loading({ text = '正在读取本机记录…' }: { text?: string }) {
  return (
    <div className="as-loading" role="status">
      <LoaderCircle size={18} className="spin" />
      {text}
    </div>
  )
}
function Stat({ title, value, hint }: { title: string; value: ReactNode; hint?: string }) {
  return (
    <div className="as-stat">
      <span>{title}</span>
      <strong>{value}</strong>
      {hint && <small>{hint}</small>}
    </div>
  )
}
function BadgeMark({
  badge,
  large = false,
}: {
  badge?: Pick<DayBadge, 'family' | 'id'>
  large?: boolean
}) {
  return (
    <span
      className={`as-badge-mark ${large ? 'large' : ''} family-${badge?.family || 'crystal'}`}
      aria-hidden="true"
    >
      {badge?.family === 'orbit' ? (
        <Layers3 />
      ) : badge?.family === 'grid' ? (
        <Zap />
      ) : badge?.family === 'lens' ? (
        <Fingerprint />
      ) : (
        <Sparkles />
      )}
    </span>
  )
}

export function AIDayWorkspace({ boot: _boot }: { boot: Bootstrap }) {
  const [tab, setTab] = useState<'overview' | 'atlas' | 'ledger' | 'privacy'>('overview')
  const [range, setRange] = useState(() => rangeFor(30))
  const [draftRange, setDraftRange] = useState(range)
  const [rangeError, setRangeError] = useState('')
  const [selected, setSelected] = useState('')
  const [ledgerDate, setLedgerDate] = useState('')
  const [offset, setOffset] = useState(0)
  const snapshot = useQuery({
    queryKey: ['day.snapshot', range],
    queryFn: ({ signal }) => call<DaySnapshot>('day.snapshot', range, signal),
    staleTime: 30_000,
  })
  const data = snapshot.data
  const selectedDay = data?.history.some((day) => day.day === selected)
    ? selected
    : data?.history[0]?.day
  const eventDay = ledgerDate || selectedDay || data?.today || localDay()
  const detail = useQuery({
    queryKey: ['day.show', selectedDay],
    queryFn: ({ signal }) => call<DayResult>('day.show', { day: selectedDay }, signal),
    enabled: tab === 'overview' && !!selectedDay,
    staleTime: 30_000,
  })
  const ledger = useQuery({
    queryKey: ['day.ledger', eventDay, offset],
    queryFn: ({ signal }) =>
      call<DayLedger>('day.ledger', { day: eventDay, offset, limit: 30 }, signal),
    enabled: tab === 'ledger',
    staleTime: 30_000,
  })
  const changeRange = (next: { from: string; to: string }) => {
    const days =
      (new Date(`${next.to}T00:00:00Z`).valueOf() - new Date(`${next.from}T00:00:00Z`).valueOf()) /
      86400000
    if (!next.from || !next.to || !Number.isFinite(days) || days < 0 || days > 365) {
      setRangeError('请选择先后有序、最多 366 天的日期范围。')
      return
    }
    setRangeError('')
    setRange(next)
    setDraftRange(next)
    setSelected('')
    setLedgerDate('')
    setOffset(0)
  }
  const refresh = () => {
    void snapshot.refetch()
    if (tab === 'overview' && selectedDay) void detail.refetch()
    if (tab === 'ledger') void ledger.refetch()
  }
  const totals = (data?.history ?? []).reduce(
    (sum, day) => ({
      days: sum.days + (day.state !== 'empty' ? 1 : 0),
      sessions: sum.sessions + day.session_count,
      tools: sum.tools + day.tool_calls,
      tokens: sum.tokens + day.work_tokens,
    }),
    { days: 0, sessions: 0, tools: 0, tokens: 0 },
  )

  return (
    <section className="as-workspace aid-workspace">
      <div className="as-page-heading">
        <div>
          <h1>AI Day</h1>
          <p>回顾每日协作、使用习惯与成长记录。</p>
        </div>
        <button
          type="button"
          className="button"
          onClick={refresh}
          disabled={snapshot.isFetching || detail.isFetching || ledger.isFetching}
        >
          <RefreshCw size={14} className={snapshot.isFetching ? 'spin' : ''} />
          刷新
        </button>
      </div>
      <div className="as-tabs" role="tablist" aria-label="AI Day 视图">
        {(
          [
            { value: 'overview', title: '日历与概览', icon: CalendarDays },
            { value: 'atlas', title: '徽章图鉴', icon: Star },
            { value: 'ledger', title: '事件明细', icon: List },
            { value: 'privacy', title: '数据与隐私', icon: ShieldCheck },
          ] as const
        ).map((item) => (
          <button
            key={item.value}
            type="button"
            role="tab"
            aria-selected={tab === item.value}
            onClick={() => setTab(item.value)}
            className={tab === item.value ? 'selected' : ''}
          >
            <item.icon size={15} />
            {item.title}
          </button>
        ))}
      </div>
      {tab === 'overview' && (
        <form
          className="as-range"
          onSubmit={(event) => {
            event.preventDefault()
            changeRange(draftRange)
          }}
        >
          <span className="as-range-title">回看</span>
          <div className="as-presets">
            {[30, 90, 180].map((days) => (
              <button
                type="button"
                key={days}
                onClick={() => changeRange(rangeFor(days, data?.today))}
                className={
                  range.from === rangeFor(days, data?.today).from &&
                  range.to === (data?.today || localDay())
                    ? 'selected'
                    : ''
                }
              >
                近 {days} 天
              </button>
            ))}
          </div>
          <div className="as-date-fields">
            <input
              type="date"
              aria-label="开始日期"
              required
              value={draftRange.from}
              onChange={(event) => setDraftRange({ ...draftRange, from: event.target.value })}
            />
            <span>—</span>
            <input
              type="date"
              aria-label="结束日期"
              required
              value={draftRange.to}
              onChange={(event) => setDraftRange({ ...draftRange, to: event.target.value })}
            />
            <button className="button" type="submit">
              查看
            </button>
          </div>
        </form>
      )}
      {rangeError && (
        <p className="as-inline-error" role="alert">
          {rangeError}
        </p>
      )}
      {snapshot.isError && <Notice error={snapshot.error} retry={() => void snapshot.refetch()} />}
      {snapshot.isPending ? (
        <Loading />
      ) : (
        data && (
          <>
            {tab === 'overview' && (
              <>
                <div className="as-stats">
                  <Stat
                    title="有记录的日子"
                    value={number(totals.days)}
                    hint={`${data.from} 至 ${data.to}`}
                  />
                  <Stat title="协作会话" value={number(totals.sessions)} />
                  <Stat title="工具调用" value={compact(totals.tools)} />
                  <Stat
                    title="工作 Token"
                    value={compact(totals.tokens)}
                    hint="输入、输出与缓存创建"
                  />
                </div>
                {data.history.length === 0 ? (
                  <Empty title="这个范围还没有日记录">
                    选择其他日期范围查看已生成的结果。新的 AI Day 日结果会在本机处理完成后出现。
                  </Empty>
                ) : (
                  <div className="aid-history-layout">
                    <aside className="aid-history">
                      <div className="as-section-caption">
                        <span>每日记录</span>
                        <span>{data.history.length} 天</span>
                      </div>
                      <div className="aid-history-list">
                        {data.history.map((day) => (
                          <button
                            className={`aid-history-day ${selectedDay === day.day ? 'selected' : ''}`}
                            key={day.day}
                            type="button"
                            onClick={() => {
                              setSelected(day.day)
                              setLedgerDate('')
                              setOffset(0)
                            }}
                          >
                            <span className="aid-date">
                              {day.day.slice(5).replace('-', ' / ')}
                              <small>
                                {day.day === data.today
                                  ? '今天'
                                  : new Date(`${day.day}T12:00:00`).toLocaleDateString('zh-CN', {
                                      weekday: 'short',
                                    })}
                              </small>
                            </span>
                            <span className="aid-day-title">
                              <strong>{day.title || '安静的一天'}</strong>
                              <small>
                                {day.state === 'empty'
                                  ? '没有活动记录'
                                  : `${day.session_count} 会话 · ${day.turn_count} 轮`}
                              </small>
                            </span>
                            <ChevronRight size={13} />
                          </button>
                        ))}
                      </div>
                    </aside>
                    <div className="aid-day-detail">
                      {detail.isPending ? (
                        <Loading />
                      ) : detail.isError ? (
                        <Notice error={detail.error} retry={() => void detail.refetch()} />
                      ) : (
                        detail.data && (
                          <DayDetail
                            result={detail.data}
                            onLedger={() => {
                              setLedgerDate(selectedDay || '')
                              setOffset(0)
                              setTab('ledger')
                            }}
                          />
                        )
                      )}
                    </div>
                  </div>
                )}
              </>
            )}
            {tab === 'atlas' && (
              <BadgeAtlas
                data={data}
                onDay={(day) => {
                  setSelected(day)
                  if (day < range.from || day > range.to) {
                    const next = rangeFor(30, day)
                    setRange(next)
                    setDraftRange(next)
                  }
                  setTab('overview')
                }}
              />
            )}
            {tab === 'ledger' && (
              <>
                <div className="as-section-title">
                  <div>
                    <h2>衍生事件明细</h2>
                    <p>查看已经记录的交互、工具与模型用量，不含对话原文。</p>
                  </div>
                  <label className="aid-ledger-date">
                    日期
                    <input
                      type="date"
                      value={eventDay}
                      onChange={(event) => {
                        if (event.target.value) {
                          setLedgerDate(event.target.value)
                          setOffset(0)
                        }
                      }}
                    />
                  </label>
                </div>
                {ledger.isPending ? (
                  <Loading />
                ) : ledger.isError ? (
                  <Notice error={ledger.error} retry={() => void ledger.refetch()} />
                ) : (
                  ledger.data && (
                    <>
                      <div className="as-table-wrap">
                        <table className="as-table">
                          <thead>
                            <tr>
                              <th>时间</th>
                              <th>来源</th>
                              <th>事件</th>
                              <th>模态</th>
                              <th>执行方式</th>
                              <th className="numeric">数量</th>
                              <th className="numeric">工作 Token</th>
                              <th className="numeric">缓存读取</th>
                            </tr>
                          </thead>
                          <tbody>
                            {ledger.data.items.map((event, index) => (
                              <tr key={`${event.occurred_at}-${index}`}>
                                <td className="as-mono">
                                  {new Date(event.occurred_at * 1000).toLocaleTimeString('zh-CN', {
                                    hour12: false,
                                  })}
                                </td>
                                <td>
                                  <span className="as-source-dot" />
                                  {sourceName(event.source)}
                                </td>
                                <td>{eventNames[event.event_type] || '其他事件'}</td>
                                <td>{metricNames[event.modality] || '其他模态'}</td>
                                <td>{executionNames[event.execution_mode] || '其他执行方式'}</td>
                                <td className="numeric">{number(event.quantity)}</td>
                                <td className="numeric">
                                  {number(
                                    event.input_tokens +
                                      event.output_tokens +
                                      event.cache_create_tokens,
                                  )}
                                </td>
                                <td className="numeric">{number(event.cache_read_tokens)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                        {ledger.data.items.length === 0 && (
                          <Empty title="这一天没有衍生事件">
                            日结果可能仍然保留，事件明细受保留期限影响。
                          </Empty>
                        )}
                      </div>
                      <div className="as-pagination">
                        <span>
                          {number(ledger.data.total)} 条记录
                          {ledger.data.total > 0 &&
                            ` · 当前 ${offset + 1}–${Math.min(offset + ledger.data.items.length, ledger.data.total)}`}
                        </span>
                        <div>
                          <button
                            type="button"
                            className="button"
                            aria-label="上一页事件"
                            disabled={offset === 0 || ledger.isFetching}
                            onClick={() => setOffset(Math.max(0, offset - 30))}
                          >
                            <ChevronLeft size={15} />
                          </button>
                          <button
                            type="button"
                            className="button"
                            aria-label="下一页事件"
                            disabled={
                              offset + 30 >= ledger.data.total ||
                              offset + 30 > 100000 ||
                              ledger.isFetching
                            }
                            onClick={() => setOffset(offset + 30)}
                          >
                            <ChevronRight size={15} />
                          </button>
                        </div>
                      </div>
                    </>
                  )
                )}
              </>
            )}
            {tab === 'privacy' &&
              (data.privacy ? (
                <PrivacyView privacy={data.privacy} />
              ) : (
                <Empty title="还没有 AI Day 数据">
                  记录建立后，这里会显示来源、保留期限和语义分类状态。
                </Empty>
              ))}
          </>
        )
      )}
    </section>
  )
}

function DayDetail({ result, onLedger }: { result: DayResult; onLedger: () => void }) {
  const features = result.features
  const semantic = Object.entries(features.semantic_counts || {})
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
  return (
    <>
      <div className="aid-detail-dateline">
        <span>
          {result.day} <small>{result.timezone}</small>
        </span>
        <span className="as-status">
          {result.provisional ? '当天记录 · 持续变化' : '已存日结果'}
        </span>
      </div>
      {result.state === 'empty' ? (
        <Empty title="安静的一天">这一天已经生成日结果，尚未记录到 AI 协作活动。</Empty>
      ) : (
        <>
          <div className="aid-hero">
            <div className="aid-hero-copy">
              <span className="as-eyebrow">
                {result.concept?.origin === 'user_corrected'
                  ? '你选择的今日主题'
                  : '这一天的协作主题'}
              </span>
              <h2>{result.concept?.title || '协作的一天'}</h2>
              <p>{result.concept?.explanation || '已记录这一天的协作活动。'}</p>
              <div className="aid-tags">
                {result.concept?.tags?.map((tag) => (
                  <span key={tag}>{tagNames[tag] || '协作特征'}</span>
                ))}
                {result.badge && <span>等级 {result.badge.level}</span>}
              </div>
            </div>
            <BadgeMark badge={result.badge} large />
          </div>
          <div className="as-card aid-evidence">
            <div className="as-section-caption">
              <h3>为什么是它</h3>
              <span>
                {result.concept
                  ? `可信度 ${Math.round(result.concept.confidence * 100)}%`
                  : '实际测量'}
              </span>
            </div>
            {(result.concept?.evidence || []).length > 0 ? (
              <div className="aid-evidence-list">
                {result.concept!.evidence.map((evidence, index) => (
                  <div key={`${evidence.metric}-${index}`}>
                    <span>
                      <span className="as-evidence-dot" />
                      {metricNames[evidence.metric] || '其他协作指标'}
                    </span>
                    <strong>{evidenceValue(evidence)}</strong>
                    {evidence.comparison && (
                      <small>{comparisonLabel(evidence.comparison, result.baseline_days)}</small>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="as-muted">这条记录没有附加证据项。</p>
            )}
            <div className="aid-feature-grid">
              <Stat title="会话" value={number(features.session_count)} />
              <Stat title="交互轮次" value={number(features.turn_count)} />
              <Stat title="工具调用" value={number(features.tool_calls)} />
              <Stat title="活跃时间" value={duration(features.active_seconds)} />
            </div>
          </div>
          {semantic.length > 0 && (
            <div className="as-card">
              <div className="as-section-caption">
                <h3>互动方式</h3>
                <span>已记录的语义标签</span>
              </div>
              <div className="aid-semantics">
                {semantic.map(([name, count]) => (
                  <span key={name}>
                    {metricNames[name] || '其他互动'}
                    <strong>{number(count)}</strong>
                  </span>
                ))}
              </div>
            </div>
          )}
          {result.concept?.origin === 'user_corrected' && result.concept.computed_title && (
            <p className="as-muted">
              你已将主题修正为「{result.concept.title}」；原始结果为「
              {result.concept.computed_title}」。
            </p>
          )}
          <div className="aid-data-note">
            <Database size={15} />
            <div>
              <p>
                {result.coverage?.complete
                  ? `覆盖 ${result.coverage.present_sources} 个来源`
                  : result.coverage
                    ? `覆盖 ${result.coverage.present_sources} / ${result.coverage.expected_sources} 个预期来源`
                    : '本机日记录'}{' '}
                · 对比基线 {result.baseline_days} 天
              </p>
              <small>
                结果生成于 {timestamp(result.generated_at)}
                {result.coverage?.missing_sources?.length
                  ? ` · 缺少 ${result.coverage.missing_sources.map(sourceName).join('、')}`
                  : ''}
              </small>
            </div>
            <button type="button" onClick={onLedger}>
              事件明细
              <ArrowRight size={13} />
            </button>
          </div>
        </>
      )}
    </>
  )
}

function BadgeAtlas({ data, onDay }: { data: DaySnapshot; onDay: (day: string) => void }) {
  const [filter, setFilter] = useState<'all' | 'unlocked'>('all')
  const [selected, setSelected] = useState('')
  const atlas = data.atlas
  if (!atlas)
    return <Empty title="徽章图鉴等待第一份记录">已有徽章进度后，你可以在这里回看每次达成。</Empty>
  const badges = atlas.badges.filter((badge) => filter === 'all' || badge.unlocked)
  return (
    <>
      <div className="as-section-title">
        <div>
          <h2>
            你的协作图鉴{' '}
            <span className="aid-atlas-count">
              {atlas.unlocked} / {atlas.total}
            </span>
          </h2>
          <p>每一枚徽章，都对应一种已经出现的协作方式。图鉴展示累计进度。</p>
        </div>
        <div className="as-presets">
          <button
            type="button"
            className={filter === 'all' ? 'selected' : ''}
            onClick={() => setFilter('all')}
          >
            全部
          </button>
          <button
            type="button"
            className={filter === 'unlocked' ? 'selected' : ''}
            onClick={() => setFilter('unlocked')}
          >
            已解锁
          </button>
        </div>
      </div>
      {badges.length === 0 ? (
        <Empty title="还没有解锁的徽章">切换到全部图鉴，了解已经记录的协作维度。</Empty>
      ) : (
        <div className="aid-badge-grid">
          {badges.map((badge) => (
            <article key={badge.id} className={`aid-badge-card ${badge.unlocked ? '' : 'locked'}`}>
              <button
                type="button"
                aria-expanded={selected === badge.id}
                onClick={() => setSelected(selected === badge.id ? '' : badge.id)}
              >
                <div className="aid-badge-top">
                  <BadgeMark badge={badge} />
                  <span className="as-status">
                    {badge.unlocked ? `等级 ${badge.level}` : '待解锁'}
                  </span>
                </div>
                <h3>{badge.name}</h3>
                <p>{badge.description}</p>
                <div className="aid-progress" aria-label={`累计 ${badge.qualified_days} 天`}>
                  <span style={{ width: `${Math.max(0, Math.min(1, badge.progress)) * 100}%` }} />
                </div>
                <div className="aid-badge-foot">
                  <span>累计 {badge.qualified_days} 天</span>
                  <span>
                    {badge.next_level_days > 0
                      ? `下一级 ${badge.next_level_days} 天`
                      : '已达最高级'}
                  </span>
                </div>
              </button>
              {selected === badge.id && (
                <div className="aid-qualified">
                  <strong>最近达成日期</strong>
                  {badge.qualified_dates?.length ? (
                    <div>
                      {badge.qualified_dates.map((day) => (
                        <button key={day} type="button" onClick={() => onDay(day)}>
                          {day}
                          <ArrowRight size={11} />
                        </button>
                      ))}
                    </div>
                  ) : (
                    <p>尚无达成记录</p>
                  )}
                </div>
              )}
            </article>
          ))}
        </div>
      )}
    </>
  )
}

function PrivacyView({ privacy }: { privacy: DayPrivacy }) {
  return (
    <>
      <div className="as-section-title">
        <div>
          <h2>数据与隐私</h2>
          <p>了解 AI Day 使用哪些来源，以及衍生记录的保留方式。</p>
        </div>
        <ShieldCheck size={27} className="as-green" />
      </div>
      <div className="as-stats aid-privacy-stats">
        <Stat
          title="语义分类"
          value={privacy.semantic_enabled ? '已开启' : '已关闭'}
          hint="使用已记录的语义统计"
        />
        <Stat
          title="事件保留期限"
          value={privacy.retention_days === 0 ? '长期保留' : `${privacy.retention_days} 天`}
          hint="日结果与徽章历史独立保留"
        />
        <Stat
          title="AI Day 原文保留"
          value={privacy.raw_content_retained ? '有原文' : '不保留'}
          hint="仅存储衍生事件与统计"
        />
      </div>
      <div className="as-card">
        <div className="as-section-caption">
          <h3>来源记录</h3>
          <span>{privacy.sources.length} 个来源</span>
        </div>
        <div className="as-table-wrap">
          <table className="as-table">
            <thead>
              <tr>
                <th>来源</th>
                <th>采集状态</th>
                <th>语义分类</th>
                <th className="numeric">衍生事件</th>
                <th>最近事件</th>
              </tr>
            </thead>
            <tbody>
              {privacy.sources.map((source) => (
                <tr key={source.source}>
                  <td>
                    <span className="as-source-dot" />
                    {sourceName(source.source)}
                  </td>
                  <td>
                    <StatePill enabled={source.enabled} />
                  </td>
                  <td>
                    <StatePill enabled={source.semantic_enabled && privacy.semantic_enabled} />
                  </td>
                  <td className="numeric">{number(source.event_count)}</td>
                  <td>{timestamp(source.last_event_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {privacy.sources.length === 0 && <p className="as-muted">还没有来源记录。</p>}
      </div>
      <p className="as-footnote">
        <CircleHelp size={14} />
        暂停的来源可能仍保留历史事件。这里显示当前状态，隐私与数据管理由本机应用维护。
      </p>
    </>
  )
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
  const [owner, setOwner] = useState('')
  const [dirty, setDirty] = useState(false)
  const [saved, setSaved] = useState(false)
  const client = useQueryClient()
  const settings = useQuery({
    queryKey: ['settings.get'],
    queryFn: ({ signal }) => call<SettingsData>('settings.get', {}, signal),
    staleTime: 30_000,
  })
  const data = settings.data
  useEffect(() => {
    if (data && !dirty) setOwner(data.owner_name)
  }, [data, dirty])
  const save = useMutation({
    mutationFn: () => call<SettingsData>('settings.preferences.save', { owner_name: owner.trim() }),
    onSuccess: (result) => {
      client.setQueryData(['settings.get'], result)
      setOwner(result.owner_name)
      setDirty(false)
      setSaved(true)
    },
  })
  const writable = boot.capabilities?.workspace_write === true
  const sectionItems = [
    { value: 'general', title: '通用偏好', subtitle: '个人信息与当前行为', icon: UserRound },
    { value: 'appearance', title: '外观', subtitle: '主题与工作台配色', icon: Palette },
    { value: 'model', title: '模型与连接', subtitle: '服务、凭证与来源', icon: Sparkles },
    { value: 'runtime', title: '运行与同步', subtitle: '工作台状态与索引', icon: Database },
  ] as const
  return (
    <section className="as-workspace settings-workspace">
      <div className="as-page-heading">
        <div>
          <h1>设置</h1>
          <p>外观与个人偏好、模型连接和本机运行状态。</p>
        </div>
        <button
          type="button"
          className="button"
          onClick={() => void settings.refetch()}
          disabled={settings.isFetching}
        >
          <RefreshCw size={14} className={settings.isFetching ? 'spin' : ''} />
          刷新状态
        </button>
      </div>
      <div className="as-settings-layout">
        <nav className="as-settings-nav" aria-label="设置分类">
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
        <div className="as-settings-content">
          {section === 'appearance' ? (
            <AppearanceSettings />
          ) : settings.isPending ? (
            <Loading />
          ) : settings.isError ? (
            <Notice error={settings.error} retry={() => void settings.refetch()} />
          ) : (
            data && (
              <>
                {section === 'general' && (
                  <>
                    <div className="as-section-title">
                      <div>
                        <h2>通用偏好</h2>
                        <p>让任务和工作记录以你熟悉的方式呈现。</p>
                      </div>
                    </div>
                    <div className="as-card">
                      <div className="as-section-caption">
                        <h3>个人信息</h3>
                        <UserRound size={16} />
                      </div>
                      <form
                        className="as-owner-form"
                        onSubmit={(event) => {
                          event.preventDefault()
                          save.mutate()
                        }}
                      >
                        <label htmlFor="settings-owner">如何称呼你</label>
                        <p>用于显示任务中的“我”，不会改变历史任务的创建者。</p>
                        <div>
                          <input
                            id="settings-owner"
                            value={owner}
                            placeholder="我"
                            maxLength={80}
                            disabled={!writable || save.isPending}
                            onChange={(event) => {
                              setOwner(event.target.value)
                              setDirty(true)
                              setSaved(false)
                              save.reset()
                            }}
                          />
                          <button
                            type="submit"
                            className="button primary"
                            disabled={!writable || !dirty || !owner.trim() || save.isPending}
                          >
                            {save.isPending ? (
                              <LoaderCircle size={14} className="spin" />
                            ) : (
                              <Check size={14} />
                            )}
                            保存昵称
                          </button>
                        </div>
                        {saved && (
                          <span className="as-save-status" role="status">
                            <Check size={13} />
                            昵称已保存
                          </span>
                        )}
                        {save.isError && (
                          <p className="as-inline-error" role="alert">
                            {errorText(save.error)}
                          </p>
                        )}
                        {!writable && <p className="as-muted">当前连接仅可查看设置。</p>}
                      </form>
                      <SettingRow title="统计时区" description="AI Day 和本机统计使用的时间范围">
                        {data.timezone}
                      </SettingRow>
                    </div>
                    <div className="as-card">
                      <div className="as-section-caption">
                        <h3>任务与自动整理</h3>
                        <span>当前偏好</span>
                      </div>
                      <SettingRow
                        title="创建任务后自动优化"
                        description="由本机应用的任务创建流程使用"
                      >
                        <StatePill enabled={data.preferences.todo_refine_on_add} />
                      </SettingRow>
                      <SettingRow title="自动采集" description="消息整理与待办发现的总开关">
                        <StatePill enabled={data.preferences.collection_enabled} />
                      </SettingRow>
                      <SettingRow title="采集间隔">
                        {data.preferences.collection_interval_minutes} 分钟
                      </SettingRow>
                      <SettingRow title="采集回看范围">
                        {data.preferences.collection_lookback_minutes} 分钟
                      </SettingRow>
                      <SettingRow title="消息保留期限">
                        {data.preferences.collection_message_retention_days === 0
                          ? '长期保留'
                          : `${data.preferences.collection_message_retention_days} 天`}
                      </SettingRow>
                      <SettingRow
                        title="Grok 实时额度"
                        description="允许本机额度服务读取实时账单状态"
                      >
                        <StatePill enabled={data.preferences.grok_live_quota} />
                      </SettingRow>
                    </div>
                    <p className="as-footnote">
                      <CircleHelp size={14} />
                      后台行为显示本机当前偏好，实际运行状态可在“运行与同步”中查看。
                    </p>
                  </>
                )}
                {section === 'model' && (
                  <>
                    <div className="as-section-title">
                      <div>
                        <h2>模型与连接</h2>
                        <p>查看文本服务与已登记的连接。凭证仅显示配置状态。</p>
                      </div>
                    </div>
                    <div className="as-card">
                      <div className="as-model-heading">
                        <span className="as-badge-mark">
                          <Sparkles />
                        </span>
                        <div>
                          <h3>{data.model.name || '未指定模型'}</h3>
                          <p>{data.model.source || '未指定来源'}</p>
                        </div>
                        <span className="as-status">文本服务</span>
                      </div>
                      <SettingRow title="模型名称">{data.model.name || '未配置'}</SettingRow>
                      <SettingRow title="来源名称">{data.model.source || '未配置'}</SettingRow>
                      <SettingRow title="本地凭证" description="不显示或传输 API Key">
                        {data.model.credential_status === 'unavailable' ? (
                          <span className="as-muted">暂时无法读取状态</span>
                        ) : (
                          <StatePill
                            enabled={data.model.credential_configured}
                            on="已配置"
                            off="未配置"
                          />
                        )}
                      </SettingRow>
                    </div>
                    <div className="as-card">
                      <div className="as-section-caption">
                        <h3>外部连接</h3>
                        <span>{data.providers.length} 项</span>
                      </div>
                      {data.providers.length === 0 ? (
                        <p className="as-muted as-provider-empty">尚未登记采集连接或额度来源。</p>
                      ) : (
                        data.providers.map((provider) => (
                          <SettingRow
                            key={`${provider.kind}-${provider.name}`}
                            title={provider.name}
                            description={provider.kind === 'quota' ? '额度来源' : '消息采集连接'}
                          >
                            <StatePill enabled={provider.enabled} on="已登记" off="采集已关闭" />
                          </SettingRow>
                        ))
                      )}
                    </div>
                    <p className="as-footnote">
                      <ShieldCheck size={14} />
                      连接凭证由本机管理。查看此页不会发起模型请求或测试连接。
                    </p>
                  </>
                )}
                {section === 'runtime' && (
                  <>
                    <div className="as-section-title">
                      <div>
                        <h2>运行与同步</h2>
                        <p>当前工作台的能力，以及已有会话索引的更新情况。</p>
                      </div>
                    </div>
                    <div className="as-runtime-banner">
                      <span className="as-runtime-icon">
                        <Layers3 size={25} />
                      </span>
                      <div>
                        <span className="as-eyebrow">WORKSPACE</span>
                        <h3>浏览器工作台</h3>
                        <p>后台同步、Agent Hook 与自动采集由本机应用负责。</p>
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
                          上次同步或索引读取未完成，请在本机应用中检查同步状态。
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
                          off="由本机应用负责"
                        />
                      </SettingRow>
                      <SettingRow title="自动采集">
                        <StatePill
                          enabled={data.runtime.collection}
                          on="运行中"
                          off="由本机应用负责"
                        />
                      </SettingRow>
                      <SettingRow title="模型执行">
                        <StatePill enabled={data.runtime.models} on="可用" off="此工作台未启用" />
                      </SettingRow>
                      <SettingRow title="Agent Hook">
                        <StatePill
                          enabled={data.runtime.agent_hooks}
                          on="运行中"
                          off="由本机应用负责"
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
  )
}
