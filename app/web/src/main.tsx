import { Loading } from './components/loading'
import { Button as AntButton, Spin } from 'antd'
import React, { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { ApiError, bootstrap, errorText, reconnect } from './api'
import { AppShell, workspaces } from './workspace-shell'
import { useLiveUpdates } from './live-updates'
import { DesignSystemProvider } from './design-system'
import { RuntimeJobNotifications } from './features/notifications/runtime-job-notifications'
import type { Bootstrap } from './types'
import './app.css'
import './theme'

const TaskWorkspace = React.lazy(() =>
  import('./features/tasks/task-workspace').then((module) => ({ default: module.TaskWorkspace })),
)
const AgentsWorkspace = React.lazy(() =>
  import('./features/agents/activity').then((module) => ({ default: module.AgentsWorkspace })),
)
const UsageWorkspace = React.lazy(() =>
  import('./features/usage/usage').then((module) => ({ default: module.UsageWorkspace })),
)
const KnowledgeWorkspace = React.lazy(() =>
  import('./features/knowledge/knowledge').then((module) => ({
    default: module.KnowledgeWorkspace,
  })),
)
const CollectionWorkspace = React.lazy(() =>
  import('./features/collection/collection').then((module) => ({
    default: module.CollectionWorkspace,
  })),
)
const SettingsWorkspace = React.lazy(() =>
  import('./features/settings/aiday-settings').then((module) => ({
    default: module.SettingsWorkspace,
  })),
)

const MessagesWorkspace = React.lazy(() =>
  import('./features/notifications/messages').then((module) => ({
    default: module.MessagesWorkspace,
  })),
)

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,
      retry: (count, err) => count < 1 && (!(err instanceof ApiError) || err.status >= 500),
    },
    mutations: { retry: false },
  },
})

function App() {
  const [boot, setBoot] = useState<Bootstrap>()
  useLiveUpdates(boot)
  const [failure, setFailure] = useState<unknown>()
  useEffect(() => {
    bootstrap().then(setBoot).catch(setFailure)
    const expire = () => setFailure(new ApiError(401, 'unauthenticated', '连接已过期'))
    window.addEventListener('atm:session-expired', expire)
    return () => window.removeEventListener('atm:session-expired', expire)
  }, [])
  if (failure)
    return (
      <div className="connection-screen">
        <img src="/mark.svg" alt="ATM" />
        <h1>连接你的工作台</h1>
        <p>{errorText(failure)}</p>
        <code>atm serve --open</code>
        <AntButton
          className="button primary"
          onClick={() => {
            setFailure(undefined)
            setBoot(undefined)
            void queryClient
              .cancelQueries()
              .then(() => {
                queryClient.clear()
                return reconnect()
              })
              .then(setBoot)
              .catch(setFailure)
          }}
        >
          <RefreshCw size={15} />
          重新连接
        </AntButton>
        <span>任务和数据仍保存在这台 Mac 上。</span>
      </div>
    )
  if (!boot)
    return (
      <div className="connection-screen">
        <img src="/mark.svg" alt="ATM" />
        <Spin />
        <p>正在打开工作台…</p>
      </div>
    )
  return (
    <>
      <RuntimeJobNotifications boot={boot} />
      <Routes>
        <Route
          path="/tasks/:id?"
          element={
            <WorkspaceErrorBoundary>
              <React.Suspense
                fallback={
                  <AppShell boot={boot}>
                    <Loading text="正在打开工作区…" />
                  </AppShell>
                }
              >
                <TaskWorkspace boot={boot} />
              </React.Suspense>
            </WorkspaceErrorBoundary>
          }
        />
        <Route
          path="/collection"
          element={
            <ModulePage boot={boot} path="/collection">
              <CollectionWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route
          path="/agents/:id?"
          element={
            <ModulePage boot={boot} path="/agents">
              <AgentsWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route
          path="/knowledge"
          element={
            <ModulePage boot={boot} path="/knowledge">
              <KnowledgeWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route
          path="/usage"
          element={
            <ModulePage boot={boot} path="/usage">
              <UsageWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route
          path="/messages"
          element={
            <ModulePage boot={boot} path="/messages">
              <MessagesWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route path="/ai-day" element={<Navigate to="/usage?view=badges" replace />} />
        <Route
          path="/settings"
          element={
            <ModulePage boot={boot} path="/settings">
              <SettingsWorkspace boot={boot} />
            </ModulePage>
          }
        />
        <Route path="*" element={<Navigate to="/tasks" replace />} />
      </Routes>
    </>
  )
}

function ModulePage({
  boot,
  path,
  children,
}: {
  boot: Bootstrap
  path: (typeof workspaces)[number]['path']
  children: React.ReactNode
}) {
  return (
    <AppShell boot={boot}>
      <div className="module-host" key={path}>
        <WorkspaceErrorBoundary key={path}>
          <React.Suspense fallback={<Loading text="正在打开工作区…" />}>{children}</React.Suspense>
        </WorkspaceErrorBoundary>
      </div>
    </AppShell>
  )
}

class WorkspaceErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error?: Error }
> {
  state: { error?: Error } = {}
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error)
      return (
        <div className="workspace-render-error" role="alert">
          <h2>这个工作区暂时无法显示</h2>
          <p>{errorText(this.state.error)}</p>
          <AntButton className="button" onClick={() => window.location.reload()}>
            <RefreshCw size={15} />
            重新加载
          </AntButton>
        </div>
      )
    return this.props.children
  }
}

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <DesignSystemProvider>
          <App />
        </DesignSystemProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
)
