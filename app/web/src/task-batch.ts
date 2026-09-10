import type { TodoDetail } from './types'
import type { TodoMutationRequest } from './api-contracts'
export type BatchOperation = 'priority' | 'project' | 'archive'
export type BatchResult = {
  id: string
  title: string
  ok: boolean
  error?: string
  undo?: { operation: 'priority' | 'project'; value: string; etag: string }
}
export async function applyTaskBatch(
  snapshots: TodoDetail[],
  operation: BatchOperation,
  value: string,
  invoke: (
    request: Extract<TodoMutationRequest, { method: 'todo.archive' | 'todo.update' }>,
  ) => Promise<unknown>,
  progress: (result: BatchResult) => void,
): Promise<BatchResult[]> {
  const results: BatchResult[] = []
  for (const snapshot of snapshots) {
    const result: BatchResult = { id: snapshot.todo.id, title: snapshot.todo.title, ok: false }
    try {
      if (operation === 'archive')
        await invoke({ method: 'todo.archive', input: { todo_id: snapshot.todo.id } })
      else {
        const updated = await invoke({
          method: 'todo.update',
          input: {
            todo_id: snapshot.todo.id,
            expected_etag: snapshot.etag,
            ...(operation === 'priority' ? { priority: value.trim() } : { project: value.trim() }),
          },
        })
        if (
          updated &&
          typeof updated === 'object' &&
          'etag' in updated &&
          typeof updated.etag === 'string'
        )
          result.undo = { operation, value: snapshot.todo[operation] || '', etag: updated.etag }
      }
      result.ok = true
    } catch (error) {
      result.error = error instanceof Error ? error.message : '操作未完成，请刷新确认后重试。'
    }
    results.push(result)
    progress(result)
  }
  return results
}

export async function undoTaskBatch(
  changes: BatchResult[],
  invoke: (request: Extract<TodoMutationRequest, { method: 'todo.update' }>) => Promise<unknown>,
): Promise<BatchResult[]> {
  const results: BatchResult[] = []
  for (const change of changes) {
    if (!change.undo) continue
    try {
      await invoke({
        method: 'todo.update',
        input: {
          todo_id: change.id,
          expected_etag: change.undo.etag,
          [change.undo.operation]: change.undo.value,
        },
      })
      results.push({ id: change.id, title: change.title, ok: true })
    } catch (error) {
      results.push({
        ...change,
        ok: false,
        error: error instanceof Error ? error.message : '撤销失败',
      })
    }
  }
  return results
}
