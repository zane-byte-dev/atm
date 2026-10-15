import { Button as AntButton, Input, Modal } from 'antd'
import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ImageOff, ZoomIn } from 'lucide-react'
import { contextImageURL, parseContext } from './collection-context-model'
import './collection-context.css'

function ContextImage({ src, alt }: { src?: string; alt?: string }) {
  const url = contextImageURL(src)
  const [failed, setFailed] = useState(false)
  const [open, setOpen] = useState(false)
  if (!url || failed)
    return (
      <span className="context-image-failed">
        <ImageOff size={16} />
        {alt || '图片'} · {failed ? '图片无法加载' : '未提供可预览的图片地址'}
        {url && (
          <AntButton htmlType="button" onClick={() => setFailed(false)}>
            重试
          </AntButton>
        )}
      </span>
    )
  return (
    <>
      <AntButton
        htmlType="button"
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
      </AntButton>
      <Modal
        open={open}
        centered
        onCancel={() => setOpen(false)}
        title={alt || '图片预览'}
        className="context-image-dialog"
        footer={null}
        width="fit-content"
        destroyOnHidden
      >
        <img src={url} alt={alt || '图片'} referrerPolicy="no-referrer" />
      </Modal>
    </>
  )
}

export function CollectionContext({ text }: { text?: string }) {
  const [raw, setRaw] = useState(false)
  const messages = parseContext(text || '')
  const [expanded, setExpanded] = useState(() => messages.length <= 8)
  const [query, setQuery] = useState('')
  const [limit, setLimit] = useState(30)
  const filtered = messages.filter((message) =>
    `${message.sender || ''} ${message.body}`
      .toLocaleLowerCase()
      .includes(query.trim().toLocaleLowerCase()),
  )
  const count = messages.filter((message) => message.sender).length
  return (
    <section className="collection-detail-section collection-context">
      <div className="context-heading">
        <h3>原始上下文{count > 0 && <span>{count} 条消息</span>}</h3>
        {text && (
          <AntButton
            htmlType="button"
            className="button"
            aria-expanded={expanded}
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? '收起上下文' : '展开上下文'}
          </AntButton>
        )}
      </div>
      {!text ? (
        <p className="collection-muted">这条记录没有保存原始上下文。</p>
      ) : !expanded ? (
        <p className="collection-muted">完整上下文已折叠。展开后可按关键词或发言人查找。</p>
      ) : (
        <>
          <div className="context-controls">
            <Input
              type="search"
              aria-label="搜索上下文"
              placeholder="搜索关键词或发言人"
              allowClear
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setLimit(30)
              }}
            />
            <AntButton htmlType="button" aria-pressed={raw} onClick={() => setRaw(!raw)}>
              {raw ? '消息视图' : '纯文本视图'}
            </AntButton>
          </div>
          {query.trim() && (
            <p className="collection-muted" role="status">
              找到 {filtered.length} 条匹配内容
            </p>
          )}
          {raw ? (
            <pre className="context-raw">
              {query.trim()
                ? filtered
                    .map((message) =>
                      [message.sender, message.time, message.body].filter(Boolean).join('\n'),
                    )
                    .join('\n\n')
                : text}
            </pre>
          ) : (
            <div className="context-messages">
              {filtered.slice(0, limit).map((message, index) => (
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
              {filtered.length > limit && (
                <AntButton onClick={() => setLimit(limit + 30)}>
                  再显示 {Math.min(30, filtered.length - limit)} 条（剩余 {filtered.length - limit}{' '}
                  条）
                </AntButton>
              )}
            </div>
          )}
        </>
      )}
    </section>
  )
}
