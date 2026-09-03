import { useSyncExternalStore } from 'react'
import { createThemePreference, themes, themeStorageKey } from './theme-preference'

const preference = createThemePreference(() => window.localStorage)

function applyTheme() {
  const { theme } = preference.getSnapshot()
  document.documentElement.dataset.theme = theme
  document
    .querySelector('meta[name="color-scheme"]')
    ?.setAttribute('content', themes.find((item) => item.id === theme)!.scheme)
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute(
      'content',
      getComputedStyle(document.documentElement).getPropertyValue('--canvas').trim(),
    )
}

// Apply before React renders, including loading and reconnect screens.
applyTheme()
preference.subscribe(applyTheme)
window.addEventListener('storage', (event) => {
  if (event.key !== null && event.key !== themeStorageKey) return
  try {
    if (event.storageArea === window.localStorage) preference.acceptExternal(event.newValue)
  } catch {
    // A blocked storage area does not prevent local theme changes.
  }
})

export function useTheme() {
  const snapshot = useSyncExternalStore(preference.subscribe, preference.getSnapshot)
  return { ...snapshot, setTheme: preference.setTheme }
}
