import type { TodoDetail } from './types'
export type BatchOperation = 'priority' | 'project' | 'archive'
export type BatchResult = { id: string; title: string; ok: boolean; error?: string }
export async function applyTaskBatch(
  snapshots: TodoDetail[],
  operation: BatchOperation,
  value: string,
  invoke: (method: string, input: object) => Promise<unknown>,
  progress: (result: BatchResult) => void,
): Promise<BatchResult[]> {
  const results: BatchResult[] = []
  for (const snapshot of snapshots) {
    const result: BatchResult = { id: snapshot.todo.id, title: snapshot.todo.title, ok: false }
    try {
      if (operation === 'archive') await invoke('todo.archive', { todo_id: snapshot.todo.id })
      else
        await invoke('todo.update', {
          todo_id: snapshot.todo.id,
          expected_etag: snapshot.etag,
          [operation]: value.trim(),
        })
      result.ok = true
    } catch (error) {
      result.error = error instanceof Error ? error.message : '操作未完成，请刷新确认后重试。'
    }
    results.push(result)
    progress(result)
  }
  return results
}
