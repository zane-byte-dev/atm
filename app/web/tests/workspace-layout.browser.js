// Run with playwright-cli against the local Vite fixture; all API responses are fixed.
async (page) => {
  // antd v6 Select keeps role="option" only on a 0x0 hidden a11y mirror listbox
  // (whose text is the raw value), while the clickable visual items carry no role
  // at all. getByRole('option') therefore resolves to an element Playwright can
  // never click, so target the visible item by its exact title instead.
  // antd animates the popup in and out with a slide-up motion. Playwright's
  // actionability check needs the target to be stable (same box across two frames),
  // so clicking mid-animation times out. A closing popup also lingers without
  // `-hidden` until its leave motion ends, so exclude it from the search.
  const LIVE_DROPDOWN =
    '.ant-select-dropdown:not(.ant-select-dropdown-hidden):not([class*="ant-slide-up-leave"])'
  const dropdownSettled = () =>
    page.waitForFunction(() => {
      const live = Array.from(document.querySelectorAll('.ant-select-dropdown')).filter(
        (d) =>
          !d.className.includes('ant-select-dropdown-hidden') &&
          !d.className.includes('ant-slide-up-leave'),
      )
      return live.length > 0 && live.every((d) => !d.className.includes('ant-slide-up-appear'))
    })
  const option = async (label) => {
    await dropdownSettled()
    return page.locator(LIVE_DROPDOWN).locator(`.ant-select-item-option[title="${label}"]`).first()
  }
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  const fixture = 'http://127.0.0.1:5173/tests/fixtures/workspace-layout.html'
  const requests = []
  page.on('request', (request) => requests.push(request.url()))
  await page.goto(`${fixture}?scene=agents`)
  await page.getByRole('button', { name: /核对工作区布局/ }).waitFor()
  check(!requests.some((url) => /\/workspaces\/(usage\.tsx|badge-calendar\.tsx)/.test(url)), 'Agent loaded the statistics workspace')
  check(await page.locator('.activity-list > .activity-heading').count() === 1, 'Agent heading escaped its navigator')
  for (const width of [390, 760, 761, 1000, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const layout = await page.evaluate(() => {
      const rail = document.querySelector('.workspace-rail').getBoundingClientRect()
      const list = document.querySelector('.activity-list').getBoundingClientRect()
      const detail = document.querySelector('.activity-session-detail').getBoundingClientRect()
      return { railRight: rail.right, listLeft: list.left, listRight: list.right, detailLeft: detail.left, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    check(!layout.overflow, `Agent overflowed at ${width}px`)
    if (width > 760) check(Math.abs(layout.railRight - layout.listLeft) < 1 && Math.abs(layout.listRight - layout.detailLeft) < 1, `Agent columns did not align at ${width}px`)
    if (width > 760) check(Math.abs(layout.listRight - layout.listLeft - (width <= 1000 ? 240 : 280)) < 1, `Agent navigator width changed at ${width}px`)
  }
  await page.getByRole('button', { name: /核对工作区布局/ }).click()
  await page.getByRole('heading', { name: '核对工作区布局', level: 2 }).waitFor()
  const reading = await page.evaluate(() => {
    const detail = document.querySelector('.activity-session-detail').getBoundingClientRect()
    const title = document.querySelector('.activity-conversation-heading h2').getBoundingClientRect()
    const surface = document.querySelector('.activity-conversation-surface').getBoundingClientRect()
    return { titleLeft: title.left - detail.left, surfaceLeft: surface.left - detail.left, titleTop: title.top - detail.top }
  })
  check(Math.abs(reading.titleLeft - 40) < 1 && Math.abs(reading.surfaceLeft - 40) < 1 && reading.titleTop >= 50, 'Agent reading column does not align with Settings')
  await page.setViewportSize({ width: 390, height: 900 })
  check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'Selected Agent detail overflowed at 390px')

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
    else check(layout.display === 'grid' && layout.navWidth === 220, `Settings navigator width changed at ${width}px`)
  }
  await page.getByRole('button', { name: '运行与同步', exact: true }).click()
  await page.getByRole('heading', { name: '索引状态' }).waitFor()

  await page.goto(`${fixture}?scene=knowledge`)
  await page.getByRole('heading', { name: '知识', exact: true }).waitFor()
  for (const width of [390, 760, 761, 1000, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const layout = await page.locator('.knowledge-workspace').evaluate((element) => {
      const nav = element.querySelector('.knowledge-collections')
      return { display: getComputedStyle(element).display, navWidth: nav.getBoundingClientRect().width, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    check(!layout.overflow, `Knowledge overflowed at ${width}px`)
    if (width <= 760) check(layout.display === 'flex', `Knowledge did not stack at ${width}px`)
    else check(layout.display === 'grid' && Math.abs(layout.navWidth - (width <= 1000 ? 240 : 280)) < 1, `Knowledge navigator width changed at ${width}px`)
  }

  for (const width of [390, 760, 761, 900, 901, 1000, 1001]) {
    await page.setViewportSize({ width, height: 900 })
    await page.goto(`${fixture}?scene=detail&expanded=1`)
    const detail = await page.locator('.detail-expanded').boundingBox()
    check(detail && detail.x >= 0 && detail.y >= 0 && detail.x + detail.width <= width && detail.y + detail.height <= 900, `Expanded details left the viewport at ${width}px`)
    if (width <= 900) check(await page.locator('.detail-scroll').evaluate((element) => getComputedStyle(element).minHeight === '0px'), 'Narrow details restored the obsolete minimum height')
  }

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(`${fixture}?scene=usage&range=last_7_days`)
  await page.locator('.usage-navigator').waitFor()
  for (const width of [390, 760, 761, 1000, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const layout = await page.evaluate(() => {
      const rail = document.querySelector('.workspace-rail').getBoundingClientRect()
      const nav = document.querySelector('.usage-navigator').getBoundingClientRect()
      const detail = document.querySelector('.usage-detail').getBoundingClientRect()
      return { railRight: rail.right, navLeft: nav.left, navRight: nav.right, detailLeft: detail.left, overflow: document.documentElement.scrollWidth > innerWidth }
    })
    check(!layout.overflow, `Statistics overflowed at ${width}px`)
    if (width > 760) check(Math.abs(layout.railRight - layout.navLeft) < 1 && Math.abs(layout.navRight - layout.detailLeft) < 1, `Statistics columns did not align at ${width}px`)
    if (width > 760) check(Math.abs(layout.navRight - layout.navLeft - 220) < 1, `Statistics navigator width changed at ${width}px`)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  // The usage workspace persists its model/agent/project filters to localStorage.
  // A previous run leaves usage_filter_model set, so the select already reads
  // "test-model" and picking it again is a no-op that never fires onChange — the
  // URL assertion below would then fail on leaked state instead of a real bug.
  await page.evaluate(() => localStorage.removeItem('atm.web.native-preferences.v1'))
  await page.reload()
  await page.getByText('已记录 Token', { exact: true }).waitFor()
  await page.getByRole('combobox', { name: '统计模型', exact: true }).click()
  await (await option('test-model')).click()
  check(await page.evaluate(() => new URL(location.href).searchParams.get('model') === 'test-model'), 'Statistics model filter lost its URL state')
  await page.getByRole('button', { name: '额度', exact: true }).click()
  await page.locator('.activity-quota-card').waitFor()
  await page.getByRole('button', { name: '徽章日历', exact: true }).click()
  await page.locator('.badge-calendar').waitFor()
  for (const width of [390, 760, 761, 1000, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    const layout = await page.locator('.activity-usage').evaluate((root) => {
      const nav = root.querySelector('.usage-navigator').getBoundingClientRect()
      const detail = root.querySelector('.usage-detail').getBoundingClientRect()
      return { display: getComputedStyle(root).display, navWidth: nav.width,
        navRight: nav.right, detailLeft: detail.left,
        overflow: document.documentElement.scrollWidth > innerWidth }
    })
    check(!layout.overflow, `Badges overflowed at ${width}px`)
    if (width > 760) check(layout.display === 'grid' && layout.navWidth === 220 && Math.abs(layout.navRight - layout.detailLeft) < 1, `Badges lost the three-column layout at ${width}px`)
  }

  // Keep the shared navigator chrome aligned after feature styles load lazily.
  const chrome = []
  for (const scene of ['agents', 'settings', 'knowledge', 'usage']) {
    await page.goto(`${fixture}?scene=${scene}`)
    await page.locator('.workspace-navigator-heading').waitFor()
    const appearance = await page.locator('.workspace-navigator').evaluate((nav) => {
      const title = nav.querySelector('.workspace-navigator-heading h1')
      const heading = title.parentElement
      const n = nav.getBoundingClientRect(), t = title.getBoundingClientRect()
      const style = getComputedStyle(title)
      return {
        inset: t.left - n.left, top: t.top - n.top,
        height: heading.getBoundingClientRect().height,
        fontSize: style.fontSize, weight: style.fontWeight,
        background: getComputedStyle(nav).backgroundColor,
      }
    })
    chrome.push(appearance)
    check(appearance.inset === 26 && appearance.fontSize === '14px', `${scene} navigator title drifted`)
  }
  check(chrome.every((item) => JSON.stringify(item) === JSON.stringify(chrome[0])), 'Navigator chrome differs between workspaces')
  await page.goto(`${fixture}?scene=agents`)
  await page.getByRole('button', { name: /核对工作区布局/ }).click()
  await page.getByRole('heading', { name: '核对工作区布局', level: 2 }).waitFor()
  await page.setViewportSize({ width: 390, height: 900 })
  check(!(await page.locator('.activity-list').isVisible()), 'Selected Agent detail did not hide the navigator')
  return 'PASS: Agent/statistics lazy boundary, Settings breakpoint layout, narrow detail scrolling, expanded viewport bounds, statistics filters, quota and badge navigation, shared navigator chrome'
}
