import { useEffect, useRef, useState } from 'react'
export type ContentDraft = {
  title: string
  content: string
  collection: string
  tags: string
  domains: string
  projects: string
  scope: string
  id: string
  name: string
  description: string
  status: string
  etag: string
}
const emptyDraft: ContentDraft = {
  title: '',
  content: '',
  collection: '',
  tags: '',
  domains: '',
  projects: '',
  scope: 'global',
  id: '',
  name: '',
  description: '',
  status: 'active',
  etag: '',
}

// A document instance owns its own record. Recovering another tab's draft
// copies it, so saving here never destroys another editor's unsubmitted work.
const draftOwner = crypto.randomUUID()
export type RecoverableDraft = { key: string; draft: ContentDraft; savedAt: number }
function parseDraft(raw: string): { draft: ContentDraft; savedAt: number } | undefined {
  try {
    const parsed = JSON.parse(raw)
    const value = parsed?.draft || parsed
    if (!value || typeof value.content !== 'string' || typeof value.title !== 'string') return
    const draft = { ...emptyDraft }
    for (const key of Object.keys(emptyDraft) as (keyof ContentDraft)[]) {
      if (typeof value[key] === 'string') draft[key] = value[key]
    }
    return {
      draft,
      savedAt:
        typeof parsed.savedAt === 'number' &&
        Number.isFinite(parsed.savedAt) &&
        parsed.savedAt > 0 &&
        parsed.savedAt < 8640000000000000
          ? parsed.savedAt
          : 0,
    }
  } catch {
    return
  }
}
export function useKnowledgeDraft(key: string, initial: Partial<ContentDraft>) {
  const prefix = `atm.web.knowledge.draft.v2:${key}:`
  const storageKey = `${prefix}${draftOwner}`
  const [storageError, setStorageError] = useState(false)
  const [restored, setRestored] = useState(false)
  const completed = useRef(false)
  const persistenceBlocked = useRef(false)
  const [candidates, setCandidates] = useState<RecoverableDraft[]>(() => {
    const result: RecoverableDraft[] = []
    try {
      for (let index = 0; index < localStorage.length; index++) {
        const item = localStorage.key(index)
        if (item?.startsWith(prefix) && item !== storageKey) {
          const parsed = parseDraft(localStorage.getItem(item) || '')
          if (parsed) result.push({ key: item, ...parsed })
        }
      }
      const legacyKey = `atm.web.knowledge.draft.v1:${key.replace(/^copy:/, 'document:')}`
      const legacy = parseDraft(sessionStorage.getItem(legacyKey) || '')
      if (legacy) result.push({ key: `session:${legacyKey}`, ...legacy })
    } catch {
      /* The editable form remains available without storage. */
    }
    return result.sort((a, b) => b.savedAt - a.savedAt).slice(0, 10)
  })
  const [draft, setDraft] = useState<ContentDraft>(() => {
    try {
      const raw = localStorage.getItem(storageKey)
      if (raw) {
        const parsed = parseDraft(raw)
        if (parsed) return parsed.draft
        persistenceBlocked.current = true
      }
    } catch {
      persistenceBlocked.current = true
    }
    return { ...emptyDraft, ...initial }
  })
  const draftRef = useRef(draft)
  draftRef.current = draft
  const lastStored = useRef('')
  const persist = (value: ContentDraft) => {
    if (completed.current || persistenceBlocked.current) {
      if (persistenceBlocked.current) setStorageError(true)
      return
    }
    try {
      const serialized = JSON.stringify({ draft: value, savedAt: Date.now() })
      localStorage.setItem(storageKey, serialized)
      lastStored.current = serialized
      setStorageError(false)
    } catch {
      setStorageError(true)
    }
  }
  useEffect(() => {
    persist(draft)
  }, [draft, storageKey])
  const patch = (value: Partial<ContentDraft>) => {
    const next = { ...draftRef.current, ...value }
    draftRef.current = next
    persist(next)
    setDraft(next)
  }
  const rebuildDraft = () => {
    persistenceBlocked.current = false
    persist(draftRef.current)
  }
  const recover = (candidate: RecoverableDraft) => {
    patch(candidate.draft)
    setCandidates([])
    setRestored(true)
  }
  const discard = (candidate: RecoverableDraft) => {
    try {
      if (candidate.key.startsWith('session:')) sessionStorage.removeItem(candidate.key.slice(8))
      else localStorage.removeItem(candidate.key)
      setCandidates((previous) => previous.filter((item) => item.key !== candidate.key))
    } catch {
      setStorageError(true)
    }
  }
  const clear = () => {
    completed.current = true
    try {
      if (localStorage.getItem(storageKey) === lastStored.current)
        localStorage.removeItem(storageKey)
    } catch {
      setStorageError(true)
    }
  }
  return { draft, patch, clear, rebuildDraft, storageError, restored, candidates, recover, discard }
}
