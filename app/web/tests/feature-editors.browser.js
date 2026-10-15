// Exercises extracted detail, editor, composer, and draft modules using isolated fixtures.
async (page) => {
  page.setDefaultTimeout(10000)
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/ux-review.html')
  await page.getByRole('heading', { name: '核对任务交互', exact: true }).waitFor()
  await page.getByRole('button', { name: '更多任务操作', exact: true }).click()
  await page.getByText('查看完整任务文档', { exact: true }).click()
  await page.getByRole('region', { name: '完整任务文档正文' }).waitFor()
  await page.getByRole('menu').waitFor({ state: 'hidden' })
  await page.getByRole('region', { name: '完整任务文档正文' }).click()
  await page.keyboard.press('Escape')
  await page.getByRole('region', { name: '完整任务文档正文' }).waitFor({ state: 'hidden' })
  check(
    await page.getByRole('heading', { name: '核对任务交互', exact: true }).isVisible(),
    'Document Escape closed task detail',
  )
  await page.getByRole('button', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: /编辑任务/ })
  await editor.getByPlaceholder('任务标题', { exact: true }).fill('结构整理后的任务')
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: '放弃未保存的修改？' })
  await confirm.getByRole('button', { name: '继续编辑', exact: true }).click()
  check(
    (await editor.getByPlaceholder('任务标题', { exact: true }).inputValue()) ===
      '结构整理后的任务',
    'Unsaved edit was lost',
  )
  await editor.getByRole('button', { name: '保存修改', exact: true }).click()
  await editor.waitFor({ state: 'hidden' })
  await page.getByRole('heading', { name: '结构整理后的任务', exact: true }).waitFor()

  const knowledge =
    'http://127.0.0.1:5173/tests/fixtures/workspace-layout.html?scene=knowledge&interactive=1&collection=test&compose=document'
  await page.goto(knowledge)
  await page.evaluate(() => {
    for (const key of Object.keys(localStorage))
      if (key.startsWith('atm.web.knowledge.draft.')) localStorage.removeItem(key)
  })
  await page.reload()
  await page.getByPlaceholder('这篇文档要讲什么？').fill('需要恢复的知识草稿')
  await page.locator('#knowledge-content').fill('拆分组件后保留正文和草稿恢复行为。')
  await page.reload()
  await page.getByText(/发现 .* 份未提交草稿/).click()
  await page.getByRole('button', { name: '恢复这份草稿', exact: true }).first().click()
  check(
    (await page.getByPlaceholder('这篇文档要讲什么？').inputValue()) === '需要恢复的知识草稿',
    'Knowledge title not recovered',
  )
  check(
    (await page.locator('#knowledge-content').inputValue()) ===
      '拆分组件后保留正文和草稿恢复行为。',
    'Knowledge content not recovered',
  )
  await page.getByRole('button', { name: '预览 Markdown', exact: true }).click()
  check(
    await page
      .locator('.knowledge-editor-preview')
      .getByText('拆分组件后保留正文和草稿恢复行为。')
      .isVisible(),
    'Knowledge preview lost content',
  )
  await page.getByRole('button', { name: '继续编辑', exact: true }).click()
  check(
    (await page.locator('#knowledge-content').inputValue()) ===
      '拆分组件后保留正文和草稿恢复行为。',
    'Preview roundtrip lost content',
  )
  await page.goto(knowledge.replace('compose=document', 'compose=memory&tab=memory'))
  await page.getByRole('heading', { name: '新增共享记忆', exact: true }).waitFor()
  await page.locator('#knowledge-content').fill('独立的记忆内容')
  await page.goto(knowledge.replace('compose=document', 'compose=collection'))
  await page.getByRole('heading', { name: '新建知识集合', exact: true }).waitFor()
  await page.getByPlaceholder('如 engineering').fill('refactor-check')
  await page.getByPlaceholder('如工程实践').fill('结构回归集合')
  await page.setViewportSize({ width: 390, height: 844 })
  check(
    await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth),
    'Composer overflows on mobile',
  )
  return 'PASS: nested document Escape, shared task editor save/unsaved protection, knowledge draft restore/preview, memory/collection composers, mobile'
}
