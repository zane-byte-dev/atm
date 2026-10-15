import { Button as AntButton } from 'antd'
import type { ReactNode } from 'react'
import type { ButtonProps } from 'antd'
import './workspace-list-item.css'

export function WorkspaceListItem({
  title,
  label,
  context,
  timestamp,
  description,
  selected = false,
  unread = false,
  tone = 'default',
  className = '',
  ...props
}: Omit<ButtonProps, 'title' | 'children'> & {
  title: string
  label: ReactNode
  context: string
  timestamp: ReactNode
  description?: string
  selected?: boolean
  unread?: boolean
  tone?: 'default' | 'danger'
}) {
  return (
    <AntButton
      htmlType="button"
      {...props}
      type="text"
      className={`workspace-list-item ${selected ? 'selected' : ''} ${className}`}
      aria-pressed={selected}
    >
      <h3 className="workspace-list-item-title" title={title}>
        {unread && <span className="workspace-list-item-unread" aria-label="未读" />}
        {title}
      </h3>
      <div className="workspace-list-item-meta">
        <span className="workspace-list-item-label" data-tone={tone}>
          {label}
        </span>
        <span className="workspace-list-item-context" title={context}>
          {context}
        </span>
        <time className="workspace-list-item-time">{timestamp}</time>
      </div>
      {description && (
        <p className="workspace-list-item-description" title={description}>
          {description}
        </p>
      )}
    </AntButton>
  )
}
