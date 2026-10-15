import { Button as AntButton, Tooltip } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { ChevronLeft, ChevronRight, Fingerprint, Layers3, Sparkles, Zap } from 'lucide-react'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import { QueryLoading } from '../../components/query-loading'
import { adjacentMonth, calendarMonth, localCalendarDay, monthDays } from './badge-calendar-model'
import type { DaySnapshot } from '../settings/aiday-settings-types'
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
  const badges = new Map(snapshot.data?.atlas?.badges.map((badge) => [badge.id, badge]) || [])
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
        </div>
        <div className="badge-calendar-navigation">
          <AntButton
            className="button"
            aria-label="上个月"
            disabled={month === '1970-01'}
            onClick={() => changeMonth(adjacentMonth(month, -1))}
          >
            <ChevronLeft size={16} />
          </AntButton>
          <AntButton
            className="button"
            onClick={() => {
              changeMonth(today.slice(0, 7))
            }}
          >
            本月
          </AntButton>
          <AntButton
            className="button"
            aria-label="下个月"
            disabled={month >= today.slice(0, 7)}
            onClick={() => changeMonth(adjacentMonth(month, 1))}
          >
            <ChevronRight size={16} />
          </AntButton>
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
                <Tooltip key={day} title={`${day} · ${title || '暂无徽章'}`}>
                  <AntButton
                    type="text"
                    htmlType="button"
                    className={`badge-calendar-day ${day === today ? 'is-today' : ''}`}
                    aria-pressed={day === selected}
                    aria-label={`${day}${day === today ? ' 今天' : ''} · ${title || (day > today ? '尚未到来' : '暂无徽章')}`}
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
                      <Badge family={award?.family} />
                    ) : (
                      <span className="badge-calendar-dot" aria-hidden="true" />
                    )}
                  </AntButton>
                </Tooltip>
              )
            })}
          </div>
        </>
      )}
    </section>
  )
}
