import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ClaudeQuotaHistory from '../ClaudeQuotaHistory.vue'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import type { QuotaPage, SubscriptionQuotaUsage } from '@/types/subscriptionQuota'
import { deferred, quotaTerm } from '@/__tests__/subscriptionQuotaFixtures'
const api = vi.hoisted(() => ({ terms: vi.fn(), usage: vi.fn() }))
vi.mock('@/api/subscriptionQuota', () => ({ subscriptionQuotaAPI: api }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: VueWrapper
beforeEach(() => { setActivePinia(createPinia()); useAuthStore().user = { id: 1 } as User; vi.clearAllMocks(); api.terms.mockResolvedValue({ items: [quotaTerm()], next_cursor: null, has_more: false }); api.usage.mockResolvedValue({ items: [], next_cursor: null, has_more: false }); wrapper = mount(ClaudeQuotaHistory, { props: { groupId: 6 } }) })
afterEach(() => wrapper.unmount())
describe('per-card history ownership and pagination', () => {
  it('requests usage for the selected term only and uses its opaque next cursor', async () => {
    api.usage.mockResolvedValueOnce({ items: [], next_cursor: 'opaque', has_more: true })
    await wrapper.get('button').trigger('click'); await flushPromises(); await wrapper.findAll('button').find(button => button.text() === 'subscriptionQuota.usage')!.trigger('click'); await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'subscriptionQuota.nextPage')!.trigger('click'); await flushPromises()
    expect(api.usage.mock.calls).toEqual([[10, undefined], [10, 'opaque']]); expect(api.terms).toHaveBeenCalledWith(6, undefined)
  })
  it('rejects stale term usage success after changing authenticated identity', async () => {
    const held = deferred<QuotaPage<SubscriptionQuotaUsage>>(); api.usage.mockReturnValueOnce(held.promise)
    await wrapper.get('button').trigger('click'); await flushPromises(); await wrapper.findAll('button').find(button => button.text() === 'subscriptionQuota.usage')!.trigger('click')
    useAuthStore().user = { id: 2 } as User; await flushPromises()
    held.resolve({ items: [{ request_id: 'old-private-request', api_key_id: 9, term_id: 10, actual_cost_usd: '1.00000000', model: 'old-model', input_tokens: 1, output_tokens: 2, settled_at: '' }], next_cursor: null, has_more: false }); await flushPromises()
    expect(wrapper.text()).not.toContain('old-private-request'); expect(wrapper.find('table').exists()).toBe(false)
  })
})
