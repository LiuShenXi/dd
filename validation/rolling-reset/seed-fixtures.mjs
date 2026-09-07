import { chmod, mkdir, readFile, writeFile } from 'node:fs/promises'
import { randomBytes } from 'node:crypto'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const runtimeDir = join(scriptDir, '.runtime')
const credentialsPath = join(runtimeDir, 'app-credentials.private.json')
const privateFixturesPath = join(runtimeDir, 'browser-fixtures.private.json')
const evidenceDir = join(scriptDir, 'evidence')
const evidencePath = join(evidenceDir, 'runtime-fixtures.json')

const credentials = JSON.parse(await readFile(credentialsPath, 'utf8'))
if (credentials.source !== 'synthetic-local-only' || !credentials.base_url || !credentials.admin?.email || !credentials.admin?.password) {
  throw new Error('invalid private synthetic credential file')
}

const baseURL = credentials.base_url.replace(/\/$/, '')

async function request(path, { method = 'GET', token, key, body } = {}) {
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  if (key) headers['Idempotency-Key'] = key
  const response = await fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok || payload?.data === undefined) {
    throw new Error(`${method} ${path} failed with HTTP ${response.status}`)
  }
  return payload.data
}

async function requestStatus(path, { method = 'GET', token, key, body } = {}) {
  const headers = { Accept: 'application/json' }
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  if (key) headers['Idempotency-Key'] = key
  const response = await fetch(`${baseURL}${path}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  await response.arrayBuffer()
  return response.status
}

const login = await request('/api/v1/auth/login', {
  method: 'POST',
  body: { email: credentials.admin.email, password: credentials.admin.password },
})
if (!login.access_token || login.user?.role !== 'admin') throw new Error('synthetic admin login contract failed')
const token = login.access_token
const nonce = `${Date.now()}-${randomBytes(4).toString('hex')}`

const group = await request('/api/v1/admin/groups', {
  method: 'POST', token, key: `rolling-reset-group-${nonce}`,
  body: {
    name: `rolling-reset-${nonce}`,
    description: 'Synthetic rolling reset browser acceptance',
    platform: 'openai',
    rate_multiplier: 1,
    is_exclusive: true,
    subscription_type: 'carpool',
    model_pricing: [{ platform: 'openai', models: ['rolling-reset-synthetic'], billing_mode: 'per_request', per_request_price: 1 }],
  },
})

const plans = await request('/api/v1/admin/carpool/plans', { token })
const plan = plans.find((candidate) => candidate.code === 'four_seat' && candidate.enabled && candidate.is_latest)
if (!plan || plan.reset_mode !== 'rolling') throw new Error('latest rolling four-seat plan is unavailable')

const day = 24 * 60 * 60 * 1000
const now = Date.now()
const scenarios = [
  { name: 'active_reset_marker', balance: 123, startsAt: new Date(now - 5 * day), mode: 'new' },
  { name: 'near_natural_reset', balance: 45, startsAt: new Date(now - (6 * day + 23 * 60 * 60 * 1000)), mode: 'new' },
  { name: 'future_membership', balance: 12, startsAt: new Date(now + 2 * day), mode: 'new' },
  { name: 'near_expiry_no_refill', balance: 9, startsAt: new Date(now - 27 * day), mode: 'new' },
  { name: 'expired_membership', balance: 7, startsAt: new Date(now - 29 * day), mode: 'new' },
  {
    name: 'takeover_membership', balance: 33, startsAt: new Date(now - 2 * day), mode: 'takeover',
    takeover: {
      current_base_balance_usd: '311.00000000',
      current_boost_balance_usd: '12.00000000',
      current_manual_balance_usd: '5.00000000',
      boost_used: 1,
      history_complete: true,
      historical_used_usd: '42.00000000',
      statistics_since: new Date(now - 12 * day).toISOString(),
      next_natural_reset_at: new Date(now + 2 * day).toISOString(),
      ordinary_balance_transfer_usd: '0',
    },
  },
]

const privateUsers = []
const publicScenarios = []
for (const scenario of scenarios) {
  const password = randomBytes(24).toString('hex')
  const email = `rolling-reset-${scenario.name}-${nonce}@example.invalid`
  const user = await request('/api/v1/admin/users', {
    method: 'POST', token,
    body: {
      email,
      password,
      username: `Rolling reset ${scenario.name.replaceAll('_', ' ')}`,
      notes: `Synthetic rolling reset browser fixture ${nonce}`,
      role: 'user',
      balance: scenario.balance,
      concurrency: 5,
      rpm_limit: 0,
      allowed_groups: [],
      restrict_public_groups: true,
    },
  })
  const takeover = scenario.takeover ?? null
  const preview = await request(`/api/v1/admin/users/${user.id}/carpool/preview`, {
    method: 'POST', token,
    body: {
      plan_id: plan.plan_id,
      starts_at: scenario.startsAt.toISOString(),
      mode: scenario.mode,
      takeover,
    },
  })
  const term = await request(`/api/v1/admin/users/${user.id}/carpool/terms`, {
    method: 'POST', token, key: `rolling-reset-term-${scenario.name}-${nonce}`,
    body: {
      plan_id: plan.plan_id,
      group_id: group.id,
      starts_at: scenario.startsAt.toISOString(),
      mode: scenario.mode,
      takeover,
      notes: `Synthetic ${scenario.name}; real HTTP opening`,
      payment: null,
    },
  })
  const startsAt = new Date(term.starts_at)
  const expiresAt = new Date(term.expires_at)
  const userAfterOpening = await request(`/api/v1/admin/users/${user.id}`, { token })
  if (expiresAt.getTime() - startsAt.getTime() !== 28 * day) throw new Error(`${scenario.name} expiry is not exactly 28 days`)
  if (term.plan_snapshot?.reset_mode !== 'rolling') throw new Error(`${scenario.name} did not freeze rolling mode`)
  if (Number(userAfterOpening.balance) !== scenario.balance) throw new Error(`${scenario.name} ordinary balance changed during opening`)
  if (preview.next_natural_reset_at !== term.next_natural_reset_at) throw new Error(`${scenario.name} preview/open deadline mismatch`)
  if (scenario.name === 'near_expiry_no_refill' && term.next_natural_reset_at !== null) throw new Error('near-expiry next refill must be null')
  if (scenario.name === 'expired_membership' && term.next_natural_reset_at !== null) throw new Error('expired next refill must be null')

  privateUsers.push({ name: scenario.name, id: user.id, email, password, term_id: term.id })
  publicScenarios.push({
    name: scenario.name,
    user_id: user.id,
    term_id: term.id,
    status: term.status,
    starts_at: term.starts_at,
    expires_at: term.expires_at,
    next_natural_reset_at: term.next_natural_reset_at ?? null,
    preview_next_natural_reset_at: preview.next_natural_reset_at ?? null,
    ordinary_balance: String(userAfterOpening.balance),
  })
}

const invalidPassword = randomBytes(24).toString('hex')
const invalidEmail = `rolling-reset-invalid-takeover-${nonce}@example.invalid`
const invalidUser = await request('/api/v1/admin/users', {
  method: 'POST', token,
  body: {
    email: invalidEmail,
    password: invalidPassword,
    username: 'Rolling reset invalid takeover',
    notes: `Synthetic invalid takeover validation ${nonce}`,
    role: 'user',
    balance: 19,
    concurrency: 5,
    rpm_limit: 0,
    allowed_groups: [],
    restrict_public_groups: true,
  },
})
const invalidTakeoverStatus = await requestStatus(`/api/v1/admin/users/${invalidUser.id}/carpool/preview`, {
  method: 'POST', token,
  body: {
    plan_id: plan.plan_id,
    starts_at: new Date(now - day).toISOString(),
    mode: 'takeover',
    takeover: {
      current_base_balance_usd: '100.00000000',
      current_boost_balance_usd: '0',
      current_manual_balance_usd: '0',
      boost_used: 0,
      history_complete: false,
      next_natural_reset_at: new Date(now + 8 * day).toISOString(),
      ordinary_balance_transfer_usd: '0',
    },
  },
})
if (invalidTakeoverStatus !== 400) throw new Error(`invalid takeover deadline returned HTTP ${invalidTakeoverStatus}, expected 400`)

const marker = publicScenarios.find((scenario) => scenario.name === 'active_reset_marker')
const renewal = await request(`/api/v1/admin/carpool/terms/${marker.term_id}/renew`, {
  method: 'POST', token, key: `rolling-reset-renew-${nonce}`,
  body: { plan_id: plan.plan_id, notes: 'Synthetic future renewal through real HTTP API', payment: null },
})
if (renewal.status !== 'pending' || renewal.starts_at !== marker.expires_at) throw new Error('renewal did not start at fixed current expiry')

await mkdir(runtimeDir, { recursive: true, mode: 0o700 })
await writeFile(privateFixturesPath, `${JSON.stringify({ source: 'synthetic-local-only', base_url: baseURL, group_id: group.id, users: privateUsers }, null, 2)}\n`, { mode: 0o600, flag: 'wx' })
await chmod(privateFixturesPath, 0o600)
await mkdir(evidenceDir, { recursive: true })
await writeFile(evidencePath, `${JSON.stringify({
  source: 'synthetic-local-only',
  created_via: 'real HTTP admin API',
  plan: { id: plan.plan_id, code: plan.code, reset_mode: plan.reset_mode },
  group_id: group.id,
  scenarios: publicScenarios,
  renewal: {
    term_id: renewal.id,
    status: renewal.status,
    starts_at: renewal.starts_at,
    expires_at: renewal.expires_at,
    next_natural_reset_at: renewal.next_natural_reset_at ?? null,
  },
  invalid_takeover_deadline: { http_status: invalidTakeoverStatus, expected_http_status: 400 },
}, null, 2)}\n`, { flag: 'wx' })

console.log(`ROLLING_RESET_FIXTURES_READY scenarios=${publicScenarios.length}`)
