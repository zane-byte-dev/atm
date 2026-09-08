export type ContextMessage = { sender?: string; time?: string; kind?: string; body: string }

export function parseContext(text: string): ContextMessage[] {
  const messages: ContextMessage[] = []
  let fence = ''
  for (const line of text.split('\n')) {
    const header =
      !fence &&
      line.match(
        /^(?:\[(新消息|上下文)\] )?(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[([^\]]+)\] (.*)$/,
      )
    if (header) {
      messages.push({ kind: header[1], time: header[2], sender: header[3], body: header[4] })
    } else if (messages.length) {
      messages[messages.length - 1].body += '\n' + line
    } else {
      messages.push({ body: line })
    }
    const marker = (header ? header[4] : line).match(/^\s{0,3}(`{3,}|~{3,})(.*)$/)
    if (marker) {
      if (!fence) fence = marker[1]
      else if (marker[1][0] === fence[0] && marker[1].length >= fence.length && !marker[2].trim())
        fence = ''
    }
  }
  return messages.filter((message) => message.sender || message.body.trim())
}

export function contextImageURL(src?: string): string | undefined {
  if (!src || /[\u0000-\u0020\\]/.test(src)) return undefined
  if (/^\/api\/v1\/attachments\/[^/?#]+$/.test(src)) return src
  try {
    const url = new URL(src)
    if ((url.protocol === 'https:' || url.protocol === 'http:') && !url.username && !url.password)
      return url.href
  } catch {
    /* Missing or unsupported image source. */
  }
  return undefined
}
