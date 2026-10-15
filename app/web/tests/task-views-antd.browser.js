// Verifies the task-views antd migration without touching the pre-existing
// `任务范围` Select accessible-name failure (present on the unmodified baseline too).
async (page) => {
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/task-views.html')
  await page.evaluate(() => {
    localStorage.setItem(
      'atm:task-views:v1',
      JSON.stringify([{ id: 'legacy', name: '旧视图', query: 'focus=review&layout=list' }]),
    )
    localStorage.removeItem('atm:task-view-preferences:v1')
  })
  await page.reload()

  const open = async (name) => {
    await page.getByRole('button', { name: '新增或管理视图' }).click()
    await page.getByRole('button', { name, exact: true }).click()
  }

  // 1. view tabs remain real <button> children of .workbench-views (test relies on this)
  check(
    await page.getByRole('button', { name: '旧视图', exact: true }).isVisible(),
    'legacy view tab missing',
  )
  const tabTags = await page.locator('.workbench-views > button').evaluateAll((els) =>
    els.map((el) => ({ tag: el.tagName, antd: el.className.includes('ant-btn') })),
  )
  check(tabTags.length > 0, 'no view tabs rendered')
  check(
    tabTags.every((t) => t.tag === 'BUTTON' && t.antd),
    `view tabs not all antd Button: ${JSON.stringify(tabTags)}`,
  )

  // 2. trigger + dropdown menu items are antd Buttons
  const trigger = await page
    .getByRole('button', { name: '新增或管理视图' })
    .evaluate((el) => el.className.includes('ant-btn'))
  check(trigger, 'trigger is not an antd Button')
  await page.getByRole('button', { name: '新增或管理视图' }).click()
  const menuItems = await page.locator('.task-views-menu > button').evaluateAll((els) =>
    els.map((el) => el.className.includes('ant-btn')),
  )
  check(
    menuItems.length === 2 && menuItems.every(Boolean),
    `menu items not antd Buttons: ${JSON.stringify(menuItems)}`,
  )
  await page.keyboard.press('Escape')

  // 3. create dialog: name field is an antd Input and receives autofocus
  await open('新增视图')
  const nameInfo = await page.getByRole('textbox', { name: '名称', exact: true }).evaluate((el) => ({
    cls: el.className,
    focused: el === document.activeElement,
    tag: el.tagName,
  }))
  check(nameInfo.cls.includes('ant-input'), `name field is not antd Input: ${nameInfo.cls}`)
  check(nameInfo.focused, 'name field lost autofocus')
  await page.getByRole('textbox', { name: '名称', exact: true }).fill('需要 Review')

  // 4. property chips are antd Buttons and keep aria-pressed toggling
  await page.locator('.task-view-defaults .ant-collapse-header').first().click()
  const chips = await page.locator('.task-view-property-chips > button').evaluateAll((els) =>
    els.map((el) => ({ antd: el.className.includes('ant-btn'), pressed: el.getAttribute('aria-pressed') })),
  )
  check(chips.length > 0, 'no property chips')
  check(
    chips.every((c) => c.antd && c.pressed !== null),
    `chips broken: ${JSON.stringify(chips.slice(0, 4))}`,
  )

  // 5. footer buttons: submit stays a real submit button
  const footer = await page.locator('.task-view-dialog footer button').evaluateAll((els) =>
    els.map((el) => ({ antd: el.className.includes('ant-btn'), type: el.getAttribute('type') })),
  )
  check(
    footer.length === 2 && footer.every((f) => f.antd),
    `footer buttons not antd: ${JSON.stringify(footer)}`,
  )
  check(footer[1].type === 'submit', `创建视图 lost htmlType=submit: ${footer[1].type}`)
  await page.getByRole('button', { name: '创建视图', exact: true }).click()
  // The original test asserts the matched-task text, but that depends on picking
  // 任务范围=review through the shared Select, which is broken on the baseline too.
  // Here we only assert the submit actually created and selected the view.
  check(
    (await page.getByRole('button', { name: /^需要 Review/ }).count()) > 0,
    'htmlType=submit did not create the view',
  )
  // antd Modal keeps its DOM node mounted after close and animates out, so wait for
  // the wrap to be hidden rather than asserting the node is gone.
  await page.waitForFunction(
    () => {
      const wrap = document.querySelector('.ant-modal-wrap')
      return !!wrap && getComputedStyle(wrap).display === 'none'
    },
    undefined,
    { timeout: 5000 },
  )

  // 6. manage dialog: antd Switch keeps role=switch, aria-label, disabled protection
  await open('管理视图')
  const allSwitch = page.getByRole('switch', { name: '显示视图 全部', exact: true })
  check(await allSwitch.isDisabled(), '全部 view is no longer protected')
  check(
    await allSwitch.evaluate((el) => el.className.includes('ant-switch')),
    'visibility toggle is not an antd Switch',
  )
  const switchCount = await page.getByRole('switch').count()
  check(switchCount >= 4, `expected a switch per view row, got ${switchCount}`)

  // 7. icon buttons stay square (antd controlHeight leak guard)
  const iconBox = await page
    .getByRole('button', { name: '上移 需要 Review', exact: true })
    .evaluate((el) => {
      const r = el.getBoundingClientRect()
      return { w: Math.round(r.width), h: Math.round(r.height), antd: el.className.includes('ant-btn') }
    })
  check(iconBox.antd, '上移 is not an antd Button')
  check(
    Math.abs(iconBox.w - iconBox.h) <= 2,
    `icon button not square: ${iconBox.w}x${iconBox.h}`,
  )

  // 8. toggling a switch still hides the view after reload
  await page.getByRole('switch', { name: '显示视图 等待中', exact: true }).click()
  await page.getByRole('button', { name: '关闭视图弹窗' }).click()
  await page.reload()
  check(
    (await page.getByRole('button', { name: /^等待中/ }).count()) === 0,
    'hidden view reappeared after switch toggle',
  )

  // 9. drag handle kept draggable + keyboard reorder affordance
  await open('管理视图')
  const handle = await page
    .getByRole('button', { name: /拖动排序/, exact: false })
    .first()
    .evaluate((el) => ({
      antd: el.className.includes('ant-btn'),
      draggable: el.getAttribute('draggable'),
    }))
  check(handle.antd && handle.draggable === 'true', `drag handle broken: ${JSON.stringify(handle)}`)

  // 10. no native form controls left in this module
  const leftovers = await page.evaluate(() => ({
    rawButtons: Array.from(document.querySelectorAll('.task-view-dialog button, .workbench-views button'))
      .filter((el) => !el.className.includes('ant-btn') && !el.className.includes('ant-switch'))
      .map((el) => el.className || el.textContent?.trim().slice(0, 12)),
  }))
  check(
    leftovers.rawButtons.length === 0,
    `unmigrated buttons remain: ${JSON.stringify(leftovers.rawButtons)}`,
  )

  return 'PASS: view tabs, trigger+menu, AntInput autofocus, chips aria-pressed, submit htmlType, Switch role/disabled/toggle, square icon buttons, draggable handle, no raw buttons left'
}
