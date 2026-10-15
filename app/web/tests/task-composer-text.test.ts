import test from 'node:test'
import assert from 'node:assert/strict'
import { composeTaskText, splitTaskText } from '../src/features/tasks/task-composer-text.ts'

test('the first line becomes the title and later lines become the description', () => {
  assert.deepEqual(splitTaskText('修复同步问题\n\n保留旧数据\n- 验证恢复'), {
    title: '修复同步问题',
    description: '保留旧数据\n- 验证恢复',
  })
  assert.deepEqual(splitTaskText('只写标题'), { title: '只写标题', description: '' })
  assert.deepEqual(splitTaskText('标题\n正文'), { title: '标题', description: '正文' })
})

test('editing an existing task preserves the full description', () => {
  const title = '已有标题'
  const description = '\n正文第一行\n\n![截图](<atm-draft-image:123>)'
  assert.deepEqual(splitTaskText(composeTaskText(title, description)), { title, description })
})
