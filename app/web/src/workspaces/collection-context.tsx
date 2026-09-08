import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ImageOff, X, ZoomIn } from 'lucide-react'
import { contextImageURL, parseContext } from './collection-context-model'
import './collection-context.css'

function ContextImage({ src, alt }: { src?: string; alt?: string }) {
  const url = contextImageURL(src)
  const [failed, setFailed] = useState(false)
  const dialog = useRef<HTMLDialogElement>(null)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    if (open) dialog.current?.showModal()
  }, [open])
  if (!url || failed)
    return (
      <span className="context-image-failed">
        <ImageOff size={16} />
        {alt || '图片'} · {failed ? '图片无法加载' : '未提供可预览的图片地址'}
        {url && (
          <button type="button" onClick={() => setFailed(false)}>
            重试
          </button>
        )}
      </span>
    )
  return (
    <>
      <button
        type="button"
        className="context-image"
        aria-label={`放大图片：${alt || '图片'}`}
        onClick={() => setOpen(true)}
      >
        <img
          src={url}
          alt={alt || '图片'}
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setFailed(true)}
        />
        <span>
          <ZoomIn size={14} /> 查看大图
        </span>
      </button>
      {open &&
        createPortal(
          <dialog
            ref={dialog}
            onClose={() => setOpen(false)}
            className="context-image-dialog"
            aria-label={alt || '图片预览'}
            onClick={(event) => {
              if (event.target === event.currentTarget) dialog.current?.close()
            }}
          >
            <div>
              <span>{alt || '图片预览'}</span>
              <button
                type="button"
                className="button"
                aria-label="关闭图片预览"
                onClick={() => dialog.current?.close()}
                autoFocus
              >
                <X size={18} />
              </button>
            </div>
            <img src={url} alt={alt || '图片'} referrerPolicy="no-referrer" />
          </dialog>,
          document.body,
        )}
    </>
  )
}

export function CollectionContext({ text }: { text?: string }) {
  const [raw, setRaw] = useState(false)
  const messages = parseContext(text || '')
  const count = messages.filter((message) => message.sender).length
  return (
    <section className="collection-detail-section collection-context">
      <div className="context-heading">
        <h3>原始上下文{count > 0 && <span>{count} 条消息</span>}</h3>
        {text && (
          <button type="button" className="button" aria-pressed={raw} onClick={() => setRaw(!raw)}>
            {raw ? '消息视图' : '查看原文'}
          </button>
        )}
      </div>
      {!text ? (
        <p className="collection-muted">这条记录没有保存原始上下文。</p>
      ) : raw ? (
        <pre className="context-raw">{text}</pre>
      ) : (
        <div className="context-messages">
          {messages.map((message, index) => (
            <article
              className={`context-message${message.kind === '新消息' ? ' is-new' : ''}`}
              key={index}
            >
              {message.sender && (
                <header>
                  <strong>{message.sender}</strong>
                  {message.kind && <span className="context-kind">{message.kind}</span>}
                  <time>{message.time}</time>
                </header>
              )}
              <div className="markdown">
                <ReactMarkdown
                  remarkPlugins={[remarkGfm]}
                  skipHtml
                  components={{
                    a: ({ href, children }) => (
                      <a href={href} target="_blank" rel="noopener noreferrer">
                        {children}
                      </a>
                    ),
                    img: ({ src, alt }) => (
                      <ContextImage
                        key={typeof src === 'string' ? src : ''}
                        src={typeof src === 'string' ? src : undefined}
                        alt={alt}
                      />
                    ),
                  }}
                >
                  {message.body}
                </ReactMarkdown>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  )
}
