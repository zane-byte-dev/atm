import test from 'node:test'
import assert from 'node:assert/strict'
import { taskTableCSV } from '../src/task-table-export.ts'
test('CSV quotes multiline values and neutralizes spreadsheet formulas', () => {
  const csv = taskTableCSV(
    [{ id: 't1', title: '=SUM(1,2)', status: 'open', priority: 'P1', tags: ['含"引号', '两\n行'] }],
    [
      { key: 'id', label: '编号' },
      { key: 'title', label: '任务' },
      { key: 'tags', label: '标签' },
    ],
  )
  assert.equal(csv, '\uFEFF"编号","任务","标签"\r\n"t1","\'=SUM(1,2)","含""引号、两\n行"')
})
