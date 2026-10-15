import { Button as AntButton } from 'antd'
import {
  ArrowDownUp,
  CalendarDays,
  Columns3,
  Flag,
  Folder,
  Hash,
  LayoutList,
  Tag,
  UserRound,
} from 'lucide-react'
import { Select } from '../../components/select'
import { fields, visibleFields } from './task-view-model'
import { readTaskLayout } from './task-navigation'
const icons = {
  id: Hash,
  priority: Flag,
  project: Folder,
  creator: UserRound,
  created: CalendarDays,
  tags: Tag,
}
export function TaskDisplayMenu({
  params,
  onChange,
}: {
  params: URLSearchParams
  onChange: (params: URLSearchParams) => void
}) {
  const patch = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    next.set(key, value)
    onChange(next)
  }
  const layout = readTaskLayout(params)
  const properties = visibleFields(params)
  return (
    <div className="task-display-menu">
      <label>
        <span>
          <ArrowDownUp size={15} />
          排序
        </span>
        <Select
          aria-label="任务排序"
          value={params.get('sort') || 'auto'}
          onChange={(event) => patch('sort', event.target.value)}
        >
          <option value="auto">默认</option>
          <option value="updated">更新时间</option>
          <option value="priority">优先级</option>
          <option value="created">创建日期</option>
          <option value="title">标题</option>
        </Select>
      </label>
      <label>
        <span>顺序</span>
        <Select
          aria-label="排序方向"
          value={params.get('direction') || 'asc'}
          onChange={(event) => patch('direction', event.target.value)}
        >
          <option value="asc">升序</option>
          <option value="desc">降序</option>
        </Select>
      </label>
      {(layout === 'list' || layout === 'table') && (
        <label>
          <span>
            <Columns3 size={15} />
            分组
          </span>
          <Select
            aria-label="任务分组"
            value={
              params.get(layout === 'table' ? 'tableGroup' : 'group') ||
              (layout === 'table' ? 'none' : 'status')
            }
            onChange={(event) =>
              patch(layout === 'table' ? 'tableGroup' : 'group', event.target.value)
            }
          >
            <option value="none">不分组</option>
            <option value="status">状态</option>
            <option value="project">项目</option>
          </Select>
        </label>
      )}
      <label>
        <span>
          <LayoutList size={15} />
          间距
        </span>
        <Select
          aria-label="显示间距"
          value={params.get('density') || 'compact'}
          onChange={(event) => patch('density', event.target.value)}
        >
          <option value="compact">紧凑</option>
          <option value="comfortable">宽松</option>
        </Select>
      </label>
      <fieldset>
        <legend>{layout === 'table' ? '显示列' : '卡片属性'}</legend>
        <div className="task-display-properties">
          {fields.map(([value, label]) => {
            const Icon = icons[value]
            return (
              <AntButton
                key={value}
                htmlType="button"
                aria-pressed={properties.includes(value)}
                onClick={() =>
                  patch(
                    'fields',
                    (properties.includes(value)
                      ? properties.filter((field) => field !== value)
                      : [...properties, value]
                    ).join(','),
                  )
                }
              >
                <Icon size={14} />
                {label}
              </AntButton>
            )
          })}
        </div>
      </fieldset>
    </div>
  )
}
