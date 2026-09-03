import type { Draft, EditableFields, TodoDetail } from './types.ts'

export const editableFields = ['title', 'description', 'project', 'priority'] as const
export type EditableField = (typeof editableFields)[number]
export type MergeChoice = 'local' | 'latest'
export type MergeChoices = Partial<Record<EditableField, MergeChoice>>
export type FieldMerge = {
  field: EditableField
  local: string
  latest: string
  base?: string
  merged: string
  conflict: boolean
}
export type MergeReview = {
  latest: EditableFields
  etag: string
  fields: FieldMerge[]
}

export function fieldsFromDetail(detail: TodoDetail): EditableFields {
  return {
    title: detail.todo.title,
    description: detail.todo.description ?? '',
    project: detail.todo.project ?? '',
    priority: detail.todo.priority,
  }
}

export function freshDraft(
  initial: TodoDetail | undefined,
  defaultProject: string,
  operationID: string,
): Draft {
  const fields = initial
    ? fieldsFromDetail(initial)
    : { title: '', description: '', project: defaultProject, priority: 'P2' }
  return {
    ...fields,
    etag: initial?.etag ?? '',
    operationID,
    ...(initial ? { base: { ...fields } } : {}),
  }
}

// A missing base is not evidence that either side is unchanged. In that case
// every differing field requires a choice; equal values remain safe to merge.
export function buildMergeReview(draft: Draft, detail: TodoDetail): MergeReview {
  const latest = fieldsFromDetail(detail)
  return {
    latest,
    etag: detail.etag,
    fields: editableFields.map((field) => {
      const local = draft[field]
      const remote = latest[field]
      const base = draft.base?.[field]
      const identical = local === remote
      const localUnchanged = base !== undefined && local === base
      const latestUnchanged = base !== undefined && remote === base
      return {
        field,
        local,
        latest: remote,
        base,
        merged: localUnchanged ? remote : local,
        conflict: !identical && !localUnchanged && !latestUnchanged,
      }
    }),
  }
}

export function unresolvedFields(review: MergeReview, choices: MergeChoices): EditableField[] {
  return review.fields
    .filter((field) => field.conflict && choices[field.field] === undefined)
    .map((field) => field.field)
}

// Rebasing the ETag is inseparable from merging all four fields. Callers cannot
// merely take the new ETag and resend a stale complete form over remote edits.
export function applyMergeReview(draft: Draft, review: MergeReview, choices: MergeChoices): Draft {
  if (unresolvedFields(review, choices).length > 0)
    throw new Error('请先为每个冲突字段选择保留的内容。')
  const fields = { ...review.latest }
  for (const field of review.fields) {
    fields[field.field] = field.conflict
      ? choices[field.field] === 'local'
        ? field.local
        : field.latest
      : field.merged
  }
  return { ...draft, ...fields, etag: review.etag, base: { ...review.latest } }
}

export function startSeparateCreation(draft: Draft, operationID: string): Draft {
  if (!operationID || operationID === draft.operationID)
    throw new Error('新的任务需要独立的创建标识。')
  return { ...draft, operationID }
}

export function existingCreationID(details: Record<string, unknown>): string | undefined {
  const id = details.todo_id
  return typeof id === 'string' && /^t\d+$/.test(id) ? id : undefined
}
