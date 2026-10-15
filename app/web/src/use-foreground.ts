import { useEffect, useState } from 'react'

export function useForeground() {
  const [foreground, setForeground] = useState(document.visibilityState === 'visible')
  useEffect(() => {
    const update = () => setForeground(document.visibilityState === 'visible')
    document.addEventListener('visibilitychange', update)
    return () => document.removeEventListener('visibilitychange', update)
  }, [])
  return foreground
}
