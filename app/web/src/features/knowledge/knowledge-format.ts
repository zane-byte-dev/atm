export const statusLabels: Record<string, string> = {
  active: '有效',
  draft: '草稿',
  archived: '已归档',
}
export const formatDate = (value?: string) =>
  value ? new Date(value).toLocaleString('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }) : '—'
export const scopeLabel = (scope: string) =>
  scope === 'global'
    ? '全局'
    : scope.startsWith('project:')
      ? `项目 · ${scope.slice(8)}`
      : scope.startsWith('session:')
        ? `会话 · ${scope.slice(8)}`
        : scope
export const splitValues = (value: string) => [
  ...new Set(
    value
      .split(/[,，\n]/)
      .map((item) => item.trim())
      .filter(Boolean),
  ),
]
