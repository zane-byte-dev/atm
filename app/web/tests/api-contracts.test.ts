import test from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'

test('API methods infer responses, reject invalid requests and match Go wire fixtures', () => {
  const root = new URL('../', import.meta.url)
  const compiler = fileURLToPath(new URL('node_modules/typescript/bin/tsc', root))
  const result = spawnSync(
    process.execPath,
    [compiler, '--project', 'tests/tsconfig.api-contracts.json'],
    {
      cwd: fileURLToPath(root),
      encoding: 'utf8',
      timeout: 60_000,
    },
  )
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stdout + result.stderr)
  const wire = JSON.parse(
    readFileSync(new URL('fixtures/api-wire-contracts.json', import.meta.url), 'utf8'),
  )
  assert.equal(wire.todoDetailEmpty.latest_plan, null)
  assert.equal(wire.todoDetailEmpty.summary, null)
  assert.deepEqual(wire.todoDetailEmpty.bindings, [])
  assert.deepEqual(wire.todoDetailEmpty.sessions, [])
  assert.equal(typeof wire.todoDetailWithPlan.latest_plan.revision, 'number')
  assert.equal(wire.knowledgeQuery.has_more, true)
  assert.equal(typeof wire.knowledgeQuery.next_cursor, 'string')
  assert.equal(typeof wire.taskMetadata.selection_revision, 'string')
  assert.deepEqual(wire.taskMetadataEmpty.groups, [])
  assert.equal(wire.taskMetadataEmpty.matched_total, 0)
  assert.notEqual(wire.taskMetadataEmpty.selection_revision, wire.taskMetadata.selection_revision)
  assert.deepEqual(wire.knowledgeQueryEmpty.documents, [])
  assert.equal(wire.knowledgeQueryEmpty.has_more, false)
  assert.equal(wire.knowledgeQueryEmpty.next_cursor, undefined)
})
