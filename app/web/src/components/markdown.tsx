import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          ),
          img: ({ src, alt }) =>
            typeof src === 'string' && src.startsWith('/api/v1/attachments/') ? (
              <img src={src} alt={alt ?? ''} loading="lazy" />
            ) : (
              <span className="image-placeholder">[图片{alt ? `：${alt}` : ''}]</span>
            ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
