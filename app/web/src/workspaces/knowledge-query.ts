import { useEffect, useMemo } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, call } from '../api'
import { loadedKnowledgeDocuments, nextKnowledgeCursor } from './knowledge-pagination'
import type { KnowledgeQueryInput } from './knowledge-types'

const staleCursor = (error: unknown) =>
  error instanceof ApiError && error.details.reason === 'knowledge_cursor_stale'

export function useKnowledgeDocuments(
  input: Omit<KnowledgeQueryInput, 'cursor' | 'limit'>,
  enabled = true,
) {
  const client = useQueryClient()
  const { collection = '', text = '', status = '', exclude_archived = false } = input
  const queryKey = useMemo(
    () => ['knowledge', 'query', collection, text, status, exclude_archived],
    [collection, text, status, exclude_archived],
  )
  const query = useInfiniteQuery({
    queryKey,
    initialPageParam: '',
    queryFn: ({ signal, pageParam }) =>
      call(
        'knowledge.query',
        {
          collection,
          text,
          status,
          exclude_archived,
          limit: 50,
          ...(pageParam ? { cursor: pageParam } : {}),
        },
        signal,
      ),
    getNextPageParam: nextKnowledgeCursor,
    enabled,
    retry: (attempt, error) => !staleCursor(error) && attempt < 1,
  })
  // Corpus changes invalidate every later cursor. Restart the page sequence
  // instead of appending a fresh first page to stale rows.
  useEffect(() => {
    if (staleCursor(query.error) && staleCursor(client.getQueryState(queryKey)?.error))
      void client.resetQueries({ queryKey, exact: true })
  }, [client, query.error, queryKey])
  const documents = useMemo(() => loadedKnowledgeDocuments(query.data?.pages || []), [query.data])
  return {
    ...query,
    documents,
    total: query.data?.pages[0]?.total,
    refresh: () => client.resetQueries({ queryKey, exact: true }),
  }
}
