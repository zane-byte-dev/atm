import { Collapse } from 'antd'
import { Children, isValidElement, type HTMLAttributes, type ReactNode } from 'react'

// One shared Ant Design disclosure for secondary information and conflict details.
export function Disclosure({
  children,
  open,
  className = '',
  ...props
}: HTMLAttributes<HTMLDivElement> & { open?: boolean }) {
  const parts = Children.toArray(children)
  const heading = parts.find((child) => isValidElement(child) && child.type === 'summary')
  const label = isValidElement<{ children?: ReactNode }>(heading)
    ? heading.props.children
    : '详细信息'
  return (
    <div {...props} className={`workspace-disclosure ${className}`}>
      <Collapse
        ghost
        size="small"
        defaultActiveKey={open ? ['content'] : []}
        items={[{ key: 'content', label, children: parts.filter((child) => child !== heading) }]}
      />
    </div>
  )
}
