import { useEffect, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MessageSquare } from 'lucide-react'
import { call } from '../api'
import type { SessionStatus } from './activity-types'
import './activity-shared.css'

export const int = (value: number) => new Intl.NumberFormat('zh-CN').format(value || 0)
export const agentName = (value: string) =>
  ({
    codex: 'Codex',
    claude: 'Claude',
    cursor: 'Cursor',
    grokbuild: 'Grok Build',
    antigravity: 'Antigravity',
  })[value] || value
export const dateTime = (value?: string) => {
  if (!value) return '尚无记录'
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? value
    : date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
}
export function useForeground() {
  const [foreground, setForeground] = useState(document.visibilityState === 'visible')
  useEffect(() => {
    const update = () => setForeground(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', update)
    return () => document.removeEventListener('visibilitychange', update)
  }, [])
  return foreground
}

export function useActivityStatus(foreground: boolean) {
  return useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: foreground ? 60000 : false,
    refetchIntervalInBackground: false,
  })
}

export function EmptyActivity({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="activity-empty">
      <MessageSquare size={26} />
      <h3>{title}</h3>
      {children && <p>{children}</p>}
    </div>
  )
}
