import { Button as AntButton, Spin } from 'antd'
import { useEffect, useState } from 'react'

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

  // The container keeps role="status" and the live-region attributes: several
  // browser tests and screen readers depend on them, and antd's Spin is only the
  // indicator, not an announcement region.
  return (
    <div className={className} role="status" aria-live="polite" aria-atomic="true">
      <Spin size="small" />
      <span>{slow ? '本机索引正忙，仍在读取…' : text}</span>
      {slow && onRetry && (
        <AntButton htmlType="button" className="text-button" onClick={onRetry}>
          重试
        </AntButton>
      )}
    </div>
  )
}
