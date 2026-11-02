import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useSubscriptionStore } from '../subscriptions'
import { useAuthStore } from '../auth'
import type { User } from '@/types'
import type { SubscriptionQuotaSummary } from '@/types/subscriptionQuota'
import { deferred, quotaSummary } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ summaries: vi.fn(), active: vi.fn() }))
vi.mock('@/api/subscriptionQuota', () => ({ subscriptionQuotaAPI: { summaries: api.summaries } }))
vi.mock('@/api/subscriptions', () => ({ default: { getActiveSubscriptions: api.active } }))
beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); useAuthStore().user = { id: 1 } as User })
describe('canonical term summaries and caller result/error identities', () => {
  it('does not copy shared card allowance per Key and preserves zero/null separately', async () => {
    api.summaries.mockResolvedValue([quotaSummary({ current_term: null })])
    const store = useSubscriptionStore(); const response = await store.fetchTermSummaries()
    expect(store.isCurrentTermSummaries(response)).toBe(true); expect(store.termSummaries).toHaveLength(1); expect(store.termSummaries[0].current_term).toBeNull()
    // Key identity is deliberately not a cache key or an input to this API.
    expect(api.summaries).toHaveBeenCalledWith()
  })
  it.each([false, true])('only newer B failure can affect callers, with account switch=%s', async switchUser => {
    const a = deferred<SubscriptionQuotaSummary[]>(), b = deferred<SubscriptionQuotaSummary[]>()
    api.summaries.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const store = useSubscriptionStore(); const first = store.fetchTermSummaries().catch(error => error)
    if (switchUser) { store.clear(); useAuthStore().user = { id: 2 } as User }
    const second = store.fetchTermSummaries().catch(error => error), currentError = new Error('B'), oldError = new Error('A')
    b.reject(currentError); await second; expect(store.isCurrentTermError(currentError)).toBe(true)
    a.reject(oldError); await first; expect(store.isCurrentTermError(oldError)).toBe(false); expect(store.isCurrentTermError(currentError)).toBe(true); expect(store.termsLoading).toBe(false)
  })
  it('rejects old success and finally after clear and leaves current B loading', async () => {
    const a = deferred<SubscriptionQuotaSummary[]>(), b = deferred<SubscriptionQuotaSummary[]>()
    api.summaries.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const store = useSubscriptionStore(); const first = store.fetchTermSummaries(); store.clear(); useAuthStore().user = { id: 2 } as User; const second = store.fetchTermSummaries()
    const old = [quotaSummary()]; a.resolve(old); await first; expect(store.isCurrentTermSummaries(old)).toBe(false); expect(store.termSummaries).toEqual([]); expect(store.termsLoading).toBe(true)
    const current = [quotaSummary({ group_id: 8 })]; b.resolve(current); await second; expect(store.isCurrentTermSummaries(current)).toBe(true); expect(store.termSummaries[0].group_id).toBe(8)
  })
  it('invalidates data and caller markers synchronously on identity changes without depending on App cleanup', async () => {
    api.summaries.mockResolvedValue([quotaSummary()]); const store = useSubscriptionStore(); const result = await store.fetchTermSummaries()
    expect(store.isCurrentTermSummaries(result)).toBe(true); useAuthStore().user = { id: 2 } as User; useAuthStore().user = { id: 1 } as User
    expect(store.isCurrentTermSummaries(result)).toBe(false); expect(store.termSummaries).toEqual([])
  })
})
