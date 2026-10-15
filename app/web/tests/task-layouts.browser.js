async (page) => {
 const check = (value, message) => { if (!value) throw new Error(message) }
 for (const width of [1440, 390]) {
  await page.setViewportSize({width, height:900})
  for (const layout of ['kanban', 'swimlane', 'table']) {
   await page.goto(`http://127.0.0.1:5173/tests/fixtures/task-layouts.html?layout=${layout}&focus=all`)
   await page.locator('[data-task-id="t410"]').waitFor()
   check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `${layout} page overflow ${width}`)
   await page.getByRole('checkbox', {name:'选择 t410', exact:true}).check()
   check(await page.getByLabel('已打开任务').textContent() === '', 'checkbox opened task')
   await page.locator('[data-task-id="t410"]').click()
   check(await page.getByLabel('已打开任务').textContent() === 't410', 'card open failed')
   if (layout !== 'table') check(await page.locator('[data-task-id="t410"]').evaluate(el => getComputedStyle(el).alignItems === 'stretch'), 'tile alignment overridden')
   await page.screenshot({path:`/private/tmp/atm-${layout}-${width}.png`})
  }
 }
 return 'PASS: kanban/swimlane/table, desktop/mobile containment, selection and opening, tile alignment'
}
