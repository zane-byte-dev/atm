import type { ReactNode } from 'react'
import { Empty as AntEmpty } from 'antd'
import { MessageSquare } from 'lucide-react'
import './activity-shared.css'

export function EmptyActivity({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <AntEmpty
      className="activity-empty"
      image={<MessageSquare size={26} />}
      description={
        <>
          <h3>{title}</h3>
          {children && <p>{children}</p>}
        </>
      }
    />
  )
}
