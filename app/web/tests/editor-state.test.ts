import test from 'node:test'
import assert from 'node:assert/strict'
import {
  applyMergeReview,
  buildMergeReview,
  existingCreationID,
  freshDraft,
  startSeparateCreation,
  unresolvedFields,
} from '../src/editor-state.ts'
import type { EditableFields, TodoDetail } from '../src/types.ts'

function detail(fields: Partial<EditableFields> = {}, etag = 'v1'): TodoDetail {
  return {
    todo: {
      id: 't1',
      status: 'open',
      created: '2026-09-03',
      title: 'Original title',
      description: 'Original description',
      project: 'atm',
      priority: 'P2',
      ...fields,
    },
    etag,
  }
}

test('non-overlapping changes retain both sides and rebase only on explicit merge', () => {
  const draft = freshDraft(detail(), '', 'stable-operation')
  draft.title = 'Local title'
  const review = buildMergeReview(draft, detail({ description: 'Remote description' }, 'v2'))
  assert.deepEqual(unresolvedFields(review, {}), [])
  assert.equal(draft.etag, 'v1')
  const merged = applyMergeReview(draft, review, {})
  assert.equal(merged.title, 'Local title')
  assert.equal(merged.description, 'Remote description')
  assert.equal(merged.etag, 'v2')
  assert.equal(merged.operationID, 'stable-operation')
  assert.equal(merged.base?.title, 'Original title')
  assert.equal(merged.base?.description, 'Remote description')
  assert.equal(draft.base?.description, 'Original description')
})

test('all four true conflicts must be explicitly resolved before taking a new ETag', () => {
  const draft = {
    ...freshDraft(detail(), '', 'same-key'),
    title: 'Local',
    description: 'Local body',
    project: 'local',
    priority: 'P0',
  }
  const review = buildMergeReview(
    draft,
    detail(
      { title: 'Remote', description: 'Remote body', project: 'remote', priority: 'P1' },
      'v2',
    ),
  )
  assert.deepEqual(unresolvedFields(review, {}), ['title', 'description', 'project', 'priority'])
  assert.throws(() => applyMergeReview(draft, review, {}), /冲突字段/)
  assert.throws(
    () =>
      applyMergeReview(draft, review, { title: 'local', description: 'latest', project: 'latest' }),
    /冲突字段/,
  )
  const merged = applyMergeReview(draft, review, {
    title: 'local',
    description: 'latest',
    project: 'latest',
    priority: 'local',
  })
  assert.deepEqual(
    [merged.title, merged.description, merged.project, merged.priority],
    ['Local', 'Remote body', 'remote', 'P0'],
  )
  assert.equal(merged.etag, 'v2')
  assert.equal(merged.operationID, 'same-key')
})

test('identical changes and intentional empty values do not become false conflicts', () => {
  const draft = {
    ...freshDraft(detail(), '', 'key'),
    title: 'Same change',
    description: '',
    project: '',
  }
  const review = buildMergeReview(draft, detail({ title: 'Same change', project: '' }, 'v2'))
  assert.deepEqual(unresolvedFields(review, {}), [])
  const merged = applyMergeReview(draft, review, {})
  assert.equal(merged.title, 'Same change')
  assert.equal(merged.description, '')
  assert.equal(merged.project, '')
})

test('a legacy draft without a baseline cannot silently overwrite newer fields', () => {
  const draft = freshDraft(detail(), '', 'key')
  delete draft.base
  const review = buildMergeReview(
    draft,
    detail({ description: 'Remote edit', priority: 'P0' }, 'v2'),
  )
  assert.deepEqual(unresolvedFields(review, {}), ['description', 'priority'])
  assert.throws(() => applyMergeReview(draft, review, {}), /冲突字段/)
})

test('a later remote edit is compared with the newly accepted baseline', () => {
  const draft = { ...freshDraft(detail(), '', 'key'), title: 'Local title' }
  const first = applyMergeReview(
    draft,
    buildMergeReview(draft, detail({ description: 'Remote first' }, 'v2')),
    {},
  )
  const locallyEdited = { ...first, description: 'Local follow-up' }
  const secondReview = buildMergeReview(
    locallyEdited,
    detail({ description: 'Remote second' }, 'v3'),
  )
  assert.deepEqual(unresolvedFields(secondReview, {}), ['description'])
  const second = applyMergeReview(locallyEdited, secondReview, { description: 'latest' })
  assert.equal(second.title, 'Local title')
  assert.equal(second.description, 'Remote second')
})

test('only an explicit separate creation changes a copied draft operation ID', () => {
  const draft = freshDraft(undefined, 'atm', 'original-operation')
  draft.title = 'Changed content after an uncertain request'
  const copiedDraft = structuredClone(draft)
  assert.equal(copiedDraft.operationID, 'original-operation')
  const separate = startSeparateCreation(copiedDraft, 'explicit-new-operation')
  assert.equal(separate.title, draft.title)
  assert.equal(separate.operationID, 'explicit-new-operation')
  assert.equal(draft.operationID, 'original-operation')
  assert.throws(() => startSeparateCreation(draft, 'original-operation'))
  assert.throws(() => startSeparateCreation(draft, ''))
})

test('the existing creation link only accepts canonical todo IDs', () => {
  assert.equal(existingCreationID({ todo_id: 't234', idempotency_key: 'key' }), 't234')
  assert.equal(existingCreationID({ todo_id: '/tasks/t1?redirect=evil' }), undefined)
  assert.equal(existingCreationID({ todo_id: 'https://example.com' }), undefined)
  assert.equal(existingCreationID({}), undefined)
})
