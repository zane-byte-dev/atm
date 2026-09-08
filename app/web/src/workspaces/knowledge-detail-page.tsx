import { useEffect, useRef, type ReactNode } from 'react'
import { BookOpen, MoreHorizontal } from 'lucide-react'

export function KnowledgeDetailPage({
  kind,
  collectionLabel,
  pathTitle,
  date,
  dateLabel,
  saveStatus,
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
  properties: (close: () => void) => ReactNode
  title: ReactNode
  children: ReactNode
}) {
  const menuRef = useRef<HTMLDetailsElement>(null)
  const closeTimer = useRef<number | undefined>(undefined)
  const cancelMenuClose = () => window.clearTimeout(closeTimer.current)
  const closeMenu = (restoreFocus = false) => {
    cancelMenuClose()
    const menu = menuRef.current
    if (!menu?.open) return
    menu.open = false
    if (restoreFocus) menu.querySelector('summary')?.focus()
  }
  useEffect(() => {
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !menuRef.current?.contains(event.target)) closeMenu()
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && menuRef.current?.open) {
        event.preventDefault()
        closeMenu(true)
      }
    }
    window.document.addEventListener('pointerdown', outside)
    window.document.addEventListener('keydown', escape)
    return () => {
      cancelMenuClose()
      window.document.removeEventListener('pointerdown', outside)
      window.document.removeEventListener('keydown', escape)
    }
  }, [])
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
          <details
            className="knowledge-page-properties"
            ref={menuRef}
            onPointerEnter={cancelMenuClose}
            onPointerLeave={(event) => {
              if (event.pointerType !== 'mouse' || !menuRef.current?.open) return
              cancelMenuClose()
              // Allow the pointer to cross the small gap between trigger and panel.
              closeTimer.current = window.setTimeout(() => closeMenu(), 200)
            }}
            onToggle={() => {
              if (!menuRef.current?.open) cancelMenuClose()
            }}
            onBlur={(event) => {
              if (
                event.relatedTarget instanceof Node &&
                !event.currentTarget.contains(event.relatedTarget)
              )
                closeMenu()
            }}
          >
            <summary aria-label={`${kind}操作`} title={`${kind}操作`}>
              <MoreHorizontal size={19} />
            </summary>
            <div className="knowledge-page-properties-panel">{properties(closeMenu)}</div>
          </details>
        </div>
      </header>
      <article className="knowledge-article knowledge-page">
        <header className="knowledge-article-header">{title}</header>
        <div className="knowledge-article-surface">{children}</div>
      </article>
    </>
  )
}
