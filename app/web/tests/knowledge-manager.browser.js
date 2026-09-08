// Run in an isolated Playwright browser with the local Vite server on port 5173.
;async (page) => {
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
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/knowledge-manager.html')
  await page.getByRole('button', { name: '管理收件箱的文档', exact: true }).click()
  check(
    (await page.getByRole('combobox', { name: '管理文档集合' }).inputValue()) === 'inbox',
    'collection scope lost',
  )
  await page.getByRole('checkbox', { name: '选择文档 接口设计草稿' }).waitFor()
  check(
    (await page.getByRole('checkbox', { name: /选择文档 / }).count()) === 2,
    'wrong scoped rows',
  )
  await page.getByRole('checkbox', { name: '选择当前列表全部文档' }).check()
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
  await page.getByRole('checkbox', { name: '选择当前列表全部文档' }).check()
  await page.getByRole('button', { name: '恢复为有效', exact: true }).click()
  await page.getByText('当前条件下没有文档。').waitFor()
  await page.getByRole('button', { name: '有效', exact: true }).click()
  await page.getByRole('checkbox', { name: '选择当前列表全部文档' }).check()
  await selectValue(page.getByRole('combobox', { name: '批量移动目标集合' }), 'reference')
  await page.getByRole('button', { name: '移动', exact: true }).click()
  await page.getByText('当前条件下没有文档。').waitFor()
  await selectValue(page.getByRole('combobox', { name: '管理文档集合' }), 'reference')
  await page.getByRole('button', { name: '全部', exact: true }).click()
  await page.getByRole('checkbox', { name: '选择文档 归档笔记' }).waitFor()
  check((await page.getByRole('checkbox', { name: /选择文档 / }).count()) === 3, 'move failed')
  await page.screenshot({ path: '/tmp/knowledge-manager.png' })
  await page.getByRole('checkbox', { name: '选择当前列表全部文档' }).check()
  await page.getByRole('button', { name: '草稿', exact: true }).click()
  await page.getByRole('button', { name: '全部', exact: true }).click()
  check(
    (await page.getByRole('checkbox', { name: '选择当前列表全部文档' }).isChecked()) === false,
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
