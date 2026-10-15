export function composeTaskText(title: string, description: string): string {
  return title + (description ? `\n\n${description}` : '')
}

export function splitTaskText(text: string): { title: string; description: string } {
  const newline = text.indexOf('\n')
  if (newline < 0) return { title: text, description: '' }
  const rest = text.slice(newline + 1)
  return {
    title: text.slice(0, newline),
    description: rest.startsWith('\n') ? rest.slice(1) : rest,
  }
}
