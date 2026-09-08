import type { KnowledgeDocumentRow, KnowledgeQueryResult } from './knowledge-types.ts'

// A refresh replaces the page sequence; only currently cached pages contribute
// selection candidates. IDs also guard against duplicate rows on a retry.
export function loadedKnowledgeDocuments(pages: readonly KnowledgeQueryResult[]) {
  const seen = new Set<string>()
  const documents: KnowledgeDocumentRow[] = []
  for (const page of pages)
    for (const document of page.documents)
      if (!seen.has(document.document_id)) {
        seen.add(document.document_id)
        documents.push(document)
      }
  return documents
}

export function nextKnowledgeCursor(page: KnowledgeQueryResult) {
  return page.has_more ? page.next_cursor : undefined
}
