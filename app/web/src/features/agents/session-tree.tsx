import { Button as AntButton } from 'antd'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { call } from '../../api'
import { Notice } from '../../components/notice'
import type { SessionList, SessionSummary } from './activity-types'

export function SessionChildren({
  parent,
  agent,
  project,
  days,
  selected,
  onSelect,
}: {
  parent: SessionSummary
  agent: string
  project: string
  days: number
  selected: string
  onSelect: (id: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [offset, setOffset] = useState(0)
  const query = useQuery({
    queryKey: ['activity-sessions', 'children', parent.id, agent, project, days, offset],
    queryFn: ({ signal }) =>
      call<SessionList>(
        'session.list',
        {
          parent_session_id: parent.id,
          agent,
          project,
          days,
          limit: 20,
          offset,
        },
        signal,
      ),
    enabled: expanded,
  })
  if (!parent.child_count) return null
  return (
    <div className="session-children">
      <AntButton
        className="session-children-toggle"
        aria-expanded={expanded}
        onClick={() => setExpanded(!expanded)}
      >
        {expanded ? '⌄' : '›'} {parent.child_count} 个子会话
      </AntButton>
      {expanded && (
        <div className="session-children-list">
          {query.isPending && <p role="status">正在读取子会话…</p>}
          {query.error && <Notice error={query.error} retry={() => void query.refetch()} />}
          {query.data?.sessions.map((row) => (
            <div key={row.id}>
              <AntButton
                className={`session-child ${selected === row.id ? 'selected' : ''}`}
                aria-pressed={selected === row.id}
                onClick={() => onSelect(row.id)}
              >
                <strong>
                  {row.summary || row.agent_nickname || row.first_q || `子会话 ${row.short_id}`}
                </strong>
                <span>{row.latest_progress || row.final_result || `${row.q_count} 轮对话`}</span>
              </AntButton>
              <SessionChildren
                parent={row}
                agent={agent}
                project={project}
                days={days}
                selected={selected}
                onSelect={onSelect}
              />
            </div>
          ))}
          {query.data && query.data.total > 20 && (
            <div className="session-child-pages">
              <AntButton
                disabled={!offset || query.isFetching}
                onClick={() => setOffset(Math.max(0, offset - 20))}
              >
                上一页子会话
              </AntButton>
              <span>
                {offset + 1}–{Math.min(offset + 20, query.data.total)} / {query.data.total}
              </span>
              <AntButton
                disabled={offset + 20 >= query.data.total || query.isFetching}
                onClick={() => setOffset(offset + 20)}
              >
                下一页子会话
              </AntButton>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
