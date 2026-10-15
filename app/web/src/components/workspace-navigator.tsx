import type { HTMLAttributes, ReactNode } from 'react'
import { Button, ConfigProvider, type ButtonProps } from 'antd'
import type { LucideIcon } from 'lucide-react'
import './workspace-navigator.css'

// AppShell owns the icon rail. This frame owns the middle column and detail split.
export function WorkspaceSplitLayout({
  as: Element = 'div',
  className = '',
  ...props
}: HTMLAttributes<HTMLElement> & { as?: 'div' | 'section' }) {
  return <Element {...props} className={`workspace-split-layout ${className}`} />
}

export function WorkspaceNavigator({
  as: Element = 'nav',
  className = '',
  variant = 'menu',
  ...props
}: HTMLAttributes<HTMLElement> & {
  as?: 'nav' | 'aside' | 'section'
  variant?: 'menu' | 'tree' | 'list'
}) {
  return (
    <ConfigProvider wave={{ disabled: true }}>
      <Element
        {...props}
        className={`workspace-navigator workspace-navigator--${variant} ${className}`}
      />
    </ConfigProvider>
  )
}

export function WorkspaceNavigatorHeading({
  title,
  actions,
  icon: Icon,
  level: Title = 'h1',
  className = '',
}: {
  title: ReactNode
  actions?: ReactNode
  icon?: LucideIcon
  level?: 'h1' | 'h2'
  className?: string
}) {
  return (
    <header className={`workspace-navigator-heading ${className}`}>
      <Title className="workspace-navigator-title">
        {Icon && <Icon size={16} strokeWidth={1.75} aria-hidden="true" />}
        {title}
      </Title>
      {actions && <div className="workspace-navigator-actions">{actions}</div>}
    </header>
  )
}

export function WorkspaceNavigatorItem({
  selected = false,
  className = '',
  ...props
}: ButtonProps & { selected?: boolean }) {
  return (
    <Button
      htmlType="button"
      {...props}
      type="text"
      aria-pressed={selected}
      className={`workspace-navigator-item ${selected ? 'selected' : ''} ${className}`}
    />
  )
}
