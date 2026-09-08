import assert from 'node:assert/strict'
import test from 'node:test'
import { taskProgress } from '../src/task-progress.ts'

test('progress omits generated metadata and adjacent sections', () => {
  assert.equal(
    taskProgress(
      '# Title\n- ID: t1\n## 需求\n需求\n## 进展\n\n- 已验证\n### 细节\n结果\n## 备注\n备注',
    ),
    '- 已验证\n### 细节\n结果',
  )
  assert.equal(taskProgress('## 进展\n\n## 备注\n备注'), '')
  assert.equal(taskProgress('# 旧文档\n正文'), '')
})

test('headings in fenced code are not section boundaries', () => {
  assert.equal(
    taskProgress('```md\n## 进展\n示例\n```\n## 进展\n~~~md\n## 备注\n~~~\n结果\n## 备注'),
    '~~~md\n## 备注\n~~~\n结果',
  )
})

test('latest summary picks last appended milestone, excluding effect markers', async () => {
  const { latestTaskProgress } = await import('../src/task-progress.ts')
  assert.equal(
    latestTaskProgress(
      '## 进展\n- [2026-09-05 10:00] first\n- [2026-09-06 10:00] latest\n<!-- atm-progress:x -->\n## 备注\nother',
    ),
    '[2026-09-06 10:00] latest',
  )
  assert.equal(latestTaskProgress('## 进展\n\n## 备注'), '')
})
