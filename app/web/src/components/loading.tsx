import { Spin } from 'antd'

export function Loading({ text }: { text: string }) {
  return (
    <div className="loading" role="status" aria-live="polite">
      <Spin size="small" />
      <span>{text}</span>
    </div>
  )
}
