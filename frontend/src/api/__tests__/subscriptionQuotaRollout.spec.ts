import { beforeEach, describe, expect, it, vi } from 'vitest'
import { adminSubscriptionQuotaRolloutAPI as api } from '../admin/subscriptionQuotaRollout'
import { rolloutState } from '@/__tests__/subscriptionQuotaRolloutFixtures'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../client', () => ({ apiClient: http }))
beforeEach(() => vi.resetAllMocks())
describe('controlled rollout API boundaries', () => {
  it('preserves explicit empty allocations, exact strings, revisions, preview identity and operation keys', async () => {
    http.post.mockResolvedValue({ data: rolloutState() })
    await api.preview(6, [])
    expect(http.post).toHaveBeenLastCalledWith('/admin/groups/6/subscription-quota/rollout/preview', { allocations: [] })
    await api.maintenance(6, { enabled: true, expected_revision: 2, reason: 'audit reason' }, 'maintenance-key')
    expect(http.post).toHaveBeenLastCalledWith('/admin/groups/6/subscription-quota/rollout/maintenance', { enabled: true, expected_revision: 2, reason: 'audit reason' }, { headers: { 'Idempotency-Key': 'maintenance-key' } })
    http.post.mockResolvedValue({ data: { state: rolloutState(), terms: null, replayed: true } })
    const input = { allocations: [{ user_id: 42, quota_usd: '999999999999.00000001' }], expected_revision: 3, preview_token: 'opaque-preview' }
    const result = await api.convert(6, input, 'convert-key')
    expect(http.post).toHaveBeenLastCalledWith('/admin/groups/6/subscription-quota/rollout/convert', input, { headers: { 'Idempotency-Key': 'convert-key' } })
    expect(result.terms).toEqual([]); expect(result.replayed).toBe(true)
  })
  it('normalizes nullable collections at the API boundary without masking request failures', async () => {
    http.get.mockResolvedValueOnce({ data: { ...rolloutState({ legacy_terminal_orders: 4 }), allocations: null, key_users: null, replicas: null, blockers: null } }).mockRejectedValueOnce(new Error('unavailable'))
    expect(await api.state(6)).toMatchObject({ allocations: [], key_users: [], replicas: [], blockers: [], legacy_terminal_orders: 4 })
    expect(http.get).toHaveBeenCalledWith('/admin/groups/6/subscription-quota/rollout')
    await expect(api.state(6)).rejects.toThrow('unavailable')
  })
})
