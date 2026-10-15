import { KnowledgeTasks } from './task-knowledge-links'
import { Disclosure } from '../../components/disclosure'
import { Button as AntButton } from 'antd'
import { useState } from 'react'
import { Pencil } from 'lucide-react'
import { Markdown } from '../../components/markdown'
import type { KnowledgeCollection, KnowledgeDocument, MemoryHit } from './knowledge-types'
import { KnowledgeDetailPage } from './knowledge-detail-page'
import { KnowledgeActions } from './knowledge-actions'
import { InlineKnowledgeField, useInlineKnowledge } from './knowledge-inline'
import { statusLabels, formatDate, scopeLabel } from './knowledge-format'
export function Tags({ values }: { values?: string[] }) {
  return values?.length ? (
    <div className="knowledge-tags">
      {values.map((value) => (
        <span key={value}>{value}</span>
      ))}
    </div>
  ) : null
}
export function Guidance({ label, items }: { label: string; items?: string[] }) {
  return items?.length ? (
    <Disclosure>
      <summary>{label}</summary>
      <ul>
        {items.map((item, index) => (
          <li key={index}>{item}</li>
        ))}
      </ul>
    </Disclosure>
  ) : null
}

export function DocumentDetail({
  document,
  collections,
  onSaved,
  onBusy,
  busy,
  collectionLabel,
  writable,
  onCopy,
  onEdit,
  todoWritable = false,
}: {
  document: KnowledgeDocument
  todoWritable?: boolean
  collections: KnowledgeCollection[]
  onSaved: (doc: KnowledgeDocument) => Promise<void>
  onBusy: (value: boolean) => void
  busy: boolean
  collectionLabel: string
  writable: boolean
  onCopy: () => void
  onEdit: () => void
}) {
  const meta = document.metadata
  const editable = writable && document.editable && !busy && meta.status !== 'archived'
  const inline = useInlineKnowledge(document, editable)
  const [editing, setEditing] = useState(() => inline.dirty)
  const editingContent = editable && editing
  const readingTitle = editable ? inline.draft.title : meta.title
  const readingContent = (editable ? inline.draft.content : document.content) || ''
  // Suppress an exact duplicate only in reading mode; keep the stored Markdown intact.
  const openingHeading = readingContent.match(/^\s*# ([^\r\n]+)(?:\r?\n|$)/)
  const readingBody =
    openingHeading && openingHeading[1].replace(/\s+#+\s*$/, '').trim() === readingTitle.trim()
      ? readingContent.slice(openingHeading[0].length)
      : readingContent
  return (
    <KnowledgeDetailPage
      kind="文档"
      collectionLabel={collectionLabel}
      pathTitle={meta.title}
      date={meta.updatedAt}
      dateLabel="上次编辑"
      saveStatus={editingContent ? inline.status : '阅读模式'}
      actions={
        editable && (
          <AntButton
            className="button"
            disabled={inline.dirty || inline.pending}
            onClick={() => setEditing(!editing)}
          >
            {editing ? '完成编辑' : '编辑文档'}
          </AntButton>
        )
      }
      properties={(closeMenu) => (
        <>
          <KnowledgeActions
            document={document}
            collections={collections}
            writable={writable}
            unsaved={inline.dirty || inline.pending}
            onCopy={() => {
              closeMenu()
              onCopy()
            }}
            onSaved={onSaved}
            onBusy={onBusy}
            onRename={() => {
              setEditing(true)
              closeMenu()
              requestAnimationFrame(() => {
                const field = window.document.querySelector<HTMLTextAreaElement>(
                  'textarea[aria-label="文档标题"]',
                )
                field?.focus()
                field?.select()
              })
            }}
          />
          <Disclosure className="knowledge-action-properties">
            <summary>文档属性与来源</summary>
            {editable && (
              <AntButton
                htmlType="button"
                className="text-button"
                disabled={inline.dirty || inline.pending}
                onClick={onEdit}
              >
                编辑属性
              </AntButton>
            )}
            <p>
              {collectionLabel} · {statusLabels[meta.status] || meta.status}
            </p>
            <div className="knowledge-metadata">
              <span>
                项目<strong>{meta.projects?.join('、') || '未指定'}</strong>
              </span>
              <span>
                领域<strong>{meta.domains?.join('、') || '未指定'}</strong>
              </span>
              <span>
                创建者<strong>{meta.producer || '—'}</strong>
              </span>
            </div>
            <Tags values={meta.tags} />
            <dl>
              <dt>文档 ID</dt>
              <dd>{meta.id}</dd>
              <dt>创建时间</dt>
              <dd>{formatDate(meta.createdAt)}</dd>
              {meta.source && (
                <>
                  <dt>来源类型</dt>
                  <dd>{meta.source.type}</dd>
                  <dt>来源</dt>
                  <dd>{meta.source.uri}</dd>
                  {meta.source.importedAt && (
                    <>
                      <dt>导入时间</dt>
                      <dd>{formatDate(meta.source.importedAt)}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
          </Disclosure>
        </>
      )}
      title={
        <>
          {editingContent ? (
            <InlineKnowledgeField
              title
              value={inline.draft.title}
              onChange={(title) => inline.patch({ title })}
              onComposing={inline.setComposing}
            />
          ) : (
            <h2>{readingTitle}</h2>
          )}
          {meta.status !== 'active' && (
            <span className={`knowledge-status ${meta.status}`}>
              {statusLabels[meta.status] || meta.status}
            </span>
          )}
        </>
      }
    >
      {meta.status === 'archived' && (
        <p className="knowledge-detail-note">文档已归档，可通过右上角菜单恢复。</p>
      )}
      {editable && inline.feedback}
      <div className="knowledge-document-body">
        {editingContent ? (
          <InlineKnowledgeField
            value={inline.draft.content}
            onChange={(content) => inline.patch({ content })}
            onComposing={inline.setComposing}
          />
        ) : (
          <Markdown text={readingBody || '此文档没有正文。'} />
        )}
      </div>
      <KnowledgeTasks document={document} writable={todoWritable} />
      {writable && !document.editable && (
        <p className="knowledge-detail-note">
          此文档来自外部来源。可以创建副本进行编辑，保留来源原文。
        </p>
      )}
    </KnowledgeDetailPage>
  )
}

export function MemoryDetail({
  memory,
  writable,
  onEdit,
}: {
  memory: MemoryHit
  writable: boolean
  onEdit: () => void
}) {
  const title = memory.content.trim().split(/\r?\n/)[0] || '未命名记忆'
  return (
    <KnowledgeDetailPage
      kind="记忆"
      collectionLabel="共享记忆"
      pathTitle={title}
      date={memory.created_at}
      dateLabel="创建于"
      title={<h2>{title}</h2>}
      properties={(closeMenu) => (
        <>
          {writable && (
            <div className="knowledge-page-actions">
              <AntButton
                htmlType="button"
                onClick={() => {
                  closeMenu()
                  onEdit()
                }}
              >
                <Pencil size={14} />
                修订记忆
              </AntButton>
            </div>
          )}
          <Disclosure className="knowledge-action-properties">
            <summary>记忆属性与来源</summary>
            <div className="knowledge-metadata">
              <span>
                作用范围<strong>{scopeLabel(memory.scope)}</strong>
              </span>
            </div>
            <Tags values={memory.tags} />
            <dl>
              <dt>记忆 ID</dt>
              <dd>{memory.id}</dd>
              <dt>创建时间</dt>
              <dd>{formatDate(memory.created_at)}</dd>
              <dt>作用范围</dt>
              <dd>{memory.scope}</dd>
              <dt>来源</dt>
              <dd>{memory.metadata?.source || memory.source || '未记录'}</dd>
              {Object.entries(memory.metadata || {})
                .filter(([key]) => key !== 'source')
                .map(([key, value]) => (
                  <div key={key}>
                    <dt>{key}</dt>
                    <dd>{value}</dd>
                  </div>
                ))}
            </dl>
            <p className="knowledge-detail-note">
              修订会追加一条新记录并替代当前记忆，历史记录仍保留。
            </p>
          </Disclosure>
        </>
      )}
    >
      <div className="knowledge-document-body">
        <Markdown text={memory.content} />
      </div>
    </KnowledgeDetailPage>
  )
}
