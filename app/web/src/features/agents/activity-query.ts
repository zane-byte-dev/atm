import { useQuery } from '@tanstack/react-query'
import { call } from '../../api'
import type { SessionStatus } from './activity-types'

export function useActivityStatus(foreground: boolean) {
  return useQuery({
    queryKey: ['activity-status'],
    queryFn: ({ signal }) => call<SessionStatus>('session.status', {}, signal),
    refetchInterval: foreground ? 60000 : false,
    refetchIntervalInBackground: false,
  })
}
