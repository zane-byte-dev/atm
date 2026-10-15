import { useRef, useState, type PointerEvent } from 'react'
import { nativeOrdered, saveNativePreferences } from '../../preferences/native-preferences'
import { useNativePreferences } from '../../preferences/native-preferences-react'
import type { KnowledgeCollection } from './knowledge-types'

export function useCollectionOrder(collections: KnowledgeCollection[]) {
  const preferences = useNativePreferences()
  const rows = nativeOrdered(collections, preferences.knowledge_collection_order)
  const source = useRef<{ id: string; x: number; y: number } | null>(null)
  const dropTarget = useRef('')
  const [target, setTarget] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const move = (id: string, to: string) => {
    const fromIndex = rows.findIndex((row) => row.id === id)
    const toIndex = rows.findIndex((row) => row.id === to)
    if (fromIndex < 0 || toIndex < 0 || fromIndex === toIndex) return
    const next = rows.map((row) => row.id)
    next.splice(fromIndex, 1)
    next.splice(toIndex, 0, id)
    try {
      saveNativePreferences({ knowledge_collection_order: next })
      setError(null)
    } catch {
      setError(new Error('无法保存集合排序，请允许浏览器本地存储后重试。'))
    }
  }
  const finish = () => {
    source.current = null
    dropTarget.current = ''
    setTarget('')
  }
  const handleProps = (id: string) => ({
    // Pointer capture keeps sorting consistent across mouse, pen and touch,
    // without relying on native HTML drag state when React reorders nodes.
    onPointerDown: (event: PointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0) return
      source.current = { id, x: event.clientX, y: event.clientY }
      event.currentTarget.setPointerCapture(event.pointerId)
    },
    onPointerMove: (event: PointerEvent<HTMLButtonElement>) => {
      const start = source.current
      if (!start || Math.hypot(event.clientX - start.x, event.clientY - start.y) < 5) return
      const element = document.elementFromPoint(event.clientX, event.clientY)
      const next =
        element?.closest<HTMLElement>('[data-collection-sort-id]')?.dataset.collectionSortId || ''
      dropTarget.current = next !== start.id ? next : ''
      setTarget(dropTarget.current)
    },
    onPointerUp: (event: PointerEvent<HTMLButtonElement>) => {
      const from = source.current?.id
      const to = dropTarget.current
      if (event.currentTarget.hasPointerCapture(event.pointerId))
        event.currentTarget.releasePointerCapture(event.pointerId)
      finish()
      if (from && to) move(from, to)
    },
    onPointerCancel: finish,
    onLostPointerCapture: finish,
  })
  const dropProps = (id: string) => ({
    'data-collection-sort-id': id,
    'data-drop-target': target === id || undefined,
  })
  return { rows, move, error, handleProps, dropProps }
}
export type CollectionOrder = ReturnType<typeof useCollectionOrder>
