export const themes = [
  { id: 'minimal', name: '极简黑白', description: '黑白与浅灰，专注内容本身。', scheme: 'light' },
  { id: 'slate', name: '石墨雾蓝', description: '清爽的灰白底色，搭配柔和雾蓝。', scheme: 'light' },
  { id: 'sage', name: '鼠尾草绿', description: '带一点自然绿意，平静而轻盈。', scheme: 'light' },
  { id: 'sand', name: '暖砂', description: '温暖的纸张底色，搭配陶土色。', scheme: 'light' },
  { id: 'night', name: '午夜', description: '深石墨底色，让内容在暗处也清晰。', scheme: 'dark' },
] as const

export type ThemeID = (typeof themes)[number]['id']
export const defaultTheme: ThemeID = 'minimal'
export const themeStorageKey = 'atm.web.theme.v1'

export function parseTheme(value: string | null): ThemeID {
  return themes.find((theme) => theme.id === value)?.id ?? defaultTheme
}

type ThemeStorage = Pick<Storage, 'getItem' | 'setItem'>
type ThemeSnapshot = { theme: ThemeID; persisted: boolean }

// Storage is optional: a browser that blocks it can still switch themes for this visit.
export function createThemePreference(storage: () => ThemeStorage | null) {
  let snapshot: ThemeSnapshot = { theme: defaultTheme, persisted: false }
  try {
    const target = storage()
    if (target) snapshot = { theme: parseTheme(target.getItem(themeStorageKey)), persisted: true }
  } catch {
    // Keep the default if browser preferences cannot be read.
  }
  const listeners = new Set<() => void>()
  function update(next: ThemeSnapshot) {
    if (snapshot.theme === next.theme && snapshot.persisted === next.persisted) return
    snapshot = next
    listeners.forEach((notify) => notify())
  }
  return {
    getSnapshot: () => snapshot,
    subscribe(notify: () => void) {
      listeners.add(notify)
      return () => {
        listeners.delete(notify)
      }
    },
    setTheme(theme: ThemeID) {
      let persisted = false
      try {
        const target = storage()
        if (target) {
          target.setItem(themeStorageKey, theme)
          persisted = true
        }
      } catch {
        // Apply the choice even when localStorage is unavailable or full.
      }
      update({ theme, persisted })
    },
    acceptExternal(value: string | null) {
      update({ theme: parseTheme(value), persisted: true })
    },
  }
}
