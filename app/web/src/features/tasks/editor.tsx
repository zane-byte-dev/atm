import { Disclosure } from '../../components/disclosure'
import { Input as AntdInput } from 'antd'
import { Button as AntButton } from 'antd'
import { useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, AutoComplete, Button, Input, Radio, Select, Spin } from 'antd'
import type { InputRef } from 'antd'
import { Check, Circle, Folder, Paperclip, X } from 'lucide-react'
import { ApiError, call, uploadTaskImage } from '../../api'
import { Notice } from '../../components/notice'
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
import type { Draft, TodoDetail, TodoList, OperationWarning } from '../../types'
import {
  draftImageURLs,
  draftImagePrefix,
  readDraftImage,
  removeDraftImages,
  removeDraftImageMarkup,
  storeDraftImage,
} from './task-draft-images'
import { TaskRefine } from './task-refine'
import { imageUploadError, taskImageLimit } from './image-uploads'

export type EditorProps = {
  initial?: TodoDetail
  onClose: () => void
  onSaved: (id: string, warnings?: OperationWarning[]) => void
  onBusyChange?: (busy: boolean) => void
  onDirtyChange?: (dirty: boolean) => void
  defaultProject?: string
  canRefine?: boolean
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
  onDirtyChange,
  defaultProject = '',
  canRefine = false,
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
  const [uploading, setUploading] = useState(false)
  const [imageError, setImageError] = useState('')
  const uploadLock = useRef(false)
  const imageTodoRef = useRef(original.current?.todo)
  const [composing, setComposing] = useState(false)
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
  const titleInput = useRef<InputRef>(null)
  const imageInput = useRef<HTMLInputElement>(null)
  const composerId = useId()
  const query = useQueryClient()
  const projects = useQuery({
    queryKey: ['todos', 'projects'],
    queryFn: () => call('todo.list', { limit: 1, offset: 0 }) as Promise<TodoList>,
    staleTime: 60_000,
  })

  const replaceDraft = (next: Draft) => {
    draftRef.current = next
    setDraft(next)
  }
  useEffect(() => {
    const base = original.current?.todo
    onDirtyChange?.(
      !!base &&
        (draft.title !== base.title ||
          draft.description !== (base.description || '') ||
          draft.project !== (base.project || '') ||
          draft.priority !== base.priority),
    )
  }, [draft.title, draft.description, draft.project, draft.priority, onDirtyChange])
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
    if (ready) {
      titleInput.current?.focus()
    }
  }, [ready, creating])

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
      const result = await call('todo.show', { todo_id: current.todo.id }, controller.signal)
      if (!alive.current || controller.signal.aborted) return
      imageTodoRef.current = result.todo
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
      let result
      if (current)
        result = await call('todo.update', {
          ...payload,
          todo_id: current.todo.id,
          expected_etag: submitted.etag,
        })
      else {
        const urls = draftImageURLs(submitted.description)
        // Validate all local files before creating a task.
        const files = await Promise.all(urls.map(readDraftImage))
        let checkpoint = submitted.creation
        if (!checkpoint) {
          result = await call(
            'todo.create',
            { ...payload, description: removeDraftImageMarkup(payload.description) },
            undefined,
            submitted.operationID,
          )
          checkpoint = { id: result.todo.id, etag: result.etag, uploaded: {} }
          if (urls.length) {
            replaceDraft({ ...draftRef.current, creation: checkpoint })
            await inputStore.save(draftRef.current)
          }
        }
        if (urls.length || submitted.creation) {
          for (let index = 0; index < urls.length; index++) {
            const url = urls[index]
            if (checkpoint.uploaded[url]) continue
            const next: TodoDetail = await uploadTaskImage<TodoDetail>(
              checkpoint.id,
              files[index],
              checkpoint.etag,
            )
            const image = next.todo.images?.at(-1)
            if (!image?.url) throw new Error('图片已上传，但未返回图片地址，请打开任务核对。')
            checkpoint = {
              ...checkpoint,
              etag: next.etag,
              uploaded: { ...checkpoint.uploaded, [url]: image.url },
            }
            replaceDraft({ ...draftRef.current, creation: checkpoint })
            await inputStore.save(draftRef.current)
          }
          let description = payload.description
          for (const [local, remote] of Object.entries(checkpoint.uploaded))
            description = description.split(local).join(remote)
          result = await call('todo.update', {
            ...payload,
            description,
            todo_id: checkpoint.id,
            expected_etag: checkpoint.etag,
          })
          replaceDraft({ ...draftRef.current, creation: { ...checkpoint, etag: result.etag } })
          await inputStore.save(draftRef.current)
        }
      }
      if (!result) throw new Error('任务尚未保存，请重试。')
      completed.current = true
      ++writeSequence.current
      let draftRemoved = !creating
      // A route change can mount a new editor for the same task while this
      // request is in flight. Its newer draft must survive the old response.
      if (creating) {
        try {
          draftRemoved = await inputStore.clearIfUnchanged(draftRef.current)
        } catch {
          /* Keep the draft and report below. */
        }
      }
      if (creating && draftRemoved)
        void removeDraftImages(draftImageURLs(submitted.description)).catch(() => {})
      return { ...result, draftRemoved }
    },
    onError: (error) => {
      if (!alive.current) return
      if (!(error instanceof ApiError) || error.status !== 409) return
      if (original.current) void loadLatest()
      else if (!draftRef.current.creation)
        setCreationConflict({ todoID: existingCreationID(error.details) })
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
    if (
      !disabled &&
      !composing &&
      draftRef.current.title.trim() &&
      draftRef.current.title.length <= 500 &&
      !creationConflict &&
      !completed.current
    )
      mutation.mutate({ ...draftRef.current })
  }
  const patch = (next: Partial<Draft>) => {
    dirty.current = true
    replaceDraft({ ...draftRef.current, ...next })
    mutation.reset()
    setMerged(false)
  }
  const uploadImages = async (files: File[]): Promise<{ name: string; url: string }[]> => {
    if (disabled || uploadLock.current) throw new Error('请等待当前操作完成。')
    const count = creating
      ? draftImageURLs(draftRef.current.description).length
      : imageTodoRef.current?.images?.length || 0
    if (count + files.length > taskImageLimit) throw new Error('每个任务最多添加 10 张图片。')
    for (const file of files) {
      const error = imageUploadError(file)
      if (error) throw new Error(error)
    }
    uploadLock.current = true
    setUploading(true)
    const images: { name: string; url: string }[] = []
    try {
      for (const file of files) {
        if (creating) images.push(await storeDraftImage(file))
        else {
          const result = await uploadTaskImage<TodoDetail>(
            original.current!.todo.id,
            file,
            draftRef.current.etag,
          )
          imageTodoRef.current = result.todo
          replaceDraft({ ...draftRef.current, etag: result.etag })
          const image = result.todo.images?.at(-1)
          if (image?.url) images.push({ name: image.name, url: image.url })
        }
      }
      return images
    } catch (error) {
      // Keep successful uploads visible even when a later file fails.
      if (images.length)
        patch({
          description:
            draftRef.current.description +
            '\n' +
            images.map((image) => `![图片](<${image.url}>)`).join('\n'),
        })
      if (error instanceof ApiError && error.status === 409 && original.current) void loadLatest()
      throw error
    } finally {
      uploadLock.current = false
      setUploading(false)
    }
  }
  const insertImages = async (files: File[]) => {
    setImageError('')
    try {
      const images = await uploadImages(files)
      if (!images.length) return
      const markup = images
        .map((image) => `![${image.name.replace(/[\\[\]<>\r\n]/g, ' ')}](<${image.url}>)`)
        .join('\n')
      const description = draftRef.current.description
      patch({ description: description ? `${description}\n${markup}` : markup })
    } catch (error) {
      setImageError(error instanceof Error ? error.message : '图片无法插入')
    }
  }
  const unresolved = merge ? unresolvedFields(merge, choices) : []

  return (
    <form
      className={`task-editor ${original.current ? '' : 'editor-create'}`}
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
        <Alert
          className="notice"
          type="error"
          title="无法记住新建任务的填写内容。刷新或关闭页面可能丢失输入，请先创建任务或复制内容。"
          description={
            <AntButton
              htmlType="button"
              disabled={mutation.isPending}
              onClick={() => {
                persistenceBlocked.current = false
                dirty.current = true
                void persist(draftRef.current)
              }}
            >
              记住当前填写内容
            </AntButton>
          }
        />
      )}
      {savedWithDraft && (
        <Alert
          className="notice"
          type="info"
          role="status"
          title={`任务 ${savedWithDraft} 已创建。浏览器中仍有未清除的填写内容，下次新建时请核对。`}
          description={
            <AntButton
              htmlType="button"
              onClick={() => onSaved(savedWithDraft, mutation.data?.warnings)}
            >
              打开已保存任务
            </AntButton>
          }
        />
      )}
      <div
        className="task-composer"
        onPaste={(event) => {
          const files = Array.from(event.clipboardData.files)
          if (files.length) {
            event.preventDefault()
            void insertImages(files)
          }
        }}
        onDragOver={(event) => {
          if (event.dataTransfer.types.includes('Files')) event.preventDefault()
        }}
        onDrop={(event) => {
          const files = Array.from(event.dataTransfer.files)
          if (files.length) {
            event.preventDefault()
            void insertImages(files)
          }
        }}
      >
        <div className="task-create-writing">
          <label className="sr-only" htmlFor={composerId}>
            任务标题
          </label>
          <Input
            ref={titleInput}
            id={composerId}
            className="task-create-title"
            value={draft.title}
            disabled={disabled}
            placeholder="任务标题"
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
            onChange={(event) => patch({ title: event.target.value })}
          />
          <label className="sr-only" htmlFor={`${composerId}-description`}>
            任务描述
          </label>
          <Input.TextArea
            id={`${composerId}-description`}
            className="task-create-description"
            value={draft.description}
            disabled={disabled}
            autoSize={{ minRows: 4, maxRows: 16 }}
            onCompositionStart={() => setComposing(true)}
            onCompositionEnd={() => setComposing(false)}
            placeholder="添加描述…"
            onChange={(event) => patch({ description: event.target.value })}
          />
        </div>
        {draft.title.length > 500 && (
          <p className="task-composer-error" role="alert">
            标题最多 500 字。
          </p>
        )}
        <TaskComposerImages
          description={draft.description}
          onRemove={(markup) => patch({ description: draft.description.replace(markup, '') })}
        />
        {imageError && (
          <p className="task-composer-error" role="alert">
            {imageError}
          </p>
        )}
        <input
          ref={imageInput}
          type="file"
          hidden
          accept="image/png,image/jpeg,image/gif"
          multiple
          onChange={(event) => {
            const files = Array.from(event.target.files || [])
            event.target.value = ''
            if (files.length) void insertImages(files)
          }}
        />
        <div className="task-create-properties" aria-label="任务属性">
          {creating && (
            <span className="task-create-property task-create-status">
              <Circle size={16} /> 待开始
            </span>
          )}
          <Select
            className="task-create-property task-create-priority"
            aria-label="优先级"
            value={draft.priority}
            disabled={disabled}
            onChange={(priority) => patch({ priority })}
            options={[
              { value: 'P2', label: 'P2 · 正常' },
              { value: 'P1', label: 'P1 · 优先' },
              { value: 'P0', label: 'P0 · 紧急' },
            ]}
          />
          <div className="task-create-property task-create-project">
            <Folder size={16} aria-hidden="true" />
            <AutoComplete
              aria-label="项目，选择已有项目或输入新项目"
              value={draft.project}
              disabled={disabled}
              onChange={(project) => patch({ project })}
              options={projects.data?.projects?.map((project) => ({ value: project })) ?? []}
              placeholder="无项目"
              variant="borderless"
              allowClear
            />
          </div>
        </div>
      </div>
      {canRefine && initial && (
        <Disclosure className="task-editor-refine">
          <summary>AI 整理（可选）</summary>
          <TaskRefine data={initial} />
        </Disclosure>
      )}
      {draft.creation && (
        <p role="status">任务 {draft.creation.id} 已创建，重试会继续保存图片和正文。</p>
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
              <Spin size="small" /> 正在读取最新版本…
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
            <AntButton
              htmlType="button"
              className="button"
              onClick={() => {
                void loadLatest()
              }}
            >
              读取最新版本
            </AntButton>
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
                          <Radio
                            style={{
                              display: 'flex',
                              gap: 7,
                              alignItems: 'center',
                              marginBottom: 8,
                            }}
                            name={`merge-${field.field}`}
                            checked={choices[field.field] === side}
                            disabled={loadingLatest}
                            onChange={() =>
                              setChoices((current) => ({ ...current, [field.field]: side }))
                            }
                          >
                            {side === 'local' ? '保留我的内容' : '采用最新内容'}
                          </Radio>
                        ) : (
                          <span className="field-label">
                            {side === 'local' ? '我的内容' : '最新内容'}
                          </span>
                        )}
                        <AntdInput.TextArea
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
                    <Disclosure style={{ marginTop: 10, fontSize: 12 }}>
                      <summary>查看开始编辑时的内容</summary>
                      <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
                        {field.base || '（空）'}
                      </pre>
                    </Disclosure>
                  )}
                </fieldset>
              ))}
              {unresolved.length > 0 && (
                <p role="status">
                  还有 {unresolved.length} 个冲突字段未选择：
                  {unresolved.map((field) => fieldLabels[field]).join('、')}。
                </p>
              )}
              <AntButton
                htmlType="button"
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
              </AntButton>
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
        <Alert
          className="notice"
          type="error"
          title="这次创建已对应一个任务，当前内容与首次提交不同。重试不会另建任务。"
          description={
            <>
              {creationConflict.todoID && (
                <p>
                  <AntButton htmlType="button" onClick={() => onSaved(creationConflict.todoID!)}>
                    打开已创建任务 {creationConflict.todoID}
                  </AntButton>
                  （当前填写内容会保留。）
                </p>
              )}
              <AntButton
                htmlType="button"
                onClick={() => {
                  dirty.current = true
                  replaceDraft(startSeparateCreation(draftRef.current, crypto.randomUUID()))
                  setCreationConflict(undefined)
                  setSeparateCreation(true)
                  mutation.reset()
                }}
              >
                另存为新任务
              </AntButton>
            </>
          }
        />
      )}
      {separateCreation && (
        <div className="draft-banner" role="status">
          <span>将保留已创建的任务。检查当前内容后，点击“创建任务”另建一个任务。</span>
        </div>
      )}
      <div className="editor-footer">
        <Button
          htmlType="button"
          variant="text"
          color="default"
          className="task-create-attachment"
          aria-label="添加图片"
          title="添加图片"
          disabled={disabled}
          onClick={() => imageInput.current?.click()}
          icon={<Paperclip size={18} />}
        />
        <Button
          htmlType="button"
          variant="text"
          color="default"
          onClick={onClose}
          disabled={mutation.isPending || uploading}
        >
          取消
        </Button>
        <span className="shortcut-hint">⌘ ↵</span>
        <Button
          type="primary"
          disabled={
            disabled || !draft.title.trim() || draft.title.length > 500 || !!creationConflict
          }
          htmlType="submit"
          loading={mutation.isPending}
          icon={<Check size={15} />}
        >
          {original.current ? '保存修改' : draft.creation ? '继续保存' : '创建任务'}
        </Button>
      </div>
    </form>
  )
}

function TaskComposerImages({
  description,
  onRemove,
}: {
  description: string
  onRemove: (markup: string) => void
}) {
  const images = [...description.matchAll(/!\[([^\]]*)\]\(<?([^)>]+)>?\)/g)].map((match) => ({
    markup: match[0],
    name: match[1] || '图片',
    url: match[2],
  }))
  if (!images.length) return null
  return (
    <div className="task-composer-images" aria-label="已插入的图片">
      {images.map((image, index) => (
        <TaskComposerImage key={`${image.url}-${index}`} {...image} onRemove={onRemove} />
      ))}
    </div>
  )
}

function TaskComposerImage({
  markup,
  name,
  url,
  onRemove,
}: {
  markup: string
  name: string
  url: string
  onRemove: (markup: string) => void
}) {
  const remoteSource = /^(https?:\/\/|\/api\/v1\/attachments\/)/i.test(url) ? url : ''
  const [source, setSource] = useState(url.startsWith(draftImagePrefix) ? '' : remoteSource)
  useEffect(() => {
    if (!url.startsWith(draftImagePrefix)) {
      setSource(remoteSource)
      return
    }
    let active = true
    let objectURL = ''
    void readDraftImage(url)
      .then((file) => {
        if (!active) return
        objectURL = URL.createObjectURL(file)
        setSource(objectURL)
      })
      .catch(() => {
        if (active) setSource('')
      })
    return () => {
      active = false
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [url, remoteSource])
  return (
    <div className="task-composer-image">
      {source ? <img src={source} alt={name} /> : <span>{name}</span>}
      <AntButton htmlType="button" aria-label={`移除图片 ${name}`} onClick={() => onRemove(markup)}>
        <X size={13} />
      </AntButton>
    </div>
  )
}
