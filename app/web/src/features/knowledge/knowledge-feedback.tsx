import { Empty as AntEmpty } from 'antd'
import { BookOpen, Sparkles } from 'lucide-react'
import { Notice } from '../../components/notice'
import { QueryLoading } from '../../components/query-loading'
export function Loading({ label, retry }: { label: string; retry?: () => void }) {
  return <QueryLoading className="knowledge-loading" text={label} onRetry={retry} />
}
export function Empty({
  icon,
  title,
  detail,
}: {
  icon: 'document' | 'memory'
  title: string
  detail: string
}) {
  const Icon = icon === 'memory' ? Sparkles : BookOpen
  return (
    <AntEmpty
      className="knowledge-empty"
      image={<Icon size={26} />}
      description={
        <>
          <h3>{title}</h3>
          <p>{detail}</p>
        </>
      }
    />
  )
}
export function DetailRequest({
  pending,
  error,
  retry,
}: {
  pending: boolean
  error: unknown
  retry: () => void
}) {
  return (
    <>
      {pending && <Loading label="读取完整内容…" retry={retry} />}
      {error ? <Notice error={error} retry={retry} /> : null}
    </>
  )
}
