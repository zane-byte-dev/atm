import assert from 'node:assert/strict'
import test from 'node:test'
import { createThemePreference, defaultTheme, themeStorageKey } from '../src/theme-preference.ts'

class MemoryStorage {
  values = new Map<string, string>()
  writes: Array<{ key: string; value: string }> = []

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
    this.writes.push({ key, value })
  }
}

test('a chosen theme is restored when the preference store is recreated', () => {
  const storage = new MemoryStorage()
  const original = createThemePreference(() => storage)
  original.setTheme('night')

  assert.deepEqual(original.getSnapshot(), { theme: 'night', persisted: true })
  assert.deepEqual(createThemePreference(() => storage).getSnapshot(), {
    theme: 'night',
    persisted: true,
  })
  assert.deepEqual(storage.writes, [{ key: themeStorageKey, value: 'night' }])
})

test('missing and unrecognized saved themes use the default without rewriting storage', () => {
  for (const saved of [null, '', '{', 'unknown-future-theme']) {
    const storage = new MemoryStorage()
    if (saved !== null) storage.values.set(themeStorageKey, saved)

    const preference = createThemePreference(() => storage)
    assert.deepEqual(preference.getSnapshot(), { theme: defaultTheme, persisted: true })
    assert.equal(storage.getItem(themeStorageKey), saved)
    assert.deepEqual(storage.writes, [])
  }
})

test('unavailable storage and a denied storage getter allow changes for the current visit', () => {
  const denied = () => {
    throw new DOMException('Storage is disabled', 'SecurityError')
  }
  for (const storage of [() => null, denied]) {
    const preference = createThemePreference(storage)
    assert.deepEqual(preference.getSnapshot(), { theme: defaultTheme, persisted: false })

    preference.setTheme('sage')
    assert.deepEqual(preference.getSnapshot(), { theme: 'sage', persisted: false })
  }
})

test('denied storage reads and writes leave the preference usable in memory', () => {
  const denied = () => {
    throw new DOMException('Storage access denied', 'SecurityError')
  }
  const preference = createThemePreference(() => ({ getItem: denied, setItem: denied }))
  let notifications = 0
  preference.subscribe(() => {
    notifications += 1
  })

  assert.deepEqual(preference.getSnapshot(), { theme: defaultTheme, persisted: false })
  preference.setTheme('sand')
  assert.deepEqual(preference.getSnapshot(), { theme: 'sand', persisted: false })
  assert.equal(notifications, 1)
})

test('a failed write applies the new theme without claiming it will survive a reload', () => {
  const storage = new MemoryStorage()
  storage.values.set(themeStorageKey, 'sage')
  const preference = createThemePreference(() => ({
    getItem: (key) => storage.getItem(key),
    setItem() {
      throw new DOMException('Storage is full', 'QuotaExceededError')
    },
  }))

  preference.setTheme('night')
  assert.deepEqual(preference.getSnapshot(), { theme: 'night', persisted: false })
  assert.deepEqual(createThemePreference(() => storage).getSnapshot(), {
    theme: 'sage',
    persisted: true,
  })
  assert.deepEqual(storage.writes, [])
})

test('retrying the current theme after storage recovers updates its persistence status', () => {
  const storage = new MemoryStorage()
  let available = false
  const preference = createThemePreference(() => (available ? storage : null))
  preference.setTheme('night')
  let notifications = 0
  preference.subscribe(() => {
    notifications += 1
  })

  available = true
  preference.setTheme('night')

  assert.deepEqual(preference.getSnapshot(), { theme: 'night', persisted: true })
  assert.equal(notifications, 1)
  assert.equal(storage.getItem(themeStorageKey), 'night')
})

test('external theme changes and clearing notify subscribers without storage writeback', () => {
  const storage = new MemoryStorage()
  const preference = createThemePreference(() => storage)
  const received: ReturnType<typeof preference.getSnapshot>[] = []
  const unsubscribe = preference.subscribe(() => received.push(preference.getSnapshot()))
  const initial = preference.getSnapshot()
  assert.strictEqual(preference.getSnapshot(), initial)

  preference.acceptExternal('night')
  const dark = preference.getSnapshot()
  preference.acceptExternal('night')
  assert.strictEqual(preference.getSnapshot(), dark)
  preference.acceptExternal(null)
  preference.acceptExternal('unknown-future-theme')

  assert.deepEqual(received, [
    { theme: 'night', persisted: true },
    { theme: defaultTheme, persisted: true },
  ])
  assert.deepEqual(storage.writes, [])

  unsubscribe()
  preference.acceptExternal('sand')
  assert.equal(received.length, 2)
  assert.deepEqual(preference.getSnapshot(), { theme: 'sand', persisted: true })
  assert.deepEqual(storage.writes, [])
})
