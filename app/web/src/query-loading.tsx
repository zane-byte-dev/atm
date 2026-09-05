import { useEffect, useState } from 'react'
import { LoaderCircle } from 'lucide-react'

const slowLoadingDelay = 2500

export function QueryLoading({
  className,
  text,
  onRetry,
}: {
  className: string
  text: string
  onRetry?: () => void
}) {
  const [slow, setSlow] = useState(false)

  useEffect(() => {
    const timer = window.setTimeout(() => setSlow(true), slowLoadingDelay)
    return () => window.clearTimeout(timer)
  }, [])

  return (
    <div className={className} role="status" aria-live="polite" aria-atomic="true">
      <LoaderCircle size={17} className="spin" />
      <span>{slow ? '本机索引正忙，仍在读取…' : text}</span>
      {slow && onRetry && (
        <button type="button" className="text-button" onClick={onRetry}>
          重试
        </button>
      )}
    </div>
  )
}
