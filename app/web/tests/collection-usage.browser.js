// Run in an isolated browser with the local Vite fixture; all requests are mocked by the fixture.
// The connector card is the browser's only view of the CLI each connector drives
// (`dws` for dingtalk), so both the healthy and the expired-login reading are checked.
async (page) => {
  const check = (condition, message) => { if (!condition) throw new Error(message) }
  const url = 'http://127.0.0.1:5173/tests/fixtures/collection-usage.html'
  const card = '.collection-connector'

  await page.goto(`${url}?scenario=0`)
  await page.waitForSelector(card)
  check(await page.locator(card).getAttribute('class') === 'collection-connector tone-ok', 'a recovered connector was not marked ready')
  check(await page.getByText('251 次运行 · 失败 14（5.6%） · 读取 1,278 条', { exact: true }).isVisible(), 'today volume missing')
  check(await page.getByText('484 次运行 · 失败 45（9.3%） · 读取 26,503 条', { exact: true }).isVisible(), 'seven day volume missing')
  check(await page.getByText('最近 20 次里失败 6 次', { exact: true }).isVisible(), 'health window missing')
  check(await page.locator('.collection-connector-alert').count() === 0, 'a ready connector raised the blocking banner')
  check(await page.getByText('统计单位是采集运行，不是外部命令次数', { exact: false }).isVisible(), 'the run-versus-command caveat disappeared')

  await page.goto(`${url}?scenario=1`)
  await page.waitForSelector(card)
  check(await page.locator(card).getAttribute('class') === 'collection-connector tone-danger', 'an expired login was not marked as blocking')
  check(await page.getByText('登录已失效', { exact: true }).first().isVisible(), 'auth_required label missing')
  check(await page.getByText('当前连续失败 4 次', { exact: false }).isVisible(), 'consecutive failures missing')
  const alert = page.locator('.collection-connector-alert')
  check(await alert.count() === 1, 'the blocking banner did not appear')
  check(await alert.getByText('已连续失败 4 次，自动采集会跳过它。', { exact: true }).isVisible(), 'the banner did not say collection is skipped')
  check(await alert.getByText('/Users/example/.qoderwork/bin/dws auth login', { exact: false }).isVisible(), 'the way back in did not travel with the banner')
  check(await page.locator('.collection-connector-login').getByText('/Users/example/.qoderwork/bin/dws auth login', { exact: false }).isVisible(), 'the card did not offer the login command')

  await page.goto(`${url}?scenario=2`)
  await page.getByRole('button', { name: '删除来源', exact: true }).click()
  const confirmation = page.getByRole('tooltip')
  check(await confirmation.getByText('删除“Fixture 群”的采集配置？').isVisible(), 'source deletion confirmation is missing')
  await confirmation.getByRole('button', { name: '保留来源' }).click()
  await confirmation.waitFor({ state: 'hidden' })
  check((await page.evaluate(() => window.fixtureDeleteCalls())) === 0, 'cancelled source deletion sent a request')
  await page.getByRole('button', { name: '删除来源', exact: true }).click()
  await confirmation.getByRole('button', { name: '确认删除来源' }).click()
  check((await page.evaluate(() => window.fixtureDeleteCalls())) === 1, 'confirmed deletion did not send exactly one request')

  return 'PASS: connector health, blocking banner, and Popconfirm cancel/confirm for source deletion'
}
