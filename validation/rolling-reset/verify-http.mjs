import { readFile, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const credentials = JSON.parse(await readFile(join(scriptDir, '.runtime/app-credentials.private.json'), 'utf8'))
const fixtures = JSON.parse(await readFile(join(scriptDir, '.runtime/browser-fixtures.private.json'), 'utf8'))
const evidencePath = join(scriptDir, 'evidence/runtime-fixtures.json')
const evidence = JSON.parse(await readFile(evidencePath, 'utf8'))
const baseURL = credentials.base_url.replace(/\/$/, '')

async function request(path, { method = 'GET', token, body } = {}) {
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  const response = await fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok || payload?.data === undefined) throw new Error(`${method} ${path} failed with HTTP ${response.status}`)
  return payload.data
}

async function login(email, password) {
  const data = await request('/api/v1/auth/login', { method: 'POST', body: { email, password } })
  if (!data.access_token || data.user?.email !== email) throw new Error(`login identity mismatch for ${email}`)
  return data.access_token
}

const adminToken = await login(credentials.admin.email, credentials.admin.password)
const results = []
for (const fixture of fixtures.users) {
  const expected = evidence.scenarios.find((scenario) => scenario.name === fixture.name)
  if (!expected) throw new Error(`missing public scenario ${fixture.name}`)
  const currentUser = await request(`/api/v1/admin/users/${fixture.id}`, { token: adminToken })
  if (String(currentUser.balance) !== String(expected.ordinary_balance)) {
    throw new Error(`${fixture.name} ordinary balance changed: ${currentUser.balance}`)
  }

  const userToken = await login(fixture.email, fixture.password)
  const details = await request('/api/v1/user/carpool/details', { token: userToken })
  if (!details.term) throw new Error(`${fixture.name} has no user term projection`)
  const duration = new Date(details.term.expires_at).getTime() - new Date(details.term.starts_at).getTime()
  if (duration !== 28 * 24 * 60 * 60 * 1000) throw new Error(`${fixture.name} user projection is not exactly 28 days`)
  if (details.term.reset_mode !== 'rolling') throw new Error(`${fixture.name} user projection is not rolling`)

  const expectedStatus = {
    active_reset_marker: 'active',
    near_natural_reset: 'active',
    future_membership: 'pending',
    near_expiry_no_refill: 'active',
    expired_membership: 'expired',
    takeover_membership: 'active',
  }[fixture.name]
  if (details.term.status !== expectedStatus) throw new Error(`${fixture.name} status ${details.term.status}, expected ${expectedStatus}`)
  if ((fixture.name === 'near_expiry_no_refill' || fixture.name === 'expired_membership') && details.term.next_natural_reset_at !== null) {
    throw new Error(`${fixture.name} must expose null next natural reset`)
  }
  if (fixture.name === 'near_natural_reset') {
    const remaining = new Date(details.term.next_natural_reset_at).getTime() - new Date(details.server_now).getTime()
    if (remaining <= 0 || remaining > 2 * 60 * 60 * 1000) throw new Error('near natural reset fixture is outside its two-hour window')
  }
  if (fixture.name === 'active_reset_marker') {
    if (details.term.reset_count !== 1 || details.term.reset_events.length !== 1) throw new Error('zero-grant reset event is missing from user projection')
    if (new Date(details.term.expires_at).getTime() !== new Date(expected.expires_at).getTime()) throw new Error('special reset changed membership expiry')
    if (new Date(details.term.next_natural_reset_at).getTime() === new Date(expected.next_natural_reset_at).getTime()) throw new Error('special reset did not move natural deadline')
  }

  results.push({
    name: fixture.name,
    user_id: fixture.id,
    current_ordinary_balance: String(currentUser.balance),
    status: details.term.status,
    starts_at: details.term.starts_at,
    expires_at: details.term.expires_at,
    next_natural_reset_at: details.term.next_natural_reset_at,
    reset_mode: details.term.reset_mode,
    reset_count: details.term.reset_count,
    reset_event_count: details.term.reset_events.length,
    available_usd: details.quota?.available_usd ?? null,
  })
}

evidence.verified_via = 'fresh admin user GET plus authenticated user carpool details GET'
evidence.verified_at = new Date().toISOString()
evidence.http_verification = results
await writeFile(evidencePath, `${JSON.stringify(evidence, null, 2)}\n`)
console.log(`ROLLING_RESET_HTTP_VERIFIED scenarios=${results.length}`)
