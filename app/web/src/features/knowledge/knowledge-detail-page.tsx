import { useState, type ReactNode } from 'react'
import { Button, Popover, Tooltip } from 'antd'
import { BookOpen, MoreHorizontal } from 'lucide-react'

export function KnowledgeDetailPage({
  kind,
  collectionLabel,
  pathTitle,
  date,
  dateLabel,
  saveStatus,
  actions,
  properties,
  title,
  children,
}: {
  kind: string
  collectionLabel: string
  pathTitle: string
  date: string
  dateLabel: string
  saveStatus?: string
  actions?: ReactNode
  properties: (close: () => void) => ReactNode
  title: ReactNode
  children: ReactNode
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const closeMenu = () => setMenuOpen(false)
  const formatDate = (value: string) =>
    value
      ? new Date(value).toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })
      : '—'
  return (
    <>
      <header className="knowledge-page-bar">
        <nav className="knowledge-page-path" aria-label={`${kind}路径`}>
          <span className="knowledge-page-collection" title={collectionLabel}>
            <BookOpen size={16} />
            <span>{collectionLabel}</span>
          </span>
          <span aria-hidden="true">/</span>
          <span aria-current="page" title={pathTitle}>
            {pathTitle}
          </span>
        </nav>
        <div className="knowledge-page-tools">
          <time className="knowledge-page-updated" dateTime={date}>
            {dateLabel} {formatDate(date)}
          </time>
          {saveStatus && (
            <span className="knowledge-save-state" role="status">
              {saveStatus}
            </span>
          )}
          {actions}
          <Popover
            trigger="click"
            placement="bottomRight"
            open={menuOpen}
            onOpenChange={setMenuOpen}
            content={<div className="knowledge-page-properties-panel">{properties(closeMenu)}</div>}
          >
            <Tooltip title={`${kind}操作`}>
              <Button type="text" aria-label={`${kind}操作`} icon={<MoreHorizontal size={19} />} />
            </Tooltip>
          </Popover>
        </div>
      </header>
      <article className="knowledge-article knowledge-page">
        <header className="knowledge-article-header">{title}</header>
        <div className="knowledge-article-surface">{children}</div>
      </article>
    </>
  )
}
