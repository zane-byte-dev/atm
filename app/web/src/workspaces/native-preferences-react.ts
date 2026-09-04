import { useSyncExternalStore } from 'react'
import { nativePreferencesSnapshot, subscribeNativePreferences } from './native-preferences'

export const useNativePreferences = () =>
  useSyncExternalStore(subscribeNativePreferences, nativePreferencesSnapshot)
