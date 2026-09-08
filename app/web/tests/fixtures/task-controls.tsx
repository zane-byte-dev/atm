import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TaskWorkbench } from '../../src/task-workbench'
import { AppShell } from '../../src/workspace-shell'
import { Select } from '../../src/components/select'
import type { Bootstrap, Todo } from '../../src/types'
import '../../src/style.css'
import '../../src/themes.css'
import '../../src/workspace-style.css'
import '../../src/web-improvements.css'
import '../../src/workspace-chrome.css'
const items = Array.from({ length: 12 }, (_, index) => ({
  id: `t${index + 1}`,
  title: ['梳理交付范围', '实现筛选菜单', '验收界面', '已完成任务'][index % 4],
  status: ['open', 'in_progress', 'review', 'done'][index % 4],
  priority: `P${index % 3}`,
  project: `project-${index}`,
  creator: index % 2 ? 'codex' : 'me',
  created: '2026-09-07',
  tags: ['UI'],
})) as Todo[]
window.fetch = async (url) => {
  if (String(url).endsWith('/session.status'))
    return new Response(JSON.stringify({ data: { sessions: [] } }))
  throw new Error('Unexpected fixture request')
}
function Harness() {
  const [params, setParams] = useState(new URLSearchParams('focus=all&layout=list'))
  const [opened, setOpened] = useState('')
  const [formValue, setFormValue] = useState('')
  return (
    <AppShell boot={{ capabilities: { todo_write: true } } as Bootstrap}>
      <TaskWorkbench
        items={items}
        params={params}
        onChange={setParams}
        onOpen={setOpened}
        onCreate={() => {}}
        writable
        loading={false}
        onRetry={() => {}}
      />
      <output aria-label="当前配置" hidden>
        {params.toString()}
      </output>
      <output hidden>{opened}</output>
      <form
        aria-label="测试选择表单"
        onSubmit={(event) => {
          event.preventDefault()
          setFormValue(String(new FormData(event.currentTarget).get('choice')))
        }}
        style={{ padding: 12 }}
      >
        <label>
          必填选项
          <Select required name="choice" defaultValue="">
            <option value="" disabled>
              请选择
            </option>
            <option value="one">选项一</option>
            <option value="two" disabled>
              选项二（禁用）
            </option>
            <option value="three">选项三</option>
          </Select>
        </label>
        <button type="submit">提交选择</button>
        <output aria-label="表单结果">{formValue}</output>
      </form>
    </AppShell>
  )
}
createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <Harness />
    </QueryClientProvider>
  </BrowserRouter>,
)
