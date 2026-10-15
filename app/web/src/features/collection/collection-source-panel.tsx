import { Button as AntButton, Switch } from 'antd'
import { BellOff, History, Pencil } from 'lucide-react'
import type { LegacyApiMethod } from '../../api-contracts'
import { Markdown } from '../../components/markdown'
import type { CollectionConnectorUsage, CollectionRun, CollectionSource } from './collection-types'
import {
  runLabels,
  healthLabels,
  healthTones,
  rate,
  total,
  stamp,
  sourceName,
} from './collection-format'
/**
 * One card per connector: what state its last runs left it in, and how hard it
 * ran today and over the last seven days. For the dingtalk connector this is the
 * only place the `dws` CLI it drives is visible in the browser.
 */
export function ConnectorUsage({ usage }: { usage?: CollectionConnectorUsage[] }) {
  if (!usage?.length) return null
  return (
    <section className="collection-connectors">
      <div className="collection-column-label">
        连接器调用 <span>{usage.length}</span>
      </div>
      {usage.map((item) => {
        const { health, today, week } = item
        const tone = healthTones[health.status] || 'danger'
        return (
          <article className={`collection-connector tone-${tone}`} key={health.connector}>
            <header>
              <strong>{health.connector}</strong>
              <span className="collection-connector-status">
                {healthLabels[health.status] || health.status}
              </span>
            </header>
            <dl>
              <div>
                <dt>今天</dt>
                <dd>
                  {total(today.runs)} 次运行 · 失败 {total(today.failed)}
                  {rate(today.failed, today.runs)} · 读取 {total(today.fetched)} 条
                </dd>
              </div>
              <div>
                <dt>最近 7 天</dt>
                <dd>
                  {total(week.runs)} 次运行 · 失败 {total(week.failed)}
                  {rate(week.failed, week.runs)} · 读取 {total(week.fetched)} 条
                </dd>
              </div>
              <div>
                <dt>健康窗口</dt>
                <dd>
                  最近 {health.recent_runs || 0} 次里失败 {health.recent_failures || 0} 次
                  {health.consecutive_failures
                    ? ` · 当前连续失败 ${health.consecutive_failures} 次`
                    : ''}
                </dd>
              </div>
              <div>
                <dt>最近运行</dt>
                <dd>
                  {stamp(week.last_run_at)}
                  {week.last_failed_at ? ` · 最近失败 ${stamp(week.last_failed_at)}` : ''}
                </dd>
              </div>
            </dl>
            {health.error && <p className="collection-failure">{health.error}</p>}
            {health.status === 'auth_required' && health.login_command && (
              <p className="collection-connector-login">
                重新登录：<code>{health.login_command}</code>
              </p>
            )}
          </article>
        )
      })}
      <p className="collection-connector-note">
        统计单位是采集运行，不是外部命令次数：一次运行会调用多次连接器背后的
        CLI，而连接器协议只回报消息，不回报命令数。
      </p>
    </section>
  )
}

export function RunStatus({ run }: { run?: CollectionRun }) {
  if (!run) return <p className="collection-muted">尚无采集运行记录。</p>
  return (
    <div className="collection-run-status">
      <span className={run.status === 'failed' ? 'collection-failure' : ''}>
        {runLabels[run.status] || run.status}
      </span>
      <time>{stamp(run.finished_at || run.started_at)}</time>
      <p>
        读取 {run.fetched_count} 条 · 分析 {run.analyzed_count} 条
        {run.failed_count > 0 && ` · 失败 ${run.failed_count} 条`}
      </p>
      {run.error && <p className="collection-failure">{run.error}</p>}
      {run.status === 'running' && <p>运行记录尚未结束，无法据此确认外部进程仍在运行。</p>}
    </div>
  )
}

export function SourcePanel({
  source,
  run,
  writable,
  busy,
  change,
  onEdit,
  onHistory,
}: {
  source: CollectionSource
  run?: CollectionRun
  writable: boolean
  busy: boolean
  change: (
    method: Extract<LegacyApiMethod, `collect.${string}`>,
    input: unknown,
    message: string,
  ) => void
  onEdit: () => void
  onHistory: () => void
}) {
  return (
    <>
      <h2>{sourceName(source)}</h2>
      <div className="collection-source-panel-actions">
        <AntButton htmlType="button" className="button" onClick={onHistory}>
          <History size={14} />
          本地消息
        </AntButton>
        {writable && (
          <AntButton htmlType="button" className="button" onClick={onEdit} disabled={busy}>
            <Pencil size={14} />
            编辑来源
          </AntButton>
        )}
      </div>
      <p className="collection-muted">
        {source.connector} · {source.kind}
      </p>
      <div className="collection-source-settings">
        <label>
          <span>
            <strong>启用采集</strong>
            <small>允许采集服务处理此来源</small>
          </span>
          <Switch
            size="small"
            aria-label="启用采集"
            checked={source.enabled}
            disabled={!writable || busy}
            onChange={(checked) =>
              change(
                'collect.source.enabled',
                { source_id: source.id, enabled: checked },
                checked ? '来源已启用' : '来源已停用',
              )
            }
          />
        </label>
        <label>
          <span>
            <strong>
              <BellOff size={13} /> 静音通知
            </strong>
            <small>保留采集结果与未读状态</small>
          </span>
          <Switch
            size="small"
            aria-label="静音通知"
            checked={source.muted}
            disabled={!writable || busy}
            onChange={(checked) =>
              change(
                'collect.source.muted',
                { source_id: source.id, muted: checked },
                checked ? '已静音此来源' : '已恢复此来源通知',
              )
            }
          />
        </label>
      </div>
      {!writable && <p className="collection-muted">当前连接只读，来源设置不可更改。</p>}
      <dl className="collection-source-facts">
        <dt>策略</dt>
        <dd>{source.strategy === 'observe' ? '观察与总结' : '任务跟进'}</dd>
        <dt>决策单位</dt>
        <dd>{source.decision_unit === 'message' ? '逐条消息' : '对话窗口'}</dd>
        <dt>采集间隔</dt>
        <dd>{source.interval_minutes} 分钟</dd>
        <dt>项目</dt>
        <dd>{source.project || '未指定'}</dd>
        <dt>优先级</dt>
        <dd>{source.priority}</dd>
      </dl>
      {source.instruction && (
        <section className="collection-detail-section">
          <h3>关注指令</h3>
          <Markdown text={source.instruction} />
        </section>
      )}
      {source.exclude_pattern && (
        <section className="collection-detail-section">
          <h3>排除规则</h3>
          <code>{source.exclude_pattern}</code>
        </section>
      )}
      <section className="collection-detail-section">
        <h3>最近运行</h3>
        <RunStatus run={run} />
      </section>
    </>
  )
}
