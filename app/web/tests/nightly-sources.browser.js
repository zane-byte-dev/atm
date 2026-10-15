// Run with playwright-cli against the isolated workspace-layout fixture.
async (page) => {
  const check = (ok, message) => { if (!ok) throw new Error(message) }
  await page.goto('http://127.0.0.1:5173/tests/fixtures/workspace-layout.html?scene=agents')
  await page.getByRole('heading', { name: 'Codex 会话', exact: true }).waitFor()
  const links = await page.locator('.rail-destinations a').evaluateAll(nodes => nodes.map(node => node.getAttribute('href')))
  check(links[0] === '/tasks' && links[1] === '/knowledge', 'Tasks and knowledge must lead navigation')
  await page.evaluate(() => {
    window.nightlyRequests = []
    const original = window.fetch
    window.fetch = (url, init) => {
      window.nightlyRequests.push({ method: String(url).split('/').at(-1), body: JSON.parse(init?.body || '{}') })
      return original(url, init)
    }
  })
  await page.getByRole('button', { name: '筛选会话', exact: true }).click()
  await page.getByText('每天 23:00 后后台采集一次，次日 06:00 前可补采。需要最新记录时可手动同步。').waitFor()
  await page.getByRole('combobox', { name: '筛选 Agent', exact: true }).click()
  await page.locator('.ant-select-item-option[title="全部来源（含辅助）"]').click()
  await page.getByRole('heading', { name: 'Agent 会话', exact: true }).waitFor()
  await page.waitForFunction(() => window.nightlyRequests.some(r => r.method === 'session.list' && r.body.agent === ''))
  check(await page.evaluate(() => new URL(location.href).searchParams.get('agent') === 'all'), 'All sources must survive reload as an explicit URL scope')
  await page.getByRole('button', { name: '恢复默认（Codex）', exact: true }).click()
  await page.getByRole('heading', { name: 'Codex 会话', exact: true }).waitFor()
  await page.waitForFunction(() => window.nightlyRequests.some(r => r.method === 'session.list' && r.body.agent === 'codex'))
  await page.getByRole('button', { name: '收起筛选', exact: true }).click()
  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Page overflow at ${width}`)
  }
  return 'PASS: primary navigation, Codex default, all-source API filter, reset, mobile/desktop layout'
}
