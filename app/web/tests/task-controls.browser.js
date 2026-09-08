// Run in an isolated browser against the local Vite fixture using playwright-cli run-code.
;async (page) => {
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
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
  await page.getByRole('option', { name: '更新时间', exact: true }).click()
  const direction = page.getByRole('combobox', { name: '排序方向', exact: true })
  await direction.press('ArrowDown')
  await direction.press('End')
  await direction.press('Enter')
  check(
    (await page.locator('output[aria-label="当前配置"]').textContent()).includes('direction=desc'),
    'keyboard did not change value',
  )
  await page.getByRole('button', { name: '提交选择', exact: true }).click()
  const required = page.getByRole('combobox', { name: '必填选项', exact: true })
  check((await required.getAttribute('aria-expanded')) === 'true', 'required control did not open')
  await required.press('Home')
  await required.press('ArrowDown')
  await required.press('Enter')
  await page.getByRole('button', { name: '提交选择', exact: true }).click()
  check(
    (await page.getByRole('status', { name: '表单结果' }).textContent()) === 'three',
    'disabled option or form submission broken',
  )
  await page.getByRole('button', { name: '新增或管理视图', exact: true }).click()
  await page.getByRole('button', { name: '新增视图', exact: true }).click()
  await page.getByRole('combobox', { name: '添加筛选', exact: true }).click()
  await page.getByRole('combobox', { name: '搜索选项', exact: true }).fill('项目')
  await page.getByRole('option', { name: '项目', exact: true }).click()
  await page.getByRole('combobox', { name: '视图筛选项目', exact: true }).click()
  await page.getByRole('combobox', { name: '搜索选项', exact: true }).fill('project-10')
  await page.getByRole('option', { name: 'project-10', exact: true }).click()
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
  check(
    await page.getByRole('listbox', { name: '必填选项', exact: true }).evaluate((el) => {
      const r = el.parentElement.getBoundingClientRect()
      return r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
    }),
    'popup outside viewport',
  )
  await required.press('Escape')
  return 'PASS: filter combinations, custom select mouse/keyboard, disabled options, required form, searchable dialog selection and save, narrow viewport, Escape'
}
