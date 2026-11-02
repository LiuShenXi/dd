import { beforeEach, describe, expect, it, vi } from 'vitest'
import { subscriptionQuotaAPI } from '../subscriptionQuota'
import { adminSubscriptionQuotaAPI } from '../admin/subscriptionQuota'
import subscriptionsAPI from '../subscriptions'
import { quotaSummary } from '@/__tests__/subscriptionQuotaFixtures'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: http }))
beforeEach(() => vi.clearAllMocks())
describe('subscription quota HTTP boundaries', () => {
  it('normalizes missing legacy fields and preserves the term discriminant with null summary', async () => {
    http.get.mockResolvedValue({ data: [{ id: 1 }, { id: 2, quota_mode: 'term', quota_summary: null }] })
    expect(await subscriptionsAPI.getMySubscriptions()).toEqual([{ id: 1, quota_mode: 'windows', quota_summary: null }, { id: 2, quota_mode: 'term', quota_summary: null }])
  })
  it('takes user scope only from auth routes and keeps stable opaque cursors', async () => {
    http.get.mockResolvedValue({ data: { items: [], has_more: false, next_cursor: null } })
    await subscriptionQuotaAPI.terms(6, 'opaque+cursor='); await subscriptionQuotaAPI.usage(10)
    expect(http.get.mock.calls).toEqual([['/subscription-quota/groups/6/terms', { params: { limit: 20, cursor: 'opaque+cursor=' } }], ['/subscription-quota/terms/10/usage', { params: { limit: 20 } }]])
  })
  it('sends exact decimal strings and caller operation keys without invented identity or nullable fields', async () => {
    http.post.mockResolvedValue({ data: quotaSummary() })
    const input = { group_id: 6, operation: 'renew' as const, quota_usd: '999999999999.99999999', expected_revision: 4 }
    await adminSubscriptionQuotaAPI.open(42, input, 'stable-operation-key')
    expect(http.post).toHaveBeenCalledWith('/admin/users/42/subscription-quota/terms', input, { headers: { 'Idempotency-Key': 'stable-operation-key' } })
    await adminSubscriptionQuotaAPI.resolve(9, { resolution: 'no_charge', expected_request_revision: 0, evidence_reference: 'ticket:1', reason: 'verified' }, 'stable-resolution-key')
    expect(http.post.mock.calls[1][0]).toBe('/admin/subscription-quota/requests/9/resolve'); expect(http.post.mock.calls[1][1]).not.toHaveProperty('verified_usage')
  })
})
