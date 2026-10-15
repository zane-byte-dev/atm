import type { KnowledgeDocument, KnowledgeDocumentRow } from './knowledge-types.ts'
export type DocumentPatch = { status?: 'active' | 'draft' | 'archived'; collection?: string }
export type DocumentBatchResult = {
  id: string
  title: string
  outcome: 'updated' | 'skipped' | 'failed'
  error?: string
}
// Fetch a fresh version before each update; conflicts are reported, never overwritten or retried silently.
export async function updateDocumentBatch(
  rows: KnowledgeDocumentRow[],
  patch: DocumentPatch,
  api: {
    get: (id: string) => Promise<KnowledgeDocument>
    update: (doc: KnowledgeDocument, patch: DocumentPatch) => Promise<KnowledgeDocument>
  },
  onProgress: (completed: number) => void = () => {},
): Promise<DocumentBatchResult[]> {
  const results: DocumentBatchResult[] = []
  for (const row of rows) {
    try {
      const doc = await api.get(row.document_id)
      const unchanged =
        (!patch.status || doc.metadata.status === patch.status) &&
        (!patch.collection || doc.collection === patch.collection)
      if (!unchanged) await api.update(doc, patch)
      results.push({
        id: row.document_id,
        title: row.title,
        outcome: unchanged ? 'skipped' : 'updated',
      })
    } catch (error) {
      results.push({
        id: row.document_id,
        title: row.title,
        outcome: 'failed',
        error: error instanceof Error ? error.message : String(error),
      })
    }
    onProgress(results.length)
  }
  return results
}
