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
  { path: '/tasks', label: '工作板', icon: CheckCheck },
  { path: '/collection', label: '收件箱', icon: Inbox },
  { path: '/agents', label: 'Agent', icon: Cpu },
  { path: '/knowledge', label: '知识', icon: BookOpen },
  { path: '/usage', label: '统计', icon: ChartNoAxesCombined },
  { path: '/ai-day', label: 'AI Day', icon: Sparkles },
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
  return (
    <div className={`workspace ${className}`}>
      <aside className="sidebar">
        <NavLink className="brand" to="/tasks" aria-label="ATM 我的工作台">
          <img src="/mark.svg" alt="" />
          <span>
            ATM<small>我的工作台</small>
          </span>
        </NavLink>
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
        </div>
      </aside>
      <main className="main">
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
