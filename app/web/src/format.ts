export const int = (value: number) => new Intl.NumberFormat('zh-CN').format(value || 0)
export const agentName = (value: string) =>
  ({
    codex: 'Codex',
    claude: 'Claude',
    cursor: 'Cursor',
    grokbuild: 'Grok Build',
    antigravity: 'Antigravity',
  })[value] || value
export const dateTime = (value?: string) => {
  if (!value) return '尚无记录'
  const date = new Date(value)
  return Number.isNaN(date.valueOf())
    ? value
    : date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
      })
}
