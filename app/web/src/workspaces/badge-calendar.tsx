import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { ChevronLeft, ChevronRight, Fingerprint, Layers3, Sparkles, Zap } from 'lucide-react'
import { call } from '../api'
import { Notice } from '../editor'
import { QueryLoading } from '../query-loading'
import { adjacentMonth, calendarMonth, localCalendarDay, monthDays } from '../badge-calendar'
import type { DaySnapshot } from './aiday-settings-types'
import './badge-calendar.css'

function Badge({ family }: { family?: string }) {
  const Icon =
    family === 'lens'
      ? Fingerprint
      : family === 'orbit'
        ? Layers3
        : family === 'grid'
          ? Zap
          : Sparkles
  return (
    <span className={`calendar-badge badge-${family || 'crystal'}`} aria-hidden="true">
      <Icon strokeWidth={1.5} />
    </span>
  )
}

export function BadgeCalendar() {
  const [params, setParams] = useSearchParams()
  const today = localCalendarDay()
  const month = calendarMonth(params.get('month'), today)
  const { cells, from, to } = monthDays(month)
  const range = { from, to: to > today ? today : to }
  const snapshot = useQuery({
    queryKey: ['day.snapshot', range],
    queryFn: ({ signal }) => call<DaySnapshot>('day.snapshot', range, signal),
    staleTime: 30_000,
  })
  const history = new Map(snapshot.data?.history.map((day) => [day.day, day]) || [])
  const selected =
    cells.includes(params.get('day')) && params.get('day')! <= today
      ? params.get('day')!
      : month === today.slice(0, 7)
        ? today
        : from
  const record = history.get(selected)
  const badges = new Map(snapshot.data?.atlas?.badges.map((badge) => [badge.id, badge]) || [])
  const badge = record?.badge_id ? badges.get(record.badge_id) : undefined
  function changeMonth(next: string) {
    setParams((current) => {
      const value = new URLSearchParams(current)
      value.set('month', next)
      value.delete('day')
      return value
    })
  }
  return (
    <section className="badge-calendar" aria-label="徽章日历">
      <header className="badge-calendar-heading">
        <div>
          <h2>{month.replace('-', ' 年 ')} 月</h2>
          <p>每天一枚徽章，留下与 AI 协作的足迹。</p>
        </div>
        <div className="badge-calendar-navigation">
          <button
            className="button"
            aria-label="上个月"
            disabled={month === '1970-01'}
            onClick={() => changeMonth(adjacentMonth(month, -1))}
          >
            <ChevronLeft size={16} />
          </button>
          <button
            className="button"
            onClick={() => {
              changeMonth(today.slice(0, 7))
            }}
          >
            本月
          </button>
          <button
            className="button"
            aria-label="下个月"
            disabled={month >= today.slice(0, 7)}
            onClick={() => changeMonth(adjacentMonth(month, 1))}
          >
            <ChevronRight size={16} />
          </button>
        </div>
      </header>
      {snapshot.isPending ? (
        <QueryLoading
          className="badge-calendar-loading"
          text="正在读取徽章…"
          onRetry={() => void snapshot.refetch()}
        />
      ) : snapshot.isError ? (
        <Notice error={snapshot.error} retry={() => void snapshot.refetch()} />
      ) : (
        <>
          <div className="badge-calendar-week" aria-hidden="true">
            {['一', '二', '三', '四', '五', '六', '日'].map((day) => (
              <span key={day}>{day}</span>
            ))}
          </div>
          <div className="badge-calendar-grid" role="group" aria-label={`${month} 每日徽章`}>
            {cells.map((day, index) => {
              if (!day) return <div className="badge-calendar-blank" key={`blank-${index}`} />
              const item = history.get(day)
              const award = item?.badge_id ? badges.get(item.badge_id) : undefined
              const title = item?.badge_id ? item.title || award?.name || '每日徽章' : ''
              return (
                <button
                  key={day}
                  className={`badge-calendar-day ${day === today ? 'is-today' : ''}`}
                  aria-pressed={day === selected}
                  aria-label={`${day}${day === today ? ' 今天' : ''} · ${title || (day > today ? '尚未到来' : '暂无徽章')}`}
                  title={`${day} · ${title || '暂无徽章'}`}
                  disabled={day > today}
                  onClick={() =>
                    setParams((current) => {
                      const value = new URLSearchParams(current)
                      value.set('day', day)
                      return value
                    })
                  }
                >
                  <time dateTime={day}>{Number(day.slice(-2))}</time>
                  {title ? (
                    <>
                      <Badge family={award?.family} />
                      <span className="badge-calendar-title">{title}</span>
                    </>
                  ) : (
                    <span className="badge-calendar-dot" aria-hidden="true">
                      ·
                    </span>
                  )}
                </button>
              )
            })}
          </div>
          <section className="badge-calendar-detail" aria-label="当天徽章" aria-live="polite">
            {record?.badge_id && <Badge family={badge?.family} />}
            <div>
              <time dateTime={selected}>
                {selected}
                {selected === today ? ' · 今天' : ''}
              </time>
              <h3>{record?.badge_id ? record.title || badge?.name || '每日徽章' : '暂无徽章'}</h3>
              <p>
                {record?.badge_id
                  ? record.explanation || badge?.description || '这一天的 AI 协作纪念。'
                  : record?.state === 'empty'
                    ? '这一天没有记录到 AI 协作活动。'
                    : '这一天尚未生成徽章，有记录后会自动展示。'}
              </p>
            </div>
          </section>
        </>
      )}
    </section>
  )
}
