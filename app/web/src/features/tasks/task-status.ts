export const statuses = [
  ['open', '待开始'],
  ['in_progress', '进行中'],
  ['review', '待验收'],
  ['done', '已完成'],
] as const

export const taskStatusLabels = Object.fromEntries(statuses) as Record<
  (typeof statuses)[number][0],
  string
>
