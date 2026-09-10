import assert from 'node:assert/strict'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { randomUUID } from 'node:crypto'

const dir = dirname(fileURLToPath(import.meta.url))
const runtime = join(dir, '.runtime')
const output = join(dir, 'evidence/official-reset-20260908')
const credentials = JSON.parse(await readFile(join(runtime, 'app-credentials.private.json'), 'utf8'))
const fixtures = JSON.parse(await readFile(join(runtime, 'browser-fixtures.private.json'), 'utf8'))
const base = 'http://127.0.0.1:38100'
assert.equal(credentials.source, 'synthetic-local-only')
assert.equal(fixtures.source, 'synthetic-local-only')
assert.equal(credentials.base_url, base)
assert.equal(fixtures.base_url, base)
const req = createRequire(join(homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/package.json'))
const { chromium } = req('playwright')
await mkdir(output, { recursive: true })

async function http(path, { token, body, key, expected = 200 } = {}) {
  const response = await fetch(base + '/api/v1' + path, {
    signal: AbortSignal.timeout(30000),
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(key ? { 'Idempotency-Key': key } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  assert.equal(response.status, expected, `${path}: HTTP ${response.status}`)
  const payload = await response.json()
  if (expected === 200) assert.equal(payload.code, 0)
  return payload.data
}
async function login(account) {
  return http('/auth/login', { body: { email: account.email, password: account.password } })
}
const admin = await login(credentials.admin)
const token = admin.access_token
const list = async path => {
  const data = await http(path + '?page=1&page_size=100', { token })
  assert(data.total <= 100, 'Acceptance assumes fewer than 100 synthetic records')
  return data.items
}
const member = fixtures.users.find(user => user.name === 'active_reset_marker')
const userLogin = await login(member)
await http('/admin/carpool/reset-batches/official', { body: { scope_id: 1, confirmed: true }, key: randomUUID(), expected: 401 })
await http('/admin/carpool/reset-batches/official', { token: userLogin.access_token, body: { scope_id: 1, confirmed: true }, key: randomUUID(), expected: 403 })
await http('/admin/carpool/reset-batches/official', { token, body: { scope_id: 1, confirmed: false }, key: randomUUID(), expected: 400 })
await http('/admin/carpool/reset-batches/official', { token, body: { scope_id: 1, confirmed: true }, expected: 400 })

const beforeTerms = await list('/admin/carpool/terms')
const beforeCycles = await list('/admin/carpool/cycles')
const beforeBatches = await list('/admin/carpool/reset-batches')
const balances = new Map()
for (const id of new Set(beforeTerms.map(term => term.user_id))) {
  balances.set(id, (await http('/admin/users/' + id, { token })).balance)
}
const browser = await chromium.launch({ headless: true, executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' })
const errors = []
let resetRequests = 0
let submittedKey
let batch
async function verifyBrowser() {
try {
  const context = await browser.newContext({ viewport: { width: 1365, height: 1000 }, locale: 'zh-CN' })
  await context.addInitScript(({ admin }) => {
    localStorage.setItem('auth_token', admin.access_token)
    localStorage.setItem('auth_user', JSON.stringify(admin.user))
    localStorage.setItem('token_expires_at', String(Date.now() + 3600000))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem(`admin_guide_${admin.user.id}_${admin.user.role}_v4_interactive`, 'true')
  }, { admin })
  const page = await context.newPage()
  page.on('pageerror', err => errors.push(err.message))
  await page.goto(base + '/admin/carpool', { waitUntil: 'networkidle' })
  await page.getByRole('tab', { name: '重置运营' }).click()
  const open = page.getByRole('button', { name: '官方全局重置', exact: true })
  const dialog = page.getByRole('dialog')
  await open.click()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  assert.deepEqual(await list('/admin/carpool/reset-batches'), beforeBatches)
  for (const width of [1365, 390]) {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
    await open.click()
    await dialog.waitFor()
    await page.waitForFunction(() => {
      const element = document.querySelector('[role="dialog"]')
      return element && getComputedStyle(element).opacity === '1'
    })
    const panel = dialog.locator('.modal-content')
    await panel.screenshot({ path: join(output, `confirmation-${width}.png`), animations: 'disabled' })
    const bounds = await panel.boundingBox()
    assert(bounds.x >= 0 && bounds.x + bounds.width <= width + 1)
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
  }
  if (process.env.OFFICIAL_RESET_SCREENSHOTS_ONLY === '1') {
    assert.deepEqual(await list('/admin/carpool/reset-batches'), beforeBatches)
    console.log('OFFICIAL_RESET_SCREENSHOTS_VERIFIED no reset submitted')
    return
  }
  await page.setViewportSize({ width: 1365, height: 1000 })
  let releaseRequest
  let sawRequest
  const intercepted = new Promise(resolve => { sawRequest = resolve })
  const released = new Promise(resolve => { releaseRequest = resolve })
  await page.route('**/api/v1/admin/carpool/reset-batches/official', async route => {
    resetRequests++
    submittedKey = route.request().headers()['idempotency-key']
    sawRequest()
    await released
    await route.continue()
  })
  await open.click()
  const confirm = dialog.getByRole('button', { name: /确认.*重置|立即重置/ })
  const responsePromise = page.waitForResponse(response => response.url().endsWith('/reset-batches/official') && response.request().method() === 'POST')
  await confirm.click()
  await intercepted
  assert.equal(await confirm.isDisabled(), true)
  releaseRequest()
  const response = await responsePromise
  assert.equal(response.status(), 200)
  batch = (await response.json()).data
  assert.equal(batch.trigger_kind, 'official')
  assert.equal(batch.status, 'completed')
  assert.equal(resetRequests, 1)
  await dialog.waitFor({ state: 'hidden' })
  await page.screenshot({ path: join(output, 'completed-1365.png'), fullPage: true })

  const replay = await http('/admin/carpool/reset-batches/official', { token, body: { scope_id: 1, confirmed: true }, key: submittedKey })
  assert.equal(replay.id, batch.id)
  assert.equal(replay.effective_at, batch.effective_at)
  const afterTerms = await list('/admin/carpool/terms')
  const afterCycles = await list('/admin/carpool/cycles')
  const effective = Date.parse(batch.effective_at)
  let affected = 0
  const changes = []
  for (const old of beforeTerms) {
    const current = afterTerms.find(term => term.id === old.id)
    assert.equal(current.expires_at, old.expires_at)
    const eligible = ['pending', 'active'].includes(old.status) && Date.parse(old.starts_at) <= effective && effective < Date.parse(old.expires_at)
    const oldCycles = beforeCycles.filter(cycle => cycle.term_id === old.id)
    const currentCycles = afterCycles.filter(cycle => cycle.term_id === old.id)
    if (eligible) {
      affected++
      assert.equal(current.current_cycle.cycle_no, old.current_cycle.cycle_no + 1)
      assert.equal(currentCycles.length, oldCycles.length + 1)
      assert.equal(Number(current.current_cycle.base_balance_usd), Number(current.current_cycle.base_quota_usd))
      assert.equal(Date.parse(current.current_cycle.starts_at), effective)
      assert.equal(Date.parse(current.current_cycle.ends_at), Math.min(effective + 7 * 86400000, Date.parse(old.expires_at)))
      const closed = currentCycles.find(cycle => cycle.id === old.current_cycle.id)
      assert(['closed', 'closing'].includes(closed.state))
      assert.equal(Date.parse(closed.ends_at), effective)
    } else {
      assert.deepEqual(currentCycles, oldCycles)
    }
    changes.push({ term_id: old.id, affected: eligible, old_cycle: old.current_cycle?.cycle_no ?? null, new_cycle: current.current_cycle?.cycle_no ?? null, expiry_unchanged: true })
  }
  assert.equal(batch.target_count, affected)
  assert(affected >= 2)
  assert.equal((await list('/admin/carpool/reset-batches')).length, beforeBatches.length + 1)
  for (const [id, balance] of balances) assert.equal((await http('/admin/users/' + id, { token })).balance, balance)

  await context.close()
  const userContext = await browser.newContext({ viewport: { width: 390, height: 844 }, locale: 'zh-CN' })
  await userContext.addInitScript(({ login }) => {
    localStorage.setItem('auth_token', login.access_token)
    localStorage.setItem('auth_user', JSON.stringify(login.user))
    localStorage.setItem('token_expires_at', String(Date.now() + 3600000))
    localStorage.setItem('sub2api_locale', 'zh')
    localStorage.setItem(`user_guide_${login.user.id}_${login.user.role}_v4_interactive`, 'true')
  }, { login: userLogin })
  const userPage = await userContext.newPage()
  userPage.on('pageerror', err => errors.push(err.message))
  await userPage.goto(base + '/carpool', { waitUntil: 'networkidle' })
  const details = await http('/user/carpool/details', { token: userLogin.access_token })
  assert((await userPage.getByTestId('carpool-current-cycle').innerText()).includes(String(details.term.current_cycle_no)))
  assert.equal(details.term.reset_events.filter(event => Date.parse(event.occurred_at) === effective).length, 1)
  await userPage.getByRole('heading', { name: '账期记录' }).locator('..').screenshot({ path: join(output, 'user-history-390.png') })
  assert.deepEqual(errors, [])
  const result = { verified_at: new Date().toISOString(), batch_id: batch.id, target_count: affected, effective_at: batch.effective_at, authorization_checks: 4, cancel_no_mutation: true, in_flight_disabled: true, request_replay_once: true, ordinary_balances_unchanged: true, desktop_mobile_dialog: true, changes }
  await writeFile(join(output, 'http-browser-results.json'), JSON.stringify(result, null, 2) + '\n')
  console.log(JSON.stringify(result, null, 2))
} finally {
  await browser.close()
}
}
await verifyBrowser()
