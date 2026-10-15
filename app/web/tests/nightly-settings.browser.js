// Isolated settings fixture; no real workspace or configuration is modified.
async (page) => {
  const check = (ok, message) => { if (!ok) throw new Error(message) }
  await page.goto('http://127.0.0.1:5173/tests/fixtures/workspace-layout.html?scene=settings&interactive=1')
  await page.getByRole('heading', { name: '夜间会话采集', exact: true }).waitFor()
  check(await page.getByRole('checkbox', { name: 'Codex（主力）', exact: true }).isChecked(), 'Codex must be enabled by default')
  check(!await page.getByRole('checkbox', { name: 'QoderWork', exact: true }).isChecked(), 'Auxiliary source enabled by default')
  await page.evaluate(async () => {
    const original = window.fetch
    let data = (await (await original('/api/v1/settings.get')).json()).data
    data.preferences.session_sync_agents = 'codex'
    window.settingsWrites = []
    window.fetch = async (url, init) => {
      const method = String(url).split('/').at(-1)
      if (method === 'settings.business.save') {
        const body = JSON.parse(init.body)
        window.settingsWrites.push(body)
        data = { ...data, revision: `${data.revision}-saved`, preferences: { ...data.preferences, session_sync_agents: body.session_sync_agents } }
        return new Response(JSON.stringify({ data }))
      }
      if (method === 'settings.get') return new Response(JSON.stringify({ data }))
      return original(url, init)
    }
  })
  await page.getByRole('checkbox', { name: 'QoderWork', exact: true }).check()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await page.waitForFunction(() => window.settingsWrites[0]?.session_sync_agents === 'codex,qoderwork')
  await page.waitForFunction(() => Array.from(document.querySelectorAll('button')).some(button => button.textContent.includes('保存设置') && button.disabled))
  await page.getByRole('checkbox', { name: 'Codex（主力）', exact: true }).uncheck()
  await page.getByRole('checkbox', { name: 'QoderWork', exact: true }).uncheck()
  await page.getByRole('button', { name: '保存设置', exact: true }).click()
  await page.waitForFunction(() => window.settingsWrites[1]?.session_sync_agents === 'none')
  check(await page.evaluate(() => window.settingsWrites.every(write => Object.keys(write).every(key => ['revision', 'session_sync_agents'].includes(key)))), 'Source preference overwrote unrelated settings')
  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Settings overflow at ${width}`)
  }
  return 'PASS: Codex default, explicit auxiliary opt-in, disable all, scoped save, mobile/desktop layout'
}
