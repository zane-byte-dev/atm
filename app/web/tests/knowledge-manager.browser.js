// Run in an isolated Playwright browser with the local Vite server on port 5173.
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
  const selectValue = async (control, value) => {
    const label = await control.evaluate(
      (element, value) =>
        Array.from(element.closest('.ds-select-bridge').querySelector('select').options).find(
          (option) => option.value === value,
        )?.textContent,
      value,
    )
    await control.click()
    await (await option(label)).click()
  }
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/knowledge-manager.html')
  // The element carrying role="combobox" is antd's <input>, whose textContent is
  // always empty. antd v6 renders the chosen label in `.ant-select-content`
  // (v5 called it `.ant-select-selection-item`), which also wraps that input.
  const selectedText = (control) =>
    control.evaluate(
      (element) => element.closest('.ant-select-content')?.textContent?.trim() ?? '',
    )
  await page.getByRole('button', { name: '管理收件箱的文档', exact: true }).click()
  check(
    (await selectedText(page.getByRole('combobox', { name: '管理文档集合' }))) === '收件箱',
    'collection scope lost',
  )
  await page.getByRole('checkbox', { name: '选择文档 接口设计草稿' }).waitFor()
  check(
    (await page.getByRole('checkbox', { name: /选择文档 / }).count()) === 2,
    'wrong scoped rows',
  )
  await page.getByRole('checkbox', { name: '选择当前已加载文档' }).check()
  await page.getByRole('button', { name: '归档', exact: true }).click()
  await page.getByText('已更新 1 篇，未变化 0 篇，失败 1 篇。', { exact: false }).waitFor()
  check(
    await page.getByRole('checkbox', { name: '选择文档 接口设计草稿' }).isChecked(),
    'failure selection lost',
  )
  check(
    !(await page.getByRole('checkbox', { name: '选择文档 开发约定' }).isChecked()),
    'successful selection retained',
  )
  await page.screenshot({ path: '/tmp/knowledge-manager-failure.png' })
  await page.getByRole('button', { name: '归档', exact: true }).click()
  await page.getByText('已更新 1 篇，未变化 0 篇，失败 0 篇。', { exact: false }).waitFor()
  await page.getByRole('button', { name: '已归档', exact: true }).click()
  await page.getByRole('checkbox', { name: '选择当前已加载文档' }).check()
  await page.getByRole('button', { name: '恢复为有效', exact: true }).click()
  await page.getByText('当前条件下没有文档。').waitFor()
  await page.getByRole('button', { name: '有效', exact: true }).click()
  await page.getByRole('checkbox', { name: '选择当前已加载文档' }).check()
  await selectValue(page.getByRole('combobox', { name: '批量移动目标集合' }), 'reference')
  await page.getByRole('button', { name: '移动', exact: true }).click()
  await page.getByText('当前条件下没有文档。').waitFor()
  await selectValue(page.getByRole('combobox', { name: '管理文档集合' }), 'reference')
  await page.getByRole('button', { name: '全部', exact: true }).click()
  await page.getByRole('checkbox', { name: '选择文档 归档笔记' }).waitFor()
  check((await page.getByRole('checkbox', { name: /选择文档 / }).count()) === 3, 'move failed')
  await page.screenshot({ path: '/tmp/knowledge-manager.png' })
  await page.getByRole('checkbox', { name: '选择当前已加载文档' }).check()
  await page.getByRole('button', { name: '草稿', exact: true }).click()
  await page.getByRole('button', { name: '全部', exact: true }).click()
  check(
    (await page.getByRole('checkbox', { name: '选择当前已加载文档' }).isChecked()) === false,
    'selection leaked across filters',
  )
  await page.getByRole('button', { name: '开发约定', exact: true }).click()
  check((await page.getByRole('dialog').count()) === 0, 'open document did not close manager')
  await page.goto('http://127.0.0.1:5173/tests/fixtures/knowledge-manager.html?readonly=1')
  await page.getByRole('button', { name: '文档管理', exact: true }).click()
  check((await page.getByRole('checkbox').count()) === 0, 'read-only checkboxes shown')
  check(
    (await page.getByRole('button', { name: '归档', exact: true }).count()) === 0,
    'read-only actions shown',
  )
  await page.setViewportSize({ width: 390, height: 844 })
  check(
    await page.getByRole('dialog').evaluate((el) => el.scrollWidth <= el.clientWidth),
    'mobile dialog overflows',
  )
  await page.screenshot({ path: '/tmp/knowledge-manager-mobile.png' })
  await page.keyboard.press('Escape')
  check((await page.getByRole('dialog').count()) === 0, 'Escape did not close')
  return 'PASS: collection scope, select all, partial failure and retry, archive, restore, move, filter selection reset, open document, read-only mode, mobile, Escape'
}
