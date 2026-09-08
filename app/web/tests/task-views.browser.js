// Run with playwright-cli run-code --filename=app/web/tests/task-views.browser.js
// Use an isolated browser session and the local Vite server on port 5173.
async (page) => {
  const selectValue = async (control, value) => {
    const label = await control.evaluate(
      (element, value) =>
        Array.from(element.closest('.ui-select').querySelector('select').options).find(
          (option) => option.value === value,
        )?.textContent,
      value,
    )
    await control.click()
    await page.getByRole('option', { name: label, exact: true }).click()
  }
  const check = (value, message) => {
    if (!value) throw new Error(message)
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
  check(
    await page.getByRole('button', { name: '旧视图', exact: true }).isVisible(),
    'legacy view missing',
  )
  await open('新增视图')
  check(
    await page
      .getByRole('textbox', { name: '名称', exact: true })
      .evaluate((el) => el === document.activeElement),
    'name not focused',
  )
  await page.screenshot({ path: '/tmp/task-view-create.png' })
  await page.getByRole('textbox', { name: '名称', exact: true }).fill('需要 Review')
  await selectValue(page.getByRole('combobox', { name: '任务范围', exact: true }), 'review')
  await selectValue(page.getByRole('combobox', { name: '添加筛选', exact: true }), 'project')
  await selectValue(page.getByRole('combobox', { name: '视图筛选项目', exact: true }), 'atm')
  await page.locator('.task-view-defaults > summary').click()
  await selectValue(page.getByRole('combobox', { name: '布局', exact: true }), 'table')
  await selectValue(page.getByRole('combobox', { name: '分组', exact: true }), 'project')
  await selectValue(page.getByRole('combobox', { name: '排序', exact: true }), 'updated')
  await page.screenshot({ path: '/tmp/task-view-expanded.png' })
  await page.getByRole('button', { name: '创建视图', exact: true }).click()
  check(
    (await page.getByRole('status', { name: '匹配任务' }).textContent()) === '需要验收的任务',
    'filters not applied',
  )
  check(
    (await page.evaluate(() => new URL(location.href).searchParams.get('tableGroup'))) ===
      'project',
    'default grouping lost',
  )
  await open('管理视图')
  check(
    await page.getByRole('switch', { name: '显示视图 全部', exact: true }).isDisabled(),
    'all can be hidden',
  )
  await page.getByRole('button', { name: '上移 需要 Review', exact: true }).click()
  await page.getByRole('switch', { name: '显示视图 等待中', exact: true }).click()
  await page.screenshot({ path: '/tmp/task-view-manage.png' })
  await page.getByRole('button', { name: '关闭视图弹窗' }).click()
  await page.reload()
  check(
    (await page.getByRole('button', { name: /^等待中/ }).count()) === 0,
    'hidden view reappeared',
  )
  const names = await page.locator('.workbench-views > button').allTextContents()
  check(names.indexOf('需要 Review') < names.indexOf('旧视图'), 'order not persisted')
  await open('管理视图')
  await page.getByRole('switch', { name: '显示视图 需要 Review', exact: true }).click()
  check(
    (await page.evaluate(() => new URL(location.href).searchParams.get('focus'))) === 'all',
    'hiding active view did not select all',
  )
  await page.getByRole('switch', { name: '显示视图 需要 Review', exact: true }).click()
  const rename = page.getByRole('textbox', { name: '重命名 需要 Review', exact: true })
  await rename.fill('验收列表')
  await rename.press('Tab')
  check(
    (await page.getByRole('textbox', { name: '重命名 验收列表', exact: true }).count()) === 1,
    'rename failed',
  )
  await page.getByRole('button', { name: '关闭视图弹窗' }).click()
  await open('新增视图')
  await page.getByRole('textbox', { name: '名称', exact: true }).fill('不保存')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  check(
    (await page.getByRole('button', { name: '不保存', exact: true }).count()) === 0,
    'cancel saved draft',
  )
  await page.setViewportSize({ width: 390, height: 844 })
  await open('新增视图')
  await page.locator('.task-view-defaults > summary').click()
  check(
    await page.locator('dialog').evaluate((el) => el.scrollWidth <= el.clientWidth),
    'mobile dialog overflows',
  )
  await page.screenshot({ path: '/tmp/task-view-mobile.png' })
  return 'PASS: legacy data, input focus, create/filter/layout/group, visibility, protected all, reorder persistence, active fallback, rename, cancel, mobile width'
}
