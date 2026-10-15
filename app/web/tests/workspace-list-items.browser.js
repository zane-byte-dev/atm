// Real Agent and inbox workspaces, backed by the isolated layout fixture.
async (page) => {
  const check = (ok, message) => { if (!ok) throw new Error(message) }
  const fixture = 'http://127.0.0.1:5173/tests/fixtures/workspace-layout.html'
  const appearances = []
  for (const scene of ['agents', 'collection']) {
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.goto(`${fixture}?scene=${scene}`)
    const item = page.locator('.workspace-list-item').first()
    await item.waitFor()
    await page.mouse.move(0, 0)
    await page.addStyleTag({ content: '.workspace-list-item { transition: none !important; }' })
    const appearance = () => item.evaluate((row) => {
      const style = getComputedStyle(row)
      const title = getComputedStyle(row.querySelector('.workspace-list-item-title'))
      const meta = getComputedStyle(row.querySelector('.workspace-list-item-meta'))
      return { padding: style.padding, margin: style.margin, radius: style.borderRadius,
        border: style.borderBottom, marker: style.borderLeft, background: style.backgroundColor,
        titleSize: title.fontSize, titleWeight: title.fontWeight, titleHeight: title.lineHeight,
        titleMargin: title.margin, metaSize: meta.fontSize, metaGap: meta.gap }
    })
    const normal = await appearance()
    await item.hover()
    const hover = await appearance()
    check(normal.background !== hover.background, `${scene} lost its hover state`)
    await page.mouse.move(0, 0)
    for (const width of [390, 760, 761, 1000, 1280]) {
      await page.setViewportSize({ width, height: 900 })
      const geometry = await item.evaluate((row) => {
        const bounds = row.getBoundingClientRect()
        const title = row.querySelector('.workspace-list-item-title').getBoundingClientRect()
        const time = row.querySelector('time').getBoundingClientRect()
        return { left: title.left - bounds.left, timeRight: time.right, rowRight: bounds.right,
          overflow: document.documentElement.scrollWidth > innerWidth }
      })
      check(!geometry.overflow, `${scene} overflowed at ${width}px`)
      check(geometry.left === 18 && geometry.timeRight <= geometry.rowRight - 18, `${scene} item alignment changed at ${width}px`)
    }
    await item.focus()
    await page.keyboard.press('Shift+Tab')
    await page.keyboard.press('Tab')
    check(await item.evaluate((row) => getComputedStyle(row).outlineStyle === 'solid'), `${scene} keyboard focus is missing`)
    await page.keyboard.press('Enter')
    const name = scene === 'agents' ? '布局会话' : '核对收件箱布局'
    await page.getByRole('heading', { name, exact: true }).waitFor()
    check(await item.getAttribute('aria-pressed') === 'true', `${scene} lost its selected state`)
    const selected = await appearance()
    check(selected.marker !== normal.marker && selected.background !== normal.background, `${scene} selection has no visual marker`)
    appearances.push({ normal, hover, selected })
    if (scene === 'collection') {
      check(await item.getByLabel('未读').count() === 1, 'Inbox unread dot disappeared in read-only mode')
      check(await page.locator('.workspace-list-item-label[data-tone="danger"]').count() === 1, 'Inbox failure status lost its tone')
      const longTitle = page.locator('.workspace-list-item-title').nth(1)
      check(await longTitle.evaluate((title) => title.scrollHeight > title.clientHeight && title.clientHeight <= parseFloat(getComputedStyle(title).lineHeight) * 2 + 1), 'Long inbox titles are not clamped to two lines')
      check(await item.locator('.workspace-list-item-context').evaluate((context) => !!context.title && context.scrollWidth > context.clientWidth), 'Truncated context lost its tooltip')
    }
    await page.screenshot({ path: `/private/tmp/atm-${scene}-list-items.png` })
  }
  check(JSON.stringify(appearances[0]) === JSON.stringify(appearances[1]), 'Agent and inbox item styles differ')
  return 'PASS: shared item typography, padding, borders, hover, selection, keyboard activation, unread/failure indicators, truncation and 390–1280px geometry'
}
