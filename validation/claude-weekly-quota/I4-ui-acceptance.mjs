/**
 * PREPARED ONLY. No browser has been installed, launched or connected for this file.
 * Run only after the human authorizes the substitute browser, using the isolated
 * candidate receipt. Real writes are restricted to fresh synthetic user C:
 * Claude override3→4 and card Open100/Renew75 with a real idempotent replay.
 * A/B latency, retry and expiry cases use explicitly mocked HTTP only.
 * No HAR, trace, storage-state, HTML, console payload or credential dump is saved.
 */
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { createHash } from 'node:crypto'

const argv = process.argv.slice(2)
const option = name => { const i = argv.indexOf(name); return i < 0 ? undefined : argv[i + 1] }
function ensure(value, code) { if (!value) throw new Error(code) }
const wait = () => { let resolve; const promise = new Promise(yes => { resolve = yes }); return { promise, resolve } }
const envelope = data => ({ code: 0, message: 'success', data })
const unwrap = value => value && typeof value === 'object' && 'code' in value ? value.data : value
const digest = value => createHash('sha256').update(JSON.stringify(value)).digest('hex')
async function bounded(promise) {
  let timer
  try { return await Promise.race([promise, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('SCENARIO_WAIT_TIMEOUT')), 15000) })]) }
  finally { clearTimeout(timer) }
}

async function run() {
  ensure(argv.includes('--execute-authorized'), 'PREPARED_ONLY_HUMAN_BROWSER_AUTHORIZATION_REQUIRED')
  const receiptPath = option('--receipt')
  ensure(receiptPath && path.isAbsolute(receiptPath), 'ABSOLUTE_ISOLATED_RECEIPT_REQUIRED')
  const receipt = JSON.parse(await readFile(receiptPath, 'utf8'))
  ensure(receipt.synthetic_only === true && receipt.external_egress === false, 'SYNTHETIC_NO_EGRESS_RECEIPT_REQUIRED')
  const base = new URL(receipt.url)
  ensure(base.protocol === 'http:' && ['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname), 'LOOPBACK_ONLY')
  ensure(receipt.private_fixture_path && path.isAbsolute(receipt.private_fixture_path), 'PRIVATE_FIXTURE_REQUIRED')
  const fixture = JSON.parse(await readFile(receipt.private_fixture_path, 'utf8'))
  ensure(new URL(fixture.URL).origin === base.origin && fixture.Source === receipt.source_sha256, 'FIXTURE_IMAGE_RECEIPT_MISMATCH')
  ensure(fixture.AEmail && fixture.BEmail && fixture.AdminEmail && fixture.Password && fixture.AKey && fixture.AKey2 && fixture.GPTKey, 'SYNTHETIC_IDENTITIES_REQUIRED')
  ensure(Number.isSafeInteger(fixture.C) && fixture.C > 0 && fixture.C !== fixture.A && fixture.C !== fixture.B && fixture.CEmail, 'FRESH_DEDICATED_BROWSER_USER_C_REQUIRED')
  const cGroup = fixture.CGroup || fixture.Group
  const playwrightModule = option('--playwright-module')
  // Nothing imports Playwright, installs packages, starts or attaches a browser
  // until all authorization and isolated-receipt checks above have succeeded.
  const { chromium } = await import(playwrightModule ? pathToFileURL(playwrightModule).href : 'playwright')
  const browser = await chromium.launch({ headless: !argv.includes('--headed') })
  const out = path.resolve(option('--out') || path.join(path.dirname(receiptPath), 'I4-browser-authorized-output'))
  await mkdir(out, { recursive: true })
  const report = { prepared_only: false, source: fixture.Source, candidate_image: fixture.ImageID, synthetic_only: true, external_upstream_called: false, real_management_mutations: false, real_financial_mutations: false, mutation_scope: 'dedicated synthetic user C only', cases: [] }
  const contexts = []
  async function testcase(name, layer, fn) {
    try { await fn(); report.cases.push({ name, layer, pass: true }) }
    catch { report.cases.push({ name, layer, pass: false, error: 'ASSERTION_OR_BROWSER_FAILURE_REQUIRES_PRIVATE_LOCAL_INSPECTION' }); throw new Error(name) }
  }
  async function context(email, locale = 'en') {
    const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: locale === 'zh' ? 'zh-CN' : 'en-US' })
    contexts.push(ctx)
    // Prevent accidental links/assets from escaping this synthetic application.
    await ctx.route('**/*', route => {
      const url = new URL(route.request().url())
      return ['data:', 'blob:'].includes(url.protocol) || url.origin === base.origin ? route.continue() : route.abort('blockedbyclient')
    })
    await ctx.addInitScript(value => localStorage.setItem('sub2api_locale', value), locale)
    const page = await ctx.newPage()
    page.setDefaultTimeout(12000)
    await page.goto(new URL('/login', base).href)
    await page.locator('#email').fill(email)
    await page.locator('#password').fill(fixture.Password)
    const loggedIn = page.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/auth/login' && r.request().method() === 'POST')
    await page.locator('form button[type="submit"]').click()
    ensure((await loggedIn).ok(), 'SYNTHETIC_LOGIN_FAILED')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))
    return page
  }
  async function jsonResponse(response) { ensure(response.ok(), 'READ_ENDPOINT_FAILED'); return unwrap(await response.json()) }
  async function authenticatedGet(page, route) {
    return page.evaluate(async target => {
      const response = await fetch(target, { headers: { Authorization: `Bearer ${localStorage.getItem('auth_token')}` } })
      if (!response.ok) return { failed: true }
      const data = await response.json(); return data && 'code' in data ? data.data : data
    }, `/api/v1${route}`)
  }
  async function visibleText(locator) { await locator.waitFor({ state: 'visible' }); return (await locator.innerText()).replace(/\s+/g, ' ').trim() }
  function amountText(value) { const [whole, fraction = ''] = value.split('.'); return `$${whole}.${fraction.replace(/0+$/, '').padEnd(2, '0')}` }
  async function canonicalCard(page, expected) {
    const card = page.getByTestId('claude-quota-card').filter({ has: page.getByTestId('quota-quota').filter({ hasText: amountText(expected) }) }).first()
    await card.waitFor({ state: 'visible' })
    ensure((await visibleText(card.getByTestId('quota-quota'))) === amountText(expected), 'CANONICAL_ALLOWANCE_NOT_VISIBLE')
    return card
  }
  async function userRow(page, email) {
    const search = page.locator('input.input.pl-10').first()
    await search.fill(email)
    const row = page.locator('tbody tr').filter({ hasText: email })
    await row.first().waitFor({ state: 'visible' })
    ensure(await row.count() === 1, 'SYNTHETIC_USER_ROW_NOT_UNIQUE')
    return row
  }
  async function quotaModal(page, email, groupID = fixture.Group) {
    const row = await userRow(page, email)
    await row.locator('.action-menu-trigger').click()
    await page.locator('.action-menu-content').getByRole('button', { name: 'Manage Claude card', exact: true }).click()
    const dialog = page.getByRole('dialog').filter({ has: page.getByTestId('quota-group') })
    await dialog.getByTestId('quota-group').selectOption(String(groupID))
    await dialog.getByTestId('quota-amount').waitFor({ state: 'visible' })
    return dialog
  }
  async function closeModal(page) { await page.getByRole('dialog').last().getByRole('button', { name: 'Close modal', exact: true }).click() }
  async function previewData(page, userID, amount) {
    const summary = await authenticatedGet(page, `/admin/users/${userID}/subscription-quota?group_id=${fixture.Group}`)
    ensure(summary && !summary.failed, 'PREVIEW_SCOPE_READ_FAILED')
    const start = new Date(summary.server_time)
    return { group_id: fixture.Group, effective_quota_usd: `${amount}.00000000`, proposed_starts_at: start.toISOString(), proposed_expires_at: new Date(+start + 168 * 3600_000).toISOString(), server_time: summary.server_time, current_revision: summary.revision, warnings: [], current_term: summary.current_term, next_terms: summary.next_terms }
  }
  let a, b, admin
  try {
    await testcase('real-login-first-Keys-profile-two-independent-pools', 'real-browser + actual display HTTP', async () => {
      a = await context(fixture.AEmail)
      const profileResponse = a.waitForResponse(r => new URL(r.url()).pathname === '/api/v1/user/profile')
      await a.goto(new URL('/keys', base).href)
      const profile = await jsonResponse(await profileResponse)
      ensure(profile.id === fixture.A && profile.default_claude_subscription_concurrency === 3 && profile.claude_subscription_concurrency === 2 && profile.effective_claude_subscription_concurrency === 2, 'A_DEFAULT_OVERRIDE_PROJECTION')
      ensure(profile.concurrency === 5 && profile.claude_concurrency_status === 'available' && profile.legacy_concurrency_status === 'available', 'INDEPENDENT_PROJECTION_UNAVAILABLE')
      ensure((await visibleText(a.getByTestId('claude-scope-stats').first())).endsWith('/ 2'), 'A_FIRST_RENDER_CLAUDE_LIMIT')
      ensure((await visibleText(a.getByTestId('legacy-scope-stats').first())).endsWith('/ 5'), 'A_FIRST_RENDER_GPT_LIMIT')
      ensure((await visibleText(a.getByTestId('claude-scope-policy').first())).includes('3'), 'SERVER_DEFAULT_NOT_RENDERED')
    })
    await testcase('real-A100-B300-canonical-summary', 'real-browser + actual summary HTTP', async () => {
      await a.goto(new URL('/subscriptions', base).href)
      await canonicalCard(a, '100.00000000')
      b = await context(fixture.BEmail)
      await b.goto(new URL('/subscriptions', base).href)
      await canonicalCard(b, '300.00000000')
      ensure((await visibleText(b.getByTestId('claude-scope-stats').first())).endsWith('/ 3'), 'B_INHERITED_CLAUDE_LIMIT')
    })
    await testcase('real-two-Keys-one-card-and-GPT-keeps-own-projection', 'real-browser + actual read-only /v1/usage', async () => {
      const p = await a.context().newPage()
      const results = []
      for (const key of [fixture.AKey, fixture.AKey2, fixture.GPTKey]) {
        await p.goto(new URL('/key-usage', base).href)
        await p.locator('main input[type="password"]').fill(key)
        const reply = p.waitForResponse(r => new URL(r.url()).pathname === '/v1/usage')
        await p.locator('main input[type="password"]').press('Enter')
        const value = await jsonResponse(await reply)
        ensure(value && !value.failed, 'KEY_USAGE_READ_FAILED')
        if (value.quota_mode === 'term') await canonicalCard(p, '100.00000000')
        results.push(value)
      }
      const term = v => v.quota_summary?.current_term
      ensure(results[0].billing_scope === 'claude_subscription' && results[1].billing_scope === 'claude_subscription', 'CLAUDE_USAGE_SCOPE')
      ensure(term(results[0])?.id === term(results[1])?.id && digest(term(results[0])) === digest(term(results[1])), 'KEYS_COPIED_OR_CHANGED_SHARED_ALLOWANCE')
      ensure(results[2].billing_scope !== 'claude_subscription' && results[2].quota_mode !== 'term', 'GPT_USAGE_BECAME_CLAUDE')
      await p.close()
    })
    await testcase('real-admin-default3-override2-preserves-GPT5-keyboard', 'real-browser + actual admin display HTTP; no mutation', async () => {
      admin = await context(fixture.AdminEmail)
      await admin.goto(new URL('/admin/users', base).href)
      const row = await userRow(admin, fixture.AEmail)
      await row.getByRole('button', { name: 'Edit', exact: true }).click()
      const d = admin.getByRole('dialog').filter({ has: admin.getByTestId('claude-concurrency-input') })
      const policy = await visibleText(d.getByTestId('claude-scope-policy'))
      ensure(policy.includes('Independent default: 3') && policy.includes('User override: 2'), 'ADMIN_SERVER_DEFAULT3_OVERRIDE2_PANEL')
      ensure((await visibleText(d.getByTestId('claude-scope-stats'))).endsWith('/ 2'), 'ADMIN_EXISTING_EFFECTIVE2_PANEL')
      // Optional NEW update: blank omits the field and keeps current policy.
      ensure(await d.getByTestId('claude-concurrency-input').inputValue() === '', 'ADMIN_OPTIONAL_UPDATE_MUST_START_BLANK')
      ensure(await d.locator('[data-test="concurrency-input"]').inputValue() === '5', 'ADMIN_LEGACY5_PRESERVED')
      await d.getByTestId('claude-concurrency-input').focus()
      await admin.keyboard.press('Tab')
      ensure(await admin.evaluate(() => document.activeElement?.closest('[role="dialog"]') !== null), 'KEYBOARD_FOCUS_ESCAPED_DIALOG')
      await admin.keyboard.press('Escape')
      await d.waitFor({ state: 'hidden' })
      await (await userRow(admin, fixture.BEmail)).getByRole('button', { name: 'Edit', exact: true }).click()
      ensure((await visibleText(admin.getByRole('dialog').getByTestId('claude-scope-policy'))).includes('Use Claude default'), 'ADMIN_B_DEFAULT_INHERIT')
      await closeModal(admin)
    })
    await testcase('real-admin-C-Claude-default3-to-override4-GPT5-unchanged', 'real-browser + actual isolated admin mutation/GET', async () => {
      const before = await authenticatedGet(admin, `/admin/users/${fixture.C}`)
      ensure(before && !before.failed && before.concurrency === 5 && before.claude_subscription_concurrency == null && before.default_claude_subscription_concurrency === 3 && before.effective_claude_subscription_concurrency === 3, 'C_MUST_BE_FRESH_INHERITED3_GPT5')
      await (await userRow(admin, fixture.CEmail)).getByRole('button', { name: 'Edit', exact: true }).click()
      const d = admin.getByRole('dialog').filter({ has: admin.getByTestId('claude-concurrency-input') })
      await d.getByTestId('claude-concurrency-input').fill('4')
      const saved = admin.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === `/api/v1/admin/users/${fixture.C}`)
      report.real_management_mutations = true
      await d.getByTestId('user-edit-submit').click()
      const response = await jsonResponse(await saved)
      ensure(response.concurrency === 5 && response.claude_subscription_concurrency === 4 && response.default_claude_subscription_concurrency === 3 && response.effective_claude_subscription_concurrency === 4, 'C_UPDATE_RESPONSE_MUST_NOT_CHANGE_GPT5')
      await d.waitFor({ state: 'hidden' })
      const actual = await authenticatedGet(admin, `/admin/users/${fixture.C}`)
      ensure(actual.concurrency === 5 && actual.claude_subscription_concurrency === 4 && actual.effective_claude_subscription_concurrency === 4, 'C_GET_DID_NOT_PERSIST_OVERRIDE4')
      await (await userRow(admin, fixture.CEmail)).getByRole('button', { name: 'Edit', exact: true }).click()
      const reopened = admin.getByRole('dialog').filter({ has: admin.getByTestId('claude-concurrency-input') })
      ensure((await visibleText(reopened.getByTestId('claude-scope-policy'))).includes('User override: 4') && (await visibleText(reopened.getByTestId('claude-scope-stats'))).endsWith('/ 4'), 'C_REOPEN_PANEL_NOT_EFFECTIVE4')
      ensure(await reopened.getByTestId('claude-concurrency-input').inputValue() === '', 'C_REOPEN_OPTIONAL_UPDATE_MUST_START_BLANK')
      await closeModal(admin)
    })
    await testcase('real-admin-C-Open100-Renew75-168h-queue-and-network-replay', 'real-browser + actual isolated quota mutation/GET; actual committed first response intentionally lost', async () => {
      const before = await authenticatedGet(admin, `/admin/users/${fixture.C}/subscription-quota?group_id=${cGroup}`)
      ensure(before && !before.failed && !before.current_term && !before.last_term && before.next_terms.length === 0, 'C_CARD_SCOPE_MUST_BE_FRESH')
      const d = await quotaModal(admin, fixture.CEmail, cGroup)
      await d.getByTestId('quota-amount').fill('100')
      const firstPreview = admin.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/admin/users/${fixture.C}/subscription-quota/terms/preview`)
      await d.getByTestId('quota-preview').click()
      ensure((await jsonResponse(await firstPreview)).effective_quota_usd === '100.00000000', 'ACTUAL_OPEN_PREVIEW_QUOTA')
      await d.getByTestId('quota-preview-result').waitFor()
      const match = `**/api/v1/admin/users/${fixture.C}/subscription-quota/terms`
      let committed, firstIntent
      const loseCommittedResponse = async route => {
        if (route.request().method() !== 'POST') return route.continue()
        ensure(!committed, 'UNEXPECTED_SECOND_CREATE_INTERCEPTION')
        firstIntent = { body: route.request().postData(), key: route.request().headers()['idempotency-key'] }
        // This is a REAL server mutation. Only its browser response is dropped,
        // proving that the UI retries the committed operation with the same key.
        const actual = await route.fetch({ maxRedirects: 0, timeout: 15000 })
        ensure(actual.ok(), 'ACTUAL_C_OPEN_FAILED')
        committed = unwrap(await actual.json())
        await route.abort('failed')
      }
      await admin.route(match, loseCommittedResponse)
      try {
        const lost = admin.waitForEvent('requestfailed', req => req.method() === 'POST' && new URL(req.url()).pathname === `/api/v1/admin/users/${fixture.C}/subscription-quota/terms`)
        report.real_financial_mutations = true
        await d.getByTestId('quota-submit').click(); await lost
        await d.getByTestId('quota-error').waitFor()
        await admin.waitForFunction(() => !document.querySelector('[data-testid="quota-submit"]').disabled)
      } finally { await admin.unroute(match, loseCommittedResponse) }
      ensure(committed?.term?.quota_usd === '100.00000000' && committed.replayed === false, 'ACTUAL_FIRST_OPEN_NOT_COMMITTED100')
      const replayReply = admin.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/admin/users/${fixture.C}/subscription-quota/terms`)
      await d.getByTestId('quota-submit').click()
      const replayResponse = await replayReply, replay = await jsonResponse(replayResponse)
      const replayIntent = { body: replayResponse.request().postData(), key: replayResponse.request().headers()['idempotency-key'] }
      ensure(firstIntent.key && digest(firstIntent) === digest(replayIntent) && replay.replayed === true && replay.term.id === committed.term.id, 'ACTUAL_REPLAY_CHANGED_KEY_PAYLOAD_OR_CARD')
      const original = committed.term
      ensure(Date.parse(original.expires_at) - Date.parse(original.starts_at) === 168 * 3600_000, 'OPEN_NOT_EXACT168H')
      await d.getByTestId('quota-saved').waitFor()
      await d.getByTestId('quota-operation').selectOption('renew')
      await d.getByTestId('quota-amount').fill('75')
      const renewPreviewReply = admin.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/admin/users/${fixture.C}/subscription-quota/terms/preview`)
      await d.getByTestId('quota-preview').click()
      const renewPreview = await jsonResponse(await renewPreviewReply)
      ensure(renewPreview.effective_quota_usd === '75.00000000' && Date.parse(renewPreview.proposed_starts_at) === Date.parse(original.expires_at), 'ACTUAL_RENEW_PREVIEW_NOT_QUEUED75')
      await d.getByTestId('quota-preview-result').waitFor()
      const renewReply = admin.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `/api/v1/admin/users/${fixture.C}/subscription-quota/terms`)
      await d.getByTestId('quota-submit').click()
      const renewed = await jsonResponse(await renewReply)
      ensure(renewed.replayed === false && renewed.term.quota_usd === '75.00000000' && renewed.term.id !== original.id && Date.parse(renewed.term.starts_at) === Date.parse(original.expires_at) && Date.parse(renewed.term.expires_at) - Date.parse(renewed.term.starts_at) === 168 * 3600_000, 'ACTUAL_RENEW_NOT_EXACT_QUEUED168H75')
      const actual = await authenticatedGet(admin, `/admin/users/${fixture.C}/subscription-quota?group_id=${cGroup}`)
      ensure(actual.current_term?.id === original.id && actual.current_term.quota_usd === '100.00000000' && actual.current_term.used_usd === '0.00000000' && actual.current_term.pending_usd === '0.00000000' && actual.next_terms.length === 1 && actual.next_terms[0].id === renewed.term.id && actual.next_terms[0].quota_usd === '75.00000000', 'ACTUAL_GET_DID_NOT_RETAIN100_PLUS_QUEUED75')
      await closeModal(admin)
    })
    await testcase('real-CN-EN-narrow-card-layout', 'real-browser DOM/layout; real API', async () => {
      await a.setViewportSize({ width: 390, height: 844 })
      const enCard = await canonicalCard(a, '100.00000000')
      ensure((await visibleText(enCard)).includes('Shared by every Key'), 'EN_SHARED_COPY')
      const switcher = a.locator('button[title="English"]').first()
      await switcher.click(); await a.getByRole('button', { name: '🇨🇳 中文', exact: true }).click()
      const zhCard = await canonicalCard(a, '100.00000000')
      ensure((await visibleText(zhCard)).includes('此分组内所有 Key 共用同一张卡'), 'ZH_SHARED_COPY')
      ensure(await zhCard.evaluate(el => el.scrollWidth <= el.clientWidth + 1), 'NARROW_CARD_HORIZONTAL_OVERFLOW')
      await zhCard.screenshot({ path: path.join(out, 'canonical-card-zh-narrow.png') })
    })
    for (const outcome of ['success', 'error']) await testcase(`mock-held-preview-A-to-B-late-${outcome}-and-finally`, 'real-browser DOM + MOCKED preview HTTP; no financial mutation', async () => {
      await admin.goto(new URL('/admin/users', base).href)
      const old = wait(), current = wait(), oldSeen = wait(), currentSeen = wait()
      const data = await previewData(admin, fixture.A, '100')
      const handler = async route => {
        const request = route.request(), id = Number(new URL(request.url()).pathname.split('/')[5])
        if (id === fixture.A) { oldSeen.resolve(); await old.promise; await route.fulfill({ status: outcome === 'success' ? 200 : 503, json: outcome === 'success' ? envelope(data) : { code: 'UNAVAILABLE', message: 'STALE_A_FAILURE' } }) }
        else { currentSeen.resolve(); await current.promise; await route.fulfill({ status: 503, json: { code: 'UNAVAILABLE', message: 'CURRENT_B_FAILURE' } }) }
      }
      const match = '**/api/v1/admin/users/*/subscription-quota/terms/preview'
      await admin.route(match, handler)
      try {
        let d = await quotaModal(admin, fixture.AEmail)
        await d.getByTestId('quota-amount').fill('100'); await d.getByTestId('quota-preview').click(); await bounded(oldSeen.promise)
        await closeModal(admin)
        d = await quotaModal(admin, fixture.BEmail)
        await d.getByTestId('quota-amount').fill('300'); await d.getByTestId('quota-preview').click(); await bounded(currentSeen.promise)
        const oldReply = admin.waitForResponse(r => r.url().includes(`/users/${fixture.A}/subscription-quota/terms/preview`))
        old.resolve()
        await oldReply
        ensure(await d.getByTestId('quota-preview').isDisabled(), 'STALE_FINALLY_CLEARED_B_LOADING')
        ensure(await d.getByTestId('quota-preview-result').count() === 0 && !(await d.innerText()).includes('STALE_A_FAILURE'), 'STALE_PREVIEW_OVERWROTE_B')
        const currentReply = admin.waitForResponse(r => r.url().includes(`/users/${fixture.B}/subscription-quota/terms/preview`))
        current.resolve(); await currentReply
        await d.getByTestId('quota-error').filter({ hasText: 'CURRENT_B_FAILURE' }).waitFor()
        ensure(await d.getByTestId('quota-preview').isEnabled(), 'CURRENT_FINALLY_DID_NOT_CLEAR_LOADING')
        await closeModal(admin)
      } finally { old.resolve(); current.resolve(); await admin.unroute(match, handler) }
    })
    await testcase('mock-exact-network-retry-key-and-409-preserves-input', 'real-browser DOM + MOCKED preview/mutation HTTP; no financial mutation', async () => {
      const data = await previewData(admin, fixture.B, '300'), attempts = []
      const previewMatch = '**/api/v1/admin/users/*/subscription-quota/terms/preview'
      const mutationMatch = '**/api/v1/admin/users/*/subscription-quota/terms'
      const previewHandler = route => route.fulfill({ status: 200, json: envelope(data) })
      const mutationHandler = route => {
        if (route.request().method() !== 'POST') return route.continue()
        attempts.push({ payload: route.request().postData(), key: route.request().headers()['idempotency-key'] })
        return attempts.length < 3 ? route.abort('failed') : route.fulfill({ status: 409, json: { code: 'REVISION_CONFLICT', message: 'revision conflict' } })
      }
      await admin.route(previewMatch, previewHandler); await admin.route(mutationMatch, mutationHandler)
      try {
        const d = await quotaModal(admin, fixture.BEmail)
        await d.getByTestId('quota-amount').fill('300'); await d.getByTestId('quota-notes').fill('Synthetic preserved operator draft')
        await d.getByTestId('quota-preview').click(); await d.getByTestId('quota-preview-result').waitFor()
        for (let i = 0; i < 3; i++) {
          const done = i < 2
            ? admin.waitForEvent('requestfailed', req => req.method() === 'POST' && new URL(req.url()).pathname.endsWith('/subscription-quota/terms'))
            : admin.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/subscription-quota/terms'))
          await d.getByTestId('quota-submit').click(); await done; await d.getByTestId('quota-error').waitFor()
          if (i < 2) await admin.waitForFunction(() => !document.querySelector('[data-testid="quota-submit"]').disabled)
          else await d.getByTestId('quota-preview-result').waitFor({ state: 'hidden' })
        }
        ensure(attempts.length === 3 && attempts.every(v => v.key && v.key === attempts[0].key && v.payload === attempts[0].payload), 'RETRY_CHANGED_OPERATION_IDENTITY')
        ensure(await d.getByTestId('quota-amount').inputValue() === '300' && await d.getByTestId('quota-notes').inputValue() === 'Synthetic preserved operator draft', 'CONFLICT_LOST_DRAFT')
        ensure(await d.getByTestId('quota-preview-result').count() === 0, 'CONFLICT_PREVIEW_NOT_INVALIDATED')
        await closeModal(admin)
      } finally { await admin.unroute(previewMatch, previewHandler); await admin.unroute(mutationMatch, mutationHandler) }
    })
    await testcase('mock-server-clock-expiry-with-held-refresh-does-not-mint', 'real-browser DOM/server clock + MOCKED summary boundary; no financial mutation', async () => {
      const page = await context(fixture.AEmail), original = await authenticatedGet(page, '/subscription-quota')
      ensure(Array.isArray(original) && original.some(s => s.group_id === fixture.Group && s.current_term), 'BOUNDARY_REQUIRES_CURRENT_CARD')
      const summaries = structuredClone(original), summary = summaries.find(s => s.group_id === fixture.Group)
      const now = Date.now(); summary.server_time = new Date(now).toISOString(); summary.current_term.expires_at = new Date(now + 3500).toISOString(); summary.next_terms = []
      let reads = 0; const held = wait(), refreshed = wait()
      const match = '**/api/v1/subscription-quota?*'
      const handler = async route => { reads++; if (reads > 1) { refreshed.resolve(); await held.promise } await route.fulfill({ status: 200, json: envelope(summaries) }) }
      await page.route(match, handler)
      try {
        await page.goto(new URL('/subscriptions', base).href)
        const card = await canonicalCard(page, '100.00000000')
        const amounts = await card.locator('[data-testid^="quota-"]:not([data-testid="quota-countdown"])').allTextContents()
        await bounded(refreshed.promise)
        await card.getByTestId('quota-countdown').filter({ hasText: 'Refreshing server state' }).waitFor()
        ensure(digest(amounts) === digest(await card.locator('[data-testid^="quota-"]:not([data-testid="quota-countdown"])').allTextContents()), 'EXPIRY_MUTATED_MONEY_WITHOUT_SERVER')
        await card.screenshot({ path: path.join(out, 'held-server-expiry-card.png') })
      } finally { held.resolve(); await page.unroute(match, handler); await page.close() }
    })
  } finally {
    report.browser_acceptance = report.cases.length === 11 && report.cases.every(c => c.pass)
    report.finished_utc = new Date().toISOString()
    await writeFile(path.join(out, 'receipt.json'), JSON.stringify(report, null, 2))
    for (const ctx of contexts) await ctx.close().catch(() => {})
    await browser.close()
  }
  ensure(report.browser_acceptance, 'BROWSER_ACCEPTANCE_INCOMPLETE')
}

run().catch(() => { process.stderr.write('I4 browser acceptance did not complete. Inspect the sanitized receipt; do not print fixture or browser state.\n'); process.exitCode = 1 })
