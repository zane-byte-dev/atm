import test from 'node:test'
import assert from 'node:assert/strict'
import { adjacentMonth, calendarMonth, monthDays } from '../src/badge-calendar.ts'

test('month grid follows Monday week alignment and leap-year boundaries', () => {
  const leap = monthDays('2024-02')
  assert.equal(leap.to, '2024-02-29')
  assert.deepEqual(leap.cells.slice(0, 4), [null, null, null, '2024-02-01'])
  assert.equal(leap.cells.length % 7, 0)
  assert.equal(monthDays('2026-02').cells.filter(Boolean).length, 28)
  assert.equal(monthDays('2026-08').cells.length, 42)
})
test('month navigation crosses years and rejects invalid or future URL months', () => {
  assert.equal(adjacentMonth('2026-01', -1), '2025-12')
  assert.equal(adjacentMonth('2025-12', 1), '2026-01')
  for (const value of ['2026-13', '2027-01', 'bad', '0000-01', null])
    assert.equal(calendarMonth(value, '2026-09-06'), '2026-09')
  assert.equal(calendarMonth('2024-02', '2026-09-06'), '2024-02')
})
