// Run in an isolated browser with the local Vite fixture; all requests are mocked by the fixture.
async (page) => {
  const check = (condition, message) => { if (!condition) throw new Error(message) }
  await page.goto('http://127.0.0.1:5199/tests/fixtures/task-pagination.html')
  await page.waitForFunction(() => document.querySelectorAll('[data-task-id]').length === 55)
  check(await page.getByText('查看更多任务 · 还有 70 项', { exact: true }).isVisible(), 'active remainder missing')
  check(await page.getByText('查看更多已完成 · 还有 4 项', { exact: true }).isVisible(), 'done remainder missing')
  check(await page.evaluate(() => window.paginationRequests.filter((input) => !input.metadata_only).every((input) => input.offset === 0 && input.partition)), 'initial view drained pages')
  await page.getByRole('checkbox', { name: '选择全部筛选结果', exact: true }).check()
  check(await page.getByText('已选择 全部筛选结果 129 项', { exact: true }).isVisible(), 'select all only counted visible rows')
  await page.getByRole('checkbox', { name: '选择 t1', exact: true }).uncheck()
  check(await page.getByText('已选择 全部筛选结果 128 项', { exact: true }).isVisible(), 'all exclusions incorrect')
  await page.getByText('查看更多任务 · 还有 70 项', { exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('[data-task-id]').length === 105)
  await page.getByText('查看更多已完成 · 还有 4 项', { exact: true }).click()
  await page.waitForFunction(() => document.querySelectorAll('[data-task-id]').length === 109)
  await page.getByRole('button', { name: '布局 table', exact: true }).click()
  await page.getByRole('button', { name: '导出所选', exact: true }).waitFor()
  check(await page.getByText('已选择 全部筛选结果 128 项', { exact: true }).isVisible(), 'layout switch lost selection')
  const downloadEvent = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出所选', exact: true }).click()
  const download = await downloadEvent
  check(download.suggestedFilename() === 'ATM-任务.csv', 'export failed')
  check(await page.evaluate(() => window.paginationRequests.some((input) => !input.metadata_only && !input.partition && input.limit === 200)), 'export did not request complete filtered set')
  await page.getByRole('button', { name: '布局 kanban', exact: true }).click()
  await page.getByRole('button', { name: '查看更多任务 · 还有 70 项', exact: true }).waitFor()
  await page.getByRole('button', { name: '布局 swimlane', exact: true }).click()
  await page.getByRole('button', { name: '查看更多任务 · 还有 70 项', exact: true }).waitFor()
  check(await page.getByText('已选择 全部筛选结果 128 项', { exact: true }).isVisible(), 'selection did not survive board/swimlane')
  await page.evaluate(() => window.removePaginationRow('t1'))
  await page.getByText('筛选结果已变化，已清除之前的选择。', { exact: true }).waitFor()
  check(await page.getByRole('region', { name: '任务批量操作' }).count() === 0, 'removed exclusion left a misleading selection count')
  return 'PASS: bounded initial pages, active and completed paging, full query selection/exclusions, all four layouts, complete export, external membership changes'
}
