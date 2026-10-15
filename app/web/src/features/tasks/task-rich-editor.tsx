import { $prose } from '@milkdown/kit/utils'
import { Button as AntButton, Input as AntInput } from 'antd'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { CrepeBuilder } from '@milkdown/crepe/builder'
import { blockEdit } from '@milkdown/crepe/feature/block-edit'
import { codeMirror } from '@milkdown/crepe/feature/code-mirror'
import { cursor } from '@milkdown/crepe/feature/cursor'
import { linkTooltip } from '@milkdown/crepe/feature/link-tooltip'
import { listItem } from '@milkdown/crepe/feature/list-item'
import { placeholder } from '@milkdown/crepe/feature/placeholder'
import { table } from '@milkdown/crepe/feature/table'
import { toolbar } from '@milkdown/crepe/feature/toolbar'
import { editorViewOptionsCtx, serializerCtx } from '@milkdown/kit/core'
import { uploadConfig } from '@milkdown/kit/plugin/upload'
import { Plugin } from '@milkdown/kit/prose/state'
import { replaceAll, insert } from '@milkdown/kit/utils'
import { canRoundTripMarkdown } from '../knowledge/knowledge-markdown'
import '@milkdown/crepe/theme/common/prosemirror.css'
import '@milkdown/crepe/theme/common/reset.css'
import '@milkdown/crepe/theme/common/block-edit.css'
import '@milkdown/crepe/theme/common/code-mirror.css'
import '@milkdown/crepe/theme/common/cursor.css'
import '@milkdown/crepe/theme/common/link-tooltip.css'
import '@milkdown/crepe/theme/common/list-item.css'
import '@milkdown/crepe/theme/common/placeholder.css'
import '@milkdown/crepe/theme/common/toolbar.css'
import '@milkdown/crepe/theme/common/table.css'
import '@milkdown/crepe/theme/frame.css'
import '../knowledge/knowledge-crepe.css'
import { draftImagePrefix, readDraftImage } from './task-draft-images'

type Props = {
  value: string
  onChange: (value: string) => void
  onComposing: (value: boolean) => void
  disabled?: boolean
  upload: (files: File[]) => Promise<{ name: string; url: string }[]>
}
export default function TaskRichEditor({
  value,
  onChange,
  onComposing,
  disabled = false,
  upload,
}: Props) {
  const host = useRef<HTMLDivElement>(null)
  const source = useRef<HTMLTextAreaElement>(null)
  const instance = useRef<CrepeBuilder | null>(null)
  const latest = useRef({ value, onChange, onComposing, disabled, upload })
  latest.current = { value, onChange, onComposing, disabled, upload }
  const lastValue = useRef(value)
  const applying = useRef(false)
  const [mode, setMode] = useState<'rich' | 'source'>('rich')
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState('')
  useLayoutEffect(() => {
    if (source.current) {
      source.current.style.height = '0px'
      source.current.style.height = `${source.current.scrollHeight}px`
    }
  }, [value, mode])
  useEffect(() => {
    if (mode !== 'rich' || !host.current) return
    let disposed = false
    let active = false
    setReady(false)
    const initial = latest.current.value
    const mount = document.createElement('div')
    host.current.append(mount)
    const crepe = new CrepeBuilder({ root: mount, defaultValue: initial })
      .addFeature(cursor)
      .addFeature(listItem)
      .addFeature(linkTooltip, { inputPlaceholder: '粘贴链接…' })
      .addFeature(placeholder, { text: '写下背景、目标，或直接粘贴、拖入图片…' })
      .addFeature(codeMirror, {
        copyText: '复制',
        searchPlaceholder: '搜索语言',
        noResultText: '无匹配语言',
      })
      .addFeature(table)
      .addFeature(toolbar, {
        boldLabel: '加粗',
        italicLabel: '斜体',
        codeLabel: '行内代码',
        linkLabel: '链接',
        strikethroughLabel: '删除线',
      })
      .addFeature(blockEdit, {
        textGroup: {
          label: '文本',
          text: { label: '正文' },
          h1: { label: '一级标题' },
          h2: { label: '二级标题' },
          h3: { label: '三级标题' },
          h4: { label: '四级标题' },
          h5: { label: '五级标题' },
          h6: { label: '六级标题' },
          quote: { label: '引用' },
          divider: { label: '分隔线' },
        },
        listGroup: {
          label: '列表',
          bulletList: { label: '无序列表' },
          orderedList: { label: '有序列表' },
          taskList: { label: '待办清单' },
        },
        advancedGroup: {
          label: '更多',
          image: null,
          math: null,
          codeBlock: { label: '代码块' },
          table: { label: '表格' },
        },
      })
    crepe.editor
      .config((ctx) => {
        ctx.update(editorViewOptionsCtx, (options) => ({
          ...options,
          editable: () => !latest.current.disabled,
          nodeViews: {
            ...options.nodeViews,
            image: (node) => {
              const img = document.createElement('img')
              img.alt = node.attrs.alt || '图片'
              let disposed = false
              let objectURL = ''
              const src = String(node.attrs.src || '')
              if (src.startsWith('/api/v1/attachments/')) img.src = src
              else if (src.startsWith(draftImagePrefix))
                void readDraftImage(src)
                  .then((file) => {
                    if (disposed) return
                    objectURL = URL.createObjectURL(file)
                    img.src = objectURL
                  })
                  .catch(() => {
                    img.alt = '草稿图片无法读取，请重新插入'
                  })
              return {
                dom: img,
                destroy: () => {
                  disposed = true
                  if (objectURL) URL.revokeObjectURL(objectURL)
                },
              }
            },
          },
          attributes: { role: 'textbox', 'aria-label': '任务说明', 'aria-multiline': 'true' },
        }))

        ctx.update(uploadConfig.key, (config) => ({
          ...config,
          uploader: async (files, schema) => {
            if (latest.current.disabled) return []
            try {
              const images = await latest.current.upload(Array.from(files))
              return images.map((image) =>
                schema.nodes.image.create({ src: image.url, alt: image.name }),
              )
            } catch (error) {
              setNotice(error instanceof Error ? error.message : '图片未上传，请重试。')
              return []
            }
          },
        }))
      })
      .use(
        $prose(
          (ctx) =>
            new Plugin({
              view: () => ({
                update: (view, previous) => {
                  if (!active || disposed || applying.current || view.state.doc.eq(previous.doc))
                    return
                  const markdown = ctx.get(serializerCtx)(view.state.doc)
                  if (markdown === lastValue.current) return
                  lastValue.current = markdown
                  latest.current.onChange(markdown)
                },
              }),
            }),
        ),
      )
    const creation = crepe
      .create()
      .then(() => {
        if (disposed) return
        if (!canRoundTripMarkdown(initial, crepe.getMarkdown())) {
          setNotice('这段说明包含富文本编辑器不能完整保留的语法，已使用源码编辑。')
          setMode('source')
          return
        }
        crepe.setReadonly(latest.current.disabled)
        instance.current = crepe
        lastValue.current = initial
        active = true
        setReady(true)
      })
      .catch(() => {
        if (!disposed) {
          setNotice('编辑器加载失败，可继续使用 Markdown 源码编辑。')
          setMode('source')
        }
      })
    return () => {
      disposed = true
      active = false
      instance.current = null
      latest.current.onComposing(false)
      void creation
        .then(() => crepe.destroy())
        .catch(() => {})
        .finally(() => mount.remove())
    }
  }, [mode])
  useEffect(() => {
    if (!ready || !instance.current || value === lastValue.current) return
    const crepe = instance.current
    applying.current = true
    try {
      crepe.editor.action(replaceAll(value, true))
      lastValue.current = value
      if (!canRoundTripMarkdown(value, crepe.getMarkdown())) {
        setNotice('这段说明包含富文本编辑器不能完整保留的语法，已使用源码编辑。')
        setMode('source')
      }
    } finally {
      applying.current = false
    }
  }, [value, ready])
  useEffect(() => {
    instance.current?.setReadonly(disabled)
  }, [disabled, ready])
  const insertSourceFiles = async (files: File[], start: number, end: number) => {
    const before = latest.current.value
    try {
      const images = await latest.current.upload(files)
      const markdown = images
        .map((image) => `![${image.name.replace(/[\\[\]<>\r\n]/g, ' ')}](<${image.url}>)`)
        .join('\n')
      latest.current.onChange(before.slice(0, start) + '\n' + markdown + '\n' + before.slice(end))
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '图片未上传')
    }
  }
  const fileInput = useRef<HTMLInputElement>(null)
  return (
    <div className="knowledge-rich-editor task-rich-editor">
      <div className="knowledge-editor-mode">
        <input
          ref={fileInput}
          type="file"
          hidden
          accept="image/png,image/jpeg,image/gif"
          multiple
          onChange={async (event) => {
            const files = Array.from(event.target.files || [])
            event.target.value = ''
            try {
              const images = await upload(files)
              const markdown = images
                .map((image) => `![${image.name.replace(/[\\[\]<>\r\n]/g, ' ')}](<${image.url}>)`)
                .join('\n')
              if (instance.current) instance.current.editor.action(insert(markdown))
              else onChange(latest.current.value + '\n' + markdown + '\n')
            } catch (error) {
              setNotice(error instanceof Error ? error.message : '图片未上传')
            }
          }}
        />
        <AntButton
          htmlType="button"
          className="button"
          disabled={disabled || (!ready && mode === 'rich')}
          onClick={() => fileInput.current?.click()}
        >
          插入图片
        </AntButton>
        <AntButton
          htmlType="button"
          className="text-button"
          disabled={disabled}
          aria-pressed={mode === 'source'}
          onClick={() => {
            onComposing(false)
            setNotice('')
            setMode(mode === 'rich' ? 'source' : 'rich')
          }}
        >
          {mode === 'rich' ? 'Markdown 源码' : '返回富文本'}
        </AntButton>
      </div>
      {notice && (
        <p className="knowledge-detail-note" role="status">
          {notice}
        </p>
      )}
      {mode === 'rich' ? (
        <>
          {!ready && (
            <p className="knowledge-detail-note" role="status">
              正在加载编辑器…
            </p>
          )}
          <div
            ref={host}
            className="knowledge-crepe-host"
            onCompositionStartCapture={() => onComposing(true)}
            onCompositionEndCapture={() => onComposing(false)}
          />
        </>
      ) : (
        <AntInput.TextArea
          ref={(instance) => {
            source.current = instance?.resizableTextArea?.textArea || null
          }}
          disabled={disabled}
          onPaste={(event) => {
            const files = Array.from(event.clipboardData.files)
            if (files.length) {
              event.preventDefault()
              void insertSourceFiles(
                files,
                event.currentTarget.selectionStart,
                event.currentTarget.selectionEnd,
              )
            }
          }}
          onDragOver={(event) => {
            if (event.dataTransfer.types.includes('Files')) event.preventDefault()
          }}
          onDrop={(event) => {
            if (event.dataTransfer.files.length) {
              event.preventDefault()
              void insertSourceFiles(
                Array.from(event.dataTransfer.files),
                event.currentTarget.selectionStart,
                event.currentTarget.selectionEnd,
              )
            }
          }}
          className="knowledge-inline-content knowledge-markdown-source"
          aria-label="Markdown 源码"
          value={value}
          rows={8}
          onChange={(event) => onChange(event.target.value)}
          onCompositionStart={() => onComposing(true)}
          onCompositionEnd={() => onComposing(false)}
          onBlur={() => onComposing(false)}
        />
      )}
    </div>
  )
}
