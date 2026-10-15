import { App } from 'antd'
import { useMemo, useSyncExternalStore } from 'react'
import { notificationStore, type MessageKind } from './notification-store'

// Share toast identities across the submitting component and global job observer.
const toastKeys = new Map<string, string>()

export function useWorkspaceMessages() {
  return useSyncExternalStore(notificationStore.subscribe, notificationStore.getSnapshot)
}
export function useWorkspaceNotifications() {
  const { notification } = App.useApp()
  return useMemo(() => {
    const open = (
      config: Parameters<typeof notification.open>[0] & {
        silent?: boolean
        activity?: boolean
        href?: string
      },
    ) => {
      const { silent, activity, href, ...toast } = config
      const id = String(config.key || crypto.randomUUID())
      notificationStore.publish({
        id,
        title: typeof config.title === 'string' ? config.title : '操作通知',
        description: typeof config.description === 'string' ? config.description : '',
        kind: (config.type || 'info') as MessageKind,
        href,
        activity,
        ...(activity ? { readAt: Date.now() } : {}),
      })
      if (!silent) {
        // A new timer mode needs a fresh toast instance: updating a running timer
        // to duration=0 can close it before the library applies its effect reset.
        // Keep the persisted message ID stable so history still has one job row.
        const toastKey = `${id}:${config.duration === 0 ? 'persistent' : 'timed'}`
        const previousKey = toastKeys.get(id)
        if (previousKey && previousKey !== toastKey) notification.destroy(previousKey)
        toastKeys.delete(id)
        toastKeys.set(id, toastKey)
        if (toastKeys.size > 200) toastKeys.delete(toastKeys.keys().next().value!)
        notification.open({ ...toast, key: toastKey })
      }
    }
    return {
      open,
      error: (config: Parameters<typeof notification.error>[0]) =>
        open({ ...config, type: 'error' }),
    }
  }, [notification])
}
