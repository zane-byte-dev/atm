// Isolated view-management fixture: never reads or writes ATM task data.
import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { TaskViews } from '../../src/task-views'
import { filterTasks } from '../../src/task-view-model'
import type { Todo } from '../../src/types'
import '../../src/style.css'
import '../../src/themes.css'
import '../../src/task-workbench.css'
const items = [
  { id: 't1', title: '需要验收的任务', status: 'review', priority: 'P1', project: 'atm' },
  { id: 't2', title: '开发中的任务', status: 'in_progress', priority: 'P0', project: 'atm' },
] as Todo[]
function Harness() {
  const [params, setParams] = useState(
    () => new URLSearchParams(location.search || 'focus=all&layout=kanban'),
  )
  return (
    <main className="task-workbench" style={{ padding: 32, minHeight: '100vh' }}>
      <h1>任务</h1>
      <div className="workbench-toolbar">
        <TaskViews
          items={items}
          params={params}
          onOpenMenu={() => {}}
          onChange={(next) => {
            history.replaceState(null, '', `?${next}`)
            setParams(next)
          }}
        />
      </div>
      <output aria-label="匹配任务">
        {filterTasks(items, params)
          .map((item) => item.title)
          .join('、')}
      </output>
    </main>
  )
}
createRoot(document.getElementById('root')!).render(<Harness />)
