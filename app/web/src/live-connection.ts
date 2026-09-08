import { useSyncExternalStore } from 'react'

let connected = false
const listeners = new Set<() => void>()
const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
export function setLiveConnection(value: boolean) {
  if (connected === value) return
  connected = value
  for (const listener of listeners) listener()
}
export function useLiveConnection() {
  return useSyncExternalStore(
    subscribe,
    () => connected,
    () => false,
  )
}
