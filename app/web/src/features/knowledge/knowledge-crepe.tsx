import { $prose } from '@milkdown/kit/utils'
import { Button as AntButton, Input as AntInput } from 'antd'
import type { TextAreaRef } from 'antd/es/input/TextArea'
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
import { replaceAll } from '@milkdown/kit/utils'
import { canRoundTripMarkdown } from './knowledge-markdown'
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
import './knowledge-crepe.css'

type Props = {
  value: string
  onChange: (value: string) => void
  onComposing: (value: boolean) => void
}
export default function KnowledgeCrepe({ value, onChange, onComposing }: Props) {
  const host = useRef<HTMLDivElement>(null)
  const source = useRef<TextAreaRef>(null)
  const instance = useRef<CrepeBuilder | null>(null)
  const latest = useRef({ value, onChange, onComposing })
  latest.current = { value, onChange, onComposing }
  const lastValue = useRef(value)
  const applying = useRef(false)
  const [mode, setMode] = useState<'rich' | 'source'>('rich')
  const [ready, setReady] = useState(false)
  const [notice, setNotice] = useState('')
  useLayoutEffect(() => {
    // Source mode only: Ant Design keeps the native textarea behind the ref.
    const area = source.current?.resizableTextArea?.textArea
    if (area) {
      area.style.height = '0px'
      area.style.height = `${area.scrollHeight}px`
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
      .addFeature(placeholder, { text: '开始书写，输入 / 插入内容…' })
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
          attributes: { role: 'textbox', 'aria-label': '文档正文', 'aria-multiline': 'true' },
        }))
        // No knowledge attachment endpoint: never save temporary blob URLs.
        ctx.update(uploadConfig.key, (config) => ({
          ...config,
          uploader: async () => {
            setNotice('图片请通过 Markdown 图片链接插入。')
            return []
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
          setNotice('这篇文档包含富文本编辑器不能完整保留的语法，已使用源码编辑。')
          setMode('source')
          return
        }
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
        setNotice('这篇文档包含富文本编辑器不能完整保留的语法，已使用源码编辑。')
        setMode('source')
      }
    } finally {
      applying.current = false
    }
  }, [value, ready])
  return (
    <div className="knowledge-rich-editor">
      <div className="knowledge-editor-mode">
        <AntButton
          htmlType="button"
          className="text-button"
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
          ref={source}
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
