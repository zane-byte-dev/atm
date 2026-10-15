import assert from 'node:assert/strict'
import test from 'node:test'
import { canRoundTripMarkdown } from '../src/features/knowledge/knowledge-markdown.ts'

test('permits formatting normalization while preserving Markdown meaning', () => {
  assert.equal(
    canRoundTripMarkdown('* one\n* two\n\n__strong__', '- one\n\n- two\n\n**strong**\n'),
    true,
  )
  assert.equal(
    canRoundTripMarkdown(
      '[docs][ref]\n\n[ref]: https://example.com "Docs"',
      '[docs](https://example.com "Docs")',
    ),
    true,
  )
})
test('detects lost or changed content, structure, code, table and task state', () => {
  for (const [before, after] of [
    ['<details><summary>Note</summary>Keep me</details>', 'Keep me'],
    ['```ts\nconst x = 1\n```', '```ts\nconst x = 2\n```'],
    ['| A | B |\n| - | - |\n| 1 | 2 |', '| A | B |\n| - | - |\n| 1 | 3 |'],
    ['- [x] Done', '- [ ] Done'],
    ['[docs](https://example.com)', '[docs](https://other.example.com)'],
    ['![alt](https://example.com/a.png)', ''],
  ])
    assert.equal(canRoundTripMarkdown(before, after), false)
})
