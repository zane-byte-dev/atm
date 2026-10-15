export type MessageKind = 'success' | 'error' | 'info' | 'warning'
export type WorkspaceMessage = {
  id: string
  title: string
  description: string
  kind: MessageKind
  createdAt: number
  readAt?: number
  activity?: boolean
  href?: string
}
const key = 'atm.web.notifications.v1'
const listeners = new Set<() => void>()
let snapshot: WorkspaceMessage[] = []
try {
  const stored: unknown = JSON.parse(localStorage.getItem(key) || '[]')
  if (Array.isArray(stored))
    snapshot = stored
      .filter(
        (item) =>
          item &&
          typeof item.id === 'string' &&
          typeof item.title === 'string' &&
          typeof item.description === 'string' &&
          typeof item.createdAt === 'number' &&
          ['success', 'error', 'info', 'warning'].includes(item.kind),
      )
      .slice(0, 200)
} catch {
  /* Storage may be unavailable; the current session still works. */
}
const update = (messages: WorkspaceMessage[]) => {
  snapshot = messages
  try {
    localStorage.setItem(key, JSON.stringify(messages))
  } catch {
    /* Keep in memory. */
  }
  listeners.forEach((listener) => listener())
}
export const notificationStore = {
  getSnapshot: () => snapshot,
  subscribe: (listener: () => void) => {
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  },
  publish: (message: Omit<WorkspaceMessage, 'createdAt'>) =>
    update(
      [
        { ...message, createdAt: Date.now() },
        ...snapshot.filter((item) => item.id !== message.id),
      ].slice(0, 200),
    ),
  read: (id?: string) =>
    update(
      snapshot.map((item) =>
        !item.readAt && (!id || item.id === id) ? { ...item, readAt: Date.now() } : item,
      ),
    ),
  clear: () => update([]),
}
if (typeof window !== 'undefined')
  window.addEventListener('storage', (event) => {
    if (event.key !== key) return
    try {
      const messages = JSON.parse(event.newValue || '[]')
      if (Array.isArray(messages)) {
        snapshot = messages
        listeners.forEach((listener) => listener())
      }
    } catch {
      /* Ignore invalid external data. */
    }
  })
