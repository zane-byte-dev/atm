import test from 'node:test'
import assert from 'node:assert/strict'
import { ApiError, call, errorText } from '../src/api.ts'

test('creation conflict retains todo ID and key while retry sends the same identity', async (context) => {
  const previousFetch = globalThis.fetch
  context.after(() => {
    globalThis.fetch = previousFetch
  })
  const keys: (string | null)[] = []
  const details = { todo_id: 't17', idempotency_key: 'persisted-operation' }
  globalThis.fetch = async (_url, init) => {
    keys.push(new Headers(init?.headers).get('Idempotency-Key'))
    return new Response(
      JSON.stringify({
        error: { code: 'conflict', message: 'key was used with different input', details },
      }),
      { status: 409 },
    )
  }
  for (let attempt = 0; attempt < 2; attempt++) {
    await assert.rejects(
      call('todo.create', { title: 'Edited after request' }, undefined, 'persisted-operation'),
      (error) => {
        assert.ok(error instanceof ApiError)
        assert.equal(error.status, 409)
        assert.deepEqual(error.details, details)
        assert.match(errorText(error), /创建已经对应一个任务/)
        assert.doesNotMatch(errorText(error), /任务已在其他地方修改/)
        return true
      },
    )
  }
  assert.deepEqual(keys, ['persisted-operation', 'persisted-operation'])
})

test('an update conflict stays distinct and keeps diagnostic fields', async (context) => {
  const previousFetch = globalThis.fetch
  context.after(() => {
    globalThis.fetch = previousFetch
  })
  globalThis.fetch = async () =>
    new Response(
      JSON.stringify({
        error: {
          code: 'conflict',
          message: 'etag changed',
          details: { todo_id: 't1', expected_etag: 'v1' },
        },
      }),
      { status: 409 },
    )
  await assert.rejects(call('todo.update', { todo_id: 't1', expected_etag: 'v1' }), (error) => {
    assert.ok(error instanceof ApiError)
    assert.equal(error.details.expected_etag, 'v1')
    assert.match(errorText(error), /合并后再保存/)
    return true
  })
})
