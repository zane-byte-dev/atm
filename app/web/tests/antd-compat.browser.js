// Guards the antd compat layer in src/design-system.css and the <label>-wrapped
// Switch/Checkbox pattern used by the settings screens.
async (page) => {
  const check = (ok, message) => {
    if (!ok) throw new Error(message)
  }
  await page.setViewportSize({ width: 1280, height: 900 })
  // playwright-cli reuses one browser across test files, and page.goto does not
  // reset the mouse. A previous test can leave the pointer over one of the probe
  // buttons, and `.workspace .button:hover` sets color: var(--accent) — which would
  // make the raw/antd comparison below fail on whichever element happens to be
  // hovered. Park the pointer somewhere neutral first.
  await page.mouse.move(0, 0)
  // The appearance test persists a theme to localStorage on the same origin. Clear
  // it and reload so DesignSystemProvider snapshots the same palette the elements
  // inherit — otherwise this compares two different themes and fails spuriously.
  await page.goto('http://127.0.0.1:5173/tests/fixtures/antd-probe.html')
  await page.evaluate(() => localStorage.removeItem('atm.web.theme.v1'))
  await page.reload()
  await page.mouse.move(0, 0)

  const box = async (probe) =>
    page.locator(`[data-probe="${probe}"]`).evaluate((el) => {
      const c = getComputedStyle(el)
      const r = el.getBoundingClientRect()
      return {
        h: Math.round(r.height),
        w: Math.round(r.width),
        pad: c.padding,
        margin: c.margin,
        display: c.display,
        radius: c.borderRadius,
        bg: c.backgroundColor,
        color: c.color,
        fs: c.fontSize,
        fw: c.fontWeight,
        border: c.borderTopWidth + ' ' + c.borderTopStyle + ' ' + c.borderTopColor,
      }
    })

  // 1. legacy classes stay authoritative: an antd Button wearing .button must match
  //    the native button wearing .button on every property the legacy CSS declares.
  for (const kind of ['button', 'primary']) {
    const raw = await box(`raw-${kind}`)
    const antd = await box(`antd-${kind}`)
    for (const prop of ['pad', 'radius', 'bg', 'color', 'fs', 'fw', 'border']) {
      check(
        raw[prop] === antd[prop],
        `.button legacy style lost on antd Button (${kind}): ${prop} raw=${raw[prop]} antd=${antd[prop]}`,
      )
    }
    check(
      Math.abs(raw.h - antd.h) <= 1,
      `height diverged for ${kind}: raw=${raw.h} antd=${antd.h}`,
    )
  }

  // 2. icon buttons stay square — antd's controlHeight and 15px inline padding must
  //    not leak into a class that pins its own width.
  const rawIcon = await box('raw-icon')
  const antdIcon = await box('antd-icon')
  check(
    Math.abs(antdIcon.w - antdIcon.h) <= 1,
    `antd icon button is not square: ${antdIcon.w}x${antdIcon.h}`,
  )
  check(
    Math.abs(antdIcon.w - rawIcon.w) <= 2 && Math.abs(antdIcon.h - rawIcon.h) <= 2,
    `icon button size diverged: raw=${rawIcon.w}x${rawIcon.h} antd=${antdIcon.w}x${antdIcon.h}`,
  )

  // 3. a <label> wrapping an antd Switch toggles exactly once per click, whether the
  //    user clicks the descriptive text or the control itself. A double-fire here
  //    would silently revert every settings toggle.
  const count = () => page.locator('output[aria-label="开关切换次数"]').textContent()
  const state = () => page.locator('output[aria-label="开关状态"]').textContent()
  check((await count()) === '0', 'switch started dirty')

  await page.locator('[data-probe="switch-label"] span').first().click()
  check((await count()) === '1', `clicking the label text fired ${(await count())} times, expected 1`)
  check((await state()) === 'on', 'clicking the label text did not turn the switch on')

  await page.locator('[data-probe="antd-switch-in-label"]').click()
  check((await count()) === '2', `clicking the control fired ${(await count())} times total, expected 2`)
  check((await state()) === 'off', 'clicking the control did not turn the switch off')

  // 4. the same for a <label>-wrapped Checkbox
  const checkState = () => page.locator('output[aria-label="勾选状态"]').textContent()
  await page.locator('[data-probe="checkbox-label"] span').first().click()
  check((await checkState()) === 'on', 'clicking the label text did not check the box')
  await page.locator('[data-probe="checkbox-label"] span').first().click()
  check((await checkState()) === 'off', 'second click did not uncheck the box')

  // 5. the Switch keeps its switch semantics
  const sw = await page.locator('[data-probe="antd-switch-in-label"]').evaluate((el) => ({
    tag: el.tagName,
    role: el.getAttribute('role'),
    hasChecked: el.hasAttribute('aria-checked'),
    name: el.getAttribute('aria-label'),
  }))
  check(sw.tag === 'BUTTON' && sw.role === 'switch', `switch semantics lost: ${JSON.stringify(sw)}`)
  check(sw.hasChecked && sw.name === '开关标题', `switch a11y lost: ${JSON.stringify(sw)}`)

  // 6. the feedback components render at all (used by the later migration phases)
  for (const probe of ['antd-empty', 'antd-spin', 'antd-notice', 'antd-input', 'antd-radio']) {
    const b = await box(probe)
    check(b.h > 0 && b.w > 0, `${probe} did not render`)
  }

  // 7. the shared Notice component maps onto antd Alert without losing the ATM
  //    danger palette. `.notice` is used at 45 call sites plus 10 hand-written divs,
  //    so this is the highest-leverage conversion in the feedback phase.
  const rawNotice = await box('raw-notice')
  const antdNotice = await box('antd-notice')
  for (const prop of ['pad', 'margin', 'radius', 'bg', 'color', 'fs', 'display']) {
    check(
      rawNotice[prop] === antdNotice[prop],
      `.notice legacy style lost on antd Alert: ${prop} raw=${rawNotice[prop]} antd=${antdNotice[prop]}`,
    )
  }
  // antd v6 renamed Alert's `message` prop to `title`; confirm the title rendered.
  check(
    (await page.locator('[data-probe="antd-notice"] .ant-alert-title').textContent()).includes(
      '读取失败',
    ),
    'Alert title prop did not render',
  )
  // The retry control must land in Alert's action slot as a real antd Button.
  check(
    (await page.locator('[data-probe="antd-notice"] .ant-alert-actions .ant-btn').count()) === 1,
    'retry action is not an antd Button in the Alert action slot',
  )
  // Alert hardcodes role="alert" but spreads restProps after it, so role is overridable.
  check(
    (await page.locator('[data-probe="antd-notice"]').getAttribute('role')) === 'alert',
    'error notice lost role=alert',
  )
  check(
    (await page.locator('[data-probe="antd-status-notice"]').getAttribute('role')) === 'status',
    'role="status" override on Alert did not take effect',
  )

  // 8. the real shared Notice component (45 call sites) renders as an antd Alert
  //    wearing the legacy .notice palette, with retry in the action slot.
  const notice = page.locator('[data-probe="real-notice-wrap"] .notice')
  check((await notice.count()) === 1, 'Notice did not render a single .notice element')
  const noticeInfo = await notice.evaluate((el) => {
    const c = getComputedStyle(el)
    return {
      isAlert: el.className.includes('ant-alert'),
      role: el.getAttribute('role'),
      bg: c.backgroundColor,
      color: c.color,
      pad: c.padding,
      radius: c.borderRadius,
      text: el.textContent || '',
      actionBtn: !!el.querySelector('.ant-alert-actions .ant-btn'),
      sectionFlex: el.querySelector('.ant-alert-section')
        ? getComputedStyle(el.querySelector('.ant-alert-section')).flexGrow
        : null,
    }
  })
  check(noticeInfo.isAlert, 'Notice is not an antd Alert')
  check(noticeInfo.role === 'alert', `Notice lost role=alert: ${noticeInfo.role}`)
  check(
    noticeInfo.text.includes('读取失败：连接中断'),
    `Notice title did not render the error text: ${noticeInfo.text}`,
  )
  check(noticeInfo.actionBtn, 'Notice retry is not in the Alert action slot')
  // The ATM danger palette must survive; antd's own error colours would differ.
  const dangerSoft = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--danger-soft').trim(),
  )
  const danger = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--danger').trim(),
  )
  const toRgb = (hex) => {
    const m = hex.match(/[\da-f]{2}/gi)
    return m ? `rgb(${m.slice(0, 3).map((v) => parseInt(v, 16)).join(', ')})` : hex
  }
  check(
    noticeInfo.bg === toRgb(dangerSoft),
    `Notice background is not --danger-soft: ${noticeInfo.bg} vs ${toRgb(dangerSoft)}`,
  )
  check(
    noticeInfo.color === toRgb(danger),
    `Notice colour is not --danger: ${noticeInfo.color} vs ${toRgb(danger)}`,
  )
  // antd's section must take the remaining width so the retry button sits right,
  // replacing the old `.notice span { flex: 1 }` trick.
  check(
    noticeInfo.sectionFlex === '1',
    `Alert section does not flex: ${noticeInfo.sectionFlex}`,
  )

  // 9. QueryLoading keeps its live region and now uses antd Spin as the indicator
  const loading = page.locator('.probe-loading')
  const loadingInfo = await loading.evaluate((el) => ({
    role: el.getAttribute('role'),
    live: el.getAttribute('aria-live'),
    atomic: el.getAttribute('aria-atomic'),
    hasSpin: !!el.querySelector('.ant-spin'),
    spinning: !!el.querySelector('.ant-spin-spinning'),
    text: (el.textContent || '').trim(),
    noLucideSvg: !el.querySelector('svg.lucide'),
  }))
  check(loadingInfo.role === 'status', `QueryLoading lost role=status: ${loadingInfo.role}`)
  check(loadingInfo.live === 'polite' && loadingInfo.atomic === 'true', 'QueryLoading lost its live region attributes')
  check(loadingInfo.hasSpin && loadingInfo.spinning, 'QueryLoading is not using a spinning antd Spin')
  check(loadingInfo.text.includes('正在读取任务…'), `QueryLoading text lost: ${loadingInfo.text}`)
  check(loadingInfo.noLucideSvg, 'QueryLoading still renders the lucide icon')

  // The shared empty state keeps its heading and detail while using antd Empty.
  const empty = page.locator('.activity-empty')
  check((await empty.count()) === 1, 'shared empty state is missing')
  check(await empty.evaluate((el) => el.classList.contains('ant-empty')), 'shared empty state is not antd Empty')
  check((await empty.textContent()).includes('暂无会话'), 'empty state title is missing')
  check((await empty.textContent()).includes('等待新的会话记录。'), 'empty state detail is missing')

  const inlineError = page.locator('.as-inline-error')
  const inlineStyle = await inlineError.evaluate((el) => {
    const style = getComputedStyle(el)
    return {
      isAlert: el.classList.contains('ant-alert'),
      role: el.getAttribute('role'),
      padding: style.padding,
      background: style.backgroundColor,
      color: getComputedStyle(el.querySelector('.ant-alert-title')).color,
    }
  })
  check(inlineStyle.isAlert && inlineStyle.role === 'alert', 'inline error lost Alert semantics')
  check(inlineStyle.padding === '0px', `inline error gained padding: ${inlineStyle.padding}`)
  check(inlineStyle.background === 'rgba(0, 0, 0, 0)', 'inline error gained a background')

  return 'PASS: legacy .button/.primary authoritative, square icon buttons, label-wrapped Switch and Checkbox fire once, switch semantics, .notice maps to Alert with ATM palette and overridable role, real Notice keeps danger palette + action slot + flexed section, QueryLoading keeps live region and uses Spin'
}
