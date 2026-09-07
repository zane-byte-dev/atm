import { useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, LoaderCircle } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { ApiError, call, errorText } from './api'
import { createNewTaskInputStore } from './new-task-input'
import {
  applyMergeReview,
  buildMergeReview,
  existingCreationID,
  freshDraft,
  startSeparateCreation,
  unresolvedFields,
} from './editor-state'
import type { MergeChoices, MergeReview } from './editor-state'
import type { Draft, Todo, TodoDetail, OperationWarning } from './types'
import { TaskImageUpload } from './task-images'
import type { TaskImageUploadHandle } from './task-images'

export type EditorProps = {
  initial?: TodoDetail
  onClose: () => void
  onSaved: (id: string, warnings?: OperationWarning[]) => void
  onBusyChange?: (busy: boolean) => void
  defaultProject?: string
}

type StorageState = 'loading' | 'saving' | 'saved' | 'failed'
const fieldLabels = {
  title: '任务标题',
  description: '任务说明',
  project: '项目',
  priority: '优先级',
}

export function Editor({
  initial,
  onClose,
  onSaved,
  onBusyChange,
  defaultProject = '',
}: EditorProps) {
  // A polling refresh must never replace the version the user started editing.
  const original = useRef(initial)
  const creating = !original.current
  const [draft, setDraft] = useState(() =>
    freshDraft(original.current, defaultProject, crypto.randomUUID()),
  )
  const draftRef = useRef(draft)
  const [inputStore] = useState(() => createNewTaskInputStore(() => globalThis.localStorage))
  const [ready, setReady] = useState(false)
  const readyRef = useRef(false)
  const [storageState, setStorageState] = useState<StorageState>('loading')
  const [preview, setPreview] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [imageTodo, setImageTodo] = useState(original.current?.todo)
  const imageUpload = useRef<TaskImageUploadHandle>(null)
  const [merge, setMerge] = useState<MergeReview>()
  const [choices, setChoices] = useState<MergeChoices>({})
  const [conflictLocked, setConflictLocked] = useState(false)
  const [loadingLatest, setLoadingLatest] = useState(false)
  const [latestError, setLatestError] = useState<unknown>()
  const [merged, setMerged] = useState(false)
  const [creationConflict, setCreationConflict] = useState<{ todoID?: string }>()
  const [separateCreation, setSeparateCreation] = useState(false)
  const [savedWithDraft, setSavedWithDraft] = useState<string>()
  const persistenceBlocked = useRef(false)
  const dirty = useRef(false)
  const completed = useRef(false)
  const alive = useRef(true)
  const writeSequence = useRef(0)
  const latestRequest = useRef<AbortController | undefined>(undefined)
  const titleInput = useRef<HTMLInputElement>(null)
  const titleId = useId()
  const descriptionId = useId()
  const query = useQueryClient()

  const replaceDraft = (next: Draft) => {
    draftRef.current = next
    setDraft(next)
  }
  const persist = (value: Draft) => {
    if (!creating || !dirty.current) {
      setStorageState('saved')
      return Promise.resolve()
    }
    const sequence = ++writeSequence.current
    setStorageState('saving')
    return inputStore
      .save(value)
      .then(() => {
        if (alive.current && sequence === writeSequence.current) {
          if (JSON.stringify(draftRef.current) === JSON.stringify(value)) dirty.current = false
          setStorageState('saved')
        }
      })
      .catch(() => {
        if (alive.current && sequence === writeSequence.current) setStorageState('failed')
      })
  }

  useEffect(() => {
    let active = true
    alive.current = true
    ;(creating ? inputStore.read() : Promise.resolve(undefined))
      .then((value) => {
        if (!active) return
        if (value) replaceDraft(value)
        persistenceBlocked.current = false
      })
      .catch(() => {
        if (!active) return
        // A failed read may mean a damaged record. Preserve it until the user
        // explicitly chooses to replace it with the form currently on screen.
        persistenceBlocked.current = true
        setStorageState('failed')
      })
      .finally(() => {
        if (active) {
          readyRef.current = true
          setReady(true)
        }
      })
    const flush = () => {
      if (
        !creating ||
        !readyRef.current ||
        !dirty.current ||
        completed.current ||
        persistenceBlocked.current
      )
        return
      // Persist the latest edited content synchronously during pagehide.
      void inputStore.save(draftRef.current).catch(() => {
        if (alive.current) setStorageState('failed')
      })
    }
    window.addEventListener('pagehide', flush)
    return () => {
      active = false
      alive.current = false
      latestRequest.current?.abort()
      window.removeEventListener('pagehide', flush)
      flush()
    }
  }, [creating])

  useEffect(() => {
    if (ready && !completed.current && !persistenceBlocked.current) void persist(draft)
  }, [draft, ready, creating])
  useEffect(() => {
    if (ready) titleInput.current?.focus()
  }, [ready])

  const loadLatest = async () => {
    const current = original.current
    if (!current) return
    setConflictLocked(true)
    setLoadingLatest(true)
    setLatestError(undefined)
    latestRequest.current?.abort()
    const controller = new AbortController()
    latestRequest.current = controller
    try {
      const result = await call<TodoDetail>(
        'todo.show',
        { todo_id: current.todo.id },
        controller.signal,
      )
      if (!alive.current || controller.signal.aborted) return
      setImageTodo(result.todo)
      setMerge(buildMergeReview(draftRef.current, result))
      setChoices({})
    } catch (error) {
      if (alive.current && !controller.signal.aborted) setLatestError(error)
    } finally {
      if (alive.current && !controller.signal.aborted) setLoadingLatest(false)
    }
  }

  const mutation = useMutation({
    retry: false,
    mutationFn: async (submitted: Draft) => {
      const current = original.current
      const payload = {
        title: submitted.title.trim(),
        description: submitted.description,
        priority: submitted.priority,
        project: submitted.project.trim(),
      }
      const result = await call<{ todo: Todo; etag: string; warnings?: OperationWarning[] }>(
        current ? 'todo.update' : 'todo.create',
        current ? { ...payload, todo_id: current.todo.id, expected_etag: submitted.etag } : payload,
        undefined,
        current ? undefined : submitted.operationID,
      )
      completed.current = true
      ++writeSequence.current
      let draftRemoved = !creating
      // A route change can mount a new editor for the same task while this
      // request is in flight. Its newer draft must survive the old response.
      if (creating) {
        try {
          draftRemoved = await inputStore.clearIfUnchanged(submitted)
        } catch {
          /* Keep the draft and report below. */
        }
      }
      return { ...result, draftRemoved }
    },
    onError: (error) => {
      if (!alive.current) return
      if (!(error instanceof ApiError) || error.status !== 409) return
      if (original.current) void loadLatest()
      else setCreationConflict({ todoID: existingCreationID(error.details) })
    },
    onSuccess: async (result) => {
      await Promise.all([
        query.invalidateQueries({ queryKey: ['todos'] }),
        query.invalidateQueries({ queryKey: ['todo', result.todo.id] }),
        query.invalidateQueries({ queryKey: ['runtime-jobs'] }),
      ])
      if (!alive.current) return
      if (result.draftRemoved) onSaved(result.todo.id, result.warnings)
      else {
        setStorageState('failed')
        setSavedWithDraft(result.todo.id)
      }
    },
  })
  useEffect(() => {
    onBusyChange?.(mutation.isPending || uploading)
  }, [mutation.isPending, uploading, onBusyChange])
  useEffect(
    () => () => {
      onBusyChange?.(false)
    },
    [onBusyChange],
  )

  const disabled = !ready || mutation.isPending || uploading || conflictLocked || !!savedWithDraft
  const submit = () => {
    if (!disabled && draftRef.current.title.trim() && !creationConflict && !completed.current)
      mutation.mutate({ ...draftRef.current })
  }
  const patch = (next: Partial<Draft>) => {
    dirty.current = true
    replaceDraft({ ...draftRef.current, ...next })
    mutation.reset()
    setMerged(false)
  }
  const unresolved = merge ? unresolvedFields(merge, choices) : []

  return (
    <form
      className="editor"
      onPaste={(event) => {
        const files = Array.from(event.clipboardData.files)
        if (files.length && imageUpload.current && !disabled) {
          event.preventDefault()
          imageUpload.current.add(files)
        }
      }}
      onDragOver={(event) => {
        if (imageUpload.current && event.dataTransfer.types.includes('Files'))
          event.preventDefault()
      }}
      onDrop={(event) => {
        if (event.dataTransfer.files.length && imageUpload.current && !disabled) {
          event.preventDefault()
          imageUpload.current.add(Array.from(event.dataTransfer.files))
        }
      }}
      onSubmit={(event) => {
        event.preventDefault()
        submit()
      }}
      onKeyDown={(event) => {
        if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
          event.preventDefault()
          submit()
        }
      }}
    >
      {storageState === 'failed' && !savedWithDraft && (
        <div className="notice" role="alert" style={{ display: 'block' }}>
          <p>无法记住新建任务的填写内容。刷新或关闭页面可能丢失输入，请先创建任务或复制内容。</p>
          <button
            type="button"
            disabled={mutation.isPending}
            onClick={() => {
              persistenceBlocked.current = false
              dirty.current = true
              void persist(draftRef.current)
            }}
          >
            记住当前填写内容
          </button>
        </div>
      )}
      {savedWithDraft && (
        <div className="notice" role="status" style={{ display: 'block' }}>
          <p>任务 {savedWithDraft} 已创建。浏览器中仍有未清除的填写内容，下次新建时请核对。</p>
          <button type="button" onClick={() => onSaved(savedWithDraft, mutation.data?.warnings)}>
            打开已保存任务
          </button>
        </div>
      )}
      <label className="field-label" htmlFor={titleId}>
        任务标题
      </label>
      <input
        ref={titleInput}
        id={titleId}
        className="title-input"
        autoFocus
        required
        maxLength={500}
        placeholder="想推进什么事情？"
        value={draft.title}
        disabled={disabled}
        onChange={(event) => patch({ title: event.target.value })}
      />
      <div className="editor-metadata">
        <label>
          <span className="field-label">项目</span>
          <input
            value={draft.project}
            disabled={disabled}
            onChange={(event) => patch({ project: event.target.value })}
            placeholder="例如 atm"
          />
        </label>
        <label>
          <span className="field-label">优先级</span>
          <select
            value={draft.priority}
            disabled={disabled}
            onChange={(event) => patch({ priority: event.target.value })}
          >
            <option value="P2">P2 · 正常</option>
            <option value="P1">P1 · 优先</option>
            <option value="P0">P0 · 紧急</option>
          </select>
        </label>
      </div>
      <div className="description-label">
        <label className="field-label" htmlFor={descriptionId}>
          任务说明
        </label>
        <button type="button" className="text-button" onClick={() => setPreview(!preview)}>
          {preview ? '继续编辑' : '预览'}
        </button>
      </div>
      {preview ? (
        <div className="editor-preview">
          <Markdown text={draft.description || '预览会显示在这里。'} />
        </div>
      ) : (
        <textarea
          id={descriptionId}
          value={draft.description}
          disabled={disabled}
          onChange={(event) => patch({ description: event.target.value })}
          placeholder="写下背景、目标，以及怎样才算完成。支持 Markdown。"
          rows={12}
        />
      )}
      {imageTodo && !conflictLocked ? (
        <TaskImageUpload
          ref={imageUpload}
          todo={imageTodo}
          etag={draft.etag}
          onBusyChange={setUploading}
          onConflict={() => void loadLatest()}
          onUploaded={(result) => {
            setImageTodo(result.todo)
            replaceDraft({ ...draftRef.current, etag: result.etag })
          }}
        />
      ) : (
        !original.current && (
          <p className="task-upload-status">创建任务后可以添加图片，支持拖入和粘贴。</p>
        )
      )}
      {mutation.error && !creationConflict && <Notice error={mutation.error} />}
      {conflictLocked && (
        <section aria-label="合并任务修改" style={{ marginBlock: 20 }}>
          <h3>合并任务修改</h3>
          <p>
            任务已经更新。请比较双方内容；没有冲突的字段会保留双方改动，有冲突的字段需要逐项选择。
          </p>
          {loadingLatest && (
            <p role="status">
              <LoaderCircle className="spin" size={15} /> 正在读取最新版本…
            </p>
          )}
          {latestError !== undefined && (
            <Notice
              error={latestError}
              retry={() => {
                void loadLatest()
              }}
            />
          )}
          {!merge && !loadingLatest && latestError === undefined && (
            <button
              type="button"
              className="button"
              onClick={() => {
                void loadLatest()
              }}
            >
              读取最新版本
            </button>
          )}
          {merge && (
            <>
              {merge.fields.map((field) => (
                <fieldset
                  key={field.field}
                  style={{
                    border: '1px solid var(--line)',
                    borderRadius: 8,
                    padding: 12,
                    marginBlock: 14,
                  }}
                >
                  <legend>
                    {fieldLabels[field.field]} · {field.conflict ? '需要选择' : '可自动合并'}
                  </legend>
                  <div
                    style={{
                      display: 'grid',
                      gridTemplateColumns: 'repeat(auto-fit, minmax(min(220px, 100%), 1fr))',
                      gap: 12,
                    }}
                  >
                    {(['local', 'latest'] as const).map((side) => (
                      <div key={side}>
                        {field.conflict ? (
                          <label
                            style={{
                              display: 'flex',
                              gap: 7,
                              alignItems: 'center',
                              marginBottom: 8,
                            }}
                          >
                            <input
                              type="radio"
                              style={{ width: 'auto' }}
                              name={`merge-${field.field}`}
                              checked={choices[field.field] === side}
                              disabled={loadingLatest}
                              onChange={() =>
                                setChoices((current) => ({ ...current, [field.field]: side }))
                              }
                            />
                            {side === 'local' ? '保留我的内容' : '采用最新内容'}
                          </label>
                        ) : (
                          <span className="field-label">
                            {side === 'local' ? '我的内容' : '最新内容'}
                          </span>
                        )}
                        <textarea
                          aria-label={`${fieldLabels[field.field]} · ${side === 'local' ? '我的内容' : '最新内容'}`}
                          readOnly
                          value={field[side] || '（空）'}
                          rows={field.field === 'description' ? 7 : 2}
                          style={{
                            minHeight: field.field === 'description' ? 145 : 60,
                            width: '100%',
                          }}
                        />
                      </div>
                    ))}
                  </div>
                  {!field.conflict && (
                    <p style={{ fontSize: 12, marginBottom: 0 }}>
                      {field.local === field.latest
                        ? '双方内容一致。'
                        : field.merged === field.latest
                          ? '你未修改此字段，将保留最新内容。'
                          : '此字段仅由你修改，将保留你的内容。'}
                    </p>
                  )}
                  {field.conflict && field.base !== undefined && (
                    <details style={{ marginTop: 10, fontSize: 12 }}>
                      <summary>查看开始编辑时的内容</summary>
                      <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                        {field.base || '（空）'}
                      </pre>
                    </details>
                  )}
                </fieldset>
              ))}
              {unresolved.length > 0 && (
                <p role="status">
                  还有 {unresolved.length} 个冲突字段未选择：
                  {unresolved.map((field) => fieldLabels[field]).join('、')}。
                </p>
              )}
              <button
                type="button"
                className="button primary"
                disabled={unresolved.length > 0 || loadingLatest || latestError !== undefined}
                onClick={() => {
                  replaceDraft(applyMergeReview(draftRef.current, merge, choices))
                  setMerge(undefined)
                  setChoices({})
                  setConflictLocked(false)
                  setMerged(true)
                  mutation.reset()
                }}
              >
                合并选择并继续编辑
              </button>
            </>
          )}
        </section>
      )}
      {merged && (
        <div className="draft-banner" role="status">
          <Check size={14} />
          <span>已合并双方改动，请检查内容后再次保存。</span>
        </div>
      )}
      {creationConflict && (
        <div className="notice" role="alert" style={{ display: 'block' }}>
          <p>这次创建已对应一个任务，当前内容与首次提交不同。重试不会另建任务。</p>
          {creationConflict.todoID && (
            <p>
              <button type="button" onClick={() => onSaved(creationConflict.todoID!)}>
                打开已创建任务 {creationConflict.todoID}
              </button>
              （当前填写内容会保留。）
            </p>
          )}
          <button
            type="button"
            onClick={() => {
              dirty.current = true
              replaceDraft(startSeparateCreation(draftRef.current, crypto.randomUUID()))
              setCreationConflict(undefined)
              setSeparateCreation(true)
              mutation.reset()
            }}
          >
            另存为新任务
          </button>
        </div>
      )}
      {separateCreation && (
        <div className="draft-banner" role="status">
          <span>将保留已创建的任务。检查当前内容后，点击“创建任务”另建一个任务。</span>
        </div>
      )}
      <div className="editor-footer">
        <button
          type="button"
          className="button subtle"
          onClick={onClose}
          disabled={mutation.isPending || uploading}
        >
          取消
        </button>
        <span className="shortcut-hint">⌘ ↵</span>
        <button
          className="button primary"
          disabled={disabled || !draft.title.trim() || !!creationConflict}
          type="submit"
        >
          {mutation.isPending ? <LoaderCircle className="spin" size={15} /> : <Check size={15} />}
          {original.current ? '保存修改' : '创建任务'}
        </button>
      </div>
    </form>
  )
}

export function Markdown({ text }: { text: string }) {
  return (
    <div className="markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ children, href }) => (
            <a href={href} target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          ),
          img: ({ src, alt }) =>
            typeof src === 'string' && src.startsWith('/api/v1/attachments/') ? (
              <img src={src} alt={alt ?? ''} loading="lazy" />
            ) : (
              <span className="image-placeholder">[图片{alt ? `：${alt}` : ''}]</span>
            ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

export function Notice({ error, retry }: { error: unknown; retry?: () => void }) {
  return (
    <div className="notice" role="alert">
      <span>{errorText(error)}</span>
      {retry && (
        <button onClick={retry} type="button">
          重试
        </button>
      )}
    </div>
  )
}
