import { Disclosure } from '../../components/disclosure'
import { Alert, Input as AntInput } from 'antd'
import { Button as AntButton } from 'antd'
import { Select } from '../../components/select'
import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { ApiError, call, errorText } from '../../api'
import { Markdown } from '../../components/markdown'
import { Notice } from '../../components/notice'
import type {
  KnowledgeCollection,
  KnowledgeDocument,
  MemoryEventResult,
  MemoryHit,
} from './knowledge-types'
import { statusLabels, formatDate, splitValues } from './knowledge-format'
import { useKnowledgeDraft, type RecoverableDraft } from './knowledge-draft'
type ComposerProps<T> = {
  onBusy: (busy: boolean) => void
  onClose: () => void
  onSaved: (result: T) => Promise<void>
}
function ComposerFeedback({
  restored,
  storageError,
  rebuildDraft,
  candidates,
  recover,
  discard,
}: {
  restored: boolean
  storageError: boolean
  rebuildDraft: () => void
  candidates: RecoverableDraft[]
  recover: (candidate: RecoverableDraft) => void
  discard: (candidate: RecoverableDraft) => void
}) {
  return (
    <>
      {restored && <p className="knowledge-draft-banner">已恢复之前的草稿，原草稿保留为备份。</p>}
      {!!candidates.length && (
        <Disclosure className="workspace-conflict">
          <summary>发现 {candidates.length} 份未提交草稿</summary>
          {candidates.map((candidate) => (
            <div key={candidate.key}>
              <p>
                {candidate.draft.title ||
                  candidate.draft.name ||
                  candidate.draft.content.slice(0, 80) ||
                  '未命名草稿'}
                {candidate.savedAt
                  ? ` · ${formatDate(new Date(candidate.savedAt).toISOString())}`
                  : ''}
              </p>
              <div className="workspace-actions">
                <AntButton htmlType="button" className="button" onClick={() => recover(candidate)}>
                  恢复这份草稿
                </AntButton>
                <AntButton
                  htmlType="button"
                  className="text-button"
                  onClick={() => discard(candidate)}
                >
                  删除这份草稿
                </AntButton>
              </div>
            </div>
          ))}
        </Disclosure>
      )}
      {storageError && (
        <Alert
          className="notice"
          type="error"
          title="无法读取或保存浏览器草稿，离开页面前请复制当前内容。"
          action={
            <AntButton htmlType="button" onClick={rebuildDraft}>
              用当前内容重建草稿
            </AntButton>
          }
        />
      )}
    </>
  )
}

function ComposerFooter({
  pending,
  disabled,
  onClose,
  label,
  storageError,
}: {
  pending: boolean
  disabled: boolean
  onClose: () => void
  label: string
  storageError: boolean
}) {
  return (
    <div className="knowledge-composer-footer">
      <span>{storageError ? '草稿保存不可用' : '草稿保存在此浏览器，关闭后可恢复'}</span>
      <AntButton htmlType="button" className="button subtle" disabled={pending} onClick={onClose}>
        取消
      </AntButton>
      <AntButton
        htmlType="submit"
        className="button primary"
        disabled={disabled || pending}
        loading={pending}
      >
        {!pending && <Check size={15} />}
        {pending ? '正在保存…' : label}
      </AntButton>
    </div>
  )
}

function ContentField({
  content,
  onChange,
  disabled,
}: {
  content: string
  onChange: (content: string) => void
  disabled: boolean
}) {
  const [preview, setPreview] = useState(false)
  return (
    <div className="knowledge-content-field">
      <div>
        <label htmlFor="knowledge-content">正文</label>
        <AntButton htmlType="button" className="text-button" onClick={() => setPreview(!preview)}>
          {preview ? '继续编辑' : '预览 Markdown'}
        </AntButton>
      </div>
      {preview ? (
        <div className="knowledge-editor-preview">
          <Markdown text={content || '正文预览'} />
        </div>
      ) : (
        <AntInput.TextArea
          id="knowledge-content"
          required
          value={content}
          onChange={(event) => onChange(event.target.value)}
          rows={13}
          maxLength={1000000}
          disabled={disabled}
          placeholder="写下背景、结论与可复用的经验。支持 Markdown。"
        />
      )}
    </div>
  )
}

export function DocumentComposer({
  collections,
  collection,
  original,
  editing = false,
  ...props
}: {
  collections: KnowledgeCollection[]
  collection: string
  original?: KnowledgeDocument
  editing?: boolean
} & ComposerProps<KnowledgeDocument>) {
  const draftState = useKnowledgeDraft(
    `${editing ? 'edit' : original ? 'copy' : 'document'}:${original?.metadata.id || collection || 'new'}`,
    {
      title: original
        ? editing
          ? original.metadata.title
          : `${original.metadata.title}（副本）`
        : '',
      etag: editing ? original?.etag || '' : '',
      status: original?.metadata.status || 'active',
      content: original?.content || '',
      collection: original?.collection || collection || collections[0]?.id || '',
      tags: original?.metadata.tags?.join(', ') || '',
      domains: original?.metadata.domains?.join(', ') || '',
      projects: original?.metadata.projects?.join(', ') || '',
    },
  )
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () => {
      const content = {
        title: draft.title.trim(),
        content: draft.content,
        tags: splitValues(draft.tags),
        domains: splitValues(draft.domains),
        projects: splitValues(draft.projects),
      }
      if (editing) {
        if (!original) throw new Error('文档尚未读取，请刷新后重试。')
        return call('knowledge.document.update', {
          document_id: original.metadata.id,
          etag: draft.etag,
          status: draft.status,
          ...content,
        })
      }
      return call('knowledge.document.create', { collection: draft.collection, ...content })
    },
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  const latest = useMutation({
    mutationFn: () => call('knowledge.document.get', { document_id: original?.metadata.id || '' }),
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending && (!editing || (original?.editable && draft.etag)))
          mutation.mutate()
      }}
    >
      <h2>{editing ? '编辑知识文档' : original ? '创建文档副本' : '新建知识文档'}</h2>
      <p>
        {editing
          ? '保存正文与元数据；其他位置的修改会在保存时检查。'
          : '将正文与元数据保存到所选知识集合。'}
      </p>
      <ComposerFeedback {...draftState} />
      <label>
        标题
        <AntInput
          value={draft.title}
          onChange={(event) => patch({ title: event.target.value })}
          required
          maxLength={500}
          disabled={mutation.isPending}
          autoFocus
          placeholder="这篇文档要讲什么？"
        />
      </label>
      <label>
        知识集合
        <Select
          value={draft.collection}
          required
          disabled={mutation.isPending || editing}
          onChange={(event) => patch({ collection: event.target.value })}
        >
          <option value="">选择一个集合</option>
          {collections.map((item) => (
            <option value={item.id} key={item.id}>
              {item.name || item.id}
            </option>
          ))}
        </Select>
      </label>
      {editing && (
        <label>
          状态
          <Select
            value={draft.status}
            disabled={mutation.isPending}
            onChange={(event) => patch({ status: event.target.value })}
          >
            <option value="active">有效</option>
            <option value="draft">草稿</option>
            <option value="archived">已归档</option>
          </Select>
        </label>
      )}
      <ContentField
        content={draft.content}
        onChange={(content) => patch({ content })}
        disabled={mutation.isPending}
      />
      <div className="knowledge-composer-meta">
        <label>
          标签
          <AntInput
            value={draft.tags}
            onChange={(event) => patch({ tags: event.target.value })}
            disabled={mutation.isPending}
            placeholder="多个值用逗号分隔"
          />
        </label>
        <label>
          领域
          <AntInput
            value={draft.domains}
            onChange={(event) => patch({ domains: event.target.value })}
            disabled={mutation.isPending}
            placeholder="如研发、产品"
          />
        </label>
        <label>
          关联项目
          <AntInput
            value={draft.projects}
            onChange={(event) => patch({ projects: event.target.value })}
            disabled={mutation.isPending}
            placeholder="如 atm"
          />
        </label>
      </div>
      {editing && mutation.error instanceof ApiError && mutation.error.status === 409 ? (
        <div className="workspace-conflict" role="alert">
          <p>
            这篇文档已在其他地方修改。当前草稿已保留，请读取最新文档，将需要的内容合并到上方编辑区。
          </p>
          {!latest.data && (
            <AntButton
              htmlType="button"
              className="button"
              disabled={latest.isPending}
              onClick={() => latest.mutate()}
            >
              读取最新文档
            </AntButton>
          )}
          {latest.error && <p>{errorText(latest.error)}</p>}
          {latest.data && (
            <>
              <Disclosure open>
                <summary>最新文档：{latest.data.metadata.title}</summary>
                <p>
                  状态：{statusLabels[latest.data.metadata.status] || latest.data.metadata.status} ·
                  标签：{latest.data.metadata.tags?.join('、') || '无'} · 领域：
                  {latest.data.metadata.domains?.join('、') || '无'} · 项目：
                  {latest.data.metadata.projects?.join('、') || '无'}
                </p>
                <Markdown text={latest.data.content || '无正文'} />
              </Disclosure>
              <AntButton
                htmlType="button"
                className="button"
                disabled={!latest.data.editable}
                onClick={() => {
                  patch({ etag: latest.data!.etag })
                  mutation.reset()
                  latest.reset()
                }}
              >
                已核对并合并，继续编辑
              </AntButton>
              {!latest.data.editable && <p>最新文档由外部来源维护，请创建副本后编辑。</p>}
            </>
          )}
        </div>
      ) : mutation.error ? (
        <Notice error={mutation.error} />
      ) : null}
      {editing && !original?.editable && (
        <p className="knowledge-detail-note">此文档不支持原地修改，请返回文档后创建副本。</p>
      )}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={
          (editing && (!original?.editable || !draft.etag)) ||
          !draft.title.trim() ||
          !draft.content.trim() ||
          !collections.some((item) => item.id === draft.collection)
        }
        onClose={props.onClose}
        label={editing ? '保存文档' : '创建文档'}
      />
    </form>
  )
}

export function MemoryComposer({
  scope,
  original,
  ...props
}: { scope: string; original?: MemoryHit } & ComposerProps<MemoryEventResult>) {
  const draftState = useKnowledgeDraft(`memory:${original?.id || scope || 'new'}`, {
    content: original?.content || '',
    tags: original?.tags?.join(', ') || '',
    scope: original?.scope || scope || 'global',
  })
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () =>
      call<MemoryEventResult>(original ? 'memory.supersede' : 'memory.create', {
        ...(original ? { target_id: original.id } : {}),
        scope: original?.scope || draft.scope.trim(),
        content: draft.content,
        tags: splitValues(draft.tags),
      }),
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  const stale = mutation.error instanceof ApiError && [404, 409].includes(mutation.error.status)
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending && !stale) mutation.mutate()
      }}
    >
      <h2>{original ? '修订共享记忆' : '新增共享记忆'}</h2>
      <p>
        {original
          ? '保存后，当前记忆会被这个完整的新版本替代。'
          : '适合保存长期有效的约定、偏好和事实。'}
      </p>
      <ComposerFeedback {...draftState} />
      <label>
        作用范围
        <AntInput
          value={original?.scope || draft.scope}
          onChange={(event) => patch({ scope: event.target.value })}
          required
          disabled={mutation.isPending || !!original}
          placeholder="global、project:atm 或 session:…"
          maxLength={500}
        />
      </label>
      <p className="knowledge-field-help">
        global 用于所有工作；project:项目名 用于指定项目；session:会话ID 用于指定会话。
      </p>
      <ContentField
        content={draft.content}
        onChange={(content) => patch({ content })}
        disabled={mutation.isPending}
      />
      <label>
        标签
        <AntInput
          value={draft.tags}
          onChange={(event) => patch({ tags: event.target.value })}
          disabled={mutation.isPending}
          placeholder="多个值用逗号分隔"
        />
      </label>
      {mutation.error &&
        (stale ? (
          <Alert
            className="notice"
            type="error"
            title="这条记忆已被其他操作替代或移除。草稿已保留，请返回列表查看最新记忆后重新修订。"
          />
        ) : (
          <Alert className="notice" type="error" title={errorText(mutation.error)} />
        ))}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={!draft.content.trim() || !draft.scope.trim() || stale}
        onClose={props.onClose}
        label={original ? '保存新版本' : '保存记忆'}
      />
    </form>
  )
}

export function CollectionComposer(props: ComposerProps<KnowledgeCollection>) {
  const draftState = useKnowledgeDraft('collection:new', {})
  const { draft, patch } = draftState
  const mutation = useMutation({
    mutationFn: () =>
      call('knowledge.collection.create', {
        id: draft.id.trim(),
        name: draft.name.trim(),
        description: draft.description.trim(),
      }),
    retry: false,
    onSuccess: async (result) => {
      draftState.clear()
      await props.onSaved(result)
    },
  })
  useEffect(() => {
    props.onBusy(mutation.isPending)
    return () => props.onBusy(false)
  }, [mutation.isPending, props.onBusy])
  return (
    <form
      className="knowledge-composer"
      onSubmit={(event) => {
        event.preventDefault()
        if (!mutation.isPending) mutation.mutate()
      }}
    >
      <h2>新建知识集合</h2>
      <p>按主题或用途组织文档。</p>
      <ComposerFeedback {...draftState} />
      <label>
        集合标识
        <AntInput
          value={draft.id}
          onChange={(event) => patch({ id: event.target.value })}
          required
          maxLength={100}
          disabled={mutation.isPending}
          autoFocus
          placeholder="如 engineering"
        />
      </label>
      <p className="knowledge-field-help">
        使用简短名称，不能包含路径分隔符，也不能以点或下划线开头。
      </p>
      <label>
        显示名称
        <AntInput
          value={draft.name}
          onChange={(event) => patch({ name: event.target.value })}
          disabled={mutation.isPending}
          maxLength={300}
          placeholder="如工程实践"
        />
      </label>
      <label>
        集合说明
        <AntInput.TextArea
          value={draft.description}
          onChange={(event) => patch({ description: event.target.value })}
          disabled={mutation.isPending}
          maxLength={8000}
          rows={5}
          placeholder="适合存放什么内容，何时查阅？"
        />
      </label>
      {mutation.error && (
        <Notice
          error={
            mutation.error instanceof ApiError && mutation.error.status === 409
              ? new Error('此集合标识已经存在，请使用其他标识。')
              : mutation.error
          }
        />
      )}
      <ComposerFooter
        storageError={draftState.storageError}
        pending={mutation.isPending}
        disabled={!draft.id.trim()}
        onClose={props.onClose}
        label="创建集合"
      />
    </form>
  )
}
