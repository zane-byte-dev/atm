import { Disclosure } from './components/disclosure'
import type { ReactNode } from 'react'
import { Badge, Button as AntButton, Dropdown, Tooltip } from 'antd'
import { NavLink, useLocation } from 'react-router'
import {
  Bell,
  BookOpen,
  ChartNoAxesCombined,
  CheckCheck,
  Cpu,
  Inbox,
  LockKeyhole,
  MoreHorizontal,
  Settings,
  Search,
  SquarePen,
} from 'lucide-react'
import { useWorkspaceMessages } from './features/notifications/workspace-notifications'
import { GlobalSearch } from './global-search'
import type { Bootstrap } from './types'

export const workspaces = [
  { path: '/tasks', label: 'ToDo', icon: CheckCheck },
  { path: '/knowledge', label: '知识库', icon: BookOpen },
  { path: '/collection', label: '收件箱', icon: Inbox },
  { path: '/agents', label: 'Agent 会话', icon: Cpu },
  { path: '/usage', label: '统计', icon: ChartNoAxesCombined },
  { path: '/messages', label: '消息', icon: Bell },
  { path: '/settings', label: '设置', icon: Settings },
] as const

export function AppShell({
  boot,
  children,
  className = '',
}: {
  boot: Bootstrap
  children: ReactNode
  className?: string
}) {
  const location = useLocation()
  const unread = useWorkspaceMessages().filter((item) => !item.readAt).length
  const selectedWorkspace =
    workspaces.find(
      ({ path }) => location.pathname === path || location.pathname.startsWith(`${path}/`),
    )?.path ?? '/tasks'
  return (
    <div className={`workspace ${className}`}>
      <nav className="workspace-rail" aria-label="主导航">
        <Tooltip title="首页" placement="right" arrow={false}>
          <NavLink className="rail-brand" to="/tasks" aria-label="ATM 首页" data-tooltip="首页">
            <img src="/mark.svg" alt="" />
          </NavLink>
        </Tooltip>
        <div className="rail-destinations">
          {workspaces
            .filter(({ path }) => path === '/tasks' || path === '/knowledge')
            .map(({ path, label, icon: Icon }) => (
              <Tooltip key={path} title={label} placement="right" arrow={false}>
                <NavLink
                  to={path}
                  className={`rail-item ${selectedWorkspace === path ? 'selected' : ''}`}
                  aria-label={label}
                  data-tooltip={label}
                >
                  <Icon size={20} strokeWidth={1.75} />
                </NavLink>
              </Tooltip>
            ))}
          <Dropdown
            trigger={['click']}
            placement="bottomLeft"
            menu={{
              selectedKeys: [selectedWorkspace],
              items: workspaces
                .filter(({ path }) => !['/tasks', '/knowledge', '/settings'].includes(path))
                .map(({ path, label, icon: Icon }) => ({
                  key: path,
                  icon: <Icon size={16} />,
                  label: (
                    <NavLink to={path}>
                      {label}
                      {path === '/messages' && unread > 0 ? ` · ${unread} 未读` : ''}
                    </NavLink>
                  ),
                })),
            }}
          >
            <AntButton
              className={`rail-item ${!['/tasks', '/knowledge', '/settings'].includes(selectedWorkspace) ? 'selected' : ''}`}
              aria-label="更多"
              title="更多"
            >
              <Badge dot={unread > 0}>
                <MoreHorizontal size={20} />
              </Badge>
            </AntButton>
          </Dropdown>
        </div>
        <div className="rail-bottom">
          {boot.capabilities?.todo_write && (
            <Tooltip title="新建任务" placement="right" arrow={false}>
              <NavLink
                className="rail-item"
                to="/tasks?new=1"
                aria-label="新建任务"
                data-tooltip="新建任务"
              >
                <SquarePen size={20} strokeWidth={1.75} />
              </NavLink>
            </Tooltip>
          )}
          <Tooltip title="搜索" placement="right" arrow={false}>
            <AntButton
              htmlType="button"
              className="rail-item"
              aria-label="搜索"
              data-tooltip="搜索"
              onClick={() => window.dispatchEvent(new Event('atm:open-search'))}
            >
              <Search size={20} strokeWidth={1.75} />
            </AntButton>
          </Tooltip>
          <Tooltip title="设置" placement="right" arrow={false}>
            <NavLink
              to="/settings"
              className={`rail-item ${selectedWorkspace === '/settings' ? 'selected' : ''}`}
              aria-label="设置"
              data-tooltip="设置"
            >
              <Settings size={20} strokeWidth={1.75} />
            </NavLink>
          </Tooltip>
        </div>
        <GlobalSearch writable={boot.capabilities?.todo_write === true} hideTrigger />
      </nav>
      <main className="main">
        {boot.capabilities?.data_upgrade_required === true && (
          <div className="upgrade-notice" role="status">
            <Disclosure>
              <summary>
                <LockKeyhole size={13} />
                <strong>只读预览</strong>
                <span className="upgrade-copy">升级数据后可编辑</span>
                <span className="upgrade-help">如何启用编辑</span>
              </summary>
              <div className="upgrade-instructions">
                <p>
                  先让 CLI 和 macOS App 使用同一份新版 ATM，停止 Web 服务与旧
                  App，再执行数据升级。升级命令会先创建备份。
                </p>
                <pre>
                  atm serve stop{'\n'}atm serve migrate{'\n'}atm serve --open
                </pre>
                <p>使用自定义数据目录时，沿用启动时的 --data-dir。</p>
              </div>
            </Disclosure>
          </div>
        )}
        {children}
      </main>
    </div>
  )
}
