import { useCallback, useEffect, useSyncExternalStore } from 'react'
import { useLocation } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import type { Bootstrap } from './types'
import { invalidateDomains, subscriptionDomains } from './query-domains'
import { setLiveConnection } from './live-connection'

export function useLiveUpdates(boot: Bootstrap | undefined) {
  const query = useQueryClient()
  const { pathname } = useLocation()
  const route = pathname.split('/')[1] || 'tasks'
  const subscribe = useCallback(
    (notify: () => void) => query.getQueryCache().subscribe(notify),
    [query],
  )
  const getDomains = useCallback(() => subscriptionDomains(query, route).join(','), [query, route])
  const domainKey = useSyncExternalStore(subscribe, getDomains, getDomains)
  useEffect(() => {
    setLiveConnection(false)
    if (boot?.capabilities?.sse !== true) return
    const domains = domainKey.split(',')
    let source: EventSource | undefined
    let stopped = false
    let lastProbe = 0
    let probing = false
    const invalidate = (changed: string[]) => {
      void invalidateDomains(query, changed)
    }
    const connect = () => {
      source?.close()
      setLiveConnection(false)
      if (stopped || document.visibilityState === 'hidden') return
      source = new EventSource(`/api/v1/events?domains=${domains.join(',')}`)
      const current = source
      source.onopen = () => {
        if (stopped || source !== current) return
        setLiveConnection(true)
        invalidate(domains)
      }
      source.onerror = () => {
        if (stopped || source !== current) return
        setLiveConnection(false)
        if (probing || Date.now() - lastProbe < 5000 || stopped) return
        lastProbe = Date.now()
        probing = true
        void fetch('/api/v1/bootstrap', { credentials: 'same-origin', cache: 'no-store' })
          .then((response) => {
            if (!stopped && response.status === 401) {
              source?.close()
              window.dispatchEvent(new Event('atm:session-expired'))
            }
          })
          .catch(() => {
            /* EventSource retries temporary disconnects. */
          })
          .finally(() => {
            probing = false
          })
      }
      source.addEventListener('resource.changed', (event) => {
        try {
          const data = JSON.parse((event as MessageEvent).data) as { domains?: unknown }
          if (Array.isArray(data.domains))
            invalidate(data.domains.filter((value): value is string => typeof value === 'string'))
        } catch {
          /* Polling and refocus remain available if an event is malformed. */
        }
      })
      source.addEventListener('reset', () => invalidate(domains))
      source.addEventListener('session.expired', () => {
        setLiveConnection(false)
        source?.close()
        window.dispatchEvent(new Event('atm:session-expired'))
      })
    }
    const visibility = () => {
      connect()
      if (document.visibilityState === 'visible') invalidate(domains)
    }
    connect()
    document.addEventListener('visibilitychange', visibility)
    return () => {
      stopped = true
      setLiveConnection(false)
      source?.close()
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [boot, query, domainKey])
}
