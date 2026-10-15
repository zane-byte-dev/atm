// Task documents include generated metadata and other sections. Only show the
// progress section by default; the original document remains available in UI.
export function taskProgress(content: string): string {
  const lines = content.split(/\r?\n/)
  let fence = ''
  let collecting = false
  const progress: string[] = []
  for (const line of lines) {
    const marker = line.match(/^\s{0,3}(`{3,}|~{3,})/)
    if (marker) {
      if (!fence) fence = marker[1]
      else if (marker[1][0] === fence[0] && marker[1].length >= fence.length) fence = ''
    } else if (!fence) {
      if (/^##\s+进展\s*#*\s*$/.test(line)) {
        collecting = true
        continue
      }
      if (collecting && /^#{1,2}\s/.test(line)) break
    }
    if (collecting) progress.push(line)
  }
  return progress.join('\n').trim()
}

// AppendProgress appends entries chronologically as dated list items. Preserve the
// whole entry, including continuation lines, and ignore document placeholders.
export function latestTaskProgress(content: string): string {
  const progress = taskProgress(content)
  const entries = progress.split(/(?=^- \[\d{4}-\d{2}-\d{2}[^\]]*\])/m)
  return (
    entries
      .filter((entry) => /^- \[\d{4}-/.test(entry))
      .at(-1)
      ?.replace(/^- /, '')
      .replace(/<!--[^]*?-->/g, '')
      .trim() || ''
  )
}
