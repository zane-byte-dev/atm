import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import './workspace-title.css'

// One type scale for workspace navigation titles, independent of document typography.
export function WorkspaceTitle({
  icon: Icon,
  children,
}: {
  icon: LucideIcon
  children: ReactNode
}) {
  return (
    <h1
      className="workspace-title"
      style={{ fontSize: 'var(--ui-title-size, 14px)', fontWeight: 500, lineHeight: '20px' }}
    >
      <Icon size={16} strokeWidth={1.75} aria-hidden="true" />
      {children}
    </h1>
  )
}
