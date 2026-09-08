import type { Todo } from './types'
// Quote all cells and neutralize spreadsheet formulas in user-controlled values.
export function taskTableCSV(rows: Todo[], columns: { key: string; label: string }[]) {
  const cell = (value: string) =>
    '"' + (/^[\s]*[=+@-]/.test(value) ? "'" + value : value).replaceAll('"', '""') + '"'
  return (
    '\uFEFF' +
    [
      columns.map((col) => col.label),
      ...rows.map((row) =>
        columns.map((col) =>
          col.key === 'tags'
            ? (row.tags || []).join('、')
            : String(row[col.key as keyof Todo] || ''),
        ),
      ),
    ]
      .map((row) => row.map(cell).join(','))
      .join('\r\n')
  )
}
