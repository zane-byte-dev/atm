import assert from 'node:assert/strict'
import test from 'node:test'
import { imageUploadError, taskImageMaxBytes } from '../src/image-uploads.ts'

test('image upload rejects executable vector content, empty files, and oversized images before sending', () => {
  assert.match(
    imageUploadError({ name: 'script.svg', type: 'image/svg+xml', size: 10 }) ?? '',
    /仅支持/,
  )
  assert.match(imageUploadError({ name: 'empty.png', type: 'image/png', size: 0 }) ?? '', /为空/)
  assert.match(
    imageUploadError({ name: 'huge.jpg', type: 'image/jpeg', size: taskImageMaxBytes + 1 }) ?? '',
    /10 MB/,
  )
  assert.equal(
    imageUploadError({ name: 'valid.gif', type: 'image/gif', size: taskImageMaxBytes }),
    undefined,
  )
})
