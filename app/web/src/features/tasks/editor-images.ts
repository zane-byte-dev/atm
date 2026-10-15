export function insertEditorImage(
  text: string,
  start: number,
  end: number,
  image: { name: string; url?: string },
) {
  if (!image.url?.startsWith('/api/v1/attachments/')) throw new Error('图片地址无效。')
  const from = Math.max(0, Math.min(start, text.length))
  const to = Math.max(from, Math.min(end, text.length))
  const alt = image.name.replace(/[\\[\]<>\r\n]/g, ' ')
  const markdown = `${from && text[from - 1] !== '\n' ? '\n' : ''}![${alt}](<${image.url}>)\n`
  return { text: text.slice(0, from) + markdown + text.slice(to), cursor: from + markdown.length }
}
