import test from 'node:test'
import assert from 'node:assert/strict'
import { insertEditorImage } from '../src/editor-images.ts'
const image = { name: 'demo.png', url: '/api/v1/attachments/t1/demo.png' }
test('image insertion preserves surrounding text and replaces only selected text', () => {
  const result = insertEditorImage('beforeSELECTafter', 6, 12, image)
  assert.equal(result.text, 'before\n![demo.png](</api/v1/attachments/t1/demo.png>)\nafter')
  assert.equal(result.text.slice(result.cursor), 'after')
})
test('multiple images keep their order at the insertion cursor', () => {
  const first = insertEditorImage('tail', 0, 0, image)
  const second = insertEditorImage(first.text, first.cursor, first.cursor, {
    ...image,
    name: 'second',
  })
  assert.ok(second.text.indexOf('demo.png') < second.text.indexOf('second'))
  assert.equal(second.text.slice(second.cursor), 'tail')
})
test('image names cannot escape Markdown and external URLs are rejected', () => {
  const result = insertEditorImage('', 0, 0, { ...image, name: '[x]\\\n<script>' })
  assert.equal(result.text, '![ x    script ](</api/v1/attachments/t1/demo.png>)\n')
  assert.throws(() => insertEditorImage('', 0, 0, { name: 'x', url: 'https://example.com/x' }))
})
