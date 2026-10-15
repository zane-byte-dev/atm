import { Alert, Input as AntInput } from 'antd'
import { Button as AntButton } from 'antd'
import { Select } from '../../components/select'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { call, errorText } from '../../api'

type Candidate = { kind: string; external_id: string; name: string; detail?: string }

export function SourceSearch({
  connector,
  disabled,
  onSelect,
}: {
  connector: string
  disabled: boolean
  onSelect: (candidate: Candidate) => void
}) {
  const [keyword, setKeyword] = useState('')
  const [kind, setKind] = useState('all')
  const [submitted, setSubmitted] = useState<{ keyword: string; kind: string }>()
  const [selected, setSelected] = useState('')
  const results = useQuery({
    queryKey: ['collect.source.search', connector, submitted],
    queryFn: ({ signal }) =>
      call<{ candidates: Candidate[] }>(
        'collect.source.search',
        {
          connector,
          ...submitted,
          limit: 30,
        },
        signal,
      ),
    enabled: !!submitted,
    retry: false,
    refetchOnWindowFocus: false,
  })
  const search = () => {
    if (!keyword.trim() || disabled) return
    if (submitted?.keyword === keyword.trim() && submitted.kind === kind) void results.refetch()
    else setSubmitted({ keyword: keyword.trim(), kind })
  }
  return (
    <section className="source-search" aria-label="按名称搜索来源">
      <label>
        搜索来源名称
        <AntInput
          value={keyword}
          disabled={disabled}
          placeholder="输入群聊、联系人或机器人名称"
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
              event.preventDefault()
              search()
            }
          }}
        />
      </label>
      <div className="workspace-actions">
        <Select
          aria-label="搜索来源类型"
          value={kind}
          disabled={disabled}
          onChange={(event) => setKind(event.target.value)}
        >
          <option value="all">全部类型</option>
          <option value="group">群聊</option>
          <option value="user">联系人</option>
          <option value="bot">机器人</option>
        </Select>
        <AntButton
          htmlType="button"
          className="button"
          disabled={disabled || !keyword.trim() || results.isFetching}
          loading={results.isFetching}
          onClick={search}
        >
          {!results.isFetching && <Search size={14} />}
          搜索
        </AntButton>
      </div>
      {results.error && (
        <Alert
          className="as-inline-error"
          type="error"
          title={`${errorText(results.error)}。也可以在下方手动填写来源 ID。`}
        />
      )}
      {results.data && (
        <div className="source-search-results" aria-live="polite">
          {results.data.candidates.length === 0 && (
            <p className="workspace-form-hint">没有找到匹配来源，请更换名称或手动填写 ID。</p>
          )}
          {results.data.candidates.map((candidate) => {
            const id = `${candidate.kind}/${candidate.external_id}`
            return (
              <AntButton
                key={id}
                htmlType="button"
                className="source-search-result"
                disabled={disabled || results.isFetching}
                aria-pressed={selected === id}
                onClick={() => {
                  setSelected(id)
                  onSelect(candidate)
                }}
              >
                <span>
                  <strong>{candidate.name || candidate.external_id}</strong>
                  <small>
                    {candidate.detail || candidate.kind} · {candidate.external_id}
                  </small>
                </span>
                <span>{selected === id ? '已选择' : '选择'}</span>
              </AntButton>
            )
          })}
        </div>
      )}
      <p className="workspace-form-hint">
        选择搜索结果后自动填写来源信息，点击“保存来源”完成添加。
      </p>
    </section>
  )
}
