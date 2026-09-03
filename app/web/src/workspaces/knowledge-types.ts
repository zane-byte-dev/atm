export type KnowledgeCollection = {
  id: string
  name: string
  description: string
  role?: string
  topics?: string[]
  use_when?: string[]
  avoid_when?: string[]
  instructions?: string[]
  document_count: number
}

export type KnowledgeDocumentRow = {
  document_id: string
  title: string
  collection: string
  status: string
  domains?: string[]
  tags?: string[]
  projects?: string[]
  producer?: string
  created_at?: string
  updated_at?: string
  snippet?: string
  score?: number
}

export type KnowledgeDocument = {
  metadata: {
    id: string
    title: string
    status: string
    domains?: string[]
    tags?: string[]
    projects?: string[]
    producer: string
    createdAt: string
    updatedAt: string
    source?: { type: string; uri: string; hash?: string; importedAt?: string }
  }
  collection: string
  content?: string
}

export type MemoryHit = {
  id: string
  scope: string
  content: string
  tags?: string[]
  created_at: string
  score: number
  source: string
  metadata?: Record<string, string>
}

export type MemoryEventResult = {
  event: {
    id: string
    scope: string
    content: string
    tags?: string[]
    targetId?: string
    createdAt: string
  }
}
