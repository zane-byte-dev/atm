import assert from 'node:assert/strict'
import test from 'node:test'
import { contextImageURL, parseContext } from '../src/features/collection/collection-context-model.ts'

test('collector messages retain multiline content, senders, timestamps and freshness', () => {
  const messages = parseContext(
    '[上下文] 2026-09-07 10:00:00 [小明] 前一条\n补充\n[新消息] 2026-09-07 10:01:00 [小红] ![截图](https://example.com/a.png)',
  )
  assert.equal(messages.length, 2)
  assert.deepEqual(messages[0], {
    kind: '上下文',
    time: '2026-09-07 10:00:00',
    sender: '小明',
    body: '前一条\n补充',
  })
  assert.equal(messages[1].kind, '新消息')
  assert.equal(messages[1].body, '![截图](https://example.com/a.png)')
  assert.equal(parseContext('2026-09-07 10:00:00 [小明] 旧格式')[0].sender, '小明')
})

test('unstructured text and message-like lines inside code blocks are preserved', () => {
  assert.deepEqual(parseContext('普通正文\n第二行'), [{ body: '普通正文\n第二行' }])
  assert.deepEqual(parseContext(''), [])
  const text =
    '[新消息] 2026-09-07 10:00:00 [小明] ```text\n2026-09-07 10:01:00 [示例] 内容\n```\n[新消息] 2026-09-07 10:02:00 [小红] 完成'
  assert.equal(parseContext(text).length, 2)
  assert.match(parseContext(text)[0].body, /\[示例\]/)
})

test('image sources allow web URLs and attachment IDs, rejecting unsafe schemes and arbitrary local paths', () => {
  for (const src of [
    'https://example.com/image.png?token=abc',
    'http://example.com/a',
    '/api/v1/attachments/abc',
  ])
    assert.equal(contextImageURL(src), src)
  for (const src of [
    'javascript:alert(1)',
    'data:image/svg+xml,abc',
    'file:///tmp/a.png',
    '/etc/passwd',
    '//example.com/a',
    'https://user:pass@example.com/a',
    '/api/v1/attachments/../secret',
    'https://example.com/\\x',
  ])
    assert.equal(contextImageURL(src), undefined)
})
