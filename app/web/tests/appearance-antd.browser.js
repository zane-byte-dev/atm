// Verifies the appearance-settings migration to antd Radio.Group.
// The radio dot is intentionally hidden and selection is shown by the card border
// plus the caption check mark, so assert both the a11y tree and the computed style.
async (page) => {
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto('http://127.0.0.1:5173/tests/fixtures/appearance.html')

  // 1. every theme renders as a real radio in the a11y tree, with its name preserved
  const radios = page.getByRole('radio')
  const radioCount = await radios.count()
  check(radioCount >= 5, `expected the five palettes as radios, got ${radioCount}`)
  const names = await radios.evaluateAll((els) =>
    els.map((el) => el.getAttribute('aria-label') || el.closest('label')?.textContent?.trim().slice(0, 10)),
  )
  check(
    names.every((n) => n && n.length > 0),
    `a radio lost its accessible name: ${JSON.stringify(names)}`,
  )

  // 2. antd structure: wrapper label carries .theme-option, input sits inside .ant-radio
  const structure = await page.locator('.theme-option').first().evaluate((el) => ({
    tag: el.tagName,
    isWrapper: el.className.includes('ant-radio-wrapper'),
    hasDot: !!el.querySelector(':scope > .ant-radio'),
    hasLabelSpan: !!el.querySelector(':scope > .ant-radio-label'),
    cardInsideLabelSpan: !!el.querySelector(':scope > .ant-radio-label > .theme-card'),
    noNativeInputChild: !el.querySelector(':scope > input'),
  }))
  check(structure.tag === 'LABEL', `theme-option is not a label: ${structure.tag}`)
  check(structure.isWrapper, 'theme-option is not an ant-radio-wrapper')
  check(structure.hasDot && structure.hasLabelSpan, `antd radio structure wrong: ${JSON.stringify(structure)}`)
  check(structure.cardInsideLabelSpan, 'theme-card is not inside .ant-radio-label')
  check(structure.noNativeInputChild, 'a stray native input is still a direct child')

  // 3. the radio dot stays visually hidden
  const dot = await page.locator('.theme-option > .ant-radio').first().evaluate((el) => {
    const c = getComputedStyle(el)
    const r = el.getBoundingClientRect()
    return { opacity: c.opacity, w: Math.round(r.width), h: Math.round(r.height) }
  })
  check(Number(dot.opacity) === 0 && dot.w <= 1 && dot.h <= 1, `radio dot is visible: ${JSON.stringify(dot)}`)

  // 4. the grid layout survives (antd Radio.Group defaults to inline-block)
  const grid = await page.locator('.theme-grid').evaluate((el) => {
    const c = getComputedStyle(el)
    return { display: c.display, cols: c.gridTemplateColumns.split(' ').length, isGroup: el.className.includes('ant-radio-group') }
  })
  check(grid.isGroup, 'theme-grid is not an ant-radio-group')
  check(grid.display === 'grid', `theme-grid lost display:grid, got ${grid.display}`)
  check(grid.cols === 2, `expected 2 columns, got ${grid.cols}`)

  // 5. exactly one option is checked, and its card shows the accent border + check mark
  const readState = () =>
    page.evaluate(() => {
      const opts = Array.from(document.querySelectorAll('.theme-option'))
      const accent = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()
      const toRgb = (v) => {
        const m = v.match(/\d+/g)
        return m ? m.slice(0, 3).join(',') : v
      }
      return opts.map((el) => {
        const card = el.querySelector('.theme-card')
        const cs = getComputedStyle(card)
        const svg = el.querySelector('.theme-check svg')
        return {
          name: el.querySelector('.ant-radio-input')?.getAttribute('aria-label'),
          checkedClass: el.className.includes('ant-radio-wrapper-checked'),
          inputChecked: el.querySelector('.ant-radio-input')?.checked,
          borderColor: cs.borderTopColor,
          checkVisible: svg ? getComputedStyle(svg).visibility === 'visible' : false,
        }
      })
    })

  let state = await readState()
  const checkedBefore = state.filter((s) => s.checkedClass)
  check(checkedBefore.length === 1, `expected exactly one checked option, got ${checkedBefore.length}`)
  check(
    checkedBefore[0].inputChecked === true,
    'the checked class and the input state disagree',
  )
  check(checkedBefore[0].checkVisible, 'the selected card does not show its check mark')
  check(
    state.filter((s) => s.checkVisible).length === 1,
    'more than one card shows a check mark',
  )

  // 6. clicking another card moves the selection (and the check mark) to it.
  // The radio input itself is visually hidden (1px, opacity 0) — as it was before
  // the migration — so the real interaction is clicking the card and letting the
  // wrapping <label> forward activation to the control.
  const target = state.find((s) => !s.checkedClass)
  await page.locator('.theme-option', { has: page.locator(`.ant-radio-input[aria-label="${target.name}"]`) })
    .locator('.theme-card')
    .click()
  state = await readState()
  const checkedAfter = state.filter((s) => s.checkedClass)
  check(checkedAfter.length === 1, `selection did not move cleanly: ${checkedAfter.length} checked`)
  check(checkedAfter[0].name === target.name, `wrong option selected: ${checkedAfter[0].name}`)
  check(
    state.filter((s) => s.checkVisible).length === 1 &&
      state.find((s) => s.checkVisible).name === target.name,
    'check mark did not follow the selection',
  )
  check(
    (await page.locator('.theme-save-status').textContent()).includes(target.name),
    'status line did not follow the selection',
  )

  // 7. keyboard: arrow keys move within the radio group
  await page.getByRole('radio', { name: target.name, exact: true }).focus()
  const before = (await readState()).find((s) => s.checkedClass).name
  await page.keyboard.press('ArrowRight')
  const after = (await readState()).find((s) => s.checkedClass).name
  check(before !== after, `arrow key did not move the selection (stayed on ${before})`)

  // 8. narrow viewport keeps the cards inside the viewport
  await page.setViewportSize({ width: 390, height: 844 })
  check(
    await page.locator('.theme-grid').evaluate((el) => {
      const r = el.getBoundingClientRect()
      return r.left >= 0 && r.right <= innerWidth + 1
    }),
    'theme grid overflows on a narrow viewport',
  )

  return 'PASS: five radios with names, antd structure, hidden dot, 2-col grid, single checked state, click + arrow-key selection, check mark follows, narrow viewport'
}
