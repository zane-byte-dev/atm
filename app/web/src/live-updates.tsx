import { useEffect } from 'react'
import { useLocation } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import type { Bootstrap } from './types'

const routes: Record<string, string[]> = {
  tasks: ['todos', 'presence', 'jobs'], collection: ['collection', 'jobs'],
  agents: ['sessions', 'presence', 'jobs'], knowledge: ['knowledge', 'memory'],
  usage: ['usage', 'jobs'], 'ai-day': ['day', 'jobs'], settings: ['settings', 'jobs', 'presence'],
}
const queryDomains: Record<string, string[]> = {
  todos: ['todos', 'todo', 'doc', 'dependencies'],
  collection: ['collection'], sessions: ['activity-status', 'activity-sessions', 'activity-search', 'activity-session'],
  usage: ['activity-usage', 'activity-quota'], knowledge: ['knowledge'], memory: ['knowledge'],
  day: ['day.snapshot', 'day.show', 'day.ledger'], settings: ['settings.get'],
  jobs: ['runtime-jobs', 'runtime-job'], presence: ['activity-status', 'presence'],
}

export function useLiveUpdates(boot: Bootstrap | undefined) {
  const query = useQueryClient()
  const { pathname } = useLocation()
  const route = pathname.split('/')[1] || 'tasks'
  useEffect(() => {
    if (boot?.capabilities?.sse !== true) return
    const domains = routes[route] ?? ['todos']
    let source: EventSource | undefined
    let stopped = false
    let lastProbe = 0
    let probing = false
    const invalidate = (changed: string[]) => {
      const keys = new Set(changed.flatMap(domain => queryDomains[domain] ?? []))
      void query.invalidateQueries({predicate: item => keys.has(String(item.queryKey[0]))})
    }
    const connect = () => {
      source?.close()
      if (stopped || document.visibilityState === 'hidden') return
      source = new EventSource(`/api/v1/events?domains=${domains.join(',')}`)
      source.onerror = () => {
        if (probing || Date.now() - lastProbe < 5000 || stopped) return
        lastProbe = Date.now()
        probing = true
        void fetch('/api/v1/bootstrap', { credentials: 'same-origin', cache: 'no-store' })
          .then(response => {
            if (!stopped && response.status === 401) {
              source?.close()
              window.dispatchEvent(new Event('atm:session-expired'))
            }
          }).catch(() => { /* EventSource retries temporary disconnects. */ })
          .finally(() => { probing = false })
      }
      source.addEventListener('resource.changed', event => {
        try {
          const data = JSON.parse((event as MessageEvent).data) as {domains?: unknown}
          if (Array.isArray(data.domains)) invalidate(data.domains.filter((value): value is string => typeof value === 'string'))
        } catch { /* Polling and refocus remain available if an event is malformed. */ }
      })
      source.addEventListener('reset', () => invalidate(domains))
      source.addEventListener('session.expired', () => {
        source?.close()
        window.dispatchEvent(new Event('atm:session-expired'))
      })
    }
    const visibility = () => { connect(); if (document.visibilityState === 'visible') invalidate(domains) }
    connect()
    document.addEventListener('visibilitychange', visibility)
    return () => { stopped = true; source?.close(); document.removeEventListener('visibilitychange', visibility) }
  }, [boot, query, route])
}
