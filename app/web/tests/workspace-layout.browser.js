// Run with playwright-cli against the local Vite fixture; all API responses are fixed.
async (page) => {
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  const fixture = 'http://127.0.0.1:5173/tests/fixtures/workspace-layout.html'
  const requests = []
  page.on('request', (request) => requests.push(request.url()))
  await page.goto(`${fixture}?scene=agents`)
  await page.getByRole('button', { name: /核对工作区布局/ }).waitFor()
  check(!requests.some((url) => /\/workspaces\/(usage\.tsx|badge-calendar\.tsx)/.test(url)), 'Agent loaded the statistics workspace')

  await page.goto(`${fixture}?scene=settings&section=appearance`)
  await page.getByRole('heading', { name: '外观', exact: true }).waitFor()
  for (const width of [390, 600, 760, 761, 1000, 1001, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const layout = await page.locator('.as-settings-layout').evaluate((element) => {
      const nav = element.querySelector('.as-settings-nav')
      const style = getComputedStyle(element)
      const navStyle = getComputedStyle(nav)
      return { display: style.display, direction: navStyle.flexDirection, navWidth: nav.getBoundingClientRect().width, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    check(!layout.overflow, `Settings overflowed the page at ${width}px`)
    if (width <= 760) check(layout.display === 'flex' && layout.direction === 'row', `Settings navigator did not become horizontal at ${width}px`)
    else check(layout.display === 'grid' && layout.navWidth === (width <= 1000 ? 220 : 232), `Settings navigator width changed at ${width}px`)
  }
  await page.getByRole('button', { name: '运行与同步 工作台状态与索引', exact: true }).click()
  await page.locator('.as-runtime-banner').waitFor()

  for (const width of [390, 760, 761, 900, 901, 1000, 1001]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`${fixture}?scene=detail&expanded=1`)
    const detail = await page.locator('.detail-expanded').boundingBox()
    check(detail && detail.x >= 0 && detail.y >= 0 && detail.x + detail.width <= width && detail.y + detail.height <= 900, `Expanded details left the viewport at ${width}px`)
    if (width <= 900) check(await page.locator('.detail-scroll').evaluate((element) => getComputedStyle(element).minHeight === '0px'), 'Narrow details restored the obsolete minimum height')
  }

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(`${fixture}?scene=usage&range=last_7_days`)
  await page.getByText('总 Token', { exact: true }).waitFor()
  await page.getByRole('combobox', { name: '统计模型', exact: true }).click()
  await page.getByRole('option', { name: 'test-model', exact: true }).click()
  check(await page.evaluate(() => new URL(location.href).searchParams.get('model') === 'test-model'), 'Statistics model filter lost its URL state')
  await page.getByRole('button', { name: '额度', exact: true }).click()
  await page.locator('.activity-quota-card').waitFor()
  await page.getByRole('button', { name: '徽章日历', exact: true }).click()
  await page.locator('.badge-calendar').waitFor()
  return 'PASS: Agent/statistics lazy boundary, Settings breakpoint layout, narrow detail scrolling, expanded viewport bounds, statistics filters, quota and badge navigation'
}
