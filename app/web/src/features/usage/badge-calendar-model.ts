export function localCalendarDay(date = new Date()): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
export function calendarMonth(value: string | null, today = localCalendarDay()): string {
  return value &&
    /^(19[7-9]\d|[2-9]\d{3})-(0[1-9]|1[0-2])$/.test(value) &&
    value <= today.slice(0, 7)
    ? value
    : today.slice(0, 7)
}
export function monthDays(month: string) {
  const [year, index] = month.split('-').map(Number)
  const count = new Date(year, index, 0, 12).getDate()
  const leading = (new Date(year, index - 1, 1, 12).getDay() + 6) % 7
  const cells: (string | null)[] = Array.from({ length: leading }, () => null)
  for (let day = 1; day <= count; day++) cells.push(`${month}-${String(day).padStart(2, '0')}`)
  while (cells.length % 7) cells.push(null)
  return { cells, from: `${month}-01`, to: `${month}-${count}` }
}
export function adjacentMonth(month: string, step: number): string {
  const [year, index] = month.split('-').map(Number)
  return localCalendarDay(new Date(year, index - 1 + step, 1, 12)).slice(0, 7)
}
