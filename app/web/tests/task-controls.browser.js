// Run in an isolated browser against the local Vite fixture using playwright-cli run-code.
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
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/task-controls.html')
  await page.getByRole('button', { name: '筛选', exact: true }).click()
  await page.getByRole('menuitem', { name: '状态', exact: true }).click()
  await page.getByRole('menuitemcheckbox', { name: '待验收 3 个任务', exact: true }).click()
  await page.getByRole('menuitemcheckbox', { name: '进行中 3 个任务', exact: true }).click()
  check(
    (await page.locator('output[aria-label="当前配置"]').textContent()).includes(
      'review%2Cin_progress',
    ),
    'status multiselect lost',
  )
  await page.getByRole('button', { name: '显示', exact: true }).click()
  await page.getByRole('combobox', { name: '任务排序', exact: true }).click()
  await (await option('更新时间')).click()
  const direction = page.getByRole('combobox', { name: '排序方向', exact: true })
  // rc-select does not implement Home/End (the native <select> did). The first
  // ArrowDown opens the list, the second moves the highlight to 降序, Enter commits.
  await direction.press('ArrowDown')
  await direction.press('ArrowDown')
  await direction.press('Enter')
  check(
    (await page.locator('output[aria-label="当前配置"]').textContent()).includes('direction=desc'),
    'keyboard did not change value',
  )
  await page.getByRole('button', { name: '提交选择', exact: true }).click()
  const required = page.getByRole('combobox', { name: '必填选项', exact: true })
  check((await required.getAttribute('aria-expanded')) === 'true', 'required control did not open')
  // Two ArrowDowns walk past the disabled 请选择 and 选项二（禁用） to 选项三.
  await required.press('ArrowDown')
  await required.press('ArrowDown')
  await required.press('Enter')
  await page.getByRole('button', { name: '提交选择', exact: true }).click()
  check(
    (await page.getByRole('status', { name: '表单结果' }).textContent()) === 'three',
    'disabled option or form submission broken',
  )
  await page.getByRole('button', { name: '新增或管理视图', exact: true }).click()
  await page.getByRole('button', { name: '新增视图', exact: true }).click()
  // antd v6 does not add a separate search box when showSearch is on — the same
  // combobox input simply drops its readonly attribute once the list opens. So type
  // into the control that was clicked; there is no `搜索选项` combobox to find.
  const addFilter = page.getByRole('combobox', { name: '添加筛选', exact: true })
  await addFilter.click()
  await addFilter.fill('项目')
  await (await option('项目')).click()
  const projectFilter = page.getByRole('combobox', { name: '视图筛选项目', exact: true })
  await projectFilter.click()
  await projectFilter.fill('project-10')
  await (await option('project-10')).click()
  await page.getByRole('textbox', { name: '名称', exact: true }).fill('筛选验证')
  await page.getByRole('button', { name: '创建视图', exact: true }).click()
  check(
    (await page.locator('output[aria-label="当前配置"]').textContent()).includes(
      'project=project-10',
    ),
    'dialog choice not saved',
  )
  await page.setViewportSize({ width: 390, height: 844 })
  await required.click()
  // antd v6 keeps role="listbox" on a 0x0 hidden a11y mirror that has no accessible
  // name; the popup the user actually sees is `.ant-select-dropdown`.
  await dropdownSettled()
  check(
    await page.locator(LIVE_DROPDOWN).last().evaluate((el) => {
      const r = el.getBoundingClientRect()
      return r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
    }),
    'popup outside viewport',
  )
  await required.press('Escape')
  return 'PASS: filter combinations, custom select mouse/keyboard, disabled options, required form, searchable dialog selection and save, narrow viewport, Escape'
}
