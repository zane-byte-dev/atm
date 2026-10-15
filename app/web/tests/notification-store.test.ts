import assert from 'node:assert/strict'
import test from 'node:test'
import { notificationStore as store } from '../src/features/notifications/notification-store.ts'

test('job transitions update one record and become unread again', () => {
  store.clear()
  store.publish({ id: 'job', title: '排队中', description: '', kind: 'info' })
  store.read('job')
  assert.ok(store.getSnapshot()[0].readAt)
  store.publish({ id: 'job', title: '已完成', description: '', kind: 'success' })
  assert.equal(store.getSnapshot().length, 1)
  assert.equal(store.getSnapshot()[0].title, '已完成')
  assert.equal(store.getSnapshot()[0].readAt, undefined)
  const snapshot = store.getSnapshot()
  assert.equal(store.getSnapshot(), snapshot)
})

test('history retains the latest 200 messages, supports all-read and clear', () => {
  store.clear()
  for (let index = 0; index < 205; index++)
    store.publish({ id: String(index), title: '通知', description: '', kind: 'info' })
  assert.equal(store.getSnapshot().length, 200)
  assert.equal(store.getSnapshot()[0].id, '204')
  assert.equal(store.getSnapshot().at(-1)?.id, '5')
  store.read()
  assert.ok(store.getSnapshot().every((message) => message.readAt))
  store.clear()
  assert.deepEqual(store.getSnapshot(), [])
})

test('background activity stays in history without becoming unread; a failure needs attention', () => {
  store.clear()
  store.publish({
    id: 'background',
    title: '已完成',
    description: '',
    kind: 'success',
    activity: true,
    readAt: Date.now(),
  })
  assert.equal(store.getSnapshot()[0].activity, true)
  assert.ok(store.getSnapshot()[0].readAt)
  store.publish({ id: 'background', title: '未完成', description: '连接失败', kind: 'error' })
  assert.equal(store.getSnapshot().length, 1)
  assert.equal(store.getSnapshot()[0].readAt, undefined)
  assert.equal(store.getSnapshot()[0].activity, undefined)
})
