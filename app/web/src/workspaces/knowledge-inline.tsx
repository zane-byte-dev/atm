import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { ApiError, call, errorText } from '../api'
import { Markdown } from '../editor'
import type { KnowledgeDocument } from './knowledge-types'

const KnowledgeCrepe = lazy(() => import('./knowledge-crepe'))

type Draft = { title: string; content: string; etag: string }
const fromDocument = (doc: KnowledgeDocument): Draft => ({
  title: doc.metadata.title,
  content: doc.content || '',
  etag: doc.etag,
})

export function useInlineKnowledge(doc: KnowledgeDocument, enabled: boolean) {
  const client = useQueryClient()
  const key = `atm.knowledge.inline:${doc.metadata.id}`
  const [storageError, setStorageError] = useState(false)
  const [draft, setDraft] = useState<Draft>(() => {
    if (enabled)
      try {
        const saved = JSON.parse(sessionStorage.getItem(key) || 'null')
        if (
          saved &&
          ['title', 'content', 'etag'].every((field) => typeof saved[field] === 'string')
        )
          return saved
      } catch {
        /* Saving reports unavailable storage on the next edit. */
      }
    return fromDocument(doc)
  })
  const current = useRef(draft)
  const [baseline, setBaseline] = useState(fromDocument(doc))
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [composing, setComposing] = useState(false)
  const [latest, setLatest] = useState<KnowledgeDocument>()
  const dirty = draft.title !== baseline.title || draft.content !== baseline.content
  const persist = (value: Draft) => {
    try {
      sessionStorage.setItem(key, JSON.stringify(value))
      setStorageError(false)
    } catch {
      setStorageError(true)
    }
  }
  const patch = (value: Partial<Draft>) => {
    const next = { ...current.current, ...value }
    current.current = next
    persist(next)
    setDraft(next)
  }
  useEffect(() => {
    if (!dirty && !pending) {
      const next = fromDocument(doc)
      current.current = next
      setDraft(next)
      setBaseline(next)
    }
  }, [doc.etag])
  useEffect(() => {
    if (
      !enabled ||
      !dirty ||
      pending ||
      error ||
      composing ||
      !draft.title.trim() ||
      !draft.content.trim()
    )
      return
    const timer = window.setTimeout(async () => {
      const sent = draft
      setPending(true)
      try {
        const result = await call<KnowledgeDocument>('knowledge.document.update', {
          document_id: doc.metadata.id,
          etag: sent.etag,
          title: sent.title.trim(),
          content: sent.content,
        })
        const saved = fromDocument(result)
        // Keep keystrokes made during the request, advancing only their base revision.
        const next = current.current === sent ? saved : { ...current.current, etag: result.etag }
        const previous = current.current
        current.current = next
        try {
          // A remounted editor may already own a newer draft while this request finishes.
          if (sessionStorage.getItem(key) === JSON.stringify(previous)) {
            if (next.title === saved.title && next.content === saved.content)
              sessionStorage.removeItem(key)
            else sessionStorage.setItem(key, JSON.stringify(next))
          }
        } catch {
          setStorageError(true)
        }
        setDraft(next)
        setBaseline(saved)
        client.setQueryData(['knowledge', 'document', doc.metadata.id], result)
        void client.invalidateQueries({ queryKey: ['knowledge', 'query'] })
      } catch (failure) {
        setError(failure)
      } finally {
        setPending(false)
      }
    }, 900)
    return () => window.clearTimeout(timer)
  }, [draft, dirty, pending, error, composing, enabled, doc.metadata.id, client])
  useEffect(() => {
    if (!dirty && !pending) return
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty, pending])
  const conflict = error instanceof ApiError && error.status === 409
  const feedback = (
    <>
      {storageError && (
        <p role="alert" className="knowledge-detail-note">
          浏览器无法保留草稿，请等待保存成功后再离开。
        </p>
      )}
      {error != null && (
        <div className="workspace-conflict" role="alert">
          <p>
            {conflict
              ? '文档已在其他地方修改，自动保存已暂停。请核对最新内容后合并。'
              : `保存失败：${errorText(error)}`}
          </p>
          {conflict ? (
            <>
              <button
                type="button"
                className="text-button"
                onClick={async () => {
                  try {
                    setLatest(
                      await call<KnowledgeDocument>('knowledge.document.get', {
                        document_id: doc.metadata.id,
                      }),
                    )
                  } catch (failure) {
                    setError(failure)
                  }
                }}
              >
                读取最新文档
              </button>
              {latest && (
                <>
                  <details open>
                    <summary>最新版本：{latest.metadata.title}</summary>
                    <Markdown text={latest.content || '无正文'} />
                  </details>
                  <button
                    type="button"
                    className="button"
                    disabled={!latest.editable}
                    onClick={() => {
                      patch({ etag: latest.etag })
                      setError(null)
                      setLatest(undefined)
                    }}
                  >
                    已核对并合并，保存当前内容
                  </button>
                </>
              )}
            </>
          ) : (
            <button type="button" className="text-button" onClick={() => setError(null)}>
              重试保存
            </button>
          )}
        </div>
      )}
    </>
  )
  return {
    draft,
    patch,
    pending,
    dirty,
    feedback,
    setComposing,
    status: error
      ? '保存失败'
      : pending
        ? '保存中…'
        : dirty
          ? !draft.title.trim()
            ? '请填写标题'
            : !draft.content.trim()
              ? '请填写正文'
              : '待保存…'
          : '已保存',
  }
}

export function InlineKnowledgeField({
  value,
  onChange,
  title = false,
  onComposing,
}: {
  value: string
  onChange: (value: string) => void
  title?: boolean
  onComposing: (value: boolean) => void
}) {
  const field = useRef<HTMLTextAreaElement>(null)
  useLayoutEffect(() => {
    if (field.current) {
      field.current.style.height = '0px'
      field.current.style.height = `${field.current.scrollHeight}px`
    }
  }, [value])
  if (!title)
    return (
      <Suspense fallback={<p className="knowledge-detail-note">正在加载编辑器…</p>}>
        <KnowledgeCrepe value={value} onChange={onChange} onComposing={onComposing} />
      </Suspense>
    )
  return (
    <textarea
      ref={field}
      className={title ? 'knowledge-inline-title' : 'knowledge-inline-content'}
      aria-label={title ? '文档标题' : '文档正文'}
      value={value}
      rows={1}
      autoFocus={!title}
      maxLength={title ? 500 : 1000000}
      placeholder={title ? '无标题' : '开始书写，支持 Markdown…'}
      onChange={(event) => onChange(event.target.value)}
      onBlur={() => {
        onComposing(false)
      }}
      onCompositionStart={() => onComposing(true)}
      onCompositionEnd={() => onComposing(false)}
    />
  )
}
