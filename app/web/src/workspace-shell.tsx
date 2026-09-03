import type { ReactNode } from 'react'
import { NavLink } from 'react-router'
import {
  BookOpen,
  ChartNoAxesCombined,
  CheckCheck,
  Cpu,
  Inbox,
  LockKeyhole,
  Settings,
  ShieldCheck,
  Sparkles,
} from 'lucide-react'
import type { Bootstrap } from './types'

export const workspaces = [
  { path: '/tasks', label: '任务', icon: CheckCheck },
  { path: '/collection', label: '收集', icon: Inbox },
  { path: '/agents', label: 'Agent', icon: Cpu },
  { path: '/knowledge', label: '知识', icon: BookOpen },
  { path: '/usage', label: '统计', icon: ChartNoAxesCombined },
  { path: '/ai-day', label: 'AI Day', icon: Sparkles },
  { path: '/settings', label: '设置', icon: Settings },
] as const

export function AppShell({
  boot,
  current,
  children,
  action,
  project,
  className = '',
}: {
  boot: Bootstrap
  current: (typeof workspaces)[number]['path']
  children: ReactNode
  action?: ReactNode
  project?: string
  className?: string
}) {
  const workspace = workspaces.find((item) => item.path === current)!
  return (
    <div className={`workspace ${className}`}>
      <aside className="sidebar">
        <NavLink className="brand" to="/tasks" aria-label="ATM 我的工作台">
          <img src="/mark.svg" alt="" />
          <span>
            ATM<small>我的工作台</small>
          </span>
        </NavLink>
        <div className="sidebar-label">工作空间</div>
        <nav aria-label="工作空间" className="workspace-navigation">
          {workspaces.map(({ path, label, icon: Icon }) => (
            <NavLink
              key={path}
              to={path}
              title={label}
              aria-label={label}
              className={({ isActive }) => `nav-item ${isActive ? 'selected' : ''}`}
            >
              <Icon size={18} />
              <span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className="connection">
            <span className="live-dot" />
            本机工作区
            <ShieldCheck size={13} />
          </div>
          <div className="footer-detail">
            数据保存在你的电脑上<span title={`ATM ${boot.version}`}>ATM</span>
          </div>
        </div>
      </aside>
      <main className="main">
        <header className="topbar">
          <div className="breadcrumb">
            <span>工作空间</span>
            <span className="slash">/</span>
            <strong>{workspace.label}</strong>
            {project && (
              <>
                <span className="slash">/</span>
                <span>{project}</span>
              </>
            )}
          </div>
          <div className="topbar-right">
            <span className="today">
              {new Intl.DateTimeFormat('zh-CN', {
                month: 'long',
                day: 'numeric',
                weekday: 'long',
              }).format(new Date())}
            </span>
            {action}
          </div>
        </header>
        {boot.capabilities?.data_upgrade_required === true && (
          <div className="upgrade-notice" role="status">
            <details>
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
            </details>
          </div>
        )}
        {children}
      </main>
    </div>
  )
}
