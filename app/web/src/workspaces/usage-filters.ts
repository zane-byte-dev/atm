import type { DayStats, MetricFields, UsageRange, UsageSnapshot } from './activity-types'

export function nativeClientAgent(value: string): string {
  return (
    (
      {
        Codex: 'codex',
        Claude: 'claude',
        Cursor: 'cursor',
        'Grok Build': 'grokbuild',
        Antigravity: 'antigravity',
        Pi: 'pi',
        Gemini: 'gemini',
      } as Record<string, string>
    )[value] || (value.length <= 100 ? value.toLowerCase() : '')
  )
}
export const usageProjectName = (project: string) => project || '未归类'
const numericFields: (keyof MetricFields)[] = [
  'sessions',
  'total_tokens',
  'total_input_tokens',
  'fresh_input_tokens',
  'output_tokens',
  'cache_read_tokens',
  'cache_create_tokens',
  'requests',
  'cost_usd',
  'estimated_cost_usd',
]
export function sumUsageDays(rows: (MetricFields & { date: string })[]): DayStats[] {
  const totals = new Map<string, DayStats>()
  for (const row of rows) {
    const current = totals.get(row.date) || ({ date: row.date, queries: 0 } as DayStats)
    for (const field of numericFields)
      (current[field] as number) = ((current[field] as number) || 0) + ((row[field] as number) || 0)
    totals.set(row.date, current)
  }
  return [...totals.values()].sort((a, b) => a.date.localeCompare(b.date))
}
/** The native app gives project totals priority: model and project buckets are independent. */
export function filterUsage(snapshot: UsageSnapshot, name: string, model: string, project: string) {
  const original = snapshot.ranges[name]
  if (!original) return undefined
  const models = original.model_stats.filter((row) => !model || row.model === model)
  const projects = original.project_stats.filter(
    (row) => !project || usageProjectName(row.project) === project,
  )
  const range: UsageRange = {
    ...original,
    model_stats: models,
    project_stats: projects,
    speed: {
      ...original.speed,
      models: original.speed.models.filter((row) => !model || row.model === model),
      turns: model ? [] : original.speed.turns,
    },
  }
  const hourly = name === 'today' || name === 'yesterday'
  let days: DayStats[]
  if (project)
    days = sumUsageDays(
      (hourly ? snapshot.project_hour_stats : snapshot.project_day_stats || [])?.filter(
        (row) => usageProjectName(row.project) === project,
      ) || [],
    )
  else if (model)
    days = sumUsageDays(
      (hourly ? snapshot.model_hour_stats : snapshot.model_day_stats || [])?.filter(
        (row) => row.model === model,
      ) || [],
    )
  else days = (hourly ? snapshot.hour_stats : snapshot.day_stats) || []
  days = days.filter(
    (row) =>
      row.date.slice(0, 10) >= original.start_date && row.date.slice(0, 10) <= original.end_date,
  )
  return { range, days, summary: project ? projects : models }
}
